//go:build hdf5

// Package hdf5store implements the production decoded-event HDF5 adapter.
package hdf5store

import (
	"errors"
	"fmt"
	"unsafe"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

const (
	SchemaVersion          = 1
	KindSpectroscopy uint8 = 1
	KindTiming       uint8 = 2
	KindCounting     uint8 = 3
	KindWaveform     uint8 = 4
	KindService      uint8 = 5
	KindTest         uint8 = 6
)

type indexRow struct {
	Sequence           uint64
	Kind               uint8
	Chain              uint8
	Node               uint8
	Qualifier          uint8
	KindRow            uint64
	TriggerID          uint64
	Timestamp          uint64
	PayloadOffsetWords uint32
	PayloadSizeWords   uint32
	CRCError           uint8
}

type spectroscopyRow struct {
	TriggerID              uint64
	Timestamp              uint64
	Validity               uint8
	RelativeTimestampClock uint32
	ChannelMask            uint64
	EnergyOffset           uint64
	EnergyCount            uint32
	TimingOffset           uint64
	TimingCount            uint32
	TimeReference          uint32
}

type energyRow struct {
	ParentRow     uint64
	Channel       uint8
	LowGain       uint16
	HighGain      uint16
	HasLowGain    uint8
	HasHighGain   uint8
	Discriminator uint8
}

type timingRow struct {
	ParentRow uint64
	Channel   uint8
	ToA       uint32
	ToT       uint16
}

type table struct {
	dataset *hdf5.Dataset
	length  uint64
}

// Writer appends spectroscopy events in the schema commit order: children,
// kind-specific parent, then run-wide index.
type Writer struct {
	file         *hdf5.File
	complete     *hdf5.Attribute
	index        table
	spectroscopy table
	energies     table
	timings      table
	timingEvents table
	timingHits   table
	counting     table
	counts       table
	waveform     table
	samples      table
	service      table
	counters     table
	unknown      table
	test         table
	words        table
	manifestJSON table
	sequenceBase uint64
	closed       bool
}

func Create(path string) (_ *Writer, err error) {
	return CreateWithMetadata(path, Metadata{})
}

type Metadata struct {
	RunID                  string
	RequestedConfiguration []byte
	AuditJSON              []byte
	EffectiveJSON          []byte
	MetadataJSON           []byte
	EffectiveConfiguration []dt5202.ConfigurationPlan
	Boards                 []runstore.BoardIdentity
	SegmentIndex           uint32
	EventSequenceBase      uint64
}

func CreateWithMetadata(path string, metadata Metadata) (_ *Writer, err error) {
	compression, err := compressionName()
	if err != nil {
		return nil, err
	}
	file, err := hdf5.CreateFile(path, hdf5.F_ACC_EXCL)
	if err != nil {
		return nil, fmt.Errorf("create HDF5 file: %w", err)
	}
	w := &Writer{file: file}
	defer func() {
		if err != nil {
			_ = w.Close()
		}
	}()
	root, err := file.OpenGroup("/")
	if err != nil {
		return nil, fmt.Errorf("open root group: %w", err)
	}
	defer root.Close()
	if err := writeUintAttribute(root, "schema_version", SchemaVersion); err != nil {
		return nil, err
	}
	if w.complete, err = createUint8Attribute(root, "complete", 0); err != nil {
		return nil, err
	}
	if err := writeUint32Attribute(root, "segment_index", metadata.SegmentIndex); err != nil {
		return nil, err
	}
	if err := writeUint64Attribute(root, "first_event_sequence", metadata.EventSequenceBase+1); err != nil {
		return nil, err
	}
	w.sequenceBase = metadata.EventSequenceBase

	events, err := file.CreateGroup("events")
	if err != nil {
		return nil, fmt.Errorf("create events group: %w", err)
	}
	defer events.Close()
	spectroscopy, err := events.CreateGroup("spectroscopy")
	if err != nil {
		return nil, fmt.Errorf("create spectroscopy group: %w", err)
	}
	defer spectroscopy.Close()
	timing, err := events.CreateGroup("timing")
	if err != nil {
		return nil, fmt.Errorf("create timing group: %w", err)
	}
	defer timing.Close()
	counting, err := events.CreateGroup("counting")
	if err != nil {
		return nil, fmt.Errorf("create counting group: %w", err)
	}
	defer counting.Close()
	waveform, err := events.CreateGroup("waveform")
	if err != nil {
		return nil, fmt.Errorf("create waveform group: %w", err)
	}
	defer waveform.Close()
	service, err := events.CreateGroup("service")
	if err != nil {
		return nil, fmt.Errorf("create service group: %w", err)
	}
	defer service.Close()
	test, err := events.CreateGroup("test")
	if err != nil {
		return nil, fmt.Errorf("create test group: %w", err)
	}
	defer test.Close()
	configuration, err := file.CreateGroup("configuration")
	if err != nil {
		return nil, fmt.Errorf("create configuration group: %w", err)
	}
	defer configuration.Close()
	run, err := file.CreateGroup("run")
	if err != nil {
		return nil, fmt.Errorf("create run group: %w", err)
	}
	defer run.Close()

	if w.index.dataset, err = createTable(events, "index", compoundIndex()); err != nil {
		return nil, err
	}
	if w.spectroscopy.dataset, err = createTable(spectroscopy, "events", compoundSpectroscopy()); err != nil {
		return nil, err
	}
	if w.energies.dataset, err = createTable(spectroscopy, "energies", compoundEnergy()); err != nil {
		return nil, err
	}
	if w.timings.dataset, err = createTable(spectroscopy, "timings", compoundTiming()); err != nil {
		return nil, err
	}
	if w.timingEvents.dataset, err = createTable(timing, "events", compoundTimingEvent()); err != nil {
		return nil, err
	}
	if w.timingHits.dataset, err = createTable(timing, "hits", compoundTiming()); err != nil {
		return nil, err
	}
	if w.counting.dataset, err = createTable(counting, "events", compoundCounting()); err != nil {
		return nil, err
	}
	if w.counts.dataset, err = createTable(counting, "counts", compoundCount()); err != nil {
		return nil, err
	}
	if w.waveform.dataset, err = createTable(waveform, "events", compoundWaveform()); err != nil {
		return nil, err
	}
	if w.samples.dataset, err = createTable(waveform, "samples", compoundSample()); err != nil {
		return nil, err
	}
	if w.service.dataset, err = createTable(service, "events", compoundService()); err != nil {
		return nil, err
	}
	if w.counters.dataset, err = createTable(service, "counters", compoundCounter()); err != nil {
		return nil, err
	}
	if w.unknown.dataset, err = createPrimitive(service, "unknown_payload", hdf5.T_STD_U8LE); err != nil {
		return nil, err
	}
	if w.test.dataset, err = createTable(test, "events", compoundTest()); err != nil {
		return nil, err
	}
	if w.words.dataset, err = createPrimitive(test, "words", hdf5.T_STD_U32LE); err != nil {
		return nil, err
	}
	if err := createBytes(configuration, "requested_janus", metadata.RequestedConfiguration); err != nil {
		return nil, err
	}
	if err := createBytes(configuration, "audit_json", metadata.AuditJSON); err != nil {
		return nil, err
	}
	if err := createBytes(configuration, "effective_json", metadata.EffectiveJSON); err != nil {
		return nil, err
	}
	if err := writeEffectiveConfiguration(configuration, metadata); err != nil {
		return nil, err
	}
	if err := createBytes(run, "format", []byte("pet-caen-daq-hdf5")); err != nil {
		return nil, err
	}
	if err := createBytes(run, "run_id", []byte(metadata.RunID)); err != nil {
		return nil, err
	}
	if err := createBytes(run, "metadata_json", metadata.MetadataJSON); err != nil {
		return nil, err
	}
	if err := createBytes(run, "compression", []byte(compression)); err != nil {
		return nil, err
	}
	if w.manifestJSON.dataset, err = createPrimitive(run, "manifest_json", hdf5.T_STD_U8LE); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) AppendEvent(wire dt5215.StreamEvent, event dt5202.Event) error {
	if w.closed {
		return errors.New("HDF5 writer is closed")
	}
	switch event.Kind {
	case dt5202.EventSpectroscopy:
		return w.appendSpectroscopy(wire, event)
	case dt5202.EventTiming:
		return w.appendTiming(wire, event)
	case dt5202.EventCounting:
		return w.appendCounting(wire, event)
	case dt5202.EventWaveform:
		return w.appendWaveform(wire, event)
	case dt5202.EventService:
		return w.appendService(wire, event)
	case dt5202.EventTest:
		return w.appendTest(wire, event)
	default:
		return fmt.Errorf("HDF5 schema v%d does not implement event kind %q", SchemaVersion, event.Kind)
	}
}

func (w *Writer) appendSpectroscopy(wire dt5215.StreamEvent, event dt5202.Event) error {
	if event.Spectroscopy == nil {
		return errors.New("spectroscopy event payload is missing")
	}
	value := event.Spectroscopy
	if value.TriggerID != wire.Descriptor.TriggerID || value.Timestamp != wire.Descriptor.Timestamp {
		return errors.New("typed spectroscopy identity does not match DT5215 descriptor")
	}
	parent := w.spectroscopy.length
	energyCount, err := uint32Count("spectroscopy energies", len(value.Energies))
	if err != nil {
		return err
	}
	timingCount, err := uint32Count("spectroscopy timings", len(value.Timings))
	if err != nil {
		return err
	}
	energies := make([]energyRow, len(value.Energies))
	for i, item := range value.Energies {
		energies[i] = energyRow{
			ParentRow: parent, Channel: item.Channel, LowGain: item.LowGain, HighGain: item.HighGain,
			HasLowGain: boolean(item.HasLowGain), HasHighGain: boolean(item.HasHighGain),
			Discriminator: boolean(item.Discriminator),
		}
	}
	timings := make([]timingRow, len(value.Timings))
	for i, item := range value.Timings {
		timings[i] = timingRow{ParentRow: parent, Channel: item.Channel, ToA: item.ToA, ToT: item.ToT}
	}
	if err := appendRows(&w.energies, energies); err != nil {
		return fmt.Errorf("append spectroscopy energies: %w", err)
	}
	if err := appendRows(&w.timings, timings); err != nil {
		return fmt.Errorf("append spectroscopy timings: %w", err)
	}
	row := spectroscopyRow{
		TriggerID: value.TriggerID, Timestamp: value.Timestamp, ChannelMask: value.ChannelMask,
		EnergyOffset: w.energies.length - uint64(len(energies)), EnergyCount: energyCount,
		TimingOffset: w.timings.length - uint64(len(timings)), TimingCount: timingCount,
	}
	if value.RelativeTimestampClock != nil {
		row.Validity |= 1
		row.RelativeTimestampClock = *value.RelativeTimestampClock
	}
	if value.TimeReference != nil {
		row.Validity |= 2
		row.TimeReference = *value.TimeReference
	}
	if err := appendRows(&w.spectroscopy, []spectroscopyRow{row}); err != nil {
		return fmt.Errorf("append spectroscopy event: %w", err)
	}
	return w.appendIndex(wire, KindSpectroscopy, parent)
}

func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return errors.Join(
		closeDataset(w.manifestJSON.dataset),
		closeDataset(w.words.dataset),
		closeDataset(w.test.dataset),
		closeDataset(w.unknown.dataset),
		closeDataset(w.counters.dataset),
		closeDataset(w.service.dataset),
		closeDataset(w.samples.dataset),
		closeDataset(w.waveform.dataset),
		closeDataset(w.counts.dataset),
		closeDataset(w.counting.dataset),
		closeDataset(w.timingHits.dataset),
		closeDataset(w.timingEvents.dataset),
		closeDataset(w.timings.dataset),
		closeDataset(w.energies.dataset),
		closeDataset(w.spectroscopy.dataset),
		closeDataset(w.index.dataset),
		closeAttribute(w.complete),
		w.file.Close(),
	)
}

