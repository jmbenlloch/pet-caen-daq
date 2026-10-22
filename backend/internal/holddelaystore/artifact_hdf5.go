//go:build hdf5

package holddelaystore

import (
	"encoding/json"
	"path/filepath"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/hdf5store"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/scanrecord"
)

func buildArtifact(dir string, manifest Manifest, points []holddelay.Point) (string, string, error) {
	path := filepath.Join(dir, "hold-delay.h5")
	metadata, err := json.Marshal(manifest)
	if err != nil {
		return "", "", err
	}
	evidence, err := scanrecord.Read(dir)
	if err != nil {
		return "", "", err
	}
	if err := hdf5store.WriteHoldDelay(path, metadata, points, evidence); err != nil {
		return "", "", err
	}
	return path, "hold-delay-hdf5", nil
}
