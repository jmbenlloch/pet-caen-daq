//go:build hdf5

package hdf5store

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	hdf5 "github.com/next-exp/hdf5-go"
)

func TestSourcePositionInHDF5AndConversion(t *testing.T) {
	for _, converted := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "conversion"}[converted], func(t *testing.T) {
			parent := t.TempDir()
			position := &runstore.SourcePosition{ScanID: "xy-example", Index: 3, XSteps: -400, YSteps: 200, CalculatedXMM: -1, CalculatedYMM: 0.5, Configuration: `{"microsteps":1}`, PositionBasis: "open_loop_steps_from_operator_confirmed_origin"}
			manifest := runstore.Manifest{RunID: "1", StartedAt: "2026-09-21T00:00:00Z", SourcePosition: position}
			wire := dt5215.StreamEvent{Descriptor: dt5215.Descriptor{Qualifier: dt5202.QualifierTest}}
			event := dt5202.Event{Kind: dt5202.EventTest, Test: &dt5202.TestEvent{Words: []uint32{1}}}
			var path string
			if converted {
				writer, err := runstore.Create(parent, manifest)
				if err != nil {
					t.Fatal(err)
				}
				if err := writer.AppendEvent(wire, event); err != nil {
					t.Fatal(err)
				}
				if err := writer.Finalize("2026-09-21T00:00:01Z", "source_scan_time"); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(parent, "converted.h5")
				if err := ConvertJSONRun(writer.Directory(), path); err != nil {
					t.Fatal(err)
				}
			} else {
				writer, err := CreateRun(parent, manifest)
				if err != nil {
					t.Fatal(err)
				}
				if err := writer.AppendEvent(wire, event); err != nil {
					t.Fatal(err)
				}
				if err := writer.Finalize("2026-09-21T00:00:01Z", "source_scan_time"); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(writer.Directory(), "run_1.0000.h5")
			}
			file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			dataset, err := file.OpenDataset("run/metadata_json")
			if err != nil {
				t.Fatal(err)
			}
			defer dataset.Close()
			space := dataset.Space()
			defer space.Close()
			dimensions, _, err := space.SimpleExtentDims()
			if err != nil {
				t.Fatal(err)
			}
			data := make([]byte, dimensions[0])
			if err := dataset.Read(&data); err != nil {
				t.Fatal(err)
			}
			var metadata struct {
				SourcePosition *runstore.SourcePosition `json:"source_position"`
			}
			if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata.SourcePosition == nil || *metadata.SourcePosition != *position {
				t.Fatalf("position metadata lost: %s", data)
			}
		})
	}
}
