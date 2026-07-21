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
