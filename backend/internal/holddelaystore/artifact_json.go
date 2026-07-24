//go:build !hdf5

package holddelaystore

import (
	"path/filepath"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
)

func buildArtifact(dir string, _ Manifest, _ []holddelay.Point) (string, string, error) {
	return filepath.Join(dir, "points.jsonl"), "hold-delay-jsonl", nil
}
