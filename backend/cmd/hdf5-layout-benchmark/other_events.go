//go:build hdf5

package main

import (
	"errors"
	"fmt"
	"unsafe"

	hdf5 "github.com/next-exp/hdf5-go"
)

const (
	kindTiming   = uint8(2)
	kindCounting = uint8(3)
	kindWaveform = uint8(4)
	kindService  = uint8(5)
	kindTest     = uint8(6)
)

type timingEventRow struct {
	TriggerID, Timestamp, TimeReference uint64
	HitOffset                           uint64
	HitCount                            uint32
}
type countingRow struct {
	TriggerID, Timestamp           uint64
	Validity                       uint8
	RelativeTimestampClock         uint32
	ChannelMask, CountOffset       uint64
	CountCount, TORCount, QORCount uint32
}
type countRow struct {
	ParentRow    uint64
	Channel      uint8
	CounterValue uint32
}
type waveformRow struct {
	TriggerID, Timestamp, SampleOffset uint64
	SampleCount                        uint32
}
type sampleRow struct {
	ParentRow         uint64
	SampleIndex       uint32
	HighGain, LowGain uint16
	DigitalProbes     uint8
}
type serviceRow struct {
	Timestamp                                                             uint64
	Version, Format, Validity                                             uint8
	FPGATemperature, BoardTemperature, DetectorTemperature, HVTemperature float64
	HVVoltage, HVCurrent                                                  float64
	HVOn, HVRamping, HVOverCurrent, HVOverVoltage                         uint8
	Status                                                                uint16
	CounterOffset                                                         uint64
	CounterCount, TORCount, QORCount                                      uint32
	UnknownOffset                                                         uint64
	UnknownCount                                                          uint32
}
type counterRow = countRow
type testRow struct {
	TriggerID, Timestamp, WordOffset uint64
	WordCount                        uint32
}

type timingFlatRow struct {
	Sequence, ParentRow, TriggerID, Timestamp, TimeReference uint64
	ToA                                                      uint32
	ToT                                                      uint16
	Chain, Node, Qualifier, Channel, ChannelValid            uint8
}
type countingFlatRow struct {
	Sequence, ParentRow, TriggerID, Timestamp, ChannelMask  uint64
	RelativeTimestampClock, TORCount, QORCount, Value       uint32
	Chain, Node, Qualifier, Validity, Channel, ChannelValid uint8
}
type waveformFlatRow struct {
	Sequence, ParentRow, TriggerID, Timestamp        uint64
	SampleIndex                                      uint32
	HighGain, LowGain                                uint16
	Chain, Node, Qualifier, DigitalProbes, DataValid uint8
}
type testFlatRow struct {
	Sequence, ParentRow, TriggerID, Timestamp uint64
	Word                                      uint32
	Chain, Node, Qualifier, DataValid         uint8
}
type serviceFlatRow struct {
	Sequence, ParentRow, Timestamp                                        uint64
	FPGATemperature, BoardTemperature, DetectorTemperature, HVTemperature float64
	HVVoltage, HVCurrent                                                  float64
	TORCount, QORCount, CounterValue                                      uint32
	Status                                                                uint16
	Chain, Node, Qualifier, Version, Format, Validity                     uint8
	HVOn, HVRamping, HVOverCurrent, HVOverVoltage                         uint8
	Channel, ChannelValid                                                 uint8
}

type otherSpec struct {
	kind       uint8
	parentPath string
	childPath  string
	group      string
	childName  string
	parentType *hdf5.CompoundType
	childType  *hdf5.CompoundType
	parentSize uint64
	childSize  uint64
}

func rewriteOtherEvent(kind, mode, input, output string, batchRows int) (result, error) {
	if mode == "split" {
		return rewriteOtherSplit(kind, input, output, batchRows)
	}
	switch kind {
	case "timing":
		return rewriteTimingFlat(input, output)
	case "counting":
		return rewriteCountingFlat(input, output)
	case "waveform":
		return rewriteWaveformFlat(input, output, batchRows)
	case "service":
		return rewriteServiceFlat(input, output)
	case "test":
		return rewriteTestFlat(input, output)
	}
	return result{}, fmt.Errorf("unsupported event kind %q", kind)
}

