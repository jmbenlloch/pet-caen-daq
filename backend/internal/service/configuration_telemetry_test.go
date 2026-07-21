package service

import (
	"errors"
	"testing"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

func TestConfigurationProgressPublisherMarksFailedBoard(t *testing.T) {
	states, _ := acquisition.NewStateMachine(acquisition.StateConfiguring, nil)
	_, _ = states.Move(acquisition.StateFault, "test")
	publisher, _ := telemetry.NewPublisher("instance", &daqv1.TelemetrySnapshot{Chains: []*daqv1.Chain{{
		Index: 2, Health: daqv1.HealthStatus_HEALTH_STATUS_OK,
		Boards: []*daqv1.Board{{Node: 0, Health: daqv1.HealthStatus_HEALTH_STATUS_OK}},
	}}}, nil)
	observe := ConfigurationProgressPublisher(publisher, states, func() time.Time { return time.Unix(10, 0) })
	observe(acquisition.ConfigurationProgress{
		Stage: acquisition.ConfigurationFailed, Target: &acquisition.ConfigurationTarget{Board: 2, Chain: 2, Node: 0},
		Message: "readback mismatch", Err: errors.New("mismatch"),
	})
	snapshot := publisher.Snapshot()
	if snapshot.State != daqv1.SystemState_SYSTEM_STATE_FAULT || snapshot.Chains[0].Health != daqv1.HealthStatus_HEALTH_STATUS_FAULT || snapshot.Chains[0].Boards[0].Health != daqv1.HealthStatus_HEALTH_STATUS_FAULT {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if len(snapshot.Diagnostics) != 1 || snapshot.Diagnostics[0].Code != "CONFIGURATION_FAILED" || snapshot.Diagnostics[0].Chain != "2" || snapshot.Diagnostics[0].Node != "0" {
		t.Fatalf("diagnostics=%+v", snapshot.Diagnostics)
	}
}
