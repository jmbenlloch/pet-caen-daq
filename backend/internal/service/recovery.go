package service

import (
	"fmt"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PublishRecoveryDiagnostics makes restart evidence visible without changing
// run files. Operators decide whether to inspect, replay, or explicitly repair.
func PublishRecoveryDiagnostics(publisher SnapshotPublisher, runs []runstore.IncompleteRun, now time.Time) *daqv1.TelemetrySnapshot {
	snapshot := publisher.Snapshot()
	if len(runs) == 0 {
		return snapshot
	}
	if snapshot.Storage == nil {
		snapshot.Storage = &daqv1.StorageTelemetry{}
	}
	snapshot.Storage.Health = daqv1.HealthStatus_HEALTH_STATUS_DEGRADED
	for _, run := range runs {
		severity := daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_WARNING
		code := "INCOMPLETE_RUN"
		message := fmt.Sprintf("run %s was not finalized; artifacts remain in %s", run.RunID, run.Directory)
		if run.Problem != "" {
			severity = daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_ERROR
			code = "INCOMPLETE_RUN_METADATA_INVALID"
			message = fmt.Sprintf("run %s recovery metadata is invalid: %s", run.RunID, run.Problem)
		}
		snapshot.Diagnostics = append(snapshot.Diagnostics, &daqv1.Diagnostic{Severity: severity, Code: code, Message: message, ObservedAt: timestamppb.New(now)})
	}
	return publisher.Publish(snapshot)
}

func PublishStartupRecovery(publisher SnapshotPublisher, result acquisition.StartupRecoveryResult, recoveryErr error, now time.Time) *daqv1.TelemetrySnapshot {
	snapshot := publisher.Snapshot()
	if !result.Detected {
		return snapshot
	}
	snapshot.State = daqv1.SystemState_SYSTEM_STATE_IDLE
	severity := daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_WARNING
	code := "STARTUP_HARDWARE_RECOVERED"
	message := result.Original.Error() + "; bounded stop, drain, reset, and status verification succeeded"
	if result.CleanupWarnings != nil {
		message += "; cleanup warning retained: " + result.CleanupWarnings.Error()
	}
	if recoveryErr != nil {
		snapshot.State = daqv1.SystemState_SYSTEM_STATE_DISCONNECTED
		severity = daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_ERROR
		code = "STARTUP_HARDWARE_RECOVERY_FAILED"
		message = recoveryErr.Error()
	}
	snapshot.Diagnostics = append(snapshot.Diagnostics, &daqv1.Diagnostic{Severity: severity, Code: code, Message: message, ObservedAt: timestamppb.New(now)})
	return publisher.Publish(snapshot)
}
