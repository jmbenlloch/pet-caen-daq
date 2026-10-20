// Package stagebridge forwards one TCP controller to a serial stage. It never
// invents commands or keepalives, and never replays bytes after a disconnect.
package stagebridge

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type OpenPort func() (io.ReadWriteCloser, error)

// Serve owns listener. OpenPort must return a port whose Close unblocks I/O.
// Additional clients are rejected, not queued to control the next session.
func Serve(ctx context.Context, listener net.Listener, open OpenPort, log *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	var sessions sync.WaitGroup
	defer func() { cancel(); _ = listener.Close(); sessions.Wait() }()
	go func() { <-ctx.Done(); _ = listener.Close() }()
	busy := make(chan struct{}, 1)
	var session uint64
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case busy <- struct{}{}:
		default:
			log.Warn("rejecting additional stage controller", "peer", conn.RemoteAddr())
			_ = conn.Close()
			continue
		}
		sessions.Add(1)
		session++
		sessionLog := log.With("session", session, "peer", conn.RemoteAddr())
		go func() {
			defer sessions.Done()
			defer func() { <-busy }()
			defer conn.Close()
			port, err := open()
			if err != nil {
				sessionLog.Error("open serial port", "error", err)
				return
			}
			sessionLog.Info("stage controller connected")
			err = forward(ctx, conn, port, sessionLog)
			sessionLog.Info("stage controller disconnected; serial session closed", "error", err)
		}()
	}
}

func forward(ctx context.Context, conn net.Conn, port io.ReadWriteCloser, logs ...*slog.Logger) error {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if len(logs) > 0 {
		log = logs[0]
	}
	tx := &observedWriter{writer: port, log: log.With("direction", "tcp_to_serial")}
	rx := &observedWriter{writer: deadlineWriter{conn}, log: log.With("direction", "serial_to_tcp")}
	start := time.Now()
	defer func() {
		log.Info("session I/O totals", "elapsed_ms", time.Since(start).Milliseconds(), "serial_written_bytes", tx.total.Load(), "tcp_written_bytes", rx.total.Load())
	}()
	results := make(chan error, 2)
	go func() {
		_, err := io.CopyBuffer(tx, struct{ io.Reader }{conn}, make([]byte, 256))
		log.Info("TCP to serial ended", "error", err)
		results <- err
	}()
	go func() {
		// Bound a stalled controller's writes without timing out long acquisitions
		// during which no serial input is expected.
		_, err := io.CopyBuffer(rx, struct{ io.Reader }{port}, make([]byte, 256))
		log.Info("serial to TCP ended", "error", err)
		results <- err
	}()
	var err error
	remaining := 2
	select {
	case err = <-results:
		remaining--
	case <-ctx.Done():
		err = ctx.Err()
	}
	_ = conn.Close()
	_ = port.Close()
	for ; remaining > 0; remaining-- {
		<-results
	}
	return err
}

// One writer per direction. Log receive and completion separately so blocked
// serial writes are distinguishable from late TCP arrivals. Payload is debug
// only, bounded to 96 bytes; JSON encoding escapes control characters.
type observedWriter struct {
	writer io.Writer
	log    *slog.Logger
	last   time.Time
	total  atomic.Int64
}

func (w *observedWriter) Write(p []byte) (int, error) {
	start := time.Now()
	gap := time.Duration(0)
	if !w.last.IsZero() {
		gap = start.Sub(w.last)
	}
	w.last = start
	preview := p
	if len(preview) > 96 {
		preview = preview[:96]
	}
	w.log.Debug("chunk received", "bytes", len(p), "gap_ms", gap.Milliseconds(), "preview", string(preview))
	n, err := w.writer.Write(p)
	w.total.Add(int64(n))
	elapsed := time.Since(start)
	w.log.Debug("chunk forwarded", "bytes", n, "write_ms", elapsed.Milliseconds(), "error", err)
	if err != nil || elapsed >= 100*time.Millisecond {
		w.log.Warn("slow or failed stream write", "requested_bytes", len(p), "written_bytes", n, "write_ms", elapsed.Milliseconds(), "error", err)
	}
	return n, err
}

type deadlineWriter struct{ net.Conn }

func (w deadlineWriter) Write(p []byte) (int, error) {
	if err := w.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return 0, err
	}
	return w.Conn.Write(p)
}
