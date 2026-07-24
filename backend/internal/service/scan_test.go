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
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

type fakeStaircaseController struct {
	release <-chan struct{}
}

func (c *fakeStaircaseController) Run(
	context.Context,
	staircase.Request,
	string,
	func(staircase.Point) error,
) (bool, error) {
	<-c.release
	return true, errors.New("test complete")
}

func (*fakeStaircaseController) StateSnapshot() acquisition.StateSnapshot {
	return acquisition.StateSnapshot{State: acquisition.StateReady}
}

func TestScanServiceUsesSharedNumericRunIDAllocator(t *testing.T) {
	release := make(chan struct{})
	publisher, err := telemetry.NewPublisher(
		"scan-test",
		&daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_READY},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	scans := &ScanService{
		Controller:    &fakeStaircaseController{release: release},
		Telemetry:     publisher,
		ScanParent:    parent,
		AllocateRunID: func(context.Context) (string, error) { return "42", nil },
	}

	response, err := scans.StartStaircase(context.Background(), connect.NewRequest(
		&daqv1.StartStaircaseRequest{
			Board: 0, MinimumThreshold: 150, MaximumThreshold: 160, Step: 5,
			DwellMilliseconds: 500, RequestedBy: "operator",
		},
	))
	if err != nil {
		t.Fatal(err)
	}
	if got := response.Msg.GetScan().GetSummary().GetScanId(); got != "42" {
		t.Fatalf("scan run number = %q, want 42", got)
	}
	if _, err := os.Stat(filepath.Join(parent, "scan-42", "manifest.json")); err != nil {
		t.Fatalf("numeric scan manifest: %v", err)
	}

	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		scans.mu.Lock()
		active := scans.active
		scans.mu.Unlock()
		if active == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scan did not finalize")
		}
		time.Sleep(time.Millisecond)
	}
}
