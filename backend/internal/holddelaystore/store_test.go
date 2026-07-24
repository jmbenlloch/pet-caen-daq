package holddelaystore

import (
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
)

func TestWriterPersistsSparseHistogramAndManifest(t *testing.T) {
	parent := t.TempDir()
	request := holddelay.Request{
		Board: 1, MinimumDelayNS: 0, MaximumDelayNS: 8, StepNS: 8,
		EventsPerDelay: 10, PointTimeout: time.Second,
	}
	writer, err := Create(parent, NewManifest("72", 1, "operator", request, time.Unix(10, 0)))
	if err != nil {
		t.Fatal(err)
	}
	point := holddelay.Point{DelayNS: 8, EffectiveDelay: 8, EventsCollected: 10}
	point.HighGainBins[63][511] = 7
	point.MissingChannels[2] = 3
	if err := writer.Append(point); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finalize(time.Unix(11, 0).UTC().Format(time.RFC3339Nano), "completed", "completed", true); err != nil {
		t.Fatal(err)
	}
	manifest, points, err := Read(parent, "72")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ScanType != "hold_delay" || manifest.CompletedPoints != 1 || !manifest.Restored ||
		manifest.Artifact == nil || manifest.Artifact.SHA256 == "" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(points) != 1 || points[0].HighGainBins[63][511] != 7 || points[0].MissingChannels[2] != 3 {
		t.Fatalf("points = %#v", points)
	}
	listed, err := List(parent, 1)
	if err != nil || len(listed) != 1 || listed[0].ScanID != "72" {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	file, err := OpenArtifact(parent, "72", manifest.Artifact.Name)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
}