func (w *Writer) Finalize(manifestJSON []byte) (err error) {
	if w.closed {
		return errors.New("HDF5 writer is closed")
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, w.Close())
		}
	}()
	if err = appendRows(&w.manifestJSON, manifestJSON); err != nil {
		return fmt.Errorf("write finalized manifest snapshot: %w", err)
	}
	if err = w.file.Flush(hdf5.F_SCOPE_GLOBAL); err != nil {
		return fmt.Errorf("flush HDF5 file: %w", err)
	}
	complete := uint8(1)
	if err = w.complete.Write(&complete, hdf5.T_STD_U8LE); err != nil {
		return fmt.Errorf("mark HDF5 file complete: %w", err)
	}
	if err = w.file.Flush(hdf5.F_SCOPE_GLOBAL); err != nil {
		return fmt.Errorf("flush completed HDF5 file: %w", err)
	}
	return w.Close()
}

func createPrimitive(location interface {
	CreateDatasetWith(string, *hdf5.Datatype, *hdf5.Dataspace, *hdf5.PropList) (*hdf5.Dataset, error)
}, name string, datatype *hdf5.Datatype) (*hdf5.Dataset, error) {
	space, err := hdf5.CreateSimpleDataspace([]uint{0}, []uint{^uint(0)})
	if err != nil {
		return nil, err
	}
	defer space.Close()
	properties, err := hdf5.NewPropList(hdf5.P_DATASET_CREATE)
	if err != nil {
		return nil, err
	}
	defer properties.Close()
	if err := properties.SetChunk([]uint{16384}); err != nil {
		return nil, err
	}
	if err := configureCompression(properties); err != nil {
		return nil, err
	}
	dataset, err := location.CreateDatasetWith(name, datatype, space, properties)
	if err != nil {
		return nil, fmt.Errorf("create dataset %s: %w", name, err)
	}
	return dataset, nil
}

