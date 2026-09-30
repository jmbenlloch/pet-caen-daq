// Package sourcestage implements the PETXY/1 host-controlled stepper protocol.
// Its coordinates are logical motor steps, not encoder measurements.
package sourcestage

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type Device interface {
	Exchange(context.Context, string, string) error
	Close() error
}

type Client struct {
	conn   io.ReadWriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
}

// Open accepts a local serial path, or tcp://host:port for the simulator or Windows bridge.
// Endpoints are server configuration, never supplied by a browser.
func Open(ctx context.Context, endpoint string) (Device, error) {
	var conn io.ReadWriteCloser
	var err error
	if strings.HasPrefix(endpoint, "tcp://") {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(endpoint, "tcp://"))
	} else {
		conn, err = openSerial(endpoint)
	}
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, reader: bufio.NewReaderSize(conn, 256)}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// Exchange closes the connection on cancellation to unblock serial I/O. Callers
// must reconnect and establish a fresh origin after any interrupted movement.
func (c *Client) Exchange(ctx context.Context, command, expected string) (result error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	start := time.Now()
	heartbeats := 0
	maxGap := time.Duration(0)
	// The heartbeat shutdown defer below runs first, making these counters safe
	// to read here. No heartbeat messages are fabricated by the bridge.
	defer func() {
		log.Printf("stage_exchange command=%q elapsed_ms=%d keepalives_sent=%d max_keepalive_gap_ms=%d error=%v", command, time.Since(start).Milliseconds(), heartbeats, maxGap.Milliseconds(), result)
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			_ = c.conn.Close()
		case <-done:
		}
	}()
	defer func() { close(done); <-stopped }()
	if err := writeCommand(c.conn, command+"\n"); err != nil {
		return errors.Join(ctx.Err(), err)
	}
	// Motion firmware requires keepalives; loss of the host stops pulse output.
	if strings.HasPrefix(command, "MOVE ") {
		heartbeatDone := make(chan struct{})
		heartbeatStopped := make(chan struct{})
		go func() {
			defer close(heartbeatStopped)
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			last := time.Now()
			for {
				select {
				case <-heartbeatDone:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := writeCommand(c.conn, "K\n"); err != nil {
						log.Printf("stage_keepalive write_failed error=%v", err)
						_ = c.conn.Close()
						return
					}
					gap := time.Since(last)
					last = time.Now()
					heartbeats++
					if gap > maxGap {
						maxGap = gap
					}
					if gap >= 500*time.Millisecond {
						log.Printf("stage_keepalive delayed gap_ms=%d", gap.Milliseconds())
					}
				}
			}
		}()
		defer func() { close(heartbeatDone); <-heartbeatStopped }()
	}
	line, err := c.reader.ReadSlice('\n')
	if err != nil {
		return errors.Join(ctx.Err(), err)
	}
	if strings.TrimSpace(string(line)) != expected {
		return fmt.Errorf("stage command %q: expected %q, received %q", command, expected, strings.TrimSpace(string(line)))
	}
	return nil
}

func writeCommand(conn io.Writer, command string) error {
	if timed, ok := conn.(interface{ SetWriteDeadline(time.Time) error }); ok {
		if err := timed.SetWriteDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			return err
		}
	}
	data := []byte(command)
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// ResetWait accommodates Arduino USB auto-reset before the first command.
func ResetWait(ctx context.Context) error {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
