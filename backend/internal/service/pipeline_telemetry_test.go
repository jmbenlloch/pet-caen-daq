package service

import (
	"testing"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

type fixedPipelineStats struct{ stats acquisition.PipelineStats }

func (s fixedPipelineStats) Stats() acquisition.PipelineStats { return s.stats }

func TestPublishPipelineTelemetryMapsCountersAndStorageFailure(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", &daqv1.TelemetrySnapshot{}, nil)
	snapshot := PublishPipelineTelemetry(publisher, fixedPipelineStats{acquisition.PipelineStats{
		Capacity: 8, QueueDepth: 3, AcceptedBatches: 10, RejectedBatches: 2,
		DecodedEvents: 50, DecodeFailures: 4, SinkFailures: 1,
	}})
	got := snapshot.Pipeline
	if got.QueueCapacity != 8 || got.QueueDepth != 3 || got.AcceptedBatches != 10 || got.RejectedBatches != 2 || got.DecodedEvents != 50 || got.DecodeFailures != 4 {
		t.Fatalf("pipeline telemetry = %+v", got)
	}
	if snapshot.Storage.GetHealth() != daqv1.HealthStatus_HEALTH_STATUS_FAULT {
		t.Fatalf("storage telemetry = %+v", snapshot.Storage)
	}
}
