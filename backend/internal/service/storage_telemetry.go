package service

import (
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
)

type StorageStatsSource interface {
	Stats() runpipeline.StorageStats
}

func PublishStorageTelemetry(publisher SnapshotPublisher, source StorageStatsSource) *daqv1.TelemetrySnapshot {
	stats := source.Stats()
	snapshot := publisher.Snapshot()
	health := daqv1.HealthStatus_HEALTH_STATUS_OK
	if stats.LastError != "" {
		health = daqv1.HealthStatus_HEALTH_STATUS_FAULT
	}
	snapshot.Storage = &daqv1.StorageTelemetry{
		Health: health, RunDirectory: stats.Directory, BytesWritten: stats.BytesWritten, LastError: stats.LastError,
	}
	if snapshot.CurrentRun != nil {
		snapshot.CurrentRun.EventCount = stats.EventCount
		snapshot.CurrentRun.RawBatchCount = stats.RawBatches
		snapshot.CurrentRun.Incomplete = !stats.Finalized
	}
	return publisher.Publish(snapshot)
}
