package service

import (
	"testing"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

type fixedStorageStats struct{ stats runpipeline.StorageStats }

func (s fixedStorageStats) Stats() runpipeline.StorageStats { return s.stats }

func TestPublishStorageTelemetryUpdatesRunAndHealth(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", &daqv1.TelemetrySnapshot{CurrentRun: &daqv1.RunSummary{RunId: "42", Incomplete: true}}, nil)
	snapshot := PublishStorageTelemetry(publisher, fixedStorageStats{runpipeline.StorageStats{
		Directory: "/runs/run-42", BytesWritten: 1234, EventCount: 50, RawBatches: 7, Finalized: false,
	}})
	if snapshot.Storage.Health != daqv1.HealthStatus_HEALTH_STATUS_OK || snapshot.Storage.RunDirectory != "/runs/run-42" || snapshot.Storage.BytesWritten != 1234 {
		t.Fatalf("storage = %+v", snapshot.Storage)
	}
	if snapshot.CurrentRun.EventCount != 50 || snapshot.CurrentRun.RawBatchCount != 7 || !snapshot.CurrentRun.Incomplete {
		t.Fatalf("run = %+v", snapshot.CurrentRun)
	}
	fault := PublishStorageTelemetry(publisher, fixedStorageStats{runpipeline.StorageStats{LastError: "disk full"}})
	if fault.Storage.Health != daqv1.HealthStatus_HEALTH_STATUS_FAULT || fault.Storage.LastError != "disk full" {
		t.Fatalf("fault storage = %+v", fault.Storage)
	}
}
