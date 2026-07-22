package service

import (
	"context"
	"testing"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mutableRunHealth struct {
	pipeline acquisition.PipelineStats
	storage  runpipeline.StorageStats
	boards   []runpipeline.BoardStats
	elapsed  time.Duration
}

func (s *mutableRunHealth) PipelineStats() acquisition.PipelineStats { return s.pipeline }
func (s *mutableRunHealth) StorageStats() runpipeline.StorageStats   { return s.storage }
func (s *mutableRunHealth) BoardStats() []runpipeline.BoardStats     { return s.boards }
func (s *mutableRunHealth) StatisticsElapsed() time.Duration         { return s.elapsed }

func TestHealthMonitorPublishesImmediateAndTickSnapshots(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", &daqv1.TelemetrySnapshot{CurrentRun: &daqv1.RunSummary{RunId: "42"}, Chains: []*daqv1.Chain{{Index: 0, Boards: []*daqv1.Board{{Node: 0}}}}}, nil)
	temperature, voltage, current := 41.5, 52.1, 0.02
	boardObservedAt := time.Date(2026, 7, 22, 17, 30, 0, 0, time.UTC)
	source := &mutableRunHealth{
		pipeline: acquisition.PipelineStats{Capacity: 8, QueueDepth: 2, AcceptedBatches: 3},
		storage:  runpipeline.StorageStats{Directory: "/runs/run-42", BytesWritten: 100, EventCount: 2, RawBatches: 1},
		boards:   []runpipeline.BoardStats{{Chain: 0, Node: 0, EventCount: 2, TriggerCount: 2, TriggerID: 9, DataBytes: 64, FPGATemperature: &temperature, HVVoltage: &voltage, HVCurrent: &current, HVOn: true, TelemetryObservedAt: &boardObservedAt}},
		elapsed:  2 * time.Second,
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
	if first.Pipeline.GetQueueDepth() != 2 || first.Storage.GetBytesWritten() != 100 || first.CurrentRun.GetEventCount() != 2 || first.Chains[0].Boards[0].GetFpgaTemperatureC() != temperature || !first.Chains[0].Boards[0].GetHvOn() {
		t.Fatalf("immediate snapshot = %+v", first)
	}
	if first.Statistics.GetElapsedMilliseconds() != 2000 || first.Statistics.Boards[0].GetTriggerId() != 9 || first.Statistics.Boards[0].GetDataBytes() != 64 {
		t.Fatalf("statistics = %+v", first.Statistics)
	}
	if first.Chains[0].Boards[0].GetTelemetryObservedAt().AsTime() != boardObservedAt {
		t.Fatalf("board telemetry timestamp = %v", first.Chains[0].Boards[0].GetTelemetryObservedAt())
	}
	source.pipeline.QueueDepth = 0
	source.pipeline.DecodedEvents = 9
	source.storage.EventCount = 9
	source.boards[0].EventCount = 9
	source.boards[0].HVOverCurrent = true
	newerBoardObservation := boardObservedAt.Add(time.Second)
	source.boards[0].TelemetryObservedAt = &newerBoardObservation
	ticks <- time.Unix(1, 0)
	second := <-updates
	if second.Sequence != first.Sequence+1 || second.Pipeline.GetDecodedEvents() != 9 || second.CurrentRun.GetEventCount() != 9 || second.Chains[0].Boards[0].GetEventCount() != 9 || second.Chains[0].Boards[0].GetHealth() != daqv1.HealthStatus_HEALTH_STATUS_FAULT {
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

func TestBoardTelemetryOnlyAppliesNewerObservations(t *testing.T) {
	current := time.Date(2026, 7, 22, 18, 0, 0, 0, time.UTC)
	older := current.Add(-time.Second)
	newer := current.Add(time.Second)
	board := &daqv1.Board{TelemetryObservedAt: timestamppb.New(current)}
	if shouldApplyBoardTelemetry(board, nil) || shouldApplyBoardTelemetry(board, &older) || shouldApplyBoardTelemetry(board, &current) {
		t.Fatal("accepted missing, older, or equal board telemetry")
	}
	if !shouldApplyBoardTelemetry(board, &newer) {
		t.Fatal("rejected newer board telemetry")
	}
}