func createBytes(location interface {
	CreateDataset(string, *hdf5.Datatype, *hdf5.Dataspace) (*hdf5.Dataset, error)
}, name string, value []byte) error {
	space, err := hdf5.CreateSimpleDataspace([]uint{uint(len(value))}, nil)
	if err != nil {
		return err
	}
	defer space.Close()
	dataset, err := location.CreateDataset(name, hdf5.T_STD_U8LE, space)
	if err != nil {
		return fmt.Errorf("create dataset %s: %w", name, err)
	}
	defer dataset.Close()
	if len(value) > 0 {
		if err := dataset.Write(&value); err != nil {
			return fmt.Errorf("write dataset %s: %w", name, err)
		}
	}
	return nil
}

func writeUintAttribute(group *hdf5.Group, name string, value int) error {
	attribute, err := createUintAttribute(group, name, hdf5.T_STD_U32LE)
	if err != nil {
		return err
	}
	defer attribute.Close()
	numeric := uint32(value)
	return attribute.Write(&numeric, hdf5.T_STD_U32LE)
}

func writeUint32Attribute(group *hdf5.Group, name string, value uint32) error {
	attribute, err := createUintAttribute(group, name, hdf5.T_STD_U32LE)
	if err != nil {
		return err
	}
	defer attribute.Close()
	return attribute.Write(&value, hdf5.T_STD_U32LE)
}

