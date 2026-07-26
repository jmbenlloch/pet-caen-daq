//go:build hdf5

// hdf5-layout-benchmark rewrites a spectroscopy capture using either the
// production split layout or a proposed self-contained observation table.
//
// It intentionally duplicates the production HDF5 table/chunk/compression
// setup so the benchmark exercises the same Go binding and Blosc library.
package main

/*
#include <hdf5.h>

static unsigned long long dataset_storage_size(long long id) {
	return (unsigned long long)H5Dget_storage_size((hid_t)id);
}
*/
import "C"

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"

	hdf5 "github.com/next-exp/hdf5-go"
)

const (
	kindSpectroscopy = uint8(1)
	chunkRows        = 16384
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

// observationRow is ordered from widest to narrowest fields. Its natural Go
// layout is 64 bytes, with no internal padding and three trailing pad bytes.
type observationRow struct {
	Sequence               uint64
	ParentRow              uint64
	TriggerID              uint64
	Timestamp              uint64
	RelativeTimestampClock uint32
	TimeReference          uint32
	ToA                    uint32
	LowGain                uint16
	HighGain               uint16
	ToT                    uint16
	Chain                  uint8
	Node                   uint8
	Qualifier              uint8
	Validity               uint8
	Channel                uint8
	ChannelValid           uint8
	HasEnergy              uint8
	HasLowGain             uint8
	HasHighGain            uint8
	Discriminator          uint8
	HasTiming              uint8
}

type sourceRow struct {
	Sequence  uint64
	Chain     uint8
	Node      uint8
	Qualifier uint8
}

type timingKey struct {
	ParentRow uint64
	Channel   uint8
}

type table struct {
	dataset *hdf5.Dataset
	length  uint64
}

type field struct {
	name   string
	offset uintptr
	kind   *hdf5.Datatype
}

type result struct {
	Mode                  string  `json:"mode"`
	Input                 string  `json:"input"`
	Output                string  `json:"output"`
	BloscVersion          string  `json:"blosc_version"`
	BloscDate             string  `json:"blosc_date"`
	Compression           string  `json:"compression"`
	BatchRows             int     `json:"batch_rows"`
	ParentRows            uint64  `json:"parent_rows"`
	EnergyRows            uint64  `json:"energy_rows"`
	TimingRows            uint64  `json:"timing_rows"`
	ObservationRows       uint64  `json:"observation_rows,omitempty"`
	TimingOnlyRows        uint64  `json:"timing_only_rows,omitempty"`
	LogicalBytes          uint64  `json:"logical_bytes"`
	AllocatedDatasetBytes uint64  `json:"allocated_dataset_bytes"`
	FileBytes             int64   `json:"file_bytes"`
	WallSeconds           float64 `json:"wall_seconds"`
	UserCPUSeconds        float64 `json:"user_cpu_seconds"`
	SystemCPUSeconds      float64 `json:"system_cpu_seconds"`
	MaxRSSKiB             int64   `json:"max_rss_kib"`
}

func main() {
	var input, output, mode string
	var batchRows int
	flag.StringVar(&input, "input", "", "source decoded-event HDF5 file")
	flag.StringVar(&output, "output", "", "benchmark output HDF5 file")
	flag.StringVar(&mode, "mode", "", "layout to write: split or flat")
	flag.IntVar(&batchRows, "batch-rows", 1_000_000, "maximum rows per read/write batch")
	flag.Parse()

	if input == "" || output == "" || (mode != "split" && mode != "flat") || batchRows <= 0 {
		flag.Usage()
		os.Exit(2)
	}
	if unsafe.Sizeof(observationRow{}) != 64 {
		fatalf("observation row is %d bytes, want 64", unsafe.Sizeof(observationRow{}))
	}
	if err := os.Remove(output); err != nil && !errors.Is(err, os.ErrNotExist) {
		fatalf("remove old output: %v", err)
	}

	bloscVersion, bloscDate, err := hdf5.RegisterBlosc()
	if err != nil {
		fatalf("register Blosc: %v", err)
	}
	before := usage()
	started := time.Now()

	var metrics result
	switch mode {
	case "split":
		metrics, err = rewriteSplit(input, output, batchRows)
	case "flat":
		metrics, err = rewriteFlat(input, output, batchRows)
	}
	if err != nil {
		fatalf("%s rewrite: %v", mode, err)
	}
	after := usage()
	info, err := os.Stat(output)
	if err != nil {
		fatalf("stat output: %v", err)
	}
	metrics.Mode = mode
	metrics.Input = input
	metrics.Output = output
	metrics.BloscVersion = bloscVersion
	metrics.BloscDate = bloscDate
	metrics.Compression = "blosc-lz4-level4-bitshuffle"
	metrics.BatchRows = batchRows
	metrics.FileBytes = info.Size()
	metrics.WallSeconds = time.Since(started).Seconds()
	metrics.UserCPUSeconds = after.user - before.user
	metrics.SystemCPUSeconds = after.system - before.system
	metrics.MaxRSSKiB = after.maxRSS

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metrics); err != nil {
		fatalf("encode result: %v", err)
	}
}