func specFor(kind string) (otherSpec, error) {
	switch kind {
	case "timing":
		return otherSpec{kindTiming, "events/timing/events", "events/timing/hits", "timing", "hits", compoundTimingEventOther(), compoundTiming(), uint64(unsafe.Sizeof(timingEventRow{})), uint64(unsafe.Sizeof(timingRow{}))}, nil
	case "counting":
		return otherSpec{kindCounting, "events/counting/events", "events/counting/counts", "counting", "counts", compoundCountingOther(), compoundCountOther(), uint64(unsafe.Sizeof(countingRow{})), uint64(unsafe.Sizeof(countRow{}))}, nil
	case "waveform":
		return otherSpec{kindWaveform, "events/waveform/events", "events/waveform/samples", "waveform", "samples", compoundWaveformOther(), compoundSampleOther(), uint64(unsafe.Sizeof(waveformRow{})), uint64(unsafe.Sizeof(sampleRow{}))}, nil
	case "service":
		return otherSpec{kindService, "events/service/events", "events/service/counters", "service", "counters", compoundServiceOther(), compoundCountOther(), uint64(unsafe.Sizeof(serviceRow{})), uint64(unsafe.Sizeof(counterRow{}))}, nil
	case "test":
		return otherSpec{kindTest, "events/test/events", "events/test/words", "test", "words", compoundTestOther(), nil, uint64(unsafe.Sizeof(testRow{})), 4}, nil
	}
	return otherSpec{}, fmt.Errorf("unsupported event kind %q", kind)
}

func rewriteOtherSplit(kind, input, output string, batchRows int) (result, error) {
	spec, err := specFor(kind)
	if err != nil {
		return result{}, err
	}
	source, err := hdf5.OpenFile(input, hdf5.F_ACC_RDONLY)
	if err != nil {
		return result{}, err
	}
	defer source.Close()
	parentsSource, err := source.OpenDataset(spec.parentPath)
	if err != nil {
		return result{}, err
	}
	defer parentsSource.Close()
	childrenSource, err := source.OpenDataset(spec.childPath)
	if err != nil {
		return result{}, err
	}
	defer childrenSource.Close()
	parentCount, err := datasetLength(parentsSource)
	if err != nil {
		return result{}, err
	}
	childCount, err := datasetLength(childrenSource)
	if err != nil {
		return result{}, err
	}
	file, group, parents, err := createOtherOutput(output, spec.group, "events", spec.parentType)
	if err != nil {
		return result{}, err
	}
	var children table
	if kind == "test" {
		children, err = createUint32Table(group, spec.childName)
	} else {
		children, err = createTable(group, spec.childName, spec.childType)
	}
	if err != nil {
		return result{}, err
	}
	switch kind {
	case "timing":
		err = copyRows[timingEventRow](parentsSource, &parents, parentCount, batchRows)
		if err == nil {
			err = copyRows[timingRow](childrenSource, &children, childCount, batchRows)
		}
	case "counting":
		err = copyRows[countingRow](parentsSource, &parents, parentCount, batchRows)
		if err == nil {
			err = copyRows[countRow](childrenSource, &children, childCount, batchRows)
		}
	case "waveform":
		err = copyRows[waveformRow](parentsSource, &parents, parentCount, batchRows)
		if err == nil {
			err = copyRows[sampleRow](childrenSource, &children, childCount, batchRows)
		}
	case "service":
		err = copyRows[serviceRow](parentsSource, &parents, parentCount, batchRows)
		if err == nil {
			err = copyRows[counterRow](childrenSource, &children, childCount, batchRows)
		}
	case "test":
		err = copyRows[testRow](parentsSource, &parents, parentCount, batchRows)
		if err == nil {
			err = copyRows[uint32](childrenSource, &children, childCount, batchRows)
		}
	}
	if err != nil {
		return result{}, err
	}
	if err := file.Flush(hdf5.F_SCOPE_GLOBAL); err != nil {
		return result{}, err
	}
	allocated := storageSize(parents.dataset) + storageSize(children.dataset)
	if err := errors.Join(children.dataset.Close(), parents.dataset.Close(), group.Close(), file.Close()); err != nil {
		return result{}, err
	}
	return result{ParentRows: uint64(parentCount), ChildRows: uint64(childCount), LogicalBytes: uint64(parentCount)*spec.parentSize + uint64(childCount)*spec.childSize, AllocatedDatasetBytes: allocated}, nil
}

