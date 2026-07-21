//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/rawcapture"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/simulator"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/transportjournal"
)

func TestCoordinatorFailureRetainsDecodeAndTransportEvidence(t *testing.T) {
	t.Run("decode failure retains complete raw batch", func(t *testing.T) {
		server, client, coordinator, states, parent := failureCoordinator(t)
		defer server.Close()
		defer client.Close()
		if err := coordinator.Start(context.Background(), "decode", "integration", acquisition.RunOptions{CaptureRaw: true, JournalTransport: true}); err != nil {
			t.Fatal(err)
		}
		malformed := append([]byte(nil), testBatch()...)
		malformed[41] = 0x7e
		server.QueueStreamBatch(malformed)
		waitCoordinatorFault(t, coordinator, states)
		directory := filepath.Join(parent, "run-decode")
		if _, err := os.Stat(filepath.Join(directory, "incomplete")); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(filepath.Join(directory, "wire.raw"))
		if err != nil {
			t.Fatal(err)
		}
		replay, err := rawcapture.NewReader(file)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := replay.Next()
		_ = file.Close()
		if err != nil || !bytes.Equal(raw, malformed) {
			t.Fatalf("raw=%x error=%v", raw, err)
		}
	})

	t.Run("malformed framing retains transport journal", func(t *testing.T) {
		server, client, coordinator, states, parent := failureCoordinator(t)
		defer server.Close()
		defer client.Close()
		if err := coordinator.Start(context.Background(), "framing", "integration", acquisition.RunOptions{CaptureRaw: true, JournalTransport: true}); err != nil {
			t.Fatal(err)
		}
		if err := server.QueueFault(simulator.Fault{Kind: simulator.FaultMalformedDescriptor}); err != nil {
			t.Fatal(err)
		}
		if err := client.SendCommand(context.Background(), 0, 0, dt5215.CommandTestPulse, 0); err != nil {
			t.Fatal(err)
		}
		waitCoordinatorFault(t, coordinator, states)
		directory := filepath.Join(parent, "run-framing")
		if _, err := os.Stat(filepath.Join(directory, "incomplete")); err != nil {
			t.Fatal(err)
		}
		journalFile, err := os.Open(filepath.Join(directory, "transport.journal"))
		if err != nil {
			t.Fatal(err)
		}
		reader, err := transportjournal.NewReader(journalFile)
		if err != nil {
			t.Fatal(err)
		}
		wire, failures, err := transportjournal.Replay(reader, "run-framing")
		_ = journalFile.Close()
		if err != nil || len(wire) == 0 || len(failures) == 0 || failures[0].Kind != transportjournal.FramingFailure {
			t.Fatalf("wire=%x failures=%+v error=%v", wire, failures, err)
		}
		if _, err := os.Stat(filepath.Join(directory, "wire.raw")); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	})
}

func failureCoordinator(t *testing.T) (*simulator.Server, *dt5215.Client, *acquisition.Coordinator, *acquisition.StateMachine, string) {
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
	states, _ := acquisition.NewStateMachine(acquisition.StateReady, nil)
	parent := t.TempDir()
	factory := runpipeline.Factory{Options: runpipeline.Options{Parent: parent, Capacity: 2, Backpressure: acquisition.BackpressureBlock}}
	coordinator, err := acquisition.NewCoordinator(states, client, factory.New, 4, 200*time.Millisecond)
	if err != nil {
		client.Close()
		server.Close()
		t.Fatal(err)
	}
	return server, client, coordinator, states, parent
}

func waitCoordinatorFault(t *testing.T, coordinator *acquisition.Coordinator, states *acquisition.StateMachine) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for states.Snapshot().State != acquisition.StateFault || coordinator.ActiveRunID() != "" {
		if time.Now().After(deadline) {
			t.Fatalf("state=%s active=%q error=%v", states.Snapshot().State, coordinator.ActiveRunID(), coordinator.LastError())
		}
		time.Sleep(time.Millisecond)
	}
}
