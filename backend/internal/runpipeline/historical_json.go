//go:build !hdf5

package runpipeline

import "fmt"

func LoadPersistedHistograms(string, string, HistogramKind, []HistogramSelection) ([]HistogramDataset, error) {
	return nil, fmt.Errorf("historical histogram reads require an HDF5-enabled build")
}
