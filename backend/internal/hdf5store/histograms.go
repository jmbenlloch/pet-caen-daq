//go:build hdf5

package hdf5store

import (
	"errors"
	"fmt"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

const HistogramSchemaVersion = 1

// SaveHistograms creates one self-contained HDF5 artifact for a finalized run.
// Each lazily allocated channel spectrum is stored as a compressed uint32
// dataset; run-wide counters and axis information remain uint64/float64
// attributes.
func SaveHistograms(path, runID string, histograms []runstore.HistogramDataset) (err error) {
	file, err := hdf5.CreateFile(path, hdf5.F_ACC_EXCL)
	if err != nil {
		return fmt.Errorf("create histogram HDF5 file: %w", err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	root, err := file.OpenGroup("/")
	if err != nil {
		return fmt.Errorf("open histogram root: %w", err)
	}
	defer root.Close()
	if err := writeUintAttribute(root, "schema_version", HistogramSchemaVersion); err != nil {
		return err
	}
	complete, err := createUint8Attribute(root, "complete", 0)
	if err != nil {
		return err
	}
	defer complete.Close()
	if err := createBytes(root, "format", []byte("pet-caen-daq-histograms")); err != nil {
		return err
	}
	if err := createBytes(root, "run_id", []byte(runID)); err != nil {
		return err
	}
	group, err := file.CreateGroup("histograms")
	if err != nil {
		return fmt.Errorf("create histograms group: %w", err)
	}
	defer group.Close()
	for _, histogram := range histograms {
		name := fmt.Sprintf("%s_%d_%d_%d", histogram.Kind, histogram.Chain, histogram.Node, histogram.Channel)
		dataset, err := createHistogramPrimitive(group, name, hdf5.T_STD_U32LE)
		if err != nil {
			return err
		}
		target := table{dataset: dataset}
		if err := appendRows(&target, histogram.Bins); err != nil {
			dataset.Close()
			return fmt.Errorf("write histogram %s: %w", name, err)
		}
		if err := writeHistogramAttributes(dataset, histogram); err != nil {
			dataset.Close()
			return fmt.Errorf("describe histogram %s: %w", name, err)
		}
		if err := dataset.Close(); err != nil {
			return fmt.Errorf("close histogram %s: %w", name, err)
		}
	}
	value := uint8(1)
	if err := complete.Write(&value, hdf5.T_STD_U8LE); err != nil {
		return fmt.Errorf("mark histogram file complete: %w", err)
	}
	if err := file.Flush(hdf5.F_SCOPE_GLOBAL); err != nil {
		return fmt.Errorf("flush histogram file: %w", err)
	}
	return nil
}

func writeHistogramAttributes(dataset *hdf5.Dataset, value runstore.HistogramDataset) error {
	for name, number := range map[string]uint64{
		"entries": value.Entries, "underflow": value.Underflow, "overflow": value.Overflow,
	} {
		if err := writeDatasetUint64Attribute(dataset, name, number); err != nil {
			return err
		}
	}
	for name, number := range map[string]uint32{
		"chain": uint32(value.Chain), "node": uint32(value.Node), "channel": uint32(value.Channel),
	} {
		if err := writeDatasetUint32Attribute(dataset, name, number); err != nil {
			return err
		}
	}
	if err := writeDatasetFloat64Attribute(dataset, "minimum", value.Minimum); err != nil {
		return err
	}
	return writeDatasetFloat64Attribute(dataset, "bin_width", value.BinWidth)
}

func writeDatasetUint32Attribute(dataset *hdf5.Dataset, name string, value uint32) error {
	attribute, err := createDatasetAttribute(dataset, name, hdf5.T_STD_U32LE)
	if err != nil {
		return err
	}
	defer attribute.Close()
	return attribute.Write(&value, hdf5.T_STD_U32LE)
}

func writeDatasetUint64Attribute(dataset *hdf5.Dataset, name string, value uint64) error {
	attribute, err := createDatasetAttribute(dataset, name, hdf5.T_STD_U64LE)
	if err != nil {
		return err
	}
	defer attribute.Close()
	return attribute.Write(&value, hdf5.T_STD_U64LE)
}

func writeDatasetFloat64Attribute(dataset *hdf5.Dataset, name string, value float64) error {
	attribute, err := createDatasetAttribute(dataset, name, hdf5.T_IEEE_F64LE)
	if err != nil {
		return err
	}
	defer attribute.Close()
	return attribute.Write(&value, hdf5.T_IEEE_F64LE)
}

func createDatasetAttribute(dataset *hdf5.Dataset, name string, datatype *hdf5.Datatype) (*hdf5.Attribute, error) {
	space, err := hdf5.CreateDataspace(hdf5.S_SCALAR)
	if err != nil {
		return nil, err
	}
	defer space.Close()
	attribute, err := dataset.CreateAttribute(name, datatype, space)
	if err != nil {
		return nil, fmt.Errorf("create attribute %s: %w", name, err)
	}
	return attribute, nil
}
