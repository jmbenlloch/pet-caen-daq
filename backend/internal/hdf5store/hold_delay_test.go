//go:build hdf5

package hdf5store

import (
	"path/filepath"
	"testing"
	"time"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
)

func TestWriteHoldDelayStoresSparseSpectra(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hold-delay.h5")
	point := holddelay.Point{
		DelayNS: 16, EffectiveDelay: 16, EventsCollected: 10, Elapsed: 25 * time.Millisecond,
	}
	point.HighGainBins[4][123] = 9
	point.MissingChannels[7] = 2
	if err := WriteHoldDelay(path, []byte(`{"scan_id":"81"}`), []holddelay.Point{point}); err != nil {
		t.Fatal(err)
	}
	file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	points, err := file.OpenDataset("hold_delay/points")
	if err != nil {
		t.Fatal(err)
	}
	defer points.Close()
	var pointRows []holdDelayPointRow
	readDatasetRows(t, points, &pointRows, 1)
	if pointRows[0].DelayNS != 16 || pointRows[0].ElapsedNanoseconds != uint64(25*time.Millisecond) {
		t.Fatalf("point row = %#v", pointRows[0])
	}

	bins, err := file.OpenDataset("hold_delay/nonzero_hg_bins")
	if err != nil {
		t.Fatal(err)
	}
	defer bins.Close()
	var binRows []holdDelayBinRow
	readDatasetRows(t, bins, &binRows, 1)
	if binRows[0].Channel != 4 || binRows[0].Bin != 123 || binRows[0].Count != 9 {
		t.Fatalf("bin row = %#v", binRows[0])
	}

	missing, err := file.OpenDataset("hold_delay/missing_channels")
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Close()
	var missingRows []holdDelayMissingRow
	readDatasetRows(t, missing, &missingRows, 1)
	if missingRows[0].Channel != 7 || missingRows[0].Count != 2 {
		t.Fatalf("missing row = %#v", missingRows[0])
	}
}
