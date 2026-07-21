//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/simulator"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/transportjournal"
)

func faultClient(t *testing.T) (*simulator.Server, *dt5215.Client) {
	t.Helper()
	server, err := simulator.Start("127.0.0.1:0", "127.0.0.1:0", simulator.ProductionTopology())
	if err != nil {
		t.Fatal(err)
	}
	client, err := dt5215.Dial(context.Background(), server.ControlAddress(), server.StreamAddress())
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	return server, client
}

func runningFaultClient(t *testing.T) (*simulator.Server, *dt5215.Client) {
	t.Helper()
	server, client := faultClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Synchronize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.SendCommand(ctx, 0, 0, dt5215.CommandAcquisitionStart, 0); err != nil {
		t.Fatal(err)
	}
	return server, client
}

func TestDeterministicControlFaults(t *testing.T) {
	for _, tt := range []struct {
		name  string
		fault simulator.Fault
	}{
		{"delay_timeout", simulator.Fault{Kind: simulator.FaultControlDelay, Operation: "RREG", Delay: 100 * time.Millisecond}},
		{"explicit_timeout", simulator.Fault{Kind: simulator.FaultControlTimeout, Operation: "RREG", Delay: 100 * time.Millisecond}},
		{"disconnect", simulator.Fault{Kind: simulator.FaultControlDisconnect, Operation: "RREG"}},
		{"partial_reply", simulator.Fault{Kind: simulator.FaultPartialReply, Operation: "RREG", AfterBytes: 3}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, client := faultClient(t)
			if err := server.QueueFault(tt.fault); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			_, err := client.ReadRegister(ctx, 0, 0, dt5215.RegisterProductID)
			if err == nil {
				t.Fatal("faulted read succeeded")
			}
		})
	}
}

func TestDeterministicCommandDelay(t *testing.T) {
	server, client := faultClient(t)
	if err := server.QueueFault(simulator.Fault{Kind: simulator.FaultControlDelay, Operation: "FCMD", Delay: 100 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := client.SendCommand(ctx, 0, 0, dt5215.CommandAcquisitionStop, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
}

func TestDeterministicStreamFramingFaults(t *testing.T) {
	for _, tt := range []struct {
		name  string
		fault simulator.Fault
		crc   bool
	}{
		{"malformed_descriptor", simulator.Fault{Kind: simulator.FaultMalformedDescriptor}, false},
		{"invalid_size", simulator.Fault{Kind: simulator.FaultInvalidSize}, false},
		{"invalid_offset", simulator.Fault{Kind: simulator.FaultInvalidOffset}, false},
		{"crc", simulator.Fault{Kind: simulator.FaultCRC}, true},
		{"disconnect", simulator.Fault{Kind: simulator.FaultStreamDisconnect}, false},
		{"truncation", simulator.Fault{Kind: simulator.FaultStreamTruncation, AfterBytes: 20}, false},
		{"delay", simulator.Fault{Kind: simulator.FaultStreamDelay, Delay: 100 * time.Millisecond}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, client := runningFaultClient(t)
			if err := server.QueueFault(tt.fault); err != nil {
				t.Fatal(err)
			}
			if err := client.SendCommand(context.Background(), 0, 0, dt5215.CommandTestPulse, 0); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			events, err := client.ReadStreamBatch(ctx)
			if tt.crc {
				if err != nil || len(events) != 1 || !events[0].Descriptor.CRCError {
					t.Fatalf("events=%#v err=%v", events, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("faulted stream succeeded: %#v", events)
			}
		})
	}
}

func TestDeterministicMissingServiceAndStalledDrain(t *testing.T) {
	for _, kind := range []simulator.FaultKind{simulator.FaultMissingCompletion, simulator.FaultMissingService, simulator.FaultStalledDrain} {
		t.Run(string(kind), func(t *testing.T) {
			server, client := runningFaultClient(t)
			if err := server.QueueFault(simulator.Fault{Kind: kind}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			_, err := acquisition.StopAndDrain(ctx, client, 1, nil)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestDeterministicMissingStartService(t *testing.T) {
	server, client := faultClient(t)
	if err := client.Synchronize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.WriteRegister(context.Background(), 0, 0, uint32(dt5202.AcquisitionControl), 1<<18|1); err != nil {
		t.Fatal(err)
	}
	if err := server.QueueFault(simulator.Fault{Kind: simulator.FaultMissingService}); err != nil {
		t.Fatal(err)
	}
	if err := client.SendCommand(context.Background(), 0, 0, dt5215.CommandAcquisitionStart, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := client.ReadStreamBatch(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
}

func TestTransportJournalReplaysTruncatedFramingFailure(t *testing.T) {
	server, client := runningFaultClient(t)
	var capture bytes.Buffer
	journal, err := transportjournal.NewWriter(&capture)
	if err != nil {
		t.Fatal(err)
	}
	client.SetStreamJournal(journal, "truncated-stream", func() time.Time { return time.Unix(0, 77) })
	if err = server.QueueFault(simulator.Fault{Kind: simulator.FaultStreamTruncation, AfterBytes: 20}); err != nil {
		t.Fatal(err)
	}
	if err = client.SendCommand(context.Background(), 0, 0, dt5215.CommandTestPulse, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = client.ReadStreamBatch(context.Background()); err == nil {
		t.Fatal("truncated stream succeeded")
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := transportjournal.NewReader(bytes.NewReader(capture.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	wire, failures, err := transportjournal.Replay(reader, "truncated-stream")
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) != 20 || len(failures) != 1 || failures[0].Kind != transportjournal.Termination || failures[0].Offset != 20 || failures[0].TimestampUnixNano != 77 {
		t.Fatalf("wire=%x failures=%#v", wire, failures)
	}
	if _, err = dt5215.DecodeStreamBatch(wire); err == nil {
		t.Fatal("replayed truncation decoded successfully")
	}
}

func TestTransportJournalReplaysMalformedDescriptor(t *testing.T) {
	server, client := runningFaultClient(t)
	var capture bytes.Buffer
	journal, _ := transportjournal.NewWriter(&capture)
	client.SetStreamJournal(journal, "malformed-stream", func() time.Time { return time.Unix(0, 88) })
	if err := server.QueueFault(simulator.Fault{Kind: simulator.FaultMalformedDescriptor}); err != nil {
		t.Fatal(err)
	}
	if err := client.SendCommand(context.Background(), 0, 0, dt5215.CommandTestPulse, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReadStreamBatch(context.Background()); err == nil {
		t.Fatal("malformed descriptor succeeded")
	}
	_ = journal.Close()
	reader, _ := transportjournal.NewReader(bytes.NewReader(capture.Bytes()))
	wire, failures, err := transportjournal.Replay(reader, "malformed-stream")
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) == 0 || len(failures) != 1 || failures[0].Kind != transportjournal.FramingFailure || failures[0].Stage != "decode" || failures[0].TimestampUnixNano != 88 {
		t.Fatalf("wire=%x failures=%#v", wire, failures)
	}
	if _, err = dt5215.DecodeStreamBatch(wire); err == nil {
		t.Fatal("replayed malformed descriptor decoded")
	}
}