func sourceRows(source *hdf5.File, kind uint8, count int) ([]sourceRow, error) {
	dataset, err := source.OpenDataset("events/index")
	if err != nil {
		return nil, err
	}
	defer dataset.Close()
	indexes, err := readAll[indexRow](dataset)
	if err != nil {
		return nil, err
	}
	rows := make([]sourceRow, count)
	for _, item := range indexes {
		if item.Kind == kind {
			if item.KindRow >= uint64(count) {
				return nil, fmt.Errorf("kind row %d out of bounds", item.KindRow)
			}
			rows[item.KindRow] = sourceRow{item.Sequence, item.Chain, item.Node, item.Qualifier}
		}
	}
	return rows, nil
}

func openOtherInput(input, parentsPath, childrenPath string) (*hdf5.File, *hdf5.Dataset, *hdf5.Dataset, error) {
	source, err := hdf5.OpenFile(input, hdf5.F_ACC_RDONLY)
	if err != nil {
		return nil, nil, nil, err
	}
	parents, err := source.OpenDataset(parentsPath)
	if err != nil {
		source.Close()
		return nil, nil, nil, err
	}
	children, err := source.OpenDataset(childrenPath)
	if err != nil {
		parents.Close()
		source.Close()
		return nil, nil, nil, err
	}
	return source, parents, children, nil
}

func createOtherOutput(output, groupName, tableName string, datatype *hdf5.CompoundType) (*hdf5.File, *hdf5.Group, table, error) {
	file, err := hdf5.CreateFile(output, hdf5.F_ACC_EXCL)
	if err != nil {
		return nil, nil, table{}, err
	}
	events, err := file.CreateGroup("events")
	if err != nil {
		return nil, nil, table{}, err
	}
	group, err := events.CreateGroup(groupName)
	events.Close()
	if err != nil {
		return nil, nil, table{}, err
	}
	rows, err := createTable(group, tableName, datatype)
	return file, group, rows, err
}

func flatResult(file *hdf5.File, group *hdf5.Group, rows table, parents, children, sentinels uint64, rowSize uintptr) (result, error) {
	if err := file.Flush(hdf5.F_SCOPE_GLOBAL); err != nil {
		return result{}, err
	}
	allocated := storageSize(rows.dataset)
	if err := errors.Join(rows.dataset.Close(), group.Close(), file.Close()); err != nil {
		return result{}, err
	}
	return result{ParentRows: parents, ChildRows: children, ObservationRows: rows.length, SentinelRows: sentinels, LogicalBytes: rows.length * uint64(rowSize), AllocatedDatasetBytes: allocated}, nil
}

func rewriteTimingFlat(input, output string) (result, error) {
	source, pd, cd, err := openOtherInput(input, "events/timing/events", "events/timing/hits")
	if err != nil {
		return result{}, err
	}
	defer source.Close()
	defer pd.Close()
	defer cd.Close()
	parents, err := readAll[timingEventRow](pd)
	if err != nil {
		return result{}, err
	}
	children, err := readAll[timingRow](cd)
	if err != nil {
		return result{}, err
	}
	sources, err := sourceRows(source, kindTiming, len(parents))
	if err != nil {
		return result{}, err
	}
	file, group, rows, err := createOtherOutput(output, "timing", "observations", compoundTimingFlat())
	if err != nil {
		return result{}, err
	}
	out := make([]timingFlatRow, 0, len(children)+len(parents))
	sentinels := uint64(0)
	for parentIndex, parent := range parents {
		sourceRow := sources[parentIndex]
		if parent.HitCount == 0 {
			out = append(out, timingFlatRow{Sequence: sourceRow.Sequence, ParentRow: uint64(parentIndex), TriggerID: parent.TriggerID, Timestamp: parent.Timestamp, TimeReference: parent.TimeReference, Chain: sourceRow.Chain, Node: sourceRow.Node, Qualifier: sourceRow.Qualifier})
			sentinels++
			continue
		}
		for _, child := range children[parent.HitOffset : parent.HitOffset+uint64(parent.HitCount)] {
			out = append(out, timingFlatRow{Sequence: sourceRow.Sequence, ParentRow: uint64(parentIndex), TriggerID: parent.TriggerID, Timestamp: parent.Timestamp, TimeReference: parent.TimeReference, ToA: child.ToA, ToT: child.ToT, Chain: sourceRow.Chain, Node: sourceRow.Node, Qualifier: sourceRow.Qualifier, Channel: child.Channel, ChannelValid: 1})
		}
	}
	if err := appendRows(&rows, out); err != nil {
		return result{}, err
	}
	return flatResult(file, group, rows, uint64(len(parents)), uint64(len(children)), sentinels, unsafe.Sizeof(timingFlatRow{}))
}

