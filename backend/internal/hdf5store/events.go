//go:build hdf5

package hdf5store

import (
	"errors"
	"fmt"
	"unsafe"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
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

type counterRow struct {
	ParentRow    uint64
	Channel      uint8
	CounterValue uint32
}

type testRow struct {
	TriggerID, Timestamp, WordOffset uint64
	WordCount                        uint32
}

func (w *Writer) appendIndex(wire dt5215.StreamEvent, kind uint8, row uint64) error {
	index := indexRow{
		Sequence: w.index.length + 1, Kind: kind, KindRow: row,
		Chain: wire.Chain, Node: wire.Descriptor.Node, Qualifier: wire.Descriptor.Qualifier,
		TriggerID: wire.Descriptor.TriggerID, Timestamp: wire.Descriptor.Timestamp,
		PayloadOffsetWords: wire.Descriptor.PayloadOffsetWords,
		PayloadSizeWords:   wire.Descriptor.PayloadSizeWords,
		CRCError:           boolean(wire.Descriptor.CRCError),
	}
	if err := appendRows(&w.index, []indexRow{index}); err != nil {
		return fmt.Errorf("append event index: %w", err)
	}
	return nil
}

func validateIdentity(name string, triggerID, timestamp uint64, wire dt5215.StreamEvent) error {
	if triggerID != wire.Descriptor.TriggerID || timestamp != wire.Descriptor.Timestamp {
		return fmt.Errorf("typed %s identity does not match DT5215 descriptor", name)
	}
	return nil
}

func (w *Writer) appendTiming(wire dt5215.StreamEvent, event dt5202.Event) error {
	if event.Timing == nil {
		return errors.New("timing event payload is missing")
	}
	value := event.Timing
	if err := validateIdentity("timing", value.TriggerID, value.Timestamp, wire); err != nil {
		return err
	}
	parent := w.timingEvents.length
	hitCount, err := uint32Count("timing hits", len(value.Hits))
	if err != nil {
		return err
	}
	hits := make([]timingRow, len(value.Hits))
	for i, hit := range value.Hits {
		hits[i] = timingRow{ParentRow: parent, Channel: hit.Channel, ToA: hit.ToA, ToT: hit.ToT}
	}
	offset := w.timingHits.length
	if err := appendRows(&w.timingHits, hits); err != nil {
		return fmt.Errorf("append timing hits: %w", err)
	}
	row := timingEventRow{TriggerID: value.TriggerID, Timestamp: value.Timestamp, TimeReference: value.TimeReference, HitOffset: offset, HitCount: hitCount}
	if err := appendRows(&w.timingEvents, []timingEventRow{row}); err != nil {
		return fmt.Errorf("append timing event: %w", err)
	}
	return w.appendIndex(wire, KindTiming, parent)
}

func (w *Writer) appendCounting(wire dt5215.StreamEvent, event dt5202.Event) error {
	if event.Counting == nil {
		return errors.New("counting event payload is missing")
	}
	value := event.Counting
	if err := validateIdentity("counting", value.TriggerID, value.Timestamp, wire); err != nil {
		return err
	}
	parent, offset := w.counting.length, w.counts.length
	countCount, err := uint32Count("counting counters", len(value.Counts))
	if err != nil {
		return err
	}
	children := make([]countRow, len(value.Counts))
	for i, count := range value.Counts {
		children[i] = countRow{ParentRow: parent, Channel: count.Channel, CounterValue: count.Value}
	}
	if err := appendRows(&w.counts, children); err != nil {
		return fmt.Errorf("append counting counters: %w", err)
	}
	row := countingRow{TriggerID: value.TriggerID, Timestamp: value.Timestamp, ChannelMask: value.ChannelMask, CountOffset: offset, CountCount: countCount, TORCount: value.TORCount, QORCount: value.QORCount}
	if value.RelativeTimestampClock != nil {
		row.Validity = 1
		row.RelativeTimestampClock = *value.RelativeTimestampClock
	}
	if err := appendRows(&w.counting, []countingRow{row}); err != nil {
		return fmt.Errorf("append counting event: %w", err)
	}
	return w.appendIndex(wire, KindCounting, parent)
}

func (w *Writer) appendWaveform(wire dt5215.StreamEvent, event dt5202.Event) error {
	if event.Waveform == nil {
		return errors.New("waveform event payload is missing")
	}
	value := event.Waveform
	if err := validateIdentity("waveform", value.TriggerID, value.Timestamp, wire); err != nil {
		return err
	}
	parent, offset := w.waveform.length, w.samples.length
	sampleCount, err := uint32Count("waveform samples", len(value.Samples))
	if err != nil {
		return err
	}
	children := make([]sampleRow, len(value.Samples))
	for i, sample := range value.Samples {
		children[i] = sampleRow{ParentRow: parent, SampleIndex: uint32(i), HighGain: sample.HighGain, LowGain: sample.LowGain, DigitalProbes: sample.DigitalProbes}
	}
	if err := appendRows(&w.samples, children); err != nil {
		return fmt.Errorf("append waveform samples: %w", err)
	}
	row := waveformRow{TriggerID: value.TriggerID, Timestamp: value.Timestamp, SampleOffset: offset, SampleCount: sampleCount}
	if err := appendRows(&w.waveform, []waveformRow{row}); err != nil {
		return fmt.Errorf("append waveform event: %w", err)
	}
	return w.appendIndex(wire, KindWaveform, parent)
}

func (w *Writer) appendService(wire dt5215.StreamEvent, event dt5202.Event) error {
	if event.Service == nil {
		return errors.New("service event payload is missing")
	}
	value := event.Service
	if value.Timestamp != wire.Descriptor.Timestamp {
		return errors.New("typed service timestamp does not match DT5215 descriptor")
	}
	parent, counterOffset, unknownOffset := w.service.length, w.counters.length, w.unknown.length
	counterCount, err := uint32Count("service counters", len(value.Counters))
	if err != nil {
		return err
	}
	unknownCount, err := uint32Count("service unknown payload", len(value.UnknownPayload))
	if err != nil {
		return err
	}
	counters := make([]counterRow, len(value.Counters))
	for i, counter := range value.Counters {
		counters[i] = counterRow{ParentRow: parent, Channel: counter.Channel, CounterValue: counter.Value}
	}
	if err := appendRows(&w.counters, counters); err != nil {
		return fmt.Errorf("append service counters: %w", err)
	}
	if err := appendRows(&w.unknown, value.UnknownPayload); err != nil {
		return fmt.Errorf("append service unknown payload: %w", err)
	}
	row := serviceRow{
		Timestamp: value.Timestamp, Version: value.Version, Format: value.Format,
		HVOn: boolean(value.HVOn), HVRamping: boolean(value.HVRamping), HVOverCurrent: boolean(value.HVOverCurrent), HVOverVoltage: boolean(value.HVOverVoltage),
		CounterOffset: counterOffset, CounterCount: counterCount, TORCount: value.TORCount, QORCount: value.QORCount,
		UnknownOffset: unknownOffset, UnknownCount: unknownCount,
	}
	setOptionalFloat(&row.Validity, 0, value.FPGATemperature, &row.FPGATemperature)
	setOptionalFloat(&row.Validity, 1, value.BoardTemperature, &row.BoardTemperature)
	setOptionalFloat(&row.Validity, 2, value.DetectorTemperature, &row.DetectorTemperature)
	setOptionalFloat(&row.Validity, 3, value.HVTemperature, &row.HVTemperature)
	setOptionalFloat(&row.Validity, 4, value.HVVoltage, &row.HVVoltage)
	setOptionalFloat(&row.Validity, 5, value.HVCurrent, &row.HVCurrent)
	if value.Status != nil {
		row.Validity |= 1 << 6
		row.Status = *value.Status
	}
	if err := appendRows(&w.service, []serviceRow{row}); err != nil {
		return fmt.Errorf("append service event: %w", err)
	}
	return w.appendIndex(wire, KindService, parent)
}

func setOptionalFloat(validity *uint8, bit uint8, source *float64, target *float64) {
	if source != nil {
		*validity |= 1 << bit
		*target = *source
	}
}

func (w *Writer) appendTest(wire dt5215.StreamEvent, event dt5202.Event) error {
	if event.Test == nil {
		return errors.New("test event payload is missing")
	}
	value := event.Test
	if err := validateIdentity("test", value.TriggerID, value.Timestamp, wire); err != nil {
		return err
	}
	parent, offset := w.test.length, w.words.length
	wordCount, err := uint32Count("test words", len(value.Words))
	if err != nil {
		return err
	}
	if err := appendRows(&w.words, value.Words); err != nil {
		return fmt.Errorf("append test words: %w", err)
	}
	row := testRow{TriggerID: value.TriggerID, Timestamp: value.Timestamp, WordOffset: offset, WordCount: wordCount}
	if err := appendRows(&w.test, []testRow{row}); err != nil {
		return fmt.Errorf("append test event: %w", err)
	}
	return w.appendIndex(wire, KindTest, parent)
}

func uint32Count(name string, count int) (uint32, error) {
	if uint64(count) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("%s count %d exceeds uint32", name, count)
	}
	return uint32(count), nil
}