func rewriteSplit(input, output string, batchRows int) (result, error) {
	source, err := hdf5.OpenFile(input, hdf5.F_ACC_RDONLY)
	if err != nil {
		return result{}, err
	}
	defer source.Close()
	parentSource, err := source.OpenDataset("events/spectroscopy/events")
	if err != nil {
		return result{}, err
	}
	defer parentSource.Close()
	energySource, err := source.OpenDataset("events/spectroscopy/energies")
	if err != nil {
		return result{}, err
	}
	defer energySource.Close()
	timingSource, err := source.OpenDataset("events/spectroscopy/timings")
	if err != nil {
		return result{}, err
	}
	defer timingSource.Close()

	parentCount, err := datasetLength(parentSource)
	if err != nil {
		return result{}, err
	}
	energyCount, err := datasetLength(energySource)
	if err != nil {
		return result{}, err
	}
	timingCount, err := datasetLength(timingSource)
	if err != nil {
		return result{}, err
	}

	destination, err := hdf5.CreateFile(output, hdf5.F_ACC_EXCL)
	if err != nil {
		return result{}, err
	}
	events, err := destination.CreateGroup("events")
	if err != nil {
		destination.Close()
		return result{}, err
	}
	spectroscopy, err := events.CreateGroup("spectroscopy")
	if err != nil {
		events.Close()
		destination.Close()
		return result{}, err
	}
	parents, err := createTable(spectroscopy, "events", compoundSpectroscopy())
	if err != nil {
		return result{}, err
	}
	energies, err := createTable(spectroscopy, "energies", compoundEnergy())
	if err != nil {
		return result{}, err
	}
	timings, err := createTable(spectroscopy, "timings", compoundTiming())
	if err != nil {
		return result{}, err
	}

	if err := copyRows[spectroscopyRow](parentSource, &parents, parentCount, batchRows); err != nil {
		return result{}, err
	}
	if err := copyRows[energyRow](energySource, &energies, energyCount, batchRows); err != nil {
		return result{}, err
	}
	if err := copyRows[timingRow](timingSource, &timings, timingCount, batchRows); err != nil {
		return result{}, err
	}
	if err := destination.Flush(hdf5.F_SCOPE_GLOBAL); err != nil {
		return result{}, err
	}
	allocated := storageSize(parents.dataset) + storageSize(energies.dataset) + storageSize(timings.dataset)
	closeErr := errors.Join(
		timings.dataset.Close(), energies.dataset.Close(), parents.dataset.Close(),
		spectroscopy.Close(), events.Close(), destination.Close(),
	)
	if closeErr != nil {
		return result{}, closeErr
	}
	return result{
		ParentRows: uint64(parentCount), EnergyRows: uint64(energyCount), TimingRows: uint64(timingCount),
		LogicalBytes: uint64(parentCount)*uint64(unsafe.Sizeof(spectroscopyRow{})) +
			uint64(energyCount)*uint64(unsafe.Sizeof(energyRow{})) +
			uint64(timingCount)*uint64(unsafe.Sizeof(timingRow{})),
		AllocatedDatasetBytes: allocated,
	}, nil
}

