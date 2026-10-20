package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelaystore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircasestore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
	"google.golang.org/protobuf/proto"
)

type scanRestorationController struct {
	restored bool
	err      error
}

func (scanRestorationController) StateSnapshot() acquisition.StateSnapshot {
	return acquisition.StateSnapshot{State: acquisition.StateReady}
}
func (c scanRestorationController) Run(context.Context, staircase.Request, string, func(staircase.Point) error) (bool, error) {
	return c.restored, c.err
}
func (c scanRestorationController) RunHoldDelay(context.Context, holddelay.Request, string, func(holddelay.Point) error) (bool, error) {
	return c.restored, c.err
}

func TestScanCompletionTracksConfigurationRestoration(t *testing.T) {
	restoreErr := errors.New("restore board 0 configuration: readback mismatch")
	for _, kind := range []string{"staircase", "hold-delay"} {
		for _, test := range []struct {
			name      string
			restored  bool
			err       error
			state     daqv1.SystemState
			scanState daqv1.ScanState
		}{
			{"completed", true, nil, daqv1.SystemState_SYSTEM_STATE_READY, daqv1.ScanState_SCAN_STATE_COMPLETED},
			{"cancelled and restored", true, context.Canceled, daqv1.SystemState_SYSTEM_STATE_READY, daqv1.ScanState_SCAN_STATE_CANCELLED},
			{"scan error but restored", true, errors.New("counter read failed"), daqv1.SystemState_SYSTEM_STATE_FAULT, daqv1.ScanState_SCAN_STATE_FAILED},
			{"restore failed", false, restoreErr, daqv1.SystemState_SYSTEM_STATE_FAULT, daqv1.ScanState_SCAN_STATE_FAILED},
			{"cancelled and restore failed", false, errors.Join(context.Canceled, restoreErr), daqv1.SystemState_SYSTEM_STATE_FAULT, daqv1.ScanState_SCAN_STATE_FAILED},
			{"restore unconfirmed", false, nil, daqv1.SystemState_SYSTEM_STATE_FAULT, daqv1.ScanState_SCAN_STATE_FAILED},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				applied := &daqv1.AppliedConfiguration{RequestedBy: "operator", Source: daqv1.AppliedConfigurationSource_APPLIED_CONFIGURATION_SOURCE_APPLY, ConfigurationRequestId: "previous-apply", BoardCount: 4, HvAuthorized: true}
				publisher, err := telemetry.NewPublisher("restoration-test", &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_READY, AppliedConfiguration: applied}, nil)
				if err != nil {
					t.Fatal(err)
				}
				parent := t.TempDir()
				controller := scanRestorationController{test.restored, test.err}
				s := &ScanService{Controller: controller, HoldDelayController: controller, Telemetry: publisher, ScanParent: parent, AllocateRunID: func(context.Context) (string, error) { return "1", nil }}
				if kind == "staircase" {
					_, err = s.StartStaircase(context.Background(), connect.NewRequest(&daqv1.StartStaircaseRequest{RequestedBy: "operator", MinimumThreshold: 100, MaximumThreshold: 110, Step: 10, DwellMilliseconds: 1}))
				} else {
					_, err = s.StartHoldDelayScan(context.Background(), connect.NewRequest(&daqv1.StartHoldDelayScanRequest{RequestedBy: "operator", MinimumDelayNs: 0, MaximumDelayNs: 8, StepNs: 8, EventsPerDelay: 10, PointTimeoutSeconds: 1}))
				}
				if err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(2 * time.Second)
				for publisher.Snapshot().GetLatestCompletedScan() == nil && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				snapshot := publisher.Snapshot()
				if snapshot.GetState() != test.state {
					t.Errorf("system state=%v, want %v", snapshot.GetState(), test.state)
				}
				summary := snapshot.GetLatestCompletedScan()
				if summary.GetState() != test.scanState {
					t.Errorf("scan state=%v, want %v", summary.GetState(), test.scanState)
				}
				if snapshot.GetCurrentStaircase() != nil || snapshot.GetCurrentHoldDelayScan() != nil {
					t.Error("completed scan still active in telemetry")
				}
				if test.restored {
					if !proto.Equal(snapshot.GetAppliedConfiguration(), applied) {
						t.Error("successful restoration lost the applied record")
					}
				} else if snapshot.GetAppliedConfiguration() != nil {
					t.Error("failed restoration still advertises the prior applied configuration")
				}
				if errors.Is(test.err, restoreErr) && !strings.Contains(summary.GetTerminationReason(), restoreErr.Error()) {
					t.Errorf("restore error hidden: %q", summary.GetTerminationReason())
				}
				var restored bool
				var state string
				if kind == "staircase" {
					manifest, _, readErr := staircasestore.Read(parent, "1")
					if readErr != nil {
						t.Fatal(readErr)
					}
					restored, state = manifest.Restored, manifest.State
				} else {
					manifest, _, readErr := holddelaystore.Read(parent, "1")
					if readErr != nil {
						t.Fatal(readErr)
					}
					restored, state = manifest.Restored, manifest.State
				}
				if restored != test.restored {
					t.Errorf("persisted restored=%t, want %t", restored, test.restored)
				}
				wantState := strings.ToLower(strings.TrimPrefix(test.scanState.String(), "SCAN_STATE_"))
				if state != wantState {
					t.Errorf("persisted state=%s, want %s", state, wantState)
				}
			})
		}
	}
}
