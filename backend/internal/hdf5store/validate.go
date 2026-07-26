//go:build hdf5

package hdf5store

import (
	"fmt"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

// Validate checks schema identity, completion state, run-wide ordering, and
// every parent/child range without modifying the file.
func Validate(path string, requireComplete bool) error {
	file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
	if err != nil {
		return fmt.Errorf("open HDF5 file: %w", err)
	}
	defer file.Close()
	root, err := file.OpenGroup("/")
	if err != nil {
		return fmt.Errorf("open root group: %w", err)
	}
	defer root.Close()
	var schema uint32
	if err := readAttribute(root, "schema_version", &schema, hdf5.T_STD_U32LE); err != nil {
		return err
	}
	if schema != SchemaVersion {
		return fmt.Errorf("unsupported HDF5 schema version %d", schema)
	}
	var complete uint8
	if err := readAttribute(root, "complete", &complete, hdf5.T_STD_U8LE); err != nil {
		return err
	}
	if complete > 1 {
		return fmt.Errorf("invalid complete marker %d", complete)
	}
	if requireComplete && complete != 1 {
		return fmt.Errorf("HDF5 file is incomplete")
	}
	var segmentIndex uint32
	if err := readAttribute(root, "segment_index", &segmentIndex, hdf5.T_STD_U32LE); err != nil {
		return err
	}
	var firstSequence uint64
	if err := readAttribute(root, "first_event_sequence", &firstSequence, hdf5.T_STD_U64LE); err != nil {
		return err
	}
	if firstSequence == 0 {
		return fmt.Errorf("invalid first event sequence 0")
	}
	if err := validateEffectiveConfiguration(file); err != nil {
		return err
	}

	index, err := readRows[indexRow](file, "events/index")
	if err != nil {
		return err
	}
	spectroscopy, err := readRows[spectroscopyRow](file, "events/spectroscopy/events")
	if err != nil {
		return err
	}
	observations, err := readRows[observationRow](file, "events/spectroscopy/observations")
	if err != nil {
		return err
	}
	timingEvents, err := readRows[timingEventRow](file, "events/timing/events")
	if err != nil {
		return err
	}
	timingHits, err := readRows[timingRow](file, "events/timing/hits")
	if err != nil {
		return err
	}
	counting, err := readRows[countingRow](file, "events/counting/events")
	if err != nil {
		return err
	}
	counts, err := readRows[countRow](file, "events/counting/counts")
	if err != nil {
		return err
	}
	waveform, err := readRows[waveformRow](file, "events/waveform/events")
	if err != nil {
		return err
	}
	samples, err := readRows[sampleRow](file, "events/waveform/samples")
	if err != nil {
		return err
	}
	service, err := readRows[serviceRow](file, "events/service/events")
	if err != nil {
		return err
	}
	counters, err := readRows[counterRow](file, "events/service/counters")
	if err != nil {
		return err
	}
	unknownLength, err := datasetLength(file, "events/service/unknown_payload")
	if err != nil {
		return err
	}
	tests, err := readRows[testRow](file, "events/test/events")
	if err != nil {
		return err
	}
	wordLength, err := datasetLength(file, "events/test/words")
	if err != nil {
		return err
	}

	kindLengths := map[uint8]int{
		KindSpectroscopy: len(spectroscopy), KindTiming: len(timingEvents), KindCounting: len(counting),
		KindWaveform: len(waveform), KindService: len(service), KindTest: len(tests),
	}
	seen := make(map[uint8][]bool, len(kindLengths))
	for kind, length := range kindLengths {
		seen[kind] = make([]bool, length)
	}
	spectroscopySources := make([]indexRow, len(spectroscopy))
	for row, item := range index {
		if item.Sequence != firstSequence+uint64(row) {
			return fmt.Errorf("events/index row %d has sequence %d", row, item.Sequence)
		}
		length, ok := kindLengths[item.Kind]
		if !ok {
			return fmt.Errorf("events/index row %d has unknown kind %d", row, item.Kind)
		}
		if item.KindRow >= uint64(length) {
			return fmt.Errorf("events/index row %d kind row %d exceeds kind %d length %d", row, item.KindRow, item.Kind, length)
		}
		if seen[item.Kind][item.KindRow] {
			return fmt.Errorf("events/index row %d repeats kind %d row %d", row, item.Kind, item.KindRow)
		}
		seen[item.Kind][item.KindRow] = true
		if item.Kind == KindSpectroscopy {
			spectroscopySources[item.KindRow] = item
		}
	}
	for kind, rows := range seen {
		for row, found := range rows {
			if !found {
				return fmt.Errorf("kind %d row %d is not committed by events/index", kind, row)
			}
		}
	}

	var observationCursor uint64
	for row, item := range spectroscopy {
		if item.ObservationOffset != observationCursor {
			return fmt.Errorf("spectroscopy row %d has non-contiguous observation offset", row)
		}
		if item.ObservationCount == 0 {
			return fmt.Errorf("spectroscopy row %d has no observation or sentinel", row)
		}
		if err := validateRange("spectroscopy observations", row, item.ObservationOffset, item.ObservationCount, len(observations)); err != nil {
			return err
		}
		if err := validateParents("spectroscopy observations", row, item.ObservationOffset, item.ObservationCount, observations, func(v observationRow) uint64 { return v.ParentRow }); err != nil {
			return err
		}
		source := spectroscopySources[row]
		var energyMask uint64
		channels := [64]bool{}
		start := item.ObservationOffset
		stop := start + uint64(item.ObservationCount)
		for offset := start; offset < stop; offset++ {
			observation := observations[offset]
			if observation.Sequence != source.Sequence || observation.Chain != source.Chain ||
				observation.Node != source.Node || observation.Qualifier != source.Qualifier {
				return fmt.Errorf("spectroscopy observation %d source identity does not match index", offset)
			}
			if observation.TriggerID != item.TriggerID || observation.Timestamp != item.Timestamp ||
				observation.Validity != item.Validity ||
				observation.RelativeTimestampClock != item.RelativeTimestampClock ||
				observation.TimeReference != item.TimeReference {
				return fmt.Errorf("spectroscopy observation %d event identity does not match parent", offset)
			}
			if observation.ChannelValid == 0 {
				if item.ObservationCount != 1 || observation.HasEnergy != 0 || observation.HasTiming != 0 {
					return fmt.Errorf("spectroscopy observation %d has invalid sentinel", offset)
				}
				continue
			}
			if observation.Channel >= 64 {
				return fmt.Errorf("spectroscopy observation %d channel %d out of range", offset, observation.Channel)
			}
			if channels[observation.Channel] {
				return fmt.Errorf("spectroscopy row %d repeats observation channel %d", row, observation.Channel)
			}
			channels[observation.Channel] = true
			if observation.HasEnergy == 0 && observation.HasTiming == 0 {
				return fmt.Errorf("spectroscopy observation %d has neither energy nor timing", offset)
			}
			if observation.HasEnergy != 0 {
				energyMask |= uint64(1) << observation.Channel
			} else if observation.LowGain != 0 || observation.HighGain != 0 ||
				observation.HasLowGain != 0 || observation.HasHighGain != 0 || observation.Discriminator != 0 {
				return fmt.Errorf("spectroscopy observation %d has energy values without energy validity", offset)
			}
			if observation.HasTiming == 0 && (observation.ToA != 0 || observation.ToT != 0) {
				return fmt.Errorf("spectroscopy observation %d has timing values without timing validity", offset)
			}
		}
		if energyMask != item.ChannelMask {
			return fmt.Errorf("spectroscopy row %d energy mask 0x%x does not match parent mask 0x%x", row, energyMask, item.ChannelMask)
		}
		observationCursor += uint64(item.ObservationCount)
	}
	if observationCursor != uint64(len(observations)) {
		return fmt.Errorf("spectroscopy observations contain unreferenced rows")
	}
	var hitCursor uint64
	for row, item := range timingEvents {
		if item.HitOffset != hitCursor {
			return fmt.Errorf("timing row %d has non-contiguous hit offset", row)
		}
		if err := validateRange("timing hits", row, item.HitOffset, item.HitCount, len(timingHits)); err != nil {
			return err
		}
		if err := validateParents("timing hits", row, item.HitOffset, item.HitCount, timingHits, func(v timingRow) uint64 { return v.ParentRow }); err != nil {
			return err
		}
		hitCursor += uint64(item.HitCount)
	}
	if hitCursor != uint64(len(timingHits)) {
		return fmt.Errorf("timing hits contains unreferenced rows")
	}
	var countCursor uint64
	for row, item := range counting {
		if item.CountOffset != countCursor {
			return fmt.Errorf("counting row %d has non-contiguous count offset", row)
		}
		if err := validateRange("counting counters", row, item.CountOffset, item.CountCount, len(counts)); err != nil {
			return err
		}
		if err := validateParents("counting counters", row, item.CountOffset, item.CountCount, counts, func(v countRow) uint64 { return v.ParentRow }); err != nil {
			return err
		}
		countCursor += uint64(item.CountCount)
	}
	if countCursor != uint64(len(counts)) {
		return fmt.Errorf("counting counters contains unreferenced rows")
	}
	var sampleCursor uint64
	for row, item := range waveform {
		if item.SampleOffset != sampleCursor {
			return fmt.Errorf("waveform row %d has non-contiguous sample offset", row)
		}
		if err := validateRange("waveform samples", row, item.SampleOffset, item.SampleCount, len(samples)); err != nil {
			return err
		}
		if err := validateParents("waveform samples", row, item.SampleOffset, item.SampleCount, samples, func(v sampleRow) uint64 { return v.ParentRow }); err != nil {
			return err
		}
		sampleCursor += uint64(item.SampleCount)
	}
	if sampleCursor != uint64(len(samples)) {
		return fmt.Errorf("waveform samples contains unreferenced rows")
	}
	var counterCursor, unknownCursor uint64
	for row, item := range service {
		if item.CounterOffset != counterCursor || item.UnknownOffset != unknownCursor {
			return fmt.Errorf("service row %d has non-contiguous child offsets", row)
		}
		if err := validateRange("service counters", row, item.CounterOffset, item.CounterCount, len(counters)); err != nil {
			return err
		}
		if err := validateParents("service counters", row, item.CounterOffset, item.CounterCount, counters, func(v counterRow) uint64 { return v.ParentRow }); err != nil {
			return err
		}
		if err := validateRange("service unknown payload", row, item.UnknownOffset, item.UnknownCount, unknownLength); err != nil {
			return err
		}
		counterCursor += uint64(item.CounterCount)
		unknownCursor += uint64(item.UnknownCount)
	}
	if counterCursor != uint64(len(counters)) || unknownCursor != uint64(unknownLength) {
		return fmt.Errorf("service child datasets contain unreferenced rows")
	}
	var wordCursor uint64
	for row, item := range tests {
		if item.WordOffset != wordCursor {
			return fmt.Errorf("test row %d has non-contiguous word offset", row)
		}
		if err := validateRange("test words", row, item.WordOffset, item.WordCount, wordLength); err != nil {
			return err
		}
		wordCursor += uint64(item.WordCount)
	}
	if wordCursor != uint64(wordLength) {
		return fmt.Errorf("test words contains unreferenced rows")
	}
	return nil
}

func validateEffectiveConfiguration(file *hdf5.File) error {
	boards, err := readRows[configurationBoardRow](file, "configuration/effective/boards")
	if err != nil {
		return err
	}
	channels, err := readRows[configurationChannelRow](file, "configuration/effective/channels")
	if err != nil {
		return err
	}
	chips, err := readRows[configurationChipRow](file, "configuration/effective/citiroc_chips")
	if err != nil {
		return err
	}
	streams, err := readRows[configurationStreamWordRow](file, "configuration/effective/citiroc_stream_words")
	if err != nil {
		return err
	}
	for _, name := range []string{"fpga_writes", "hv_plans", "hv_transactions", "pedestal_plans", "pedestal_channels"} {
		if _, err := datasetLength(file, "configuration/effective/"+name); err != nil {
			return err
		}
	}
	boardSeen := make(map[uint32]bool, len(boards))
	for row, board := range boards {
		if boardSeen[board.Board] {
			return fmt.Errorf("configuration/effective/boards row %d repeats board %d", row, board.Board)
		}
		boardSeen[board.Board] = true
	}
	channelSeen := make(map[[2]uint32]bool, len(channels))
	for row, channel := range channels {
		if channel.Channel >= dt5202.ChannelCount || channel.Chip != channel.Channel/32 || channel.ChipChannel != channel.Channel%32 {
			return fmt.Errorf("configuration/effective/channels row %d has invalid board/channel mapping", row)
		}
		key := [2]uint32{channel.Board, uint32(channel.Channel)}
		if channelSeen[key] {
			return fmt.Errorf("configuration/effective/channels row %d repeats board %d channel %d", row, channel.Board, channel.Channel)
		}
		channelSeen[key] = true
	}
	chipSeen := make(map[[2]uint32]bool, len(chips))
	for row, chip := range chips {
		if chip.Chip > 1 {
			return fmt.Errorf("configuration/effective/citiroc_chips row %d has invalid chip %d", row, chip.Chip)
		}
		key := [2]uint32{chip.Board, uint32(chip.Chip)}
		if chipSeen[key] {
			return fmt.Errorf("configuration/effective/citiroc_chips row %d repeats board %d chip %d", row, chip.Board, chip.Chip)
		}
		chipSeen[key] = true
	}
	streamSeen := make(map[[3]uint32]bool, len(streams))
	for row, stream := range streams {
		if stream.Chip > 1 || stream.WordIndex >= dt5202.CitirocWordCount || stream.BitCount != dt5202.CitirocBitCount {
			return fmt.Errorf("configuration/effective/citiroc_stream_words row %d has invalid layout", row)
		}
		key := [3]uint32{stream.Board, uint32(stream.Chip), uint32(stream.WordIndex)}
		if streamSeen[key] {
			return fmt.Errorf("configuration/effective/citiroc_stream_words row %d repeats a stream word", row)
		}
		streamSeen[key] = true
	}
	if len(channels) != len(chips)*32 || len(streams) != len(chips)*dt5202.CitirocWordCount {
		return fmt.Errorf("effective Citiroc table cardinalities are inconsistent")
	}
	return nil
}

func readAttribute(group *hdf5.Group, name string, destination any, datatype *hdf5.Datatype) error {
	attribute, err := group.OpenAttribute(name)
	if err != nil {
		return fmt.Errorf("open attribute %s: %w", name, err)
	}
	defer attribute.Close()
	if err := attribute.Read(destination, datatype); err != nil {
		return fmt.Errorf("read attribute %s: %w", name, err)
	}
	return nil
}

func readRows[T any](file *hdf5.File, name string) ([]T, error) {
	dataset, err := file.OpenDataset(name)
	if err != nil {
		return nil, fmt.Errorf("open dataset %s: %w", name, err)
	}
	defer dataset.Close()
	length, err := openDatasetLength(dataset, name)
	if err != nil {
		return nil, err
	}
	rows := make([]T, length)
	if length > 0 {
		if err := dataset.Read(&rows); err != nil {
			return nil, fmt.Errorf("read dataset %s: %w", name, err)
		}
	}
	return rows, nil
}

func datasetLength(file *hdf5.File, name string) (int, error) {
	dataset, err := file.OpenDataset(name)
	if err != nil {
		return 0, fmt.Errorf("open dataset %s: %w", name, err)
	}
	defer dataset.Close()
	return openDatasetLength(dataset, name)
}

func openDatasetLength(dataset *hdf5.Dataset, name string) (int, error) {
	space := dataset.Space()
	if space == nil {
		return 0, fmt.Errorf("dataset %s has no dataspace", name)
	}
	defer space.Close()
	dimensions, _, err := space.SimpleExtentDims()
	if err != nil {
		return 0, fmt.Errorf("inspect dataset %s: %w", name, err)
	}
	if len(dimensions) != 1 {
		return 0, fmt.Errorf("dataset %s has rank %d, want 1", name, len(dimensions))
	}
	if uint64(dimensions[0]) > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("dataset %s length exceeds platform int", name)
	}
	return int(dimensions[0]), nil
}

func validateRange(name string, parent int, offset uint64, count uint32, childLength int) error {
	end := offset + uint64(count)
	if end < offset || end > uint64(childLength) {
		return fmt.Errorf("%s parent %d range [%d:%d] exceeds child length %d", name, parent, offset, end, childLength)
	}
	return nil
}

func validateParents[T any](name string, parent int, offset uint64, count uint32, children []T, parentOf func(T) uint64) error {
	end := offset + uint64(count)
	for child := offset; child < end; child++ {
		if got := parentOf(children[int(child)]); got != uint64(parent) {
			return fmt.Errorf("%s row %d points to parent %d, want %d", name, child, got, parent)
		}
	}
	return nil
}
