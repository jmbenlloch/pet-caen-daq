package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelaystore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/scanrecord"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircasestore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

type recordedController struct{}

func (recordedController) StateSnapshot() acquisition.StateSnapshot {
	return acquisition.StateSnapshot{State: acquisition.StateReady}
}
func (recordedController) Run(context.Context, staircase.Request, string, func(staircase.Point) error) (bool, error) {
	panic("unrecorded staircase path")
}
func (recordedController) RunHoldDelay(context.Context, holddelay.Request, string, func(holddelay.Point) error) (bool, error) {
	panic("unrecorded hold-delay path")
}
func (recordedController) RunRecorded(_ context.Context, _ staircase.Request, _ string, point func(staircase.Point) error, sink scanrecord.Sink) (bool, error) {
	if err := scanrecord.Emit(sink, scanrecord.Record{Type: "configuration", Configuration: &scanrecord.Configuration{}}); err != nil {
		return false, err
	}
	return true, point(staircase.Point{Threshold: 100})
}
func (recordedController) RunHoldDelayRecorded(_ context.Context, _ holddelay.Request, _ string, point func(holddelay.Point) error, sink scanrecord.Sink) (bool, error) {
	if err := scanrecord.Emit(sink, scanrecord.Record{Type: "configuration", Configuration: &scanrecord.Configuration{}}); err != nil {
		return false, err
	}
	return true, point(holddelay.Point{EventsCollected: 10})
}

func TestScanServicesPersistControllerEvidence(t *testing.T) {
	for _, kind := range []string{"staircase", "hold-delay"} {
		t.Run(kind, func(t *testing.T) {
			publisher, err := telemetry.NewPublisher("test", &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_READY}, nil)
			if err != nil {
				t.Fatal(err)
			}
			parent := t.TempDir()
			controller := recordedController{}
			service := &ScanService{Controller: controller, HoldDelayController: controller, Telemetry: publisher, ScanParent: parent, AllocateRunID: func(context.Context) (string, error) { return "42", nil }}
			if kind == "staircase" {
				_, err = service.StartStaircase(context.Background(), connect.NewRequest(&daqv1.StartStaircaseRequest{RequestedBy: "operator", MinimumThreshold: 100, MaximumThreshold: 100, Step: 1, DwellMilliseconds: 1}))
			} else {
				_, err = service.StartHoldDelayScan(context.Background(), connect.NewRequest(&daqv1.StartHoldDelayScanRequest{RequestedBy: "operator", StepNs: 8, EventsPerDelay: 10, PointTimeoutSeconds: 1}))
			}
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				service.mu.Lock()
				active := service.active != nil
				service.mu.Unlock()
				if !active {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("scan did not finalize")
				}
				time.Sleep(time.Millisecond)
			}
			dirname := "scan-42"
			if kind == "staircase" {
				m, _, e := staircasestore.Read(parent, "42")
				if e != nil || m.EvidenceArtifact == nil || m.State != "completed" {
					t.Fatalf("manifest=%+v error=%v", m, e)
				}
			} else {
				dirname = "hold-scan-42"
				m, _, e := holddelaystore.Read(parent, "42")
				if e != nil || m.EvidenceArtifact == nil || m.State != "completed" {
					t.Fatalf("manifest=%+v error=%v", m, e)
				}
			}
			if data, err := os.ReadFile(filepath.Join(parent, dirname, scanrecord.JournalName)); err != nil || len(data) == 0 {
				t.Fatalf("journal missing: %v", err)
			}
		})
	}
}

func TestEvidenceFailureIsNotHiddenByCancellation(t *testing.T) {
	err := errors.Join(context.Canceled, scanrecord.ErrPersistence)
	state, _ := scanOutcome(err, true)
	if state != daqv1.ScanState_SCAN_STATE_FAILED || stateAfterScan(err, true) != daqv1.SystemState_SYSTEM_STATE_FAULT {
		t.Fatal("evidence failure was treated as a clean cancellation")
	}
}