func writeUint64Attribute(group *hdf5.Group, name string, value uint64) error {
	attribute, err := createUintAttribute(group, name, hdf5.T_STD_U64LE)
	if err != nil {
		return err
	}
	defer attribute.Close()
	return attribute.Write(&value, hdf5.T_STD_U64LE)
}

func createUint8Attribute(group *hdf5.Group, name string, value uint8) (*hdf5.Attribute, error) {
	attribute, err := createUintAttribute(group, name, hdf5.T_STD_U8LE)
	if err != nil {
		return nil, err
	}
	if err := attribute.Write(&value, hdf5.T_STD_U8LE); err != nil {
		attribute.Close()
		return nil, err
	}
	return attribute, nil
}

func createUintAttribute(group *hdf5.Group, name string, datatype *hdf5.Datatype) (*hdf5.Attribute, error) {
	space, err := hdf5.CreateDataspace(hdf5.S_SCALAR)
	if err != nil {
		return nil, err
	}
	defer space.Close()
	attribute, err := group.CreateAttribute(name, datatype, space)
	if err != nil {
		return nil, fmt.Errorf("create attribute %s: %w", name, err)
	}
	return attribute, nil
}

func createTable(location interface {
	CreateDatasetWith(string, *hdf5.Datatype, *hdf5.Dataspace, *hdf5.PropList) (*hdf5.Dataset, error)
}, name string, datatype *hdf5.CompoundType) (*hdf5.Dataset, error) {
	defer datatype.Close()
	space, err := hdf5.CreateSimpleDataspace([]uint{0}, []uint{^uint(0)})
	if err != nil {
		return nil, err
	}
	defer space.Close()
	properties, err := hdf5.NewPropList(hdf5.P_DATASET_CREATE)
	if err != nil {
		return nil, err
	}
	defer properties.Close()
	if err := properties.SetChunk([]uint{16384}); err != nil {
		return nil, err
	}
	if err := configureCompression(properties); err != nil {
		return nil, err
	}
	dataset, err := location.CreateDatasetWith(name, &datatype.Datatype, space, properties)
	if err != nil {
		return nil, fmt.Errorf("create dataset %s: %w", name, err)
	}
	return dataset, nil
}

