package service

import (
	"strconv"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ConfigurationProgressPublisher adapts configuration lifecycle events to
// complete telemetry snapshots. Progress is deliberately low-volume and kept
// as diagnostics so a reconnecting operator can see the completed sequence.
func ConfigurationProgressPublisher(publisher SnapshotPublisher, states *acquisition.StateMachine, now func() time.Time) acquisition.ConfigurationObserver {
	if now == nil {
		now = time.Now
	}
	return func(progress acquisition.ConfigurationProgress) {
		snapshot := publisher.Snapshot()
		snapshot.State = protobufState(states.Snapshot().State)
		severity := daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_INFO
		code := "CONFIGURATION_PROGRESS"
		if progress.Err != nil {
			severity = daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_ERROR
			code = "CONFIGURATION_FAILED"
		}
		diagnostic := &daqv1.Diagnostic{Severity: severity, Code: code, Message: string(progress.Stage) + ": " + progress.Message, ObservedAt: timestamppb.New(now())}
		if progress.Target != nil {
			diagnostic.Chain = strconv.Itoa(int(progress.Target.Chain))
			diagnostic.Node = strconv.Itoa(int(progress.Target.Node))
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
		snapshot.Diagnostics = append(snapshot.Diagnostics, diagnostic)
		publisher.Publish(snapshot)
	}
}
