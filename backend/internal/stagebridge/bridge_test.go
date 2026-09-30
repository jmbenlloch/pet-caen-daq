package stagebridge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticWriterPreservesBytesAndBoundsPreview(t *testing.T) {
	var logs, output bytes.Buffer
	w := observedWriter{writer: &output, log: slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	payload := strings.Repeat("x", 96) + "DO_NOT_LOG_THIS_SUFFIX"
	if n, err := w.Write([]byte(payload)); err != nil || n != len(payload) {
		t.Fatalf("write %d %v", n, err)
	}
	if output.String() != payload || w.total.Load() != int64(len(payload)) {
		t.Fatal("diagnostics changed forwarded bytes")
	}
	if strings.Contains(logs.String(), "DO_NOT_LOG_THIS_SUFFIX") {
		t.Fatal("unbounded preview")
	}
	for _, field := range []string{"chunk received", "chunk forwarded", "gap_ms", "write_ms"} {
		if !strings.Contains(logs.String(), field) {
			t.Fatalf("missing %s", field)
		}
	}
}

func TestForward(t *testing.T) {
	for _, end := range []string{"client disconnect", "serial disconnect", "cancel"} {
		t.Run(end, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client, conn := net.Pipe()
			port, device := net.Pipe()
			defer client.Close()
			defer device.Close()
			done := make(chan error, 1)
			go func() { done <- forward(ctx, conn, port) }()
			// Deliberately fragmented protocol bytes, including malformed data:
			// the bridge must preserve them, not interpret or repair them.
			transfer(t, client, device, "MO", "VE 12 -34\n", "K\n")
			transfer(t, device, client, "REA", "DY 12 -34\n", "\x00bad\xff\n")
			switch end {
			case "client disconnect":
				_ = client.Close()
			case "serial disconnect":
				_ = device.Close()
			case "cancel":
				cancel()
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("forwarding did not stop")
			}
		})
	}
}

func TestStalledTCPReaderTimesOut(t *testing.T) {
	client, conn := net.Pipe()
	port, device := net.Pipe()
	defer client.Close()
	defer device.Close()
	done := make(chan error, 1)
	go func() { done <- forward(context.Background(), conn, port) }()
	_ = device.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := device.Write([]byte("READY\n")); err != nil {
		t.Fatal(err)
	}
	// Never read from client: backpressure must not hold ownership forever.
	select {
	case err := <-done:
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatalf("want timeout, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stalled reader did not time out")
	}
}

type shortPort struct{ net.Conn }

func (p shortPort) Write(b []byte) (int, error) { return 0, nil }

func TestSerialShortWriteClosesSession(t *testing.T) {
	client, conn := net.Pipe()
	port, device := net.Pipe()
	defer client.Close()
	defer device.Close()
	done := make(chan error, 1)
	go func() { done <- forward(context.Background(), conn, shortPort{port}) }()
	_ = client.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := client.Write([]byte("MOVE\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("want short write, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("short write did not terminate session")
	}
}

func transfer(t *testing.T, from, to net.Conn, chunks ...string) {
	t.Helper()
	_ = from.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_ = to.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer from.SetWriteDeadline(time.Time{})
	defer to.SetReadDeadline(time.Time{})
	for _, chunk := range chunks {
		done := make(chan error, 1)
		go func() { _, err := io.WriteString(from, chunk); done <- err }()
		buf := make([]byte, len(chunk))
		if _, err := io.ReadFull(to, buf); err != nil {
			t.Fatal(err)
		}
		if string(buf) != chunk {
			t.Fatalf("got %q, want %q", buf, chunk)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestServeExclusiveAndReconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	devices := make(chan net.Conn, 4)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, func() (io.ReadWriteCloser, error) {
			port, device := net.Pipe()
			devices <- device
			return port, nil
		}, logger)
	}()
	dial := func() net.Conn {
		t.Helper()
		conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
	client := dial()
	defer client.Close()
	var device net.Conn
	select {
	case device = <-devices:
	case <-time.After(time.Second):
		t.Fatal("port not opened")
	}
	defer device.Close()
	second := dial()
	_ = second.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Fatal("second client accepted")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("second client queued instead of rejected")
	}
	_ = second.Close()
	transfer(t, client, device, "HELLO\n")
	_ = client.Close()
	_ = device.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := device.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("serial not closed: %v", err)
	}
	// The worker releases ownership just after closing the port. Retry rejected
	// connections until that cleanup finishes; no old bytes may be replayed.
	deadline := time.Now().Add(2 * time.Second)
	for {
		next := dial()
		select {
		case nextDevice := <-devices:
			transfer(t, next, nextDevice, "NEW\n")
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown stuck")
			}
			_ = next.Close()
			_ = nextDevice.Close()
			return
		case <-time.After(10 * time.Millisecond):
			_ = next.Close()
		}
		if time.Now().After(deadline) {
			t.Fatal("could not reconnect")
		}
	}
}

func TestOpenFailureClosesClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, func() (io.ReadWriteCloser, error) { return nil, errors.New("COM port unavailable") }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("expected closed client: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown stuck")
	}
}
