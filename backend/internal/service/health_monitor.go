package service

import (
	"context"
	"fmt"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
)

type RunHealthSource interface {
	PipelineStats() acquisition.PipelineStats
	StorageStats() runpipeline.StorageStats
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
	return m.Publisher.Publish(snapshot)
}
