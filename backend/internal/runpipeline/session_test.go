package runpipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

func TestSessionFinalizesTypedEventsAndRawCapture(t *testing.T) {
	parent := t.TempDir()
	factory := Factory{Options: Options{Parent: parent, CaptureRaw: true, Capacity: 2, Backpressure: acquisition.BackpressureBlock, Now: func() time.Time { return time.Unix(100, 0) }}}
	created, err := factory.New("42")
	if err != nil {
		t.Fatal(err)
	}
	session := created.(*Session)
	event := dt5215.StreamEvent{Chain: 0, Descriptor: dt5215.Descriptor{Qualifier: dt5202.QualifierTest, TriggerID: 7, Timestamp: 8}, Payload: []byte{1, 0, 0, 0}}
	if err := session.Submit(context.Background(), acquisition.PipelineBatch{Raw: []byte{9}, Events: []dt5215.StreamEvent{event}}); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Finalize("2026-07-21T16:00:00Z", "operator_stop"); err != nil {
		t.Fatal(err)
	}
	stats := session.Stats()
	if stats.Directory != session.Directory() || stats.BytesWritten == 0 || stats.EventCount != 1 || stats.RawBatches != 1 || !stats.Finalized || stats.LastError != "" {
		t.Fatalf("storage stats = %+v", stats)
	}
	for _, name := range []string{"manifest.json", "events.jsonl", "wire.raw"} {
		if _, err := os.Stat(filepath.Join(session.Directory(), name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(session.Directory(), "incomplete")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete marker remains: %v", err)
	}
}

func TestSessionAbortRetainsIncompleteMarker(t *testing.T) {
	factory := Factory{Options: Options{Parent: t.TempDir(), Capacity: 1, Backpressure: acquisition.BackpressureBlock}}
	created, err := factory.New("failed")
	if err != nil {
		t.Fatal(err)
	}
	session := created.(*Session)
	bad := dt5215.StreamEvent{Descriptor: dt5215.Descriptor{Qualifier: 0x7e}}
	if err := session.Submit(context.Background(), acquisition.PipelineBatch{Raw: []byte{1}, Events: []dt5215.StreamEvent{bad}}); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err == nil {
		t.Fatal("expected decode failure")
	}
	if err := session.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(session.Directory(), "incomplete")); err != nil {
		t.Fatalf("incomplete marker missing: %v", err)
	}
	stats := session.Stats()
	if stats.Finalized || stats.LastError == "" || stats.EventCount != 0 {
		t.Fatalf("failed storage stats = %+v", stats)
	}
}
