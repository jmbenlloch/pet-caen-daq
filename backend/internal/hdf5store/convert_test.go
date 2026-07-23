//go:build hdf5

package hdf5store

import (
	"path/filepath"
	"testing"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestConvertJSONRunStreamsTypedEventsToHDF5(t *testing.T) {
	parent := t.TempDir()
	source, err := runstore.Create(parent, runstore.Manifest{
		RunID: "conversion", StartedAt: "2026-07-23T10:00:00Z",
		RequestedConfiguration: "AcquisitionMode TEST\r\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	wire := dt5215.StreamEvent{Chain: 2, Descriptor: dt5215.Descriptor{
		Node: 3, Qualifier: dt5202.QualifierTest, TriggerID: 8, Timestamp: 9,
	}}
	event := dt5202.Event{Kind: dt5202.EventTest, Qualifier: dt5202.QualifierTest, Test: &dt5202.TestEvent{
		TriggerID: 8, Timestamp: 9, Words: []uint32{0x11223344, 0xaabbccdd},
	}}
	if err := source.AppendEvent(wire, event); err != nil {
		t.Fatal(err)
	}
	if err := source.Finalize("2026-07-23T10:01:00Z", "operator_stop"); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(parent, "converted.h5")
	if err := ConvertJSONRun(source.Directory(), output); err != nil {
		t.Fatal(err)
	}
	file, err := hdf5.OpenFile(output, hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	index, err := file.OpenDataset("events/index")
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	var rows [1]indexRow
	if err := index.Read(&rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].Kind != KindTest || rows[0].Chain != 2 || rows[0].Node != 3 ||
		rows[0].TriggerID != 8 || rows[0].Timestamp != 9 {
		t.Fatalf("converted index = %+v", rows[0])
	}
}

func TestConvertJSONRunWithBloscLZ4Level4BitShuffle(t *testing.T) {
	t.Setenv(CompressionEnvironment, CompressionBloscLZ4)
	parent := t.TempDir()
	source, err := runstore.Create(parent, runstore.Manifest{RunID: "compressed", StartedAt: "now"})
	if err != nil {
		t.Fatal(err)
	}
	event := dt5202.Event{Kind: dt5202.EventTest, Qualifier: dt5202.QualifierTest, Test: &dt5202.TestEvent{
		TriggerID: 1, Timestamp: 2, Words: []uint32{1, 2, 3, 4},
	}}
	wire := dt5215.StreamEvent{Descriptor: dt5215.Descriptor{
		Qualifier: dt5202.QualifierTest, TriggerID: 1, Timestamp: 2,
	}}
	for range 100 {
		if err := source.AppendEvent(wire, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.Finalize("later", "test"); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(parent, "compressed.h5")
	if err := ConvertJSONRun(source.Directory(), output); err != nil {
		t.Fatal(err)
	}
	if err := Validate(output, true); err != nil {
		t.Fatal(err)
	}
}
