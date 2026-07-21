package telemetry

import (
	"context"
	"testing"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
)

func TestPublisherSequencesAndClonesSnapshots(t *testing.T) {
	now := time.Date(2026, 7, 21, 14, 0, 0, 0, time.UTC)
	initial := &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_IDLE}
	publisher, err := NewPublisher("instance-a", initial, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	initial.State = daqv1.SystemState_SYSTEM_STATE_FAULT
	first := publisher.Snapshot()
	if first.InstanceId != "instance-a" || first.Sequence != 1 || first.State != daqv1.SystemState_SYSTEM_STATE_IDLE {
		t.Fatalf("initial snapshot = %+v", first)
	}
	first.State = daqv1.SystemState_SYSTEM_STATE_FAULT
	if got := publisher.Snapshot().State; got != daqv1.SystemState_SYSTEM_STATE_IDLE {
		t.Fatalf("publisher snapshot was mutated: %v", got)
	}
	published := publisher.Publish(&daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_READY})
	if published.Sequence != 2 || !published.ObservedAt.AsTime().Equal(now) {
		t.Fatalf("published snapshot = %+v", published)
	}
}

func TestSubscriberAndReconnectReceiveImmediateFullSnapshot(t *testing.T) {
	publisher, _ := NewPublisher("instance-a", &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_IDLE}, nil)
	ctx1, cancel1 := context.WithCancel(context.Background())
	updates1 := publisher.Subscribe(ctx1)
	if first := <-updates1; first.Sequence != 1 || first.State != daqv1.SystemState_SYSTEM_STATE_IDLE {
		t.Fatalf("first subscription snapshot = %+v", first)
	}
	publisher.Publish(&daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_RUNNING, CurrentRun: &daqv1.RunSummary{RunId: "42"}})
	if update := <-updates1; update.Sequence != 2 || update.CurrentRun.GetRunId() != "42" {
		t.Fatalf("subscription update = %+v", update)
	}
	cancel1()

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	reconnected := <-publisher.Subscribe(ctx2)
	if reconnected.Sequence != 2 || reconnected.State != daqv1.SystemState_SYSTEM_STATE_RUNNING || reconnected.CurrentRun.GetRunId() != "42" {
		t.Fatalf("reconnect snapshot = %+v", reconnected)
	}
}

func TestSlowSubscriberReceivesNewestSnapshot(t *testing.T) {
	publisher, _ := NewPublisher("instance-a", nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := publisher.Subscribe(ctx)
	<-updates
	for i := 0; i < 10; i++ {
		publisher.Publish(&daqv1.TelemetrySnapshot{Pipeline: &daqv1.PipelineTelemetry{DecodedEvents: uint64(i)}})
	}
	latest := <-updates
	if latest.Sequence != 11 || latest.Pipeline.GetDecodedEvents() != 9 {
		t.Fatalf("latest snapshot = %+v", latest)
	}
}

func TestIsStale(t *testing.T) {
	now := time.Date(2026, 7, 21, 14, 0, 10, 0, time.UTC)
	publisher, _ := NewPublisher("instance-a", nil, func() time.Time { return now.Add(-5 * time.Second) })
	snapshot := publisher.Snapshot()
	if IsStale(snapshot, now, 6*time.Second) {
		t.Fatal("fresh snapshot reported stale")
	}
	if !IsStale(snapshot, now, 4*time.Second) {
		t.Fatal("old snapshot reported fresh")
	}
	if !IsStale(nil, now, time.Second) {
		t.Fatal("missing snapshot reported fresh")
	}
}
