//go:build !hdf5

package runpipeline

import "github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"

func createRunWriter(parent string, manifest runstore.Manifest) (runWriter, error) {
	return runstore.Create(parent, manifest)
}

func decodedArtifactName() string   { return "events.jsonl" }
func expectedStorageFormat() string { return "jsonl" }

func storageIdentity() runstore.StorageIdentity {
	return runstore.StorageIdentity{Format: "jsonl", WriterVersion: runstore.SchemaVersion, Compression: "none"}
}
