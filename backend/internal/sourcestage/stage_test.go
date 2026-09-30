package sourcestage

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestExchange(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		wantErr     bool
	}{{"fragmented", "PETXY 1\n", false}, {"malformed", "WRONG\n", true}, {"oversize", strings.Repeat("x", 300) + "\n", true}, {"disconnect", "", true}} {
		t.Run(tc.name, func(t *testing.T) {
			host, device := net.Pipe()
			defer host.Close()
			go func() {
				defer device.Close()
				r := bufio.NewReader(device)
				_, _ = r.ReadString('\n')
				for _, b := range []byte(tc.reply) {
					if _, err := device.Write([]byte{b}); err != nil {
						return
					}
				}
			}()
			c := &Client{conn: host, reader: bufio.NewReaderSize(host, 256)}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := c.Exchange(ctx, "HELLO", "PETXY 1"); (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCommandWriteTimeout(t *testing.T) {
	host, device := net.Pipe()
	defer host.Close()
	defer device.Close()
	start := time.Now()
	err := writeCommand(host, "K\n") // peer never reads
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("expected write timeout, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("write timeout was not bounded")
	}
}

func TestMoveHeartbeatAndCancellation(t *testing.T) {
	host, device := net.Pipe()
	defer device.Close()
	heartbeat := make(chan struct{})
	go func() {
		r := bufio.NewReader(device)
		_, _ = r.ReadString('\n')
		line, _ := r.ReadString('\n')
		if line == "K\n" {
			close(heartbeat)
		}
		_, _ = r.ReadString('\n')
	}()
	c := &Client{conn: host, reader: bufio.NewReaderSize(host, 256)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Exchange(ctx, "MOVE 1 2", "READY 1 2") }()
	select {
	case <-heartbeat:
	case <-time.After(time.Second):
		t.Fatal("missing heartbeat")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not unblock I/O")
	}
}

func TestTimeout(t *testing.T) {
	host, device := net.Pipe()
	defer device.Close()
	go func() { _, _ = bufio.NewReader(device).ReadString('\n') }()
	c := &Client{conn: host, reader: bufio.NewReaderSize(host, 256)}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := c.Exchange(ctx, "HELLO", "PETXY 1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(fmt.Sprint(err))
	}
}

type shortWriter struct{ net.Conn }

func (c shortWriter) Write(data []byte) (int, error) {
	if len(data) > 2 {
		data = data[:2]
	}
	return c.Conn.Write(data)
}

func TestExchangeCompletesPartialWrites(t *testing.T) {
	host, device := net.Pipe()
	defer host.Close()
	defer device.Close()
	go func() {
		line, _ := bufio.NewReader(device).ReadString('\n')
		if line == "HELLO\n" {
			_, _ = device.Write([]byte("PETXY 1\n"))
		}
	}()
	c := &Client{conn: shortWriter{host}, reader: bufio.NewReaderSize(host, 256)}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Exchange(ctx, "HELLO", "PETXY 1"); err != nil {
		t.Fatal(err)
	}
}
