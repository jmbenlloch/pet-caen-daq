package service

import (
	"strconv"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ConfigurationProgressPublisher adapts configuration lifecycle events to a
// complete, reconnect-safe telemetry progress snapshot. Only failures are also
// retained as diagnostics.
func ConfigurationProgressPublisher(publisher SnapshotPublisher, states *acquisition.StateMachine, now func() time.Time) acquisition.ConfigurationObserver {
	if now == nil {
		now = time.Now
	}
	var operationID string
	var startedAt time.Time
	return func(progress acquisition.ConfigurationProgress) {
		snapshot := publisher.Snapshot()
		snapshot.State = protobufState(states.Snapshot().State)
		updatedAt := now()
		if operationID != progress.OperationID {
			operationID, startedAt = progress.OperationID, updatedAt
		}
		structured := &daqv1.ConfigurationProgress{
			OperationId:     progress.OperationID,
			Stage:           protobufConfigurationStage(progress.Stage),
			Active:          progress.Stage != acquisition.ConfigurationComplete && progress.Stage != acquisition.ConfigurationFailed,
			BoardsCompleted: uint32(progress.BoardsCompleted),
			BoardsTotal:     uint32(progress.BoardsTotal),
			Completed:       uint32(progress.Completed),
			Total:           uint32(progress.Total),
			Unit:            progress.Unit,
			Reused:          progress.Reused,
			Message:         progress.Message,
			StartedAt:       timestamppb.New(startedAt),
			UpdatedAt:       timestamppb.New(updatedAt),
		}
		if progress.Target != nil {
			board, chain, node := uint32(progress.Target.Board), uint32(progress.Target.Chain), uint32(progress.Target.Node)
			structured.Board, structured.Chain, structured.Node = &board, &chain, &node
			if progress.Err != nil {
				for _, chain := range snapshot.Chains {
					if chain.Index == uint32(progress.Target.Chain) {
						chain.Health = daqv1.HealthStatus_HEALTH_STATUS_FAULT
						for _, board := range chain.Boards {
							if board.Node == uint32(progress.Target.Node) {
								board.Health = daqv1.HealthStatus_HEALTH_STATUS_FAULT
							}
						}
					}
				}
			}
		}
		snapshot.ConfigurationProgress = structured
		if progress.Err != nil {
			diagnostic := &daqv1.Diagnostic{
				Severity: daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_ERROR,
				Code:     "CONFIGURATION_FAILED", Message: string(progress.Stage) + ": " + progress.Message,
				ObservedAt: timestamppb.New(updatedAt),
			}
			if progress.Target != nil {
				diagnostic.Chain = strconv.Itoa(int(progress.Target.Chain))
				diagnostic.Node = strconv.Itoa(int(progress.Target.Node))
			}
			snapshot.Diagnostics = append(snapshot.Diagnostics, diagnostic)
		}
		publisher.Publish(snapshot)
	}
}

func protobufConfigurationStage(stage acquisition.ConfigurationStage) daqv1.ConfigurationStage {
	switch stage {
	case acquisition.ConfigurationPlanning:
		return daqv1.ConfigurationStage_CONFIGURATION_STAGE_PLANNING
	case acquisition.ConfigurationPedestal:
		return daqv1.ConfigurationStage_CONFIGURATION_STAGE_PEDESTAL
	case acquisition.ConfigurationWriting:
		return daqv1.ConfigurationStage_CONFIGURATION_STAGE_WRITING_REGISTERS
	case acquisition.ConfigurationCitiroc:
		return daqv1.ConfigurationStage_CONFIGURATION_STAGE_CONFIGURING_CITIROC
	case acquisition.ConfigurationReadback:
		return daqv1.ConfigurationStage_CONFIGURATION_STAGE_READING_REGISTERS
	case acquisition.ConfigurationHV:
		return daqv1.ConfigurationStage_CONFIGURATION_STAGE_HV
	case acquisition.ConfigurationComplete:
		return daqv1.ConfigurationStage_CONFIGURATION_STAGE_COMPLETE
	case acquisition.ConfigurationFailed:
		return daqv1.ConfigurationStage_CONFIGURATION_STAGE_FAILED
	default:
		return daqv1.ConfigurationStage_CONFIGURATION_STAGE_UNSPECIFIED
	}
}
