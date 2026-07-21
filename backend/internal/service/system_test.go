package service

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

func TestGetSystemSnapshotIncludesCompatibleAndCompleteRepresentations(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", &daqv1.TelemetrySnapshot{
		State:  daqv1.SystemState_SYSTEM_STATE_READY,
		Chains: []*daqv1.Chain{{Index: 2, Enabled: true}},
	}, nil)
	service := &SystemService{Source: publisher}
	response, err := service.GetSystemSnapshot(context.Background(), connect.NewRequest(&daqv1.GetSystemSnapshotRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	message := response.Msg
	if message.InstanceId != "instance-a" || message.State != daqv1.SystemState_SYSTEM_STATE_READY || len(message.Chains) != 1 {
		t.Fatalf("legacy snapshot fields = %+v", message)
	}
	if message.Snapshot == nil || message.Snapshot.Sequence != 1 || message.Snapshot.InstanceId != "instance-a" {
		t.Fatalf("complete snapshot = %+v", message.Snapshot)
	}
}
