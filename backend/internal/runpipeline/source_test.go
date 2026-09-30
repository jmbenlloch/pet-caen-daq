package runpipeline

import (
	"path/filepath"
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestSourcePositionPersistedBeforeAndAfterRun(t *testing.T) {
	parent := t.TempDir()
	position := &runstore.SourcePosition{ScanID: "xy-test", Index: 3, Row: 1, Column: 2, XSteps: -400, CalculatedXMM: -1, PositionBasis: "open_loop_steps_from_operator_confirmed_origin", Configuration: `{"microsteps":1}`}
	created, err := (Factory{Options: Options{Parent: parent, Capacity: 4}}).New("42", acquisition.RunOptions{SourcePosition: position})
	if err != nil {
		t.Fatal(err)
	}
	session := created.(*Session)
	for _, final := range []bool{false, true} {
		if final {
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
			if err := session.Finalize("2026-09-21T00:00:00Z", "source_scan_time"); err != nil {
				t.Fatal(err)
			}
		}
		manifest, err := runstore.ReadManifest(filepath.Join(parent, "run-42"), "42")
		if err != nil {
			t.Fatal(err)
		}
		if manifest.SourcePosition == nil || *manifest.SourcePosition != *position {
			t.Fatalf("source position lost: %+v", manifest.SourcePosition)
		}
	}
}
