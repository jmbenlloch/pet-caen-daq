//go:build !hdf5

package runpipeline

import "github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"

func createRunWriter(parent string, manifest runstore.Manifest) (runWriter, error) {
	return runstore.Create(parent, manifest)
}

func decodedArtifactName() string { return "events.jsonl" }
