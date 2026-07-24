//go:build hdf5

package staircasestore

import (
	"encoding/json"
	"path/filepath"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/hdf5store"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
)

func buildArtifact(dir string, manifest Manifest, points []staircase.Point) (string, string, error) {
	path := filepath.Join(dir, "staircase.h5")
	metadata, err := json.Marshal(manifest)
	if err != nil {
		return "", "", err
	}
	if err := hdf5store.WriteStaircase(path, metadata, points); err != nil {
		return "", "", err
	}
	return path, "staircase-hdf5", nil
}
