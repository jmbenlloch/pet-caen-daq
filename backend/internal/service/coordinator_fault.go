package service

import (
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func PublishCoordinatorFault(publisher SnapshotPublisher, err error, now func() time.Time) {
	if publisher == nil || err == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	snapshot := publisher.Snapshot()
	snapshot.State = daqv1.SystemState_SYSTEM_STATE_FAULT
	snapshot.Diagnostics = append(snapshot.Diagnostics, &daqv1.Diagnostic{
		Severity: daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_ERROR,
		Code:     "COORDINATOR_FAULT", Message: err.Error(), ObservedAt: timestamppb.New(now()),
	})
	publisher.Publish(snapshot)
}
