//go:build hdf5

package runpipeline

import (
	"errors"
	"fmt"
	"path/filepath"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/hdf5store"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func LoadPersistedHistograms(parent, runID string, kind HistogramKind, selections []HistogramSelection) ([]HistogramDataset, error) {
	manifest, err := runstore.ReadManifest(filepath.Join(parent, "run-"+runID), runID)
	if err != nil {
		return nil, err
	}
	artifactName := ""
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == "histograms" {
			artifactName = artifact.Name
			break
		}
	}
	if artifactName == "" {
		return nil, runstore.ErrArtifactNotFound
	}
	artifact, _, err := runstore.OpenArtifact(parent, runID, artifactName)
	if err != nil {
		return nil, err
	}
	path := artifact.Name()
	if err := artifact.Close(); err != nil {
		return nil, fmt.Errorf("close verified histogram artifact: %w", err)
	}
	runtime := manifest.ExecutionIdentity.Runtime
	energyChannels := runtime.EnergyHistogramChannels
	if energyChannels == 0 {
		energyChannels = 1 << 13
	}
	options := acquisition.HistogramOptions{
		EnergyBins: runtime.EnergyHistogramBins, EnergyChannels: energyChannels,
		ToABins: runtime.ToAHistogramBins, ToARebin: runtime.ToAHistogramRebin,
		ToAMinNS: runtime.ToAHistogramMinNS, ToTBins: runtime.ToTHistogramBins,
	}
	binCount, minimum, width, err := histogramSpec(options, kind)
	if err != nil || binCount <= 0 {
		return nil, fmt.Errorf("histogram %s is disabled", kind)
	}
	file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("open histogram artifact: %w", err)
	}
	defer file.Close()
	root, err := file.OpenGroup("/")
	if err != nil {
		return nil, fmt.Errorf("open histogram root: %w", err)
	}
	defer root.Close()
	complete, err := root.OpenAttribute("complete")
	if err != nil {
		return nil, fmt.Errorf("open histogram completion marker: %w", err)
	}
	var marker uint8
	readErr := complete.Read(&marker, hdf5.T_STD_U8LE)
	closeErr := complete.Close()
	if err := errors.Join(readErr, closeErr); err != nil || marker != 1 {
		if err != nil {
			return nil, fmt.Errorf("read histogram completion marker: %w", err)
		}
		return nil, errors.New("histogram artifact is incomplete")
	}
	if file.LinkExists(fmt.Sprintf("histograms/%s/spectra", kind)) {
		return readPersistedHistogramV2(file, kind, selections, binCount, minimum, width)
	}
	result := make([]HistogramDataset, 0, len(selections))
	for _, selection := range selections {
		dataset := HistogramDataset{
			HistogramSelection: selection, Minimum: minimum, BinWidth: width, Bins: make([]uint32, binCount),
		}
		name := fmt.Sprintf("histograms/%s_%d_%d_%d", kind, selection.Chain, selection.Node, selection.Channel)
		if file.LinkExists(name) {
			if err := readPersistedHistogram(file, name, &dataset); err != nil {
				return nil, err
			}
		}
		result = append(result, dataset)
	}
	return result, nil
}

