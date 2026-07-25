//go:build hdf5

package hdf5store

import (
	"errors"
	"fmt"
	"sort"
	"unsafe"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

const HistogramSchemaVersion = 2

// HistogramSpectrumRow locates one channel spectrum in its kind's concatenated
// bins dataset. Axis and counter metadata travel with the row so a copied
// histogram artifact remains self-describing.
type HistogramSpectrumRow struct {
	BinOffset            uint64
	Entries              uint64
	Underflow            uint64
	Overflow             uint64
	Minimum              float64
	BinWidth             float64
	BinCount             uint32
	Chain, Node, Channel uint8
}

// SaveHistograms creates one self-contained HDF5 artifact for a finalized run.
// Each histogram kind uses one compressed bins dataset and one spectrum index
// table, avoiding thousands of per-channel HDF5 objects and attributes.
func SaveHistograms(path, runID string, histograms []runstore.HistogramDataset) (err error) {
	compression, err := histogramCompressionName()
	if err != nil {
		return err
	}
	return saveHistograms(path, runID, histograms, compression)
}

func saveHistograms(path, runID string, histograms []runstore.HistogramDataset, compression string) (err error) {
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
	sort.Slice(histograms, func(i, j int) bool {
		if histograms[i].Kind != histograms[j].Kind {
			return histograms[i].Kind < histograms[j].Kind
		}
		if histograms[i].Chain != histograms[j].Chain {
			return histograms[i].Chain < histograms[j].Chain
		}
		if histograms[i].Node != histograms[j].Node {
			return histograms[i].Node < histograms[j].Node
		}
		return histograms[i].Channel < histograms[j].Channel
	})
	for start := 0; start < len(histograms); {
		end := start + 1
		for end < len(histograms) && histograms[end].Kind == histograms[start].Kind {
			end++
		}
		if err := writeHistogramKind(group, histograms[start:end], compression); err != nil {
			return err
		}
		start = end
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

func writeHistogramKind(parent *hdf5.Group, histograms []runstore.HistogramDataset, compression string) (err error) {
	kind := histograms[0].Kind
	group, err := parent.CreateGroup(kind)
	if err != nil {
		return fmt.Errorf("create histogram kind %s: %w", kind, err)
	}
	defer func() { err = errors.Join(err, group.Close()) }()
	binDataset, err := createPrimitiveNamed(group, "bins", hdf5.T_STD_U32LE, compression)
	if err != nil {
		return err
	}
	defer binDataset.Close()
	spectrumDataset, err := createTable(group, "spectra", compoundHistogramSpectrum(), compression)
	if err != nil {
		return err
	}
	defer spectrumDataset.Close()
	var bins []uint32
	rows := make([]HistogramSpectrumRow, 0, len(histograms))
	for _, histogram := range histograms {
		if uint64(len(histogram.Bins)) > uint64(^uint32(0)) {
			return fmt.Errorf("histogram %s %d/%d/%d has too many bins", kind, histogram.Chain, histogram.Node, histogram.Channel)
		}
		rows = append(rows, HistogramSpectrumRow{
			BinOffset: uint64(len(bins)), BinCount: uint32(len(histogram.Bins)),
			Entries: histogram.Entries, Underflow: histogram.Underflow, Overflow: histogram.Overflow,
			Minimum: histogram.Minimum, BinWidth: histogram.BinWidth,
			Chain: histogram.Chain, Node: histogram.Node, Channel: histogram.Channel,
		})
		bins = append(bins, histogram.Bins...)
	}
	if err := appendRows(&table{dataset: binDataset}, bins); err != nil {
		return fmt.Errorf("write histogram %s bins: %w", kind, err)
	}
	if err := appendRows(&table{dataset: spectrumDataset}, rows); err != nil {
		return fmt.Errorf("write histogram %s spectra: %w", kind, err)
	}
	return nil
}

func compoundHistogramSpectrum() *hdf5.CompoundType {
	var value HistogramSpectrumRow
	return mustCompound(unsafe.Sizeof(value), []field{
		{"bin_offset", unsafe.Offsetof(value.BinOffset), hdf5.T_STD_U64LE},
		{"entries", unsafe.Offsetof(value.Entries), hdf5.T_STD_U64LE},
		{"underflow", unsafe.Offsetof(value.Underflow), hdf5.T_STD_U64LE},
		{"overflow", unsafe.Offsetof(value.Overflow), hdf5.T_STD_U64LE},
		{"minimum", unsafe.Offsetof(value.Minimum), hdf5.T_IEEE_F64LE},
		{"bin_width", unsafe.Offsetof(value.BinWidth), hdf5.T_IEEE_F64LE},
		{"bin_count", unsafe.Offsetof(value.BinCount), hdf5.T_STD_U32LE},
		{"chain", unsafe.Offsetof(value.Chain), hdf5.T_STD_U8LE},
		{"node", unsafe.Offsetof(value.Node), hdf5.T_STD_U8LE},
		{"channel", unsafe.Offsetof(value.Channel), hdf5.T_STD_U8LE},
	})
}
