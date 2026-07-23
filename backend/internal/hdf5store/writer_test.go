//go:build hdf5

package hdf5store

import (
	"path/filepath"
	"strings"
	"testing"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

func TestSpectroscopyWriterCreatesTypedAppendableDatasets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.h5")
	writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	relative, reference := uint32(99), uint32(428870)
	wire := dt5215.StreamEvent{Chain: 1, Descriptor: dt5215.Descriptor{
		Node: 2, Qualifier: 0x33, TriggerID: 7, Timestamp: 26865,
		PayloadOffsetWords: 4, PayloadSizeWords: 18, CRCError: true,
	}}
	event := dt5202.Event{Kind: dt5202.EventSpectroscopy, Spectroscopy: &dt5202.SpectroscopyEvent{
		TriggerID: 7, Timestamp: 26865, RelativeTimestampClock: &relative, ChannelMask: 5,
		Energies: []dt5202.Energy{{Channel: 0, LowGain: 263, HighGain: 2225, HasLowGain: true, HasHighGain: true, Discriminator: true}},
		Timings:  []dt5202.Timing{{Channel: 2, ToA: 861, ToT: 12}}, TimeReference: &reference,
	}}
	if err := writer.AppendEvent(wire, event); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	index, err := file.OpenDataset("events/index")
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	var indices [1]indexRow
	if err := index.Read(&indices); err != nil {
		t.Fatal(err)
	}
	if got := indices[0]; got.Sequence != 1 || got.Kind != KindSpectroscopy || got.Chain != 1 ||
		got.Node != 2 || got.Qualifier != 0x33 || got.TriggerID != 7 || got.Timestamp != 26865 ||
		got.PayloadOffsetWords != 4 || got.PayloadSizeWords != 18 || got.CRCError != 1 {
		t.Fatalf("index row = %+v", got)
	}
	parents, err := file.OpenDataset("events/spectroscopy/events")
	if err != nil {
		t.Fatal(err)
	}
	defer parents.Close()
	var parent [1]spectroscopyRow
	if err := parents.Read(&parent); err != nil {
		t.Fatal(err)
	}
	if got := parent[0]; got.Validity != 3 || got.RelativeTimestampClock != 99 ||
		got.EnergyOffset != 0 || got.EnergyCount != 1 || got.TimingOffset != 0 ||
		got.TimingCount != 1 || got.TimeReference != 428870 {
		t.Fatalf("spectroscopy row = %+v", got)
	}
}

