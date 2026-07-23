//go:build hdf5

package hdf5store

import (
	"fmt"
	"os"
	"sync"

	hdf5 "github.com/next-exp/hdf5-go"
)

const (
	CompressionEnvironment = "PET_CAEN_HDF5_COMPRESSION"
	CompressionNone        = "none"
	CompressionBloscLZ4    = "blosc-lz4-level4-bitshuffle"
)

var registerBloscOnce sync.Once
var registerBloscErr error

func compressionName() (string, error) {
	switch value := os.Getenv(CompressionEnvironment); value {
	case "", CompressionNone:
		return CompressionNone, nil
	case CompressionBloscLZ4:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported HDF5 compression %q", value)
	}
}

func configureCompression(properties *hdf5.PropList) error {
	name, err := compressionName()
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