func appendRows[T any](target *table, rows []T) error {
	if len(rows) == 0 {
		return nil
	}
	if uint64(len(rows)) > ^uint64(0)-target.length {
		return errors.New("dataset extent overflows uint64")
	}
	old := target.length
	target.length += uint64(len(rows))
	if uint64(uint(target.length)) != target.length || uint64(uint(old)) != old {
		target.length = old
		return errors.New("dataset extent exceeds platform address space")
	}
	if err := setExtent(target.dataset, target.length); err != nil {
		target.length = old
		return err
	}
	fileSpace := target.dataset.Space()
	if fileSpace == nil {
		return errors.New("get extended dataset space")
	}
	defer fileSpace.Close()
	if err := fileSpace.SelectHyperslab([]uint{uint(old)}, nil, []uint{uint(len(rows))}, nil); err != nil {
		return err
	}
	memorySpace, err := hdf5.CreateSimpleDataspace([]uint{uint(len(rows))}, nil)
	if err != nil {
		return err
	}
	defer memorySpace.Close()
	return target.dataset.WriteSubset(&rows, memorySpace, fileSpace)
}

func closeDataset(dataset *hdf5.Dataset) error {
	if dataset == nil {
		return nil
	}
	return dataset.Close()
}

func closeAttribute(attribute *hdf5.Attribute) error {
	if attribute == nil {
		return nil
	}
	return attribute.Close()
}

