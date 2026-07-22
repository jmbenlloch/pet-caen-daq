package service

import (
	"context"
	"fmt"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type RunHealthSource interface {
	PipelineStats() acquisition.PipelineStats
	StorageStats() runpipeline.StorageStats
}

type BoardHealthSource interface {
	BoardStats() []runpipeline.BoardStats
}

type StatisticsSource interface {
	BoardStats() []runpipeline.BoardStats
	StatisticsElapsed() time.Duration
}

// HealthMonitor publishes one complete, coalesced snapshot per sample. Tick is
// injectable for deterministic tests; production callers normally use Interval.
type HealthMonitor struct {
	Publisher SnapshotPublisher
	Source    RunHealthSource
	Interval  time.Duration
	Tick      <-chan time.Time
}

func (m *HealthMonitor) Run(ctx context.Context) error {
	if m.Publisher == nil || m.Source == nil {
		return fmt.Errorf("health monitor publisher and source are required")
	}
	ticks := m.Tick
	var ticker *time.Ticker
	if ticks == nil {
		if m.Interval <= 0 {
			return fmt.Errorf("health monitor interval must be positive")
		}
		ticker = time.NewTicker(m.Interval)
		defer ticker.Stop()
		ticks = ticker.C
	}
	m.publish()
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-ticks:
			if !ok {
				return nil
			}
			m.publish()
		}
	}
}

func (m *HealthMonitor) publish() *daqv1.TelemetrySnapshot {
	pipeline := m.Source.PipelineStats()
	storage := m.Source.StorageStats()
	snapshot := m.Publisher.Snapshot()
	snapshot.Pipeline = &daqv1.PipelineTelemetry{
		QueueCapacity: uint64(pipeline.Capacity), QueueDepth: uint64(pipeline.QueueDepth),
		AcceptedBatches: pipeline.AcceptedBatches, RejectedBatches: pipeline.RejectedBatches,
		DecodedEvents: pipeline.DecodedEvents, DecodeFailures: pipeline.DecodeFailures,
	}
	health := daqv1.HealthStatus_HEALTH_STATUS_OK
	if storage.LastError != "" || pipeline.SinkFailures > 0 {
		health = daqv1.HealthStatus_HEALTH_STATUS_FAULT
	}
	snapshot.Storage = &daqv1.StorageTelemetry{
		Health: health, RunDirectory: storage.Directory, BytesWritten: storage.BytesWritten, LastError: storage.LastError,
	}
	if snapshot.CurrentRun != nil {
		snapshot.CurrentRun.EventCount = storage.EventCount
		snapshot.CurrentRun.RawBatchCount = storage.RawBatches
		snapshot.CurrentRun.Incomplete = !storage.Finalized
	}
	if boards, ok := m.Source.(BoardHealthSource); ok {
		observations := boards.BoardStats()
		for _, observation := range observations {
			for _, chain := range snapshot.Chains {
				if chain.Index != uint32(observation.Chain) {
					continue
				}
				for _, board := range chain.Boards {
					if board.Node != uint32(observation.Node) {
						continue
					}
					board.EventCount = observation.EventCount
					board.Health = daqv1.HealthStatus_HEALTH_STATUS_OK
					if observation.FPGATemperature != nil {
						board.FpgaTemperatureC = *observation.FPGATemperature
					}
					if observation.BoardTemperature != nil {
						board.BoardTemperatureC = *observation.BoardTemperature
					}
					if observation.DetectorTemperature != nil {
						board.DetectorTemperatureC = *observation.DetectorTemperature
					}
					if observation.HVTemperature != nil {
						board.HvTemperatureC = *observation.HVTemperature
					}
					if observation.HVVoltage != nil {
						board.HvVoltageV = *observation.HVVoltage
					}
					if observation.HVCurrent != nil {
						board.HvCurrentA = *observation.HVCurrent
					}
					if observation.TelemetryObservedAt != nil {
						board.TelemetryObservedAt = timestamppb.New(*observation.TelemetryObservedAt)
					}
					board.HvOn = observation.HVOn
					board.HvRamping = observation.HVRamping
					board.HvOverCurrent = observation.HVOverCurrent
					board.HvOverVoltage = observation.HVOverVoltage
					if observation.HVOverCurrent || observation.HVOverVoltage {
						board.Health = daqv1.HealthStatus_HEALTH_STATUS_FAULT
					}
				}
			}
		}
		if statistics, ok := m.Source.(StatisticsSource); ok {
			elapsed := statistics.StatisticsElapsed().Milliseconds()
			if elapsed < 0 {
				elapsed = 0
			}
			snapshot.Statistics = &daqv1.StatisticsTelemetry{ElapsedMilliseconds: uint64(elapsed)}
			for _, observation := range observations {
				snapshot.Statistics.Boards = append(snapshot.Statistics.Boards, &daqv1.BoardStatistics{
					Chain: observationChain(observation), Node: uint32(observation.Node), Timestamp: observation.Timestamp,
					TriggerId: observation.TriggerID, TriggerCount: observation.TriggerCount, LostTriggerCount: observation.LostTriggerCount,
					EventBuildCount: observation.EventBuildCount, DataBytes: observation.DataBytes,
					ChannelTriggerCounts: observation.ChannelTriggerCount[:], TimestampCounts: observation.TimestampCount[:], PhaCounts: observation.PHACount[:],
				})
			}
		}
	}
	return m.Publisher.Publish(snapshot)
}

func observationChain(observation runpipeline.BoardStats) uint32 { return uint32(observation.Chain) }