func rewriteCountingFlat(input, output string) (result, error) {
	source, pd, cd, err := openOtherInput(input, "events/counting/events", "events/counting/counts")
	if err != nil {
		return result{}, err
	}
	defer source.Close()
	defer pd.Close()
	defer cd.Close()
	parents, err := readAll[countingRow](pd)
	if err != nil {
		return result{}, err
	}
	children, err := readAll[countRow](cd)
	if err != nil {
		return result{}, err
	}
	sources, err := sourceRows(source, kindCounting, len(parents))
	if err != nil {
		return result{}, err
	}
	file, group, rows, err := createOtherOutput(output, "counting", "observations", compoundCountingFlat())
	if err != nil {
		return result{}, err
	}
	out := make([]countingFlatRow, 0, len(children)+len(parents))
	sentinels := uint64(0)
	for parentIndex, parent := range parents {
		src := sources[parentIndex]
		common := countingFlatRow{Sequence: src.Sequence, ParentRow: uint64(parentIndex), TriggerID: parent.TriggerID, Timestamp: parent.Timestamp, ChannelMask: parent.ChannelMask, RelativeTimestampClock: parent.RelativeTimestampClock, TORCount: parent.TORCount, QORCount: parent.QORCount, Chain: src.Chain, Node: src.Node, Qualifier: src.Qualifier, Validity: parent.Validity}
		if parent.CountCount == 0 {
			out = append(out, common)
			sentinels++
		} else {
			for _, child := range children[parent.CountOffset : parent.CountOffset+uint64(parent.CountCount)] {
				row := common
				row.Channel, row.ChannelValid, row.Value = child.Channel, 1, child.CounterValue
				out = append(out, row)
			}
		}
	}
	if err := appendRows(&rows, out); err != nil {
		return result{}, err
	}
	return flatResult(file, group, rows, uint64(len(parents)), uint64(len(children)), sentinels, unsafe.Sizeof(countingFlatRow{}))
}

func rewriteWaveformFlat(input, output string, batchRows int) (result, error) {
	source, pd, cd, err := openOtherInput(input, "events/waveform/events", "events/waveform/samples")
	if err != nil {
		return result{}, err
	}
	defer source.Close()
	defer pd.Close()
	defer cd.Close()
	parents, err := readAll[waveformRow](pd)
	if err != nil {
		return result{}, err
	}
	sources, err := sourceRows(source, kindWaveform, len(parents))
	if err != nil {
		return result{}, err
	}
	file, group, rows, err := createOtherOutput(output, "waveform", "observations", compoundWaveformFlat())
	if err != nil {
		return result{}, err
	}
	childCount, _ := datasetLength(cd)
	sentinels := uint64(0)
	for parentIndex, parent := range parents {
		src := sources[parentIndex]
		if parent.SampleCount == 0 {
			if err := appendRows(&rows, []waveformFlatRow{{Sequence: src.Sequence, ParentRow: uint64(parentIndex), TriggerID: parent.TriggerID, Timestamp: parent.Timestamp, Chain: src.Chain, Node: src.Node, Qualifier: src.Qualifier}}); err != nil {
				return result{}, err
			}
			sentinels++
			continue
		}
		for begin := int(parent.SampleOffset); begin < int(parent.SampleOffset+uint64(parent.SampleCount)); begin += batchRows {
			end := min(begin+batchRows, int(parent.SampleOffset+uint64(parent.SampleCount)))
			children, err := readSubset[sampleRow](cd, begin, end)
			if err != nil {
				return result{}, err
			}
			out := make([]waveformFlatRow, len(children))
			for i, child := range children {
				out[i] = waveformFlatRow{Sequence: src.Sequence, ParentRow: uint64(parentIndex), TriggerID: parent.TriggerID, Timestamp: parent.Timestamp, SampleIndex: child.SampleIndex, HighGain: child.HighGain, LowGain: child.LowGain, Chain: src.Chain, Node: src.Node, Qualifier: src.Qualifier, DigitalProbes: child.DigitalProbes, DataValid: 1}
			}
			if err := appendRows(&rows, out); err != nil {
				return result{}, err
			}
		}
	}
	return flatResult(file, group, rows, uint64(len(parents)), uint64(childCount), sentinels, unsafe.Sizeof(waveformFlatRow{}))
}

