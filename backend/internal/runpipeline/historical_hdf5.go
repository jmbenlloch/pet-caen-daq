//go:build hdf5

package runpipeline

import (
	"errors"
	"fmt"
	"path/filepath"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
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
