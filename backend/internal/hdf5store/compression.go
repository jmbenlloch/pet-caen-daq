//go:build hdf5

package hdf5store

import (
	"fmt"
	"os"
	"sync"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	hdf5 "github.com/next-exp/hdf5-go"
)

const (
	CompressionEnvironment = "PET_CAEN_HDF5_COMPRESSION"
	CompressionNone        = runstore.HDF5CompressionNone
	CompressionBloscLZ4    = runstore.HDF5CompressionBloscLZ4
)

var registerBloscOnce sync.Once
var registerBloscErr error

func compressionName() (string, error) {
	return compressionNameWithDefault(CompressionNone)
}

func histogramCompressionName() (string, error) {
	return compressionNameWithDefault(CompressionBloscLZ4)
}

func compressionNameWithDefault(defaultName string) (string, error) {
	switch value := os.Getenv(CompressionEnvironment); value {
	case "":
		return defaultName, nil
	case CompressionNone:
		return CompressionNone, nil
	case CompressionBloscLZ4:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported HDF5 compression %q", value)
	}
}

func configureNamedCompression(properties *hdf5.PropList, name string, err error) error {
	if err != nil {
		return err
	}
	if name == CompressionNone {
		return nil
	}
	registerBloscOnce.Do(func() {
		_, _, registerBloscErr = hdf5.RegisterBlosc()
	})
	if registerBloscErr != nil {
		return fmt.Errorf("register Blosc: %w", registerBloscErr)
	}
	if err := hdf5.ConfigureBloscFilter(
		properties,
		hdf5.BLOSC_LZ4,
		4,
		hdf5.BLOSC_BITSHUFFLE,
	); err != nil {
		return fmt.Errorf("configure Blosc LZ4 level 4 bit-shuffle: %w", err)
	}
	return nil
}
