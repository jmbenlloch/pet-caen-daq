package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

func TestPublishRecoveryDiagnosticsDistinguishesIncompleteAndCorrupt(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", &daqv1.TelemetrySnapshot{Storage: &daqv1.StorageTelemetry{Health: daqv1.HealthStatus_HEALTH_STATUS_OK}}, nil)
	snapshot := PublishRecoveryDiagnostics(publisher, []runstore.IncompleteRun{
		{RunID: "42", Directory: "/runs/run-42", Manifest: &runstore.Manifest{RunID: "42"}},
		{RunID: "43", Directory: "/runs/run-43", Problem: "decode manifest"},
	}, time.Unix(100, 0))
	if snapshot.Storage.Health != daqv1.HealthStatus_HEALTH_STATUS_DEGRADED || len(snapshot.Diagnostics) != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Diagnostics[0].Code != "INCOMPLETE_RUN" || snapshot.Diagnostics[0].Severity != daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_WARNING {
		t.Fatalf("valid diagnostic = %+v", snapshot.Diagnostics[0])
	}
	if snapshot.Diagnostics[1].Code != "INCOMPLETE_RUN_METADATA_INVALID" || snapshot.Diagnostics[1].Severity != daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_ERROR {
		t.Fatalf("corrupt diagnostic = %+v", snapshot.Diagnostics[1])
	}
}

func TestPublishStartupRecoveryRetainsWarningsAndFailure(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", nil, nil)
	result := acquisition.StartupRecoveryResult{Detected: true, Original: errors.New("already running"), CleanupWarnings: errors.New("drain incomplete")}
	recovered := PublishStartupRecovery(publisher, result, nil, time.Unix(1, 0))
	if recovered.State != daqv1.SystemState_SYSTEM_STATE_IDLE || recovered.Diagnostics[len(recovered.Diagnostics)-1].GetCode() != "STARTUP_HARDWARE_RECOVERED" || !strings.Contains(recovered.Diagnostics[len(recovered.Diagnostics)-1].GetMessage(), "drain incomplete") {
		t.Fatalf("recovered = %+v", recovered)
	}
	failed := PublishStartupRecovery(publisher, result, errors.New("reset failed"), time.Unix(2, 0))
	if failed.State != daqv1.SystemState_SYSTEM_STATE_DISCONNECTED || failed.Diagnostics[len(failed.Diagnostics)-1].GetCode() != "STARTUP_HARDWARE_RECOVERY_FAILED" {
		t.Fatalf("failed = %+v", failed)
	}
}