func TestWriterRoundTripsEveryEventFamily(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.h5")
	writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	float := func(value float64) *float64 { return &value }
	status := uint16(0x4321)
	events := []struct {
		qualifier uint8
		triggerID uint64
		timestamp uint64
		event     dt5202.Event
	}{
		{0x02, 7, 291, dt5202.Event{Kind: dt5202.EventTiming, Timing: &dt5202.TimingEvent{TriggerID: 7, Timestamp: 291, TimeReference: 4666, Hits: []dt5202.Timing{{Channel: 3, ToA: 13398, ToT: 18}}}}},
		{0x84, 11, 12, dt5202.Event{Kind: dt5202.EventCounting, Counting: &dt5202.CountingEvent{TriggerID: 11, Timestamp: 12, ChannelMask: 4, Counts: []dt5202.Count{{Channel: 2, Value: 123}}, TORCount: 456, QORCount: 789}}},
		{0x08, 1, 2, dt5202.Event{Kind: dt5202.EventWaveform, Waveform: &dt5202.WaveformEvent{TriggerID: 1, Timestamp: 2, Samples: []dt5202.WaveformSample{{HighGain: 111, LowGain: 222, DigitalProbes: 10}}}}},
		{0x2f, 0, 55, dt5202.Event{Kind: dt5202.EventService, Service: &dt5202.ServiceEvent{
			Timestamp: 55, Version: 1, Format: 3, FPGATemperature: float(-21.1625),
			BoardTemperature: float(25), DetectorTemperature: float(25.6), HVTemperature: float(51.2),
			HVVoltage: float(45.4), HVCurrent: float(0.001), HVOn: true, HVOverCurrent: true,
			Status: &status, Counters: []dt5202.ServiceCounter{{Channel: 7, Value: 88}},
			TORCount: 99, QORCount: 111, UnknownPayload: []byte{0x78, 0x56, 0x34, 0x12},
		}}},
		{0xff, 8, 9, dt5202.Event{Kind: dt5202.EventTest, Test: &dt5202.TestEvent{TriggerID: 8, Timestamp: 9, Words: []uint32{0x11223344, 0xaabbccdd}}}},
	}
	for _, item := range events {
		wire := dt5215.StreamEvent{Chain: 1, Descriptor: dt5215.Descriptor{Node: 2, Qualifier: item.qualifier, TriggerID: item.triggerID, Timestamp: item.timestamp}}
		if err := writer.AppendEvent(wire, item.event); err != nil {
			t.Fatalf("append %s: %v", item.event.Kind, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	index, err := file.OpenDataset("events/index")
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	var indices [5]indexRow
	if err := index.Read(&indices); err != nil {
		t.Fatal(err)
	}
	for i, kind := range []uint8{KindTiming, KindCounting, KindWaveform, KindService, KindTest} {
		if indices[i].Sequence != uint64(i+1) || indices[i].Kind != kind || indices[i].KindRow != 0 {
			t.Fatalf("index[%d] = %+v", i, indices[i])
		}
	}
	service, err := file.OpenDataset("events/service/events")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	var serviceRows [1]serviceRow
	if err := service.Read(&serviceRows); err != nil {
		t.Fatal(err)
	}
	if got := serviceRows[0]; got.Validity != 0x7f || got.Status != 0x4321 ||
		got.CounterCount != 1 || got.UnknownCount != 4 || got.HVOn != 1 || got.HVOverCurrent != 1 {
		t.Fatalf("service row = %+v", got)
	}
	unknown, err := file.OpenDataset("events/service/unknown_payload")
	if err != nil {
		t.Fatal(err)
	}
	defer unknown.Close()
	var bytes [4]byte
	if err := unknown.Read(&bytes); err != nil {
		t.Fatal(err)
	}
	if bytes != [4]byte{0x78, 0x56, 0x34, 0x12} {
		t.Fatalf("unknown payload = %x", bytes)
	}
}

func TestWriterRejectsInvalidKindsAndMismatchedIdentity(t *testing.T) {
	writer, err := Create(filepath.Join(t.TempDir(), "events.h5"))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.AppendEvent(dt5215.StreamEvent{}, dt5202.Event{Kind: "future"}); err == nil ||
		!strings.Contains(err.Error(), "does not implement") {
		t.Fatalf("invalid-kind error = %v", err)
	}
	event := dt5202.Event{Kind: dt5202.EventSpectroscopy, Spectroscopy: &dt5202.SpectroscopyEvent{TriggerID: 2}}
	if err := writer.AppendEvent(dt5215.StreamEvent{}, event); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("identity error = %v", err)
	}
}

func TestWriterEmbedsMetadataAndMarksOnlyFinalizedFilesComplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.h5")
	writer, err := CreateWithMetadata(path, Metadata{
		RunID: "run-42", RequestedConfiguration: []byte("AcquisitionMode SPECT_TIMING\r\n"),
		AuditJSON: []byte(`{"schema_version":1}`), EffectiveJSON: []byte(`[{"board":0}]`),
		MetadataJSON: []byte(`{"started_at":"2026-07-23T10:00:00Z"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"schema_version":1,"run_id":"run-42","event_count":"0"}`)
	if err := writer.Finalize(manifest); err != nil {
		t.Fatal(err)
	}
	file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	root, err := file.OpenGroup("/")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	complete, err := root.OpenAttribute("complete")
	if err != nil {
		t.Fatal(err)
	}
	defer complete.Close()
	var marker uint8
	if err := complete.Read(&marker, hdf5.T_STD_U8LE); err != nil {
		t.Fatal(err)
	}
	if marker != 1 {
		t.Fatalf("complete = %d", marker)
	}
	requested, err := file.OpenDataset("configuration/requested_janus")
	if err != nil {
		t.Fatal(err)
	}
	defer requested.Close()
	gotRequested := make([]byte, len("AcquisitionMode SPECT_TIMING\r\n"))
	if err := requested.Read(&gotRequested); err != nil {
		t.Fatal(err)
	}
	if string(gotRequested) != "AcquisitionMode SPECT_TIMING\r\n" {
		t.Fatalf("requested configuration = %q", gotRequested)
	}
	storedManifest, err := file.OpenDataset("run/manifest_json")
	if err != nil {
		t.Fatal(err)
	}
	defer storedManifest.Close()
	gotManifest := make([]byte, len(manifest))
	if err := storedManifest.Read(&gotManifest); err != nil {
		t.Fatal(err)
	}
	if string(gotManifest) != string(manifest) {
		t.Fatalf("manifest = %s", gotManifest)
	}
}