func rewriteFlat(input, output string, batchRows int) (result, error) {
	source, err := hdf5.OpenFile(input, hdf5.F_ACC_RDONLY)
	if err != nil {
		return result{}, err
	}
	defer source.Close()
	indexSource, err := source.OpenDataset("events/index")
	if err != nil {
		return result{}, err
	}
	defer indexSource.Close()
	parentSource, err := source.OpenDataset("events/spectroscopy/events")
	if err != nil {
		return result{}, err
	}
	defer parentSource.Close()
	energySource, err := source.OpenDataset("events/spectroscopy/energies")
	if err != nil {
		return result{}, err
	}
	defer energySource.Close()
	timingSource, err := source.OpenDataset("events/spectroscopy/timings")
	if err != nil {
		return result{}, err
	}
	defer timingSource.Close()

	parents, err := readAll[spectroscopyRow](parentSource)
	if err != nil {
		return result{}, err
	}
	timings, err := readAll[timingRow](timingSource)
	if err != nil {
		return result{}, err
	}
	indexes, err := readAll[indexRow](indexSource)
	if err != nil {
		return result{}, err
	}
	energyCount, err := datasetLength(energySource)
	if err != nil {
		return result{}, err
	}
	sources := make([]sourceRow, len(parents))
	for _, item := range indexes {
		if item.Kind != kindSpectroscopy {
			continue
		}
		if item.KindRow >= uint64(len(sources)) {
			return result{}, fmt.Errorf("index kind row %d out of bounds", item.KindRow)
		}
		sources[item.KindRow] = sourceRow{
			Sequence: item.Sequence, Chain: item.Chain, Node: item.Node, Qualifier: item.Qualifier,
		}
	}
	timingByKey := make(map[timingKey]timingRow, len(timings))
	for _, item := range timings {
		key := timingKey{ParentRow: item.ParentRow, Channel: item.Channel}
		if _, duplicate := timingByKey[key]; duplicate {
			return result{}, fmt.Errorf("duplicate spectroscopy timing parent=%d channel=%d", item.ParentRow, item.Channel)
		}
		timingByKey[key] = item
	}
	seenTiming := make(map[timingKey]struct{}, len(timings))

	destination, err := hdf5.CreateFile(output, hdf5.F_ACC_EXCL)
	if err != nil {
		return result{}, err
	}
	events, err := destination.CreateGroup("events")
	if err != nil {
		return result{}, err
	}
	spectroscopy, err := events.CreateGroup("spectroscopy")
	if err != nil {
		return result{}, err
	}
	observations, err := createTable(spectroscopy, "observations", compoundObservation())
	if err != nil {
		return result{}, err
	}

	for begin := 0; begin < energyCount; begin += batchRows {
		end := min(begin+batchRows, energyCount)
		energies, err := readSubset[energyRow](energySource, begin, end)
		if err != nil {
			return result{}, err
		}
		rows := make([]observationRow, len(energies))
		for index, energy := range energies {
			if energy.ParentRow >= uint64(len(parents)) {
				return result{}, fmt.Errorf("energy parent %d out of bounds", energy.ParentRow)
			}
			parent := parents[energy.ParentRow]
			sourceRow := sources[energy.ParentRow]
			row := observationRow{
				Sequence: sourceRow.Sequence, ParentRow: energy.ParentRow,
				TriggerID: parent.TriggerID, Timestamp: parent.Timestamp,
				RelativeTimestampClock: parent.RelativeTimestampClock, TimeReference: parent.TimeReference,
				Chain: sourceRow.Chain, Node: sourceRow.Node, Qualifier: sourceRow.Qualifier,
				Validity: parent.Validity, Channel: energy.Channel, ChannelValid: 1, HasEnergy: 1,
				LowGain: energy.LowGain, HighGain: energy.HighGain,
				HasLowGain: energy.HasLowGain, HasHighGain: energy.HasHighGain,
				Discriminator: energy.Discriminator,
			}
			key := timingKey{ParentRow: energy.ParentRow, Channel: energy.Channel}
			if timing, ok := timingByKey[key]; ok {
				row.HasTiming = 1
				row.ToA = timing.ToA
				row.ToT = timing.ToT
				seenTiming[key] = struct{}{}
			}
			rows[index] = row
		}
		if err := appendRows(&observations, rows); err != nil {
			return result{}, err
		}
	}
	timingOnlyRows := uint64(0)
	buffer := make([]observationRow, 0, batchRows)
	for _, timing := range timings {
		key := timingKey{ParentRow: timing.ParentRow, Channel: timing.Channel}
		if _, seen := seenTiming[key]; seen {
			continue
		}
		if timing.ParentRow >= uint64(len(parents)) {
			return result{}, fmt.Errorf("timing parent %d out of bounds", timing.ParentRow)
		}
		parent := parents[timing.ParentRow]
		sourceRow := sources[timing.ParentRow]
		buffer = append(buffer, observationRow{
			Sequence: sourceRow.Sequence, ParentRow: timing.ParentRow,
			TriggerID: parent.TriggerID, Timestamp: parent.Timestamp,
			RelativeTimestampClock: parent.RelativeTimestampClock, TimeReference: parent.TimeReference,
			ToA: timing.ToA, ToT: timing.ToT,
			Chain: sourceRow.Chain, Node: sourceRow.Node, Qualifier: sourceRow.Qualifier,
			Validity: parent.Validity, Channel: timing.Channel, ChannelValid: 1, HasTiming: 1,
		})
		timingOnlyRows++
		if len(buffer) == cap(buffer) {
			if err := appendRows(&observations, buffer); err != nil {
				return result{}, err
			}
			buffer = make([]observationRow, 0, batchRows)
		}
	}
	if err := appendRows(&observations, buffer); err != nil {
		return result{}, err
	}
	if uint64(len(seenTiming))+timingOnlyRows != uint64(len(timings)) {
		return result{}, fmt.Errorf("timing accounting mismatch")
	}
	if err := destination.Flush(hdf5.F_SCOPE_GLOBAL); err != nil {
		return result{}, err
	}
	allocated := storageSize(observations.dataset)
	observationCount := observations.length
	closeErr := errors.Join(
		observations.dataset.Close(), spectroscopy.Close(), events.Close(), destination.Close(),
	)
	if closeErr != nil {
		return result{}, closeErr
	}
	return result{
		ParentRows: uint64(len(parents)), EnergyRows: uint64(energyCount), TimingRows: uint64(len(timings)),
		ObservationRows: observationCount, TimingOnlyRows: timingOnlyRows,
		LogicalBytes:          observationCount * uint64(unsafe.Sizeof(observationRow{})),
		AllocatedDatasetBytes: allocated,
	}, nil
}