func rewriteTestFlat(input, output string) (result, error) {
	source, pd, cd, err := openOtherInput(input, "events/test/events", "events/test/words")
	if err != nil {
		return result{}, err
	}
	defer source.Close()
	defer pd.Close()
	defer cd.Close()
	parents, err := readAll[testRow](pd)
	if err != nil {
		return result{}, err
	}
	children, err := readAll[uint32](cd)
	if err != nil {
		return result{}, err
	}
	sources, err := sourceRows(source, kindTest, len(parents))
	if err != nil {
		return result{}, err
	}
	file, group, rows, err := createOtherOutput(output, "test", "observations", compoundTestFlat())
	if err != nil {
		return result{}, err
	}
	out := make([]testFlatRow, 0, len(children)+len(parents))
	sentinels := uint64(0)
	for parentIndex, parent := range parents {
		src := sources[parentIndex]
		common := testFlatRow{Sequence: src.Sequence, ParentRow: uint64(parentIndex), TriggerID: parent.TriggerID, Timestamp: parent.Timestamp, Chain: src.Chain, Node: src.Node, Qualifier: src.Qualifier}
		if parent.WordCount == 0 {
			out = append(out, common)
			sentinels++
		} else {
			for _, word := range children[parent.WordOffset : parent.WordOffset+uint64(parent.WordCount)] {
				row := common
				row.Word, row.DataValid = word, 1
				out = append(out, row)
			}
		}
	}
	if err := appendRows(&rows, out); err != nil {
		return result{}, err
	}
	return flatResult(file, group, rows, uint64(len(parents)), uint64(len(children)), sentinels, unsafe.Sizeof(testFlatRow{}))
}

// Service-flat deliberately keeps unknown raw bytes out of the counter table.
// Duplicating an arbitrary payload into every counter row is not a sensible
// one-table representation; the result measures flattening decoded counters.
func rewriteServiceFlat(input, output string) (result, error) {
	source, pd, cd, err := openOtherInput(input, "events/service/events", "events/service/counters")
	if err != nil {
		return result{}, err
	}
	defer source.Close()
	defer pd.Close()
	defer cd.Close()
	parents, err := readAll[serviceRow](pd)
	if err != nil {
		return result{}, err
	}
	children, err := readAll[counterRow](cd)
	if err != nil {
		return result{}, err
	}
	sources, err := sourceRows(source, kindService, len(parents))
	if err != nil {
		return result{}, err
	}
	file, group, rows, err := createOtherOutput(output, "service", "observations", compoundServiceFlat())
	if err != nil {
		return result{}, err
	}
	out := make([]serviceFlatRow, 0, len(children)+len(parents))
	sentinels := uint64(0)
	for parentIndex, parent := range parents {
		src := sources[parentIndex]
		common := serviceFlatRow{Sequence: src.Sequence, ParentRow: uint64(parentIndex), Timestamp: parent.Timestamp, FPGATemperature: parent.FPGATemperature, BoardTemperature: parent.BoardTemperature, DetectorTemperature: parent.DetectorTemperature, HVTemperature: parent.HVTemperature, HVVoltage: parent.HVVoltage, HVCurrent: parent.HVCurrent, TORCount: parent.TORCount, QORCount: parent.QORCount, Status: parent.Status, Chain: src.Chain, Node: src.Node, Qualifier: src.Qualifier, Version: parent.Version, Format: parent.Format, Validity: parent.Validity, HVOn: parent.HVOn, HVRamping: parent.HVRamping, HVOverCurrent: parent.HVOverCurrent, HVOverVoltage: parent.HVOverVoltage}
		if parent.CounterCount == 0 {
			out = append(out, common)
			sentinels++
		} else {
			for _, child := range children[parent.CounterOffset : parent.CounterOffset+uint64(parent.CounterCount)] {
				row := common
				row.Channel, row.ChannelValid, row.CounterValue = child.Channel, 1, child.CounterValue
				out = append(out, row)
			}
		}
	}
	if err := appendRows(&rows, out); err != nil {
		return result{}, err
	}
	return flatResult(file, group, rows, uint64(len(parents)), uint64(len(children)), sentinels, unsafe.Sizeof(serviceFlatRow{}))
}

