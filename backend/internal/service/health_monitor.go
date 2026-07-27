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
	var observations []runpipeline.BoardStats
	boards, hasBoards := m.Source.(BoardHealthSource)
	if hasBoards {
		observations = boards.BoardStats()
	}
	var elapsed time.Duration
	statistics, hasStatistics := m.Source.(StatisticsSource)
	if hasStatistics {
		elapsed = statistics.StatisticsElapsed()
	}
	return m.Publisher.Update(func(snapshot *daqv1.TelemetrySnapshot) {
		snapshot.Pipeline = &daqv1.PipelineTelemetry{
			QueueCapacity: uint64(pipeline.Capacity), QueueDepth: uint64(pipeline.QueueDepth),
			AcceptedBatches: pipeline.AcceptedBatches, RejectedBatches: pipeline.RejectedBatches,
			DecodedEvents: pipeline.DecodedEvents, DecodeFailures: pipeline.DecodeFailures,
			ReceivedBatches: pipeline.ReceivedBatches, ReceivedEvents: pipeline.ReceivedEvents,
			RawQueueDepth: uint64(pipeline.RawQueueDepth), EventQueueDepth: uint64(pipeline.EventQueueDepth),
			RawBatchesPersisted:   pipeline.RawBatchesPersisted,
			EventBatchesPersisted: pipeline.EventBatchesPersisted,
			PersistedEvents:       pipeline.PersistedEvents, SinkFailures: pipeline.SinkFailures,
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
		if hasBoards {
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
						if shouldApplyBoardTelemetry(board, observation.TelemetryObservedAt) {
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
							board.TelemetryObservedAt = timestamppb.New(*observation.TelemetryObservedAt)
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
			}
			if hasStatistics {
				elapsedMilliseconds := elapsed.Milliseconds()
				if elapsedMilliseconds < 0 {
					elapsedMilliseconds = 0
				}
				snapshot.Statistics = &daqv1.StatisticsTelemetry{ElapsedMilliseconds: uint64(elapsedMilliseconds)}
				snapshot.Statistics.Boards = statisticsBoards(snapshot, observations)
			}
		}
	})
}

func statisticsBoards(snapshot *daqv1.TelemetrySnapshot, observations []runpipeline.BoardStats) []*daqv1.BoardStatistics {
	type boardKey struct {
		chain uint32
		node  uint32
	}
	observed := make(map[boardKey]runpipeline.BoardStats, len(observations))
	for _, observation := range observations {
		observed[boardKey{chain: uint32(observation.Chain), node: uint32(observation.Node)}] = observation
	}
	result := make([]*daqv1.BoardStatistics, 0, len(observations))
	for _, chain := range snapshot.GetChains() {
		for _, board := range chain.GetBoards() {
			logicalIndex := board.GetLogicalIndex()
			statistic := &daqv1.BoardStatistics{
				Chain: chain.GetIndex(), Node: board.GetNode(), LogicalIndex: &logicalIndex,
			}
			if observation, ok := observed[boardKey{chain: chain.GetIndex(), node: board.GetNode()}]; ok {
				statistic.Timestamp = observation.Timestamp
				statistic.TriggerId = observation.TriggerID
				statistic.TriggerCount = observation.TriggerCount
				statistic.LostTriggerCount = observation.LostTriggerCount
				statistic.DataBytes = observation.DataBytes
				statistic.TOrCount = observation.TORCount
				statistic.ChannelTriggerCounts = observation.ChannelTriggerCount[:]
				statistic.TimestampCounts = observation.TimestampCount[:]
				statistic.PhaCounts = observation.PHACount[:]
			}
			result = append(result, statistic)
		}
	}
	return result
}

func shouldApplyBoardTelemetry(board *daqv1.Board, observedAt *time.Time) bool {
	if observedAt == nil {
		return false
	}
	current := board.GetTelemetryObservedAt()
	return current == nil || current.CheckValid() != nil || observedAt.After(current.AsTime())
}