func createTable(location interface {
	CreateDatasetWith(string, *hdf5.Datatype, *hdf5.Dataspace, *hdf5.PropList) (*hdf5.Dataset, error)
}, name string, datatype *hdf5.CompoundType) (table, error) {
	defer datatype.Close()
	space, err := hdf5.CreateSimpleDataspace([]uint{0}, []uint{^uint(0)})
	if err != nil {
		return table{}, err
	}
	defer space.Close()
	properties, err := hdf5.NewPropList(hdf5.P_DATASET_CREATE)
	if err != nil {
		return table{}, err
	}
	defer properties.Close()
	if err := properties.SetChunk([]uint{chunkRows}); err != nil {
		return table{}, err
	}
	if err := hdf5.ConfigureBloscFilter(properties, hdf5.BLOSC_LZ4, 4, hdf5.BLOSC_BITSHUFFLE); err != nil {
		return table{}, err
	}
	dataset, err := location.CreateDatasetWith(name, &datatype.Datatype, space, properties)
	if err != nil {
		return table{}, err
	}
	return table{dataset: dataset}, nil
}

func appendRows[T any](target *table, rows []T) error {
	if len(rows) == 0 {
		return nil
	}
	old := target.length
	target.length += uint64(len(rows))
	if err := target.dataset.Resize([]uint{uint(target.length)}); err != nil {
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

func copyRows[T any](source *hdf5.Dataset, destination *table, count, batchRows int) error {
	for begin := 0; begin < count; begin += batchRows {
		end := min(begin+batchRows, count)
		rows, err := readSubset[T](source, begin, end)
		if err != nil {
			return err
		}
		if err := appendRows(destination, rows); err != nil {
			return err
		}
	}
	return nil
}

func readAll[T any](dataset *hdf5.Dataset) ([]T, error) {
	count, err := datasetLength(dataset)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	return readSubset[T](dataset, 0, count)
}

func readSubset[T any](dataset *hdf5.Dataset, begin, end int) ([]T, error) {
	if begin < 0 || end < begin {
		return nil, errors.New("invalid subset")
	}
	rows := make([]T, end-begin)
	if len(rows) == 0 {
		return rows, nil
	}
	fileSpace := dataset.Space()
	if fileSpace == nil {
		return nil, errors.New("get source dataset space")
	}
	defer fileSpace.Close()
	if err := fileSpace.SelectHyperslab([]uint{uint(begin)}, nil, []uint{uint(len(rows))}, nil); err != nil {
		return nil, err
	}
	memorySpace, err := hdf5.CreateSimpleDataspace([]uint{uint(len(rows))}, nil)
	if err != nil {
		return nil, err
	}
	defer memorySpace.Close()
	if err := dataset.ReadSubset(&rows, memorySpace, fileSpace); err != nil {
		return nil, err
	}
	return rows, nil
}

func datasetLength(dataset *hdf5.Dataset) (int, error) {
	space := dataset.Space()
	if space == nil {
		return 0, errors.New("dataset has no dataspace")
	}
	defer space.Close()
	dimensions, _, err := space.SimpleExtentDims()
	if err != nil {
		return 0, err
	}
	if len(dimensions) != 1 {
		return 0, fmt.Errorf("dataset has rank %d, want 1", len(dimensions))
	}
	return int(dimensions[0]), nil
}

func storageSize(dataset *hdf5.Dataset) uint64 {
	return uint64(C.dataset_storage_size(C.longlong(dataset.ID())))
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

func compoundObservation() *hdf5.CompoundType {
	var value observationRow
	return mustCompound(unsafe.Sizeof(value), []field{
		{"sequence", unsafe.Offsetof(value.Sequence), hdf5.T_STD_U64LE},
		{"parent_row", unsafe.Offsetof(value.ParentRow), hdf5.T_STD_U64LE},
		{"trigger_id", unsafe.Offsetof(value.TriggerID), hdf5.T_STD_U64LE},
		{"timestamp", unsafe.Offsetof(value.Timestamp), hdf5.T_STD_U64LE},
		{"relative_timestamp_clock", unsafe.Offsetof(value.RelativeTimestampClock), hdf5.T_STD_U32LE},
		{"time_reference", unsafe.Offsetof(value.TimeReference), hdf5.T_STD_U32LE},
		{"toa", unsafe.Offsetof(value.ToA), hdf5.T_STD_U32LE},
		{"low_gain", unsafe.Offsetof(value.LowGain), hdf5.T_STD_U16LE},
		{"high_gain", unsafe.Offsetof(value.HighGain), hdf5.T_STD_U16LE},
		{"tot", unsafe.Offsetof(value.ToT), hdf5.T_STD_U16LE},
		{"chain", unsafe.Offsetof(value.Chain), hdf5.T_STD_U8LE},
		{"node", unsafe.Offsetof(value.Node), hdf5.T_STD_U8LE},
		{"qualifier", unsafe.Offsetof(value.Qualifier), hdf5.T_STD_U8LE},
		{"validity", unsafe.Offsetof(value.Validity), hdf5.T_STD_U8LE},
		{"channel", unsafe.Offsetof(value.Channel), hdf5.T_STD_U8LE},
		{"channel_valid", unsafe.Offsetof(value.ChannelValid), hdf5.T_STD_U8LE},
		{"has_energy", unsafe.Offsetof(value.HasEnergy), hdf5.T_STD_U8LE},
		{"has_low_gain", unsafe.Offsetof(value.HasLowGain), hdf5.T_STD_U8LE},
		{"has_high_gain", unsafe.Offsetof(value.HasHighGain), hdf5.T_STD_U8LE},
		{"discriminator", unsafe.Offsetof(value.Discriminator), hdf5.T_STD_U8LE},
		{"has_timing", unsafe.Offsetof(value.HasTiming), hdf5.T_STD_U8LE},
	})
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

type resourceUsage struct {
	user, system float64
	maxRSS       int64
}

func usage() resourceUsage {
	var value syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &value); err != nil {
		fatalf("get resource usage: %v", err)
	}
	return resourceUsage{
		user:   float64(value.Utime.Sec) + float64(value.Utime.Usec)/1e6,
		system: float64(value.Stime.Sec) + float64(value.Stime.Usec)/1e6,
		maxRSS: value.Maxrss,
	}
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
