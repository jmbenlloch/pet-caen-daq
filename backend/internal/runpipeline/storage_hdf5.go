//go:build hdf5

package runpipeline

import (
	"os"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/hdf5store"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func createRunWriter(parent string, manifest runstore.Manifest) (runWriter, error) {
	return hdf5store.CreateRun(parent, manifest)
}

func decodedArtifactName() string   { return "events.0000.h5" }
func expectedStorageFormat() string { return "hdf5" }

func storageIdentity() runstore.StorageIdentity {
	compression := "none"
	if value := os.Getenv(hdf5store.CompressionEnvironment); value != "" {
		compression = value
	}
	return runstore.StorageIdentity{Format: "hdf5", WriterVersion: hdf5store.SchemaVersion, Compression: compression}
}
