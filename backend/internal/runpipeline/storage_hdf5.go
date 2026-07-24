//go:build hdf5

package runpipeline

import (
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/hdf5store"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func createRunWriter(parent string, manifest runstore.Manifest) (runWriter, error) {
	return hdf5store.CreateRun(parent, manifest)
}

func decodedArtifactName(runID string) string { return "run_" + runID + ".0000.h5" }
func expectedStorageFormat() string           { return "hdf5" }
func histogramPersistenceSupported() bool     { return true }

func storageIdentity(options acquisition.RunOptions) runstore.StorageIdentity {
	compression := options.HDF5Compression
	if compression == "" {
		compression = runstore.HDF5CompressionBloscLZ4
	}
	return runstore.StorageIdentity{Format: "hdf5", WriterVersion: hdf5store.SchemaVersion, Compression: compression}
}
