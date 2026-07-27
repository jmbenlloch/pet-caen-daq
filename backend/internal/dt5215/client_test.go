package dt5215

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type deadlineRecordingConn struct {
	net.Conn
	mu       sync.Mutex
	deadline time.Time
}

func (c *deadlineRecordingConn) SetDeadline(deadline time.Time) error {
	c.mu.Lock()
	if deadline.After(c.deadline) {
		c.deadline = deadline
	}
	c.mu.Unlock()
	return c.Conn.SetDeadline(deadline)
}

func TestConcentratorInfoAllowsCaptureObservedResponseLatency(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	recording := &deadlineRecordingConn{Conn: clientConn}
	client := &Client{control: recording}
	go func() {
		request := make([]byte, 4)
		_, _ = io.ReadFull(serverConn, request)
		response := make([]byte, 68)
		littleEndian.PutUint32(response[:4], 64)
		copy(response[4:20], "2026.4.1.1")
		copy(response[20:52], "25.11.24.01-2-2")
		copy(response[52:68], "66643")
		_, _ = serverConn.Write(response)
	}()

	started := time.Now()
	if _, err := client.ConcentratorInfo(context.Background()); err != nil {
		t.Fatalf("ConcentratorInfo() error = %v", err)
	}
	recording.mu.Lock()
	deadline := recording.deadline
	recording.mu.Unlock()
	if allowed := deadline.Sub(started); allowed < 9*time.Second {
		t.Fatalf("VERS deadline allows %s, want approximately %s", allowed, versionOperationTimeout)
	}
}

func TestRecoverTDLMatchesJANUSMultiChainSequence(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := &Client{control: clientConn}
	chains := []int{0, 2}
	var expected [][]byte
	for _, chain := range chains {
		link := uint32(chain) << 24
		expected = append(expected,
			EncodeConcentratorWriteRegisterRequest(VirtualRegisterSyncDelay, link|tdlSyncDelaySetting),
			EncodeConcentratorWriteRegisterRequest(VirtualRegisterSyncDelay, link|tdlSyncAdjustment),
		)
	}
	for _, chain := range chains {
		request, _ := EncodeChainControlRequest(uint16(chain), false, 0)
		expected = append(expected, request)
	}
	for _, command := range []uint32{CommandSync, CommandResetTime, CommandResetPeriodic} {
		request, _ := EncodeCommandRequest(false, 0xff, 0xff, command, TDLCommandDelay)
		expected = append(expected, request)
	}
	for _, chain := range chains {
		request, _ := EncodeChainControlRequest(uint16(chain), true, 256)
		expected = append(expected, request)
	}

	serverErr := make(chan error, 1)
	go func() {
		for index, want := range expected {
			got := make([]byte, len(want))
			if _, err := io.ReadFull(serverConn, got); err != nil {
				serverErr <- err
				return
			}
			if !bytes.Equal(got, want) {
				serverErr <- fmt.Errorf("request %d = %x, want %x", index, got, want)
				return
			}
			if _, err := serverConn.Write([]byte{0, 0, 0, 0}); err != nil {
				serverErr <- err
				return
			}
		}
		serverErr <- nil
	}()

	if err := client.recoverTDL(context.Background(), chains); err != nil {
		t.Fatalf("recoverTDL() error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

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
