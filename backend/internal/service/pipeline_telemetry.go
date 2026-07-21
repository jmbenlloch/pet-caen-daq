package service

import (
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
)

type PipelineStatsSource interface {
	Stats() acquisition.PipelineStats
}

// PublishPipelineTelemetry copies operational counters into a new complete
// snapshot. Polling/orchestration owns cadence; the acquisition package has no
// dependency on protobuf or ConnectRPC.
func PublishPipelineTelemetry(publisher SnapshotPublisher, source PipelineStatsSource) *daqv1.TelemetrySnapshot {
	stats := source.Stats()
	snapshot := publisher.Snapshot()
	snapshot.Pipeline = &daqv1.PipelineTelemetry{
		QueueCapacity:   uint64(stats.Capacity),
		QueueDepth:      uint64(stats.QueueDepth),
		AcceptedBatches: stats.AcceptedBatches,
		RejectedBatches: stats.RejectedBatches,
		DecodedEvents:   stats.DecodedEvents,
		DecodeFailures:  stats.DecodeFailures,
	}
	if stats.SinkFailures > 0 {
		if snapshot.Storage == nil {
			snapshot.Storage = &daqv1.StorageTelemetry{}
		}
		snapshot.Storage.Health = daqv1.HealthStatus_HEALTH_STATUS_FAULT
	}
	return publisher.Publish(snapshot)
}