func compoundTimingEvent() *hdf5.CompoundType {
	var v timingEventRow
	return mustCompound(unsafe.Sizeof(v), []field{{"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"time_reference", unsafe.Offsetof(v.TimeReference), hdf5.T_STD_U64LE}, {"hit_offset", unsafe.Offsetof(v.HitOffset), hdf5.T_STD_U64LE}, {"hit_count", unsafe.Offsetof(v.HitCount), hdf5.T_STD_U32LE}})
}
func compoundCounting() *hdf5.CompoundType {
	var v countingRow
	return mustCompound(unsafe.Sizeof(v), []field{{"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"validity", unsafe.Offsetof(v.Validity), hdf5.T_STD_U8LE}, {"relative_timestamp_clock", unsafe.Offsetof(v.RelativeTimestampClock), hdf5.T_STD_U32LE}, {"channel_mask", unsafe.Offsetof(v.ChannelMask), hdf5.T_STD_U64LE}, {"count_offset", unsafe.Offsetof(v.CountOffset), hdf5.T_STD_U64LE}, {"count_count", unsafe.Offsetof(v.CountCount), hdf5.T_STD_U32LE}, {"t_or_count", unsafe.Offsetof(v.TORCount), hdf5.T_STD_U32LE}, {"q_or_count", unsafe.Offsetof(v.QORCount), hdf5.T_STD_U32LE}})
}
func compoundCount() *hdf5.CompoundType {
	var v countRow
	return mustCompound(unsafe.Sizeof(v), []field{{"parent_row", unsafe.Offsetof(v.ParentRow), hdf5.T_STD_U64LE}, {"channel", unsafe.Offsetof(v.Channel), hdf5.T_STD_U8LE}, {"counter_value", unsafe.Offsetof(v.CounterValue), hdf5.T_STD_U32LE}})
}
func compoundWaveform() *hdf5.CompoundType {
	var v waveformRow
	return mustCompound(unsafe.Sizeof(v), []field{{"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"sample_offset", unsafe.Offsetof(v.SampleOffset), hdf5.T_STD_U64LE}, {"sample_count", unsafe.Offsetof(v.SampleCount), hdf5.T_STD_U32LE}})
}
func compoundSample() *hdf5.CompoundType {
	var v sampleRow
	return mustCompound(unsafe.Sizeof(v), []field{{"parent_row", unsafe.Offsetof(v.ParentRow), hdf5.T_STD_U64LE}, {"sample_index", unsafe.Offsetof(v.SampleIndex), hdf5.T_STD_U32LE}, {"high_gain", unsafe.Offsetof(v.HighGain), hdf5.T_STD_U16LE}, {"low_gain", unsafe.Offsetof(v.LowGain), hdf5.T_STD_U16LE}, {"digital_probes", unsafe.Offsetof(v.DigitalProbes), hdf5.T_STD_U8LE}})
}
func compoundService() *hdf5.CompoundType {
	var v serviceRow
	return mustCompound(unsafe.Sizeof(v), []field{
		{"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"version", unsafe.Offsetof(v.Version), hdf5.T_STD_U8LE}, {"format", unsafe.Offsetof(v.Format), hdf5.T_STD_U8LE}, {"validity", unsafe.Offsetof(v.Validity), hdf5.T_STD_U8LE},
		{"fpga_temperature_c", unsafe.Offsetof(v.FPGATemperature), hdf5.T_IEEE_F64LE}, {"board_temperature_c", unsafe.Offsetof(v.BoardTemperature), hdf5.T_IEEE_F64LE}, {"detector_temperature_c", unsafe.Offsetof(v.DetectorTemperature), hdf5.T_IEEE_F64LE}, {"hv_temperature_c", unsafe.Offsetof(v.HVTemperature), hdf5.T_IEEE_F64LE}, {"hv_voltage_v", unsafe.Offsetof(v.HVVoltage), hdf5.T_IEEE_F64LE}, {"hv_current_a", unsafe.Offsetof(v.HVCurrent), hdf5.T_IEEE_F64LE},
		{"hv_on", unsafe.Offsetof(v.HVOn), hdf5.T_STD_U8LE}, {"hv_ramping", unsafe.Offsetof(v.HVRamping), hdf5.T_STD_U8LE}, {"hv_over_current", unsafe.Offsetof(v.HVOverCurrent), hdf5.T_STD_U8LE}, {"hv_over_voltage", unsafe.Offsetof(v.HVOverVoltage), hdf5.T_STD_U8LE}, {"status", unsafe.Offsetof(v.Status), hdf5.T_STD_U16LE},
		{"counter_offset", unsafe.Offsetof(v.CounterOffset), hdf5.T_STD_U64LE}, {"counter_count", unsafe.Offsetof(v.CounterCount), hdf5.T_STD_U32LE}, {"t_or_count", unsafe.Offsetof(v.TORCount), hdf5.T_STD_U32LE}, {"q_or_count", unsafe.Offsetof(v.QORCount), hdf5.T_STD_U32LE}, {"unknown_offset", unsafe.Offsetof(v.UnknownOffset), hdf5.T_STD_U64LE}, {"unknown_count", unsafe.Offsetof(v.UnknownCount), hdf5.T_STD_U32LE},
	})
}
func compoundCounter() *hdf5.CompoundType {
	var v counterRow
	return mustCompound(unsafe.Sizeof(v), []field{{"parent_row", unsafe.Offsetof(v.ParentRow), hdf5.T_STD_U64LE}, {"channel", unsafe.Offsetof(v.Channel), hdf5.T_STD_U8LE}, {"counter_value", unsafe.Offsetof(v.CounterValue), hdf5.T_STD_U32LE}})
}
func compoundTest() *hdf5.CompoundType {
	var v testRow
	return mustCompound(unsafe.Sizeof(v), []field{{"trigger_id", unsafe.Offsetof(v.TriggerID), hdf5.T_STD_U64LE}, {"timestamp", unsafe.Offsetof(v.Timestamp), hdf5.T_STD_U64LE}, {"word_offset", unsafe.Offsetof(v.WordOffset), hdf5.T_STD_U64LE}, {"word_count", unsafe.Offsetof(v.WordCount), hdf5.T_STD_U32LE}})
}
