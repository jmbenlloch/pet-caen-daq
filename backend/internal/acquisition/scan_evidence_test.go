package acquisition

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/scanrecord"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
)

func TestRecordedStaircaseRestoresOnCancelAndEvidenceFailure(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := janusconfig.Parse(strings.NewReader(string(fixture)))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := dt5202.PlanProductionConfiguration(document, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"completed", "cancelled", "storage_failure", "restore_failure", "cancelled_storage_failure"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hardware := &staircaseHardware{registers: map[dt5202.Register]uint32{}, onCounting: func() {
				if scenario == "cancelled" || scenario == "cancelled_storage_failure" {
					cancel()
				}
			}}
			if scenario == "restore_failure" {
				hardware.restoreError = errors.New("failed restore")
				hardware.restoreFailure = "write"
			}
			states, _ := NewStateMachine(StateReady, nil)
			coordinator, err := NewStaircaseCoordinator(states, hardware, []StaircaseTarget{{Board: 0, Chain: 1, Node: 2, Plan: plan}})
			if err != nil {
				t.Fatal(err)
			}
			var records []scanrecord.Record
			full := errors.New("evidence disk full")
			restored, err := coordinator.RunRecorded(ctx, staircase.Request{Minimum: 100, Maximum: 100, Step: 1, Dwell: time.Millisecond}, "test", func(staircase.Point) error { return nil }, func(r scanrecord.Record) error {
				records = append(records, r)
				if (scenario == "storage_failure" && r.Type == "overrides") || (scenario == "cancelled_storage_failure" && r.Type == "restoration") {
					return full
				}
				return nil
			})
			if restored != (scenario != "restore_failure") {
				t.Fatalf("restored=%t error=%v", restored, err)
			}
			switch scenario {
			case "completed":
				if err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error=%v", err)
				}
			case "storage_failure":
				if !errors.Is(err, full) {
					t.Fatalf("error=%v", err)
				}
			case "cancelled_storage_failure":
				if !errors.Is(err, context.Canceled) || !errors.Is(err, scanrecord.ErrPersistence) || states.Snapshot().State != StateFault {
					t.Fatalf("lost persistence fault during cancellation: state=%s error=%v", states.Snapshot().State, err)
				}
			case "restore_failure":
				if err == nil {
					t.Fatal("lost restoration failure")
				}
			}
			baseline, restoration, after := false, false, false
			for _, r := range records {
				if r.Type == "baseline" {
					baseline = r.BaselinePlan != nil
				}
				if r.Type == "restoration" {
					restoration = r.Restored != nil && *r.Restored == restored
				}
				if r.Phase == "after_restore" {
					after = true
				}
			}
			if !baseline || !restoration || !after {
				t.Fatalf("incomplete evidence: baseline=%t restoration=%t after=%t", baseline, restoration, after)
			}
		})
	}
}
