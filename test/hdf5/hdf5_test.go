package hdf5_test

import (
	"path/filepath"
	"slices"
	"testing"

	hdf5 "github.com/next-exp/hdf5-go"
)

func TestBloscRoundTrip(t *testing.T) {
	version, date, err := hdf5.RegisterBlosc()
	if err != nil {
		t.Fatalf("register Blosc: %v", err)
	}
	if version == "" || date == "" {
		t.Fatalf("Blosc registration returned incomplete identity: version=%q date=%q", version, date)
	}

	path := filepath.Join(t.TempDir(), "go-blosc-round-trip.h5")
	file, err := hdf5.CreateFile(path, hdf5.F_ACC_TRUNC)
	if err != nil {
		t.Fatalf("create HDF5 file: %v", err)
	}

	space, err := hdf5.CreateSimpleDataspace([]uint{8}, []uint{8})
	if err != nil {
		t.Fatalf("create dataspace: %v", err)
	}
	properties, err := hdf5.NewPropList(hdf5.P_DATASET_CREATE)
	if err != nil {
		t.Fatalf("create dataset properties: %v", err)
	}
	if err := properties.SetChunk([]uint{8}); err != nil {
		t.Fatalf("set chunk shape: %v", err)
	}
	if err := hdf5.ConfigureBloscFilter(
		properties,
		hdf5.BLOSC_ZSTD,
		5,
		hdf5.BLOSC_BITSHUFFLE,
	); err != nil {
		t.Fatalf("configure Blosc: %v", err)
	}

	dataset, err := file.CreateDatasetWith(
		"fibonacci",
		hdf5.T_STD_U32LE,
		space,
		properties,
	)
	if err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	expected := []uint32{0, 1, 1, 2, 3, 5, 8, 13}
	if err := dataset.Write(&expected[0]); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
	actual := make([]uint32, len(expected))
	if err := dataset.Read(&actual[0]); err != nil {
		t.Fatalf("read dataset: %v", err)
	}
	if !slices.Equal(actual, expected) {
		t.Fatalf("round trip mismatch: got %v, want %v", actual, expected)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("close dataset: %v", err)
	}
	if err := properties.Close(); err != nil {
		t.Fatalf("close properties: %v", err)
	}
	if err := space.Close(); err != nil {
		t.Fatalf("close dataspace: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	file, err = hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatalf("reopen HDF5 file: %v", err)
	}
	dataset, err = file.OpenDataset("fibonacci")
	if err != nil {
		t.Fatalf("reopen dataset: %v", err)
	}
	reopened := make([]uint32, len(expected))
	if err := dataset.Read(&reopened[0]); err != nil {
		t.Fatalf("read reopened dataset: %v", err)
	}
	if !slices.Equal(reopened, expected) {
		t.Fatalf("reopened round trip mismatch: got %v, want %v", reopened, expected)
	}
	if err := dataset.Close(); err != nil {
		t.Fatalf("close reopened dataset: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close reopened file: %v", err)
	}
}
