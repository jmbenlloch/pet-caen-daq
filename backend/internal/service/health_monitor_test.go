package service

import (
	"context"
	"testing"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

type mutableRunHealth struct {
	pipeline acquisition.PipelineStats
	storage  runpipeline.StorageStats
}

func (s *mutableRunHealth) PipelineStats() acquisition.PipelineStats { return s.pipeline }
func (s *mutableRunHealth) StorageStats() runpipeline.StorageStats   { return s.storage }

func TestHealthMonitorPublishesImmediateAndTickSnapshots(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", &daqv1.TelemetrySnapshot{CurrentRun: &daqv1.RunSummary{RunId: "42"}}, nil)
	source := &mutableRunHealth{
		pipeline: acquisition.PipelineStats{Capacity: 8, QueueDepth: 2, AcceptedBatches: 3},
		storage:  runpipeline.StorageStats{Directory: "/runs/run-42", BytesWritten: 100, EventCount: 2, RawBatches: 1},
	}
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (&HealthMonitor{Publisher: publisher, Source: source, Tick: ticks}).Run(ctx) }()

	updatesCtx, updatesCancel := context.WithCancel(context.Background())
	defer updatesCancel()
	updates := publisher.Subscribe(updatesCtx)
	first := <-updates
	if first.Sequence < 2 {
		// Subscription can race the monitor's immediate sample; consume it.
		first = <-updates
	}
	if first.Pipeline.GetQueueDepth() != 2 || first.Storage.GetBytesWritten() != 100 || first.CurrentRun.GetEventCount() != 2 {
		t.Fatalf("immediate snapshot = %+v", first)
	}
	source.pipeline.QueueDepth = 0
	source.pipeline.DecodedEvents = 9
	source.storage.EventCount = 9
	ticks <- time.Unix(1, 0)
	second := <-updates
	if second.Sequence != first.Sequence+1 || second.Pipeline.GetDecodedEvents() != 9 || second.CurrentRun.GetEventCount() != 9 {
		t.Fatalf("tick snapshot = %+v", second)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestHealthMonitorValidatesDependenciesAndCadence(t *testing.T) {
	if err := (&HealthMonitor{}).Run(context.Background()); err == nil {
		t.Fatal("accepted missing dependencies")
	}
	publisher, _ := telemetry.NewPublisher("instance-a", nil, nil)
	if err := (&HealthMonitor{Publisher: publisher, Source: &mutableRunHealth{}}).Run(context.Background()); err == nil {
		t.Fatal("accepted missing interval")
	}
}
