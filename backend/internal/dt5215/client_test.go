package dt5215

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestControlCancellationInterruptsStalledReply(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := &Client{control: clientConn}
	requestRead := make(chan struct{})
	go func() { buffer := make([]byte, 20); _, _ = io.ReadFull(serverConn, buffer); close(requestRead) }()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-requestRead; cancel() }()
	err := client.SendCommand(ctx, 0, 0, CommandAcquisitionStop, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestControlRejectsAlreadyCanceledContext(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := &Client{control: clientConn}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	err := client.SendCommand(ctx, 0, 0, CommandAcquisitionStop, 0)
	if !errors.Is(err, context.Canceled) || time.Since(started) > time.Second {
		t.Fatalf("error = %v elapsed=%s", err, time.Since(started))
	}
}

func TestSendCommandWaitsForScheduledExecutionBeforeReturning(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := &Client{control: clientConn}
	requestRead := make(chan time.Time, 1)
	go func() {
		buffer := make([]byte, 20)
		_, _ = io.ReadFull(serverConn, buffer)
		requestRead <- time.Now()
		_, _ = serverConn.Write([]byte{0, 0, 0, 0})
	}()

	const delay = uint32(1_000_000)
	started := time.Now()
	if err := client.SendCommand(context.Background(), 0xff, 0xff, CommandResetPeriodic, delay); err != nil {
		t.Fatalf("SendCommand() error = %v", err)
	}
	sentAt := <-requestRead
	if elapsed := time.Since(sentAt); elapsed < 10*time.Millisecond {
		t.Fatalf("SendCommand() returned after %s from write, want at least 10ms", elapsed)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("SendCommand() took unexpectedly long: %s", elapsed)
	}
}

func TestSendCommandRetriesTransientStatus(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := &Client{control: clientConn}
	requests := make(chan int, 1)
	go func() {
		buffer := make([]byte, 20)
		count := 0
		for count < 2 {
			_, _ = io.ReadFull(serverConn, buffer)
			count++
			status := uint32(StatusOK)
			if count == 1 {
				status = 26
			}
			response := []byte{byte(status), byte(status >> 8), byte(status >> 16), byte(status >> 24)}
			_, _ = serverConn.Write(response)
		}
		requests <- count
	}()

	if err := client.SendCommand(context.Background(), 0xff, 0xff, CommandResetPeriodic, 0); err != nil {
		t.Fatalf("SendCommand() error = %v", err)
	}
	if count := <-requests; count != 2 {
		t.Fatalf("request count = %d, want 2", count)
	}
}

func TestSendCommandDelayIsContextCancelable(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := &Client{control: clientConn}
	requestRead := make(chan struct{})
	go func() {
		buffer := make([]byte, 20)
		_, _ = io.ReadFull(serverConn, buffer)
		close(requestRead)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-requestRead
		cancel()
	}()

	started := time.Now()
	err := client.SendCommand(ctx, 0xff, 0xff, CommandResetPeriodic, 100_000_000)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("canceled command took unexpectedly long: %s", elapsed)
	}
}
