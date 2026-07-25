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
		OperationID: "configuration-1",
		Stage:       acquisition.ConfigurationFailed, Target: &acquisition.ConfigurationTarget{Board: 2, Chain: 2, Node: 0},
		BoardsCompleted: 1, BoardsTotal: 4, Message: "readback mismatch", Err: errors.New("mismatch"),
	})
	snapshot := publisher.Snapshot()
	if snapshot.State != daqv1.SystemState_SYSTEM_STATE_FAULT || snapshot.Chains[0].Health != daqv1.HealthStatus_HEALTH_STATUS_FAULT || snapshot.Chains[0].Boards[0].Health != daqv1.HealthStatus_HEALTH_STATUS_FAULT {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if len(snapshot.Diagnostics) != 1 || snapshot.Diagnostics[0].Code != "CONFIGURATION_FAILED" || snapshot.Diagnostics[0].Chain != "2" || snapshot.Diagnostics[0].Node != "0" {
		t.Fatalf("diagnostics=%+v", snapshot.Diagnostics)
	}
	progress := snapshot.GetConfigurationProgress()
	if progress.GetOperationId() != "configuration-1" || progress.GetStage() != daqv1.ConfigurationStage_CONFIGURATION_STAGE_FAILED ||
		progress.GetActive() || progress.GetBoard() != 2 || progress.GetBoardsCompleted() != 1 || progress.GetBoardsTotal() != 4 {
		t.Fatalf("progress=%+v", progress)
	}
}

func TestConfigurationProgressPublisherReplacesStructuredSnapshotWithoutDiagnostics(t *testing.T) {
	states, _ := acquisition.NewStateMachine(acquisition.StateConfiguring, nil)
	publisher, _ := telemetry.NewPublisher("instance", &daqv1.TelemetrySnapshot{}, nil)
	times := []time.Time{time.Unix(10, 0), time.Unix(11, 0)}
	observe := ConfigurationProgressPublisher(publisher, states, func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	})
	target := &acquisition.ConfigurationTarget{Board: 1, Chain: 1, Node: 0}
	observe(acquisition.ConfigurationProgress{
		OperationID: "configuration-7", Stage: acquisition.ConfigurationWriting, Target: target,
		BoardsCompleted: 1, BoardsTotal: 4, Completed: 50, Total: 596, Unit: "registers",
		Message: "writing registers",
	})
	observe(acquisition.ConfigurationProgress{
		OperationID: "configuration-7", Stage: acquisition.ConfigurationWriting, Target: target,
		BoardsCompleted: 1, BoardsTotal: 4, Completed: 100, Total: 596, Unit: "registers",
		Message: "writing registers",
	})
	snapshot := publisher.Snapshot()
	progress := snapshot.GetConfigurationProgress()
	if progress.GetCompleted() != 100 || progress.GetTotal() != 596 || progress.GetUnit() != "registers" ||
		!progress.GetStartedAt().AsTime().Equal(time.Unix(10, 0)) || !progress.GetUpdatedAt().AsTime().Equal(time.Unix(11, 0)) {
		t.Fatalf("progress=%+v", progress)
	}
	if len(snapshot.Diagnostics) != 0 {
		t.Fatalf("diagnostics=%+v", snapshot.Diagnostics)
	}
}