func readPersistedHistogramV2(file *hdf5.File, kind HistogramKind, selections []HistogramSelection, binCount int, minimum, width float64) ([]HistogramDataset, error) {
	base := fmt.Sprintf("histograms/%s", kind)
	spectra, err := file.OpenDataset(base + "/spectra")
	if err != nil {
		return nil, fmt.Errorf("open histogram %s spectra: %w", kind, err)
	}
	space := spectra.Space()
	if space == nil {
		spectra.Close()
		return nil, fmt.Errorf("histogram %s spectra has no dataspace", kind)
	}
	dimensions, _, dimensionsErr := space.SimpleExtentDims()
	space.Close()
	if dimensionsErr != nil {
		spectra.Close()
		return nil, fmt.Errorf("inspect histogram %s spectra: %w", kind, dimensionsErr)
	}
	if len(dimensions) != 1 {
		spectra.Close()
		return nil, fmt.Errorf("histogram %s spectra has dimensions %v", kind, dimensions)
	}
	rows := make([]hdf5store.HistogramSpectrumRow, dimensions[0])
	readErr := spectra.Read(&rows)
	closeErr := spectra.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("read histogram %s spectra: %w", kind, err)
	}
	index := make(map[HistogramSelection]hdf5store.HistogramSpectrumRow, len(rows))
	for _, row := range rows {
		selection := HistogramSelection{Chain: row.Chain, Node: row.Node, Channel: row.Channel}
		if _, exists := index[selection]; exists {
			return nil, fmt.Errorf("histogram %s contains duplicate spectrum %+v", kind, selection)
		}
		index[selection] = row
	}
	bins, err := file.OpenDataset(base + "/bins")
	if err != nil {
		return nil, fmt.Errorf("open histogram %s bins: %w", kind, err)
	}
	defer bins.Close()
	result := make([]HistogramDataset, 0, len(selections))
	for _, selection := range selections {
		target := HistogramDataset{HistogramSelection: selection, Minimum: minimum, BinWidth: width, Bins: make([]uint32, binCount)}
		row, exists := index[selection]
		if exists {
			if int(row.BinCount) != binCount {
				return nil, fmt.Errorf("histogram %s %+v has %d bins, want %d", kind, selection, row.BinCount, binCount)
			}
			target.Minimum, target.BinWidth = row.Minimum, row.BinWidth
			target.Entries, target.Underflow, target.Overflow = row.Entries, row.Underflow, row.Overflow
			if err := readHistogramBinRange(bins, row.BinOffset, target.Bins); err != nil {
				return nil, fmt.Errorf("read histogram %s %+v: %w", kind, selection, err)
			}
		}
		result = append(result, target)
	}
	return result, nil
}

func readHistogramBinRange(dataset *hdf5.Dataset, offset uint64, target []uint32) error {
	if uint64(uint(offset)) != offset {
		return errors.New("histogram bin offset exceeds platform address space")
	}
	fileSpace := dataset.Space()
	if fileSpace == nil {
		return errors.New("histogram bins has no dataspace")
	}
	defer fileSpace.Close()
	if err := fileSpace.SelectHyperslab([]uint{uint(offset)}, nil, []uint{uint(len(target))}, nil); err != nil {
		return err
	}
	memorySpace, err := hdf5.CreateSimpleDataspace([]uint{uint(len(target))}, nil)
	if err != nil {
		return err
	}
	defer memorySpace.Close()
	return dataset.ReadSubset(&target, memorySpace, fileSpace)
}

func readPersistedHistogram(file *hdf5.File, name string, target *HistogramDataset) error {
	dataset, err := file.OpenDataset(name)
	if err != nil {
		return fmt.Errorf("open histogram %s: %w", name, err)
	}
	defer dataset.Close()
	space := dataset.Space()
	if space == nil {
		return fmt.Errorf("histogram %s has no dataspace", name)
	}
	dimensions, _, err := space.SimpleExtentDims()
	space.Close()
	if err != nil {
		return fmt.Errorf("inspect histogram %s: %w", name, err)
	}
	if len(dimensions) != 1 || int(dimensions[0]) != len(target.Bins) {
		return fmt.Errorf("histogram %s has %v bins, want %d", name, dimensions, len(target.Bins))
	}
	if err := dataset.Read(&target.Bins); err != nil {
		return fmt.Errorf("read histogram %s: %w", name, err)
	}
	if target.Entries, err = readHistogramUint64Attribute(dataset, "entries"); err != nil {
		return err
	}
	if target.Underflow, err = readHistogramUint64Attribute(dataset, "underflow"); err != nil {
		return err
	}
	target.Overflow, err = readHistogramUint64Attribute(dataset, "overflow")
	return err
}

func readHistogramUint64Attribute(dataset *hdf5.Dataset, name string) (uint64, error) {
	attribute, err := dataset.OpenAttribute(name)
	if err != nil {
		return 0, fmt.Errorf("open histogram attribute %s: %w", name, err)
	}
	var value uint64
	readErr := attribute.Read(&value, hdf5.T_STD_U64LE)
	closeErr := attribute.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return 0, fmt.Errorf("read histogram attribute %s: %w", name, err)
	}
	return value, nil
}