func createUint32Table(group *hdf5.Group, name string) (table, error) {
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
	dataset, err := group.CreateDatasetWith("words", hdf5.T_STD_U32LE, space, properties)
	return table{dataset: dataset}, err
}

func compoundTimingEventOther() *hdf5.CompoundType {
	var v timingEventRow
	return mustCompound(unsafe.Sizeof(v), []field{{"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"time_reference", unsafe.Offsetof(v.TimeReference), hdf5.T_STD_U64LE}, {"hit_offset", unsafe.Offsetof(v.HitOffset), hdf5.T_STD_U64LE}, {"hit_count", unsafe.Offsetof(v.HitCount), hdf5.T_STD_U32LE}})
}
func compoundCountingOther() *hdf5.CompoundType {
	var v countingRow
	return mustCompound(unsafe.Sizeof(v), []field{{"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"validity", unsafe.Offsetof(v.Validity), hdf5.T_STD_U8LE}, {"relative_timestamp_clock", unsafe.Offsetof(v.RelativeTimestampClock), hdf5.T_STD_U32LE}, {"channel_mask", unsafe.Offsetof(v.ChannelMask), hdf5.T_STD_U64LE}, {"count_offset", unsafe.Offsetof(v.CountOffset), hdf5.T_STD_U64LE}, {"count_count", unsafe.Offsetof(v.CountCount), hdf5.T_STD_U32LE}, {"t_or_count", unsafe.Offsetof(v.TORCount), hdf5.T_STD_U32LE}, {"q_or_count", unsafe.Offsetof(v.QORCount), hdf5.T_STD_U32LE}})
}
func compoundCountOther() *hdf5.CompoundType {
	var v countRow
	return mustCompound(unsafe.Sizeof(v), []field{{"parent_row", unsafe.Offsetof(v.ParentRow), hdf5.T_STD_U64LE}, {"channel", unsafe.Offsetof(v.Channel), hdf5.T_STD_U8LE}, {"counter_value", unsafe.Offsetof(v.CounterValue), hdf5.T_STD_U32LE}})
}
func compoundWaveformOther() *hdf5.CompoundType {
	var v waveformRow
	return mustCompound(unsafe.Sizeof(v), []field{{"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"sample_offset", unsafe.Offsetof(v.SampleOffset), hdf5.T_STD_U64LE}, {"sample_count", unsafe.Offsetof(v.SampleCount), hdf5.T_STD_U32LE}})
}
func compoundSampleOther() *hdf5.CompoundType {
	var v sampleRow
	return mustCompound(unsafe.Sizeof(v), []field{{"parent_row", unsafe.Offsetof(v.ParentRow), hdf5.T_STD_U64LE}, {"sample_index", unsafe.Offsetof(v.SampleIndex), hdf5.T_STD_U32LE}, {"high_gain", unsafe.Offsetof(v.HighGain), hdf5.T_STD_U16LE}, {"low_gain", unsafe.Offsetof(v.LowGain), hdf5.T_STD_U16LE}, {"digital_probes", unsafe.Offsetof(v.DigitalProbes), hdf5.T_STD_U8LE}})
}
func compoundServiceOther() *hdf5.CompoundType {
	var v serviceRow
	return mustCompound(unsafe.Sizeof(v), []field{{"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"version", unsafe.Offsetof(v.Version), hdf5.T_STD_U8LE}, {"format", unsafe.Offsetof(v.Format), hdf5.T_STD_U8LE}, {"validity", unsafe.Offsetof(v.Validity), hdf5.T_STD_U8LE}, {"fpga_temperature_c", unsafe.Offsetof(v.FPGATemperature), hdf5.T_IEEE_F64LE}, {"board_temperature_c", unsafe.Offsetof(v.BoardTemperature), hdf5.T_IEEE_F64LE}, {"detector_temperature_c", unsafe.Offsetof(v.DetectorTemperature), hdf5.T_IEEE_F64LE}, {"hv_temperature_c", unsafe.Offsetof(v.HVTemperature), hdf5.T_IEEE_F64LE}, {"hv_voltage_v", unsafe.Offsetof(v.HVVoltage), hdf5.T_IEEE_F64LE}, {"hv_current_a", unsafe.Offsetof(v.HVCurrent), hdf5.T_IEEE_F64LE}, {"hv_on", unsafe.Offsetof(v.HVOn), hdf5.T_STD_U8LE}, {"hv_ramping", unsafe.Offsetof(v.HVRamping), hdf5.T_STD_U8LE}, {"hv_over_current", unsafe.Offsetof(v.HVOverCurrent), hdf5.T_STD_U8LE}, {"hv_over_voltage", unsafe.Offsetof(v.HVOverVoltage), hdf5.T_STD_U8LE}, {"status", unsafe.Offsetof(v.Status), hdf5.T_STD_U16LE}, {"counter_offset", unsafe.Offsetof(v.CounterOffset), hdf5.T_STD_U64LE}, {"counter_count", unsafe.Offsetof(v.CounterCount), hdf5.T_STD_U32LE}, {"t_or_count", unsafe.Offsetof(v.TORCount), hdf5.T_STD_U32LE}, {"q_or_count", unsafe.Offsetof(v.QORCount), hdf5.T_STD_U32LE}, {"unknown_offset", unsafe.Offsetof(v.UnknownOffset), hdf5.T_STD_U64LE}, {"unknown_count", unsafe.Offsetof(v.UnknownCount), hdf5.T_STD_U32LE}})
}
func compoundTestOther() *hdf5.CompoundType {
	var v testRow
	return mustCompound(unsafe.Sizeof(v), []field{{"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"word_offset", unsafe.Offsetof(v.WordOffset), hdf5.T_STD_U64LE}, {"word_count", unsafe.Offsetof(v.WordCount), hdf5.T_STD_U32LE}})
}
func compoundTimingFlat() *hdf5.CompoundType {
	var v timingFlatRow
	return mustCompound(unsafe.Sizeof(v), []field{{"sequence", unsafe.Offsetof(v.Sequence), hdf5.T_STD_U64LE}, {"parent_row", unsafe.Offsetof(v.ParentRow), hdf5.T_STD_U64LE}, {"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"time_reference", unsafe.Offsetof(v.TimeReference), hdf5.T_STD_U64LE}, {"toa", unsafe.Offsetof(v.ToA), hdf5.T_STD_U32LE}, {"tot", unsafe.Offsetof(v.ToT), hdf5.T_STD_U16LE}, {"chain", unsafe.Offsetof(v.Chain), hdf5.T_STD_U8LE}, {"node", unsafe.Offsetof(v.Node), hdf5.T_STD_U8LE}, {"qualifier", unsafe.Offsetof(v.Qualifier), hdf5.T_STD_U8LE}, {"channel", unsafe.Offsetof(v.Channel), hdf5.T_STD_U8LE}, {"channel_valid", unsafe.Offsetof(v.ChannelValid), hdf5.T_STD_U8LE}})
}
func compoundCountingFlat() *hdf5.CompoundType {
	var v countingFlatRow
	return mustCompound(unsafe.Sizeof(v), []field{{"sequence", unsafe.Offsetof(v.Sequence), hdf5.T_STD_U64LE}, {"parent_row", unsafe.Offsetof(v.ParentRow), hdf5.T_STD_U64LE}, {"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"channel_mask", unsafe.Offsetof(v.ChannelMask), hdf5.T_STD_U64LE}, {"relative_timestamp_clock", unsafe.Offsetof(v.RelativeTimestampClock), hdf5.T_STD_U32LE}, {"t_or_count", unsafe.Offsetof(v.TORCount), hdf5.T_STD_U32LE}, {"q_or_count", unsafe.Offsetof(v.QORCount), hdf5.T_STD_U32LE}, {"counter_value", unsafe.Offsetof(v.Value), hdf5.T_STD_U32LE}, {"chain", unsafe.Offsetof(v.Chain), hdf5.T_STD_U8LE}, {"node", unsafe.Offsetof(v.Node), hdf5.T_STD_U8LE}, {"qualifier", unsafe.Offsetof(v.Qualifier), hdf5.T_STD_U8LE}, {"validity", unsafe.Offsetof(v.Validity), hdf5.T_STD_U8LE}, {"channel", unsafe.Offsetof(v.Channel), hdf5.T_STD_U8LE}, {"channel_valid", unsafe.Offsetof(v.ChannelValid), hdf5.T_STD_U8LE}})
}
func compoundWaveformFlat() *hdf5.CompoundType {
	var v waveformFlatRow
	return mustCompound(unsafe.Sizeof(v), []field{{"sequence", unsafe.Offsetof(v.Sequence), hdf5.T_STD_U64LE}, {"parent_row", unsafe.Offsetof(v.ParentRow), hdf5.T_STD_U64LE}, {"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"sample_index", unsafe.Offsetof(v.SampleIndex), hdf5.T_STD_U32LE}, {"high_gain", unsafe.Offsetof(v.HighGain), hdf5.T_STD_U16LE}, {"low_gain", unsafe.Offsetof(v.LowGain), hdf5.T_STD_U16LE}, {"chain", unsafe.Offsetof(v.Chain), hdf5.T_STD_U8LE}, {"node", unsafe.Offsetof(v.Node), hdf5.T_STD_U8LE}, {"qualifier", unsafe.Offsetof(v.Qualifier), hdf5.T_STD_U8LE}, {"digital_probes", unsafe.Offsetof(v.DigitalProbes), hdf5.T_STD_U8LE}, {"data_valid", unsafe.Offsetof(v.DataValid), hdf5.T_STD_U8LE}})
}
func compoundTestFlat() *hdf5.CompoundType {
	var v testFlatRow
	return mustCompound(unsafe.Sizeof(v), []field{{"sequence", unsafe.Offsetof(v.Sequence), hdf5.T_STD_U64LE}, {"parent_row", unsafe.Offsetof(v.ParentRow), hdf5.T_STD_U64LE}, {"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"word", unsafe.Offsetof(v.Word), hdf5.T_STD_U32LE}, {"chain", unsafe.Offsetof(v.Chain), hdf5.T_STD_U8LE}, {"node", unsafe.Offsetof(v.Node), hdf5.T_STD_U8LE}, {"qualifier", unsafe.Offsetof(v.Qualifier), hdf5.T_STD_U8LE}, {"data_valid", unsafe.Offsetof(v.DataValid), hdf5.T_STD_U8LE}})
}
func compoundServiceFlat() *hdf5.CompoundType {
	var v serviceFlatRow
	return mustCompound(unsafe.Sizeof(v), []field{{"sequence", unsafe.Offsetof(v.Sequence), hdf5.T_STD_U64LE}, {"parent_row", unsafe.Offsetof(v.ParentRow), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"fpga_temperature_c", unsafe.Offsetof(v.FPGATemperature), hdf5.T_IEEE_F64LE}, {"board_temperature_c", unsafe.Offsetof(v.BoardTemperature), hdf5.T_IEEE_F64LE}, {"detector_temperature_c", unsafe.Offsetof(v.DetectorTemperature), hdf5.T_IEEE_F64LE}, {"hv_temperature_c", unsafe.Offsetof(v.HVTemperature), hdf5.T_IEEE_F64LE}, {"hv_voltage_v", unsafe.Offsetof(v.HVVoltage), hdf5.T_IEEE_F64LE}, {"hv_current_a", unsafe.Offsetof(v.HVCurrent), hdf5.T_IEEE_F64LE}, {"t_or_count", unsafe.Offsetof(v.TORCount), hdf5.T_STD_U32LE}, {"q_or_count", unsafe.Offsetof(v.QORCount), hdf5.T_STD_U32LE}, {"counter_value", unsafe.Offsetof(v.CounterValue), hdf5.T_STD_U32LE}, {"status", unsafe.Offsetof(v.Status), hdf5.T_STD_U16LE}, {"chain", unsafe.Offsetof(v.Chain), hdf5.T_STD_U8LE}, {"node", unsafe.Offsetof(v.Node), hdf5.T_STD_U8LE}, {"qualifier", unsafe.Offsetof(v.Qualifier), hdf5.T_STD_U8LE}, {"version", unsafe.Offsetof(v.Version), hdf5.T_STD_U8LE}, {"format", unsafe.Offsetof(v.Format), hdf5.T_STD_U8LE}, {"validity", unsafe.Offsetof(v.Validity), hdf5.T_STD_U8LE}, {"hv_on", unsafe.Offsetof(v.HVOn), hdf5.T_STD_U8LE}, {"hv_ramping", unsafe.Offsetof(v.HVRamping), hdf5.T_STD_U8LE}, {"hv_over_current", unsafe.Offsetof(v.HVOverCurrent), hdf5.T_STD_U8LE}, {"hv_over_voltage", unsafe.Offsetof(v.HVOverVoltage), hdf5.T_STD_U8LE}, {"channel", unsafe.Offsetof(v.Channel), hdf5.T_STD_U8LE}, {"channel_valid", unsafe.Offsetof(v.ChannelValid), hdf5.T_STD_U8LE}})
}
