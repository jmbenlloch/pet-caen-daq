package service

import (
	"errors"
	"testing"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

func TestPublishCoordinatorFaultIsImmediateAndDiagnostic(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_RUNNING}, nil)
	PublishCoordinatorFault(publisher, errors.New("stream disconnected"), func() time.Time { return time.Unix(10, 0) })
	snapshot := publisher.Snapshot()
	if snapshot.State != daqv1.SystemState_SYSTEM_STATE_FAULT || len(snapshot.Diagnostics) != 1 || snapshot.Diagnostics[0].GetCode() != "COORDINATOR_FAULT" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}
