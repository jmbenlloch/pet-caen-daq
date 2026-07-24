//go:build hdf5

package hdf5store

import (
	"path/filepath"
	"testing"
	"time"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
)

func TestWriteStaircaseDatasets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "staircase.h5")
	point := staircase.Point{Threshold: 301, Elapsed: 700 * time.Millisecond, TORCount: 10, QORCount: 11, TORRateCPS: 20, QORRateCPS: 22}
	point.ChannelCounts[63], point.ChannelRatesCPS[63] = 12, 24
	if err := WriteStaircase(path, []byte(`{"scan_id":"test"}`), []staircase.Point{point}); err != nil {
		t.Fatal(err)
	}
	file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	points, err := file.OpenDataset("staircase/points")
	if err != nil {
		t.Fatal(err)
	}
	defer points.Close()
	var pointRows []staircasePointRow
	readDatasetRows(t, points, &pointRows, 1)
	if pointRows[0].Threshold != 301 || pointRows[0].ElapsedNanoseconds != uint64(700*time.Millisecond) {
		t.Fatalf("point row = %#v", pointRows[0])
	}
	channels, err := file.OpenDataset("staircase/channel_measurements")
	if err != nil {
		t.Fatal(err)
	}
	defer channels.Close()
	var channelRows []staircaseChannelRow
	readDatasetRows(t, channels, &channelRows, staircase.ChannelCount)
	if channelRows[63].Channel != 63 || channelRows[63].Count != 12 || channelRows[63].RateCPS != 24 {
		t.Fatalf("channel row = %#v", channelRows[63])
	}
}

func readDatasetRows[T any](t *testing.T, dataset *hdf5.Dataset, rows *[]T, count int) {
	t.Helper()
	*rows = make([]T, count)
	if err := dataset.Read(rows); err != nil {
		t.Fatal(err)
	}
}
