package staircasestore

import (
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
)

func TestWriterPersistsRecoverablePointsAndManifest(t *testing.T) {
	parent := t.TempDir()
	request := staircase.Request{Board: 2, Minimum: 200, Maximum: 300, Step: 100, Dwell: 500 * time.Millisecond}
	writer, err := Create(parent, NewManifest("scan-test", 2, "operator", request, time.Unix(10, 0)))
	if err != nil {
		t.Fatal(err)
	}
	point := staircase.Point{Threshold: 300, TORCount: 9}
	point.ChannelCounts[63] = 12
	if err := writer.Append(point); err != nil {
		t.Fatal(err)
	}
	manifest, points, err := Read(parent, "scan-test")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.CompletedPoints != 1 || len(points) != 1 || points[0].ChannelCounts[63] != 12 {
		t.Fatalf("manifest=%#v points=%#v", manifest, points)
	}
	if err := writer.Finalize(time.Unix(11, 0).UTC().Format(time.RFC3339Nano), "completed", "completed", true); err != nil {
		t.Fatal(err)
	}
	manifest, _, err = Read(parent, "scan-test")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.State != "completed" || !manifest.Restored || manifest.Artifact == nil || manifest.Artifact.SHA256 == "" {
		t.Fatalf("final manifest = %#v", manifest)
	}
}

func TestListPageFiltersBeforePaginating(t *testing.T) {
	parent := t.TempDir()
	request := staircase.Request{Minimum: 100, Maximum: 100, Step: 1, Dwell: time.Millisecond}
	for _, item := range []struct {
		id      string
		board   uint32
		started time.Time
	}{
		{id: "1", board: 2, started: time.Unix(10, 0)},
		{id: "2", board: 0, started: time.Unix(20, 0)},
		{id: "3", board: 2, started: time.Unix(30, 0)},
	} {
		writer, err := Create(parent, NewManifest(item.id, item.board, "operator", request, item.started))
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Finalize(item.started.Add(time.Second).UTC().Format(time.RFC3339Nano), "completed", "completed", true); err != nil {
			t.Fatal(err)
		}
	}

	board := uint32(2)
	page, total, err := ListPage(parent, 1, 1, &board)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(page) != 1 || page[0].ScanID != "1" {
		t.Fatalf("page=%+v total=%d, want second of two board-2 scans", page, total)
	}
}