func boolean(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

func compoundIndex() *hdf5.CompoundType {
	var value indexRow
	return mustCompound(unsafe.Sizeof(value), []field{
		{"sequence", unsafe.Offsetof(value.Sequence), hdf5.T_STD_U64LE},
		{"kind", unsafe.Offsetof(value.Kind), hdf5.T_STD_U8LE},
		{"chain", unsafe.Offsetof(value.Chain), hdf5.T_STD_U8LE},
		{"node", unsafe.Offsetof(value.Node), hdf5.T_STD_U8LE},
		{"qualifier", unsafe.Offsetof(value.Qualifier), hdf5.T_STD_U8LE},
		{"kind_row", unsafe.Offsetof(value.KindRow), hdf5.T_STD_U64LE},
		{"trigger_id", unsafe.Offsetof(value.TriggerID), hdf5.T_STD_U64LE},
		{"timestamp", unsafe.Offsetof(value.Timestamp), hdf5.T_STD_U64LE},
		{"payload_offset_words", unsafe.Offsetof(value.PayloadOffsetWords), hdf5.T_STD_U32LE},
		{"payload_size_words", unsafe.Offsetof(value.PayloadSizeWords), hdf5.T_STD_U32LE},
		{"crc_error", unsafe.Offsetof(value.CRCError), hdf5.T_STD_U8LE},
	})
}

func compoundSpectroscopy() *hdf5.CompoundType {
	var value spectroscopyRow
	return mustCompound(unsafe.Sizeof(value), []field{
		{"trigger_id", unsafe.Offsetof(value.TriggerID), hdf5.T_STD_U64LE},
		{"timestamp", unsafe.Offsetof(value.Timestamp), hdf5.T_STD_U64LE},
		{"validity", unsafe.Offsetof(value.Validity), hdf5.T_STD_U8LE},
		{"relative_timestamp_clock", unsafe.Offsetof(value.RelativeTimestampClock), hdf5.T_STD_U32LE},
		{"channel_mask", unsafe.Offsetof(value.ChannelMask), hdf5.T_STD_U64LE},
		{"energy_offset", unsafe.Offsetof(value.EnergyOffset), hdf5.T_STD_U64LE},
		{"energy_count", unsafe.Offsetof(value.EnergyCount), hdf5.T_STD_U32LE},
		{"timing_offset", unsafe.Offsetof(value.TimingOffset), hdf5.T_STD_U64LE},
		{"timing_count", unsafe.Offsetof(value.TimingCount), hdf5.T_STD_U32LE},
		{"time_reference", unsafe.Offsetof(value.TimeReference), hdf5.T_STD_U32LE},
	})
}

func compoundEnergy() *hdf5.CompoundType {
	var value energyRow
	return mustCompound(unsafe.Sizeof(value), []field{
		{"parent_row", unsafe.Offsetof(value.ParentRow), hdf5.T_STD_U64LE},
		{"channel", unsafe.Offsetof(value.Channel), hdf5.T_STD_U8LE},
		{"low_gain", unsafe.Offsetof(value.LowGain), hdf5.T_STD_U16LE},
		{"high_gain", unsafe.Offsetof(value.HighGain), hdf5.T_STD_U16LE},
		{"has_low_gain", unsafe.Offsetof(value.HasLowGain), hdf5.T_STD_U8LE},
		{"has_high_gain", unsafe.Offsetof(value.HasHighGain), hdf5.T_STD_U8LE},
		{"discriminator", unsafe.Offsetof(value.Discriminator), hdf5.T_STD_U8LE},
	})
}

func compoundTiming() *hdf5.CompoundType {
	var value timingRow
	return mustCompound(unsafe.Sizeof(value), []field{
		{"parent_row", unsafe.Offsetof(value.ParentRow), hdf5.T_STD_U64LE},
		{"channel", unsafe.Offsetof(value.Channel), hdf5.T_STD_U8LE},
		{"toa", unsafe.Offsetof(value.ToA), hdf5.T_STD_U32LE},
		{"tot", unsafe.Offsetof(value.ToT), hdf5.T_STD_U16LE},
	})
}

type field struct {
	name   string
	offset uintptr
	kind   *hdf5.Datatype
}

func mustCompound(size uintptr, fields []field) *hdf5.CompoundType {
	value, err := hdf5.NewCompoundType(int(size))
	if err != nil {
		panic(err)
	}
	for _, item := range fields {
		if err := value.Insert(item.name, int(item.offset), item.kind); err != nil {
			panic(err)
		}
	}
	return value
}
