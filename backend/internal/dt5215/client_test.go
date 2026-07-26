package dt5215

import (
	"bytes"
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

func TestSendSynchronizedCommandStagesThenTriggers(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := &Client{control: clientConn}
	requests := make(chan [][]byte, 1)
	times := make(chan []time.Time, 1)
	go func() {
		dcmd := make([]byte, 20)
		_, _ = io.ReadFull(serverConn, dcmd)
		stagedAt := time.Now()
		_, _ = serverConn.Write(make([]byte, 4))
		cwrg := make([]byte, 12)
		_, _ = io.ReadFull(serverConn, cwrg)
		triggeredAt := time.Now()
		_, _ = serverConn.Write(make([]byte, 4))
		requests <- [][]byte{dcmd, cwrg}
		times <- []time.Time{stagedAt, triggeredAt}
	}()

	if err := client.SendSynchronizedCommand(context.Background(), CommandResetPeriodic); err != nil {
		t.Fatalf("SendSynchronizedCommand() error = %v", err)
	}
	got := <-requests
	wantDCMD, _ := EncodeCommandRequest(true, 0xff, 0xff, CommandResetPeriodic, TDLSynchronizedCommandDelay)
	wantCWRG := EncodeConcentratorWriteRegisterRequest(ConcentratorSyncSend, 1)
	if !bytes.Equal(got[0], wantDCMD) || !bytes.Equal(got[1], wantCWRG) {
		t.Fatalf("requests = %x, %x; want %x, %x", got[0], got[1], wantDCMD, wantCWRG)
	}
	stamps := <-times
	if elapsed := stamps[1].Sub(stamps[0]); elapsed < tdlSynchronizedArmDelay {
		t.Fatalf("sync trigger followed DCMD after %s, want at least %s", elapsed, tdlSynchronizedArmDelay)
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
