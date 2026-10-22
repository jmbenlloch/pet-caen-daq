package runpipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestPerBoardQuotaPersistsExactCountsAcrossBatchesAndDrain(t *testing.T) {
	factory := Factory{Options: Options{Parent: t.TempDir(), Capacity: 16, Backpressure: acquisition.BackpressureBlock,
		ExecutionIdentity: runstore.ExecutionIdentity{Topology: runstore.TopologyIdentity{Boards: []runstore.BoardIdentity{{Board: 0, Chain: 1, Node: 0}, {Board: 1, Chain: 1, Node: 1}}}}}}
	created, err := factory.New("1", acquisition.RunOptions{EventLimitPerBoard: 3, CaptureRaw: true})
	if err != nil {
		t.Fatal(err)
	}
	session := created.(*Session)
	defer session.Close()
	submit := func(node uint8, n int) {
		t.Helper()
		events := make([]dt5215.StreamEvent, n)
		for i := range events {
			events[i] = dt5215.StreamEvent{Chain: 1, Descriptor: dt5215.Descriptor{Node: node, Qualifier: dt5202.QualifierTest, TriggerID: uint64(i)}, Payload: []byte{1, 0, 0, 0}}
		}
		if err := session.Submit(context.Background(), acquisition.PipelineBatch{Raw: []byte{1, 2, 3}, Events: events}); err != nil {
			t.Fatal(err)
		}
	}
	submit(0, 20)
	deadline := time.Now().Add(time.Second)
	for session.Stats().EventCount < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if session.Stats().EventCount != 3 || session.PerBoardQuotaReached() {
		t.Fatal("fast board must cap at three and wait for silent board")
	}
	submit(0, 10)
	submit(2, 10) // An unconfigured card cannot satisfy the quota or enter saved output.
	submit(1, 2)
	submit(1, 10) // Includes excess events already queued before stop/drain.
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if !session.PerBoardQuotaReached() {
		t.Fatal("all quotas should be complete")
	}
	if got := session.Stats(); got.EventCount != 6 || got.RawBatches != 5 {
		t.Fatalf("storage = %+v", got)
	}

	if err := session.Finalize("2026-10-22T16:00:00Z", "preset_counts_per_board"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(session.Directory(), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest runstore.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ExecutionIdentity.Runtime.EventLimitPerBoard != 3 {
		t.Fatal("quota missing from provenance")
	}
	next, err := factory.New("2", acquisition.RunOptions{EventLimitPerBoard: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.(*Session).PerBoardQuotaReached() {
		t.Fatal("new run inherited completed quotas")
	}
	for _, board := range session.BoardStats() {
		if board.TriggerCount != 3 {
			t.Fatalf("board = %+v", board)
		}
	}
	if got := session.PipelineStats(); got.PersistedEvents != 6 || got.EventBatchesPersisted != 3 {
		t.Fatalf("pipeline = %+v", got)
	}
}

func TestPerBoardQuotaServiceRecordsAndCommitBoundary(t *testing.T) {
	key := boardKey{chain: 1, node: 2}
	s := &sink{eventLimitPerBoard: 2, quotaTargets: map[boardKey]struct{}{key: {}}, boards: map[boardKey]BoardStats{}}
	session := &Session{sink: s}
	event := func(kind dt5202.EventKind) acquisition.DecodedEvent {
		return acquisition.DecodedEvent{Wire: dt5215.StreamEvent{Chain: 1, Descriptor: dt5215.Descriptor{Node: 2}}, Event: dt5202.Event{Kind: kind}}
	}
	events := []acquisition.DecodedEvent{event(dt5202.EventService), event(dt5202.EventTest), event(dt5202.EventService), event(dt5202.EventTest), event(dt5202.EventService), event(dt5202.EventTest)}
	selected := s.SelectEvents(events)
	if len(selected) != 4 {
		t.Fatalf("selected %d records, want two data and two service", len(selected))
	}
	if session.PerBoardQuotaReached() {
		t.Fatal("selection alone must not commit counts before storage succeeds")
	}
	s.boards[key] = BoardStats{TriggerCount: 1}
	if got := s.SelectEvents([]acquisition.DecodedEvent{event(dt5202.EventTest), event(dt5202.EventTest)}); len(got) != 1 {
		t.Fatal("prior persisted count ignored")
	}
	s.boards[key] = BoardStats{TriggerCount: 2}
	if !session.PerBoardQuotaReached() || len(s.SelectEvents([]acquisition.DecodedEvent{event(dt5202.EventService)})) != 0 {
		t.Fatal("completed board still saving")
	}
	s.eventLimitPerBoard = 0
	if len(s.SelectEvents(events)) != len(events) || session.PerBoardQuotaReached() {
		t.Fatal("disabled quota changed normal behavior")
	}
}

func TestPerBoardQuotaRequiresConfiguredTargets(t *testing.T) {
	factory := Factory{Options: Options{Parent: t.TempDir(), Capacity: 2, Backpressure: acquisition.BackpressureBlock}}
	if _, err := factory.New("1", acquisition.RunOptions{EventLimitPerBoard: 1}); err == nil {
		t.Fatal("accepted quota without configured boards")
	}
}
