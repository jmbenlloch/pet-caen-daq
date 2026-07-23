//go:build hdf5

package hdf5store

import (
	"fmt"

	hdf5 "github.com/next-exp/hdf5-go"
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

	index, err := readRows[indexRow](file, "events/index")
	if err != nil {
		return err
	}
	spectroscopy, err := readRows[spectroscopyRow](file, "events/spectroscopy/events")
	if err != nil {
		return err
	}
	energies, err := readRows[energyRow](file, "events/spectroscopy/energies")
	if err != nil {
		return err
	}
	spectroscopyTimings, err := readRows[timingRow](file, "events/spectroscopy/timings")
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
	for row, item := range index {
		if item.Sequence != uint64(row+1) {
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
	}
	for kind, rows := range seen {
		for row, found := range rows {
			if !found {
				return fmt.Errorf("kind %d row %d is not committed by events/index", kind, row)
			}
		}
	}

	for row, item := range spectroscopy {
		if err := validateRange("spectroscopy energies", row, item.EnergyOffset, item.EnergyCount, len(energies)); err != nil {
			return err
		}
		if err := validateParents("spectroscopy energies", row, item.EnergyOffset, item.EnergyCount, energies, func(v energyRow) uint64 { return v.ParentRow }); err != nil {
			return err
		}
		if err := validateRange("spectroscopy timings", row, item.TimingOffset, item.TimingCount, len(spectroscopyTimings)); err != nil {
			return err
		}
		if err := validateParents("spectroscopy timings", row, item.TimingOffset, item.TimingCount, spectroscopyTimings, func(v timingRow) uint64 { return v.ParentRow }); err != nil {
			return err
		}
	}
	for row, item := range timingEvents {
		if err := validateRange("timing hits", row, item.HitOffset, item.HitCount, len(timingHits)); err != nil {
			return err
		}
		if err := validateParents("timing hits", row, item.HitOffset, item.HitCount, timingHits, func(v timingRow) uint64 { return v.ParentRow }); err != nil {
			return err
		}
	}
	for row, item := range counting {
		if err := validateRange("counting counters", row, item.CountOffset, item.CountCount, len(counts)); err != nil {
			return err
		}
		if err := validateParents("counting counters", row, item.CountOffset, item.CountCount, counts, func(v countRow) uint64 { return v.ParentRow }); err != nil {
			return err
		}
	}
	for row, item := range waveform {
		if err := validateRange("waveform samples", row, item.SampleOffset, item.SampleCount, len(samples)); err != nil {
			return err
		}
		if err := validateParents("waveform samples", row, item.SampleOffset, item.SampleCount, samples, func(v sampleRow) uint64 { return v.ParentRow }); err != nil {
			return err
		}
	}
	for row, item := range service {
		if err := validateRange("service counters", row, item.CounterOffset, item.CounterCount, len(counters)); err != nil {
			return err
		}
		if err := validateParents("service counters", row, item.CounterOffset, item.CounterCount, counters, func(v counterRow) uint64 { return v.ParentRow }); err != nil {
			return err
		}
		if err := validateRange("service unknown payload", row, item.UnknownOffset, item.UnknownCount, unknownLength); err != nil {
			return err
		}
	}
	for row, item := range tests {
		if err := validateRange("test words", row, item.WordOffset, item.WordCount, wordLength); err != nil {
			return err
		}
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
