//go:build !hdf5

package staircasestore

import (
	"path/filepath"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
)

func buildArtifact(dir string, _ Manifest, _ []staircase.Point) (string, string, error) {
	return filepath.Join(dir, "points.jsonl"), "staircase-jsonl", nil
}
