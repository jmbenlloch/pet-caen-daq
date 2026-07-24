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
