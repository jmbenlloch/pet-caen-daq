//go:build hdf5

package runpipeline

import (
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestSessionPersistsFinalHistogramSnapshotWhenSelected(t *testing.T) {
	parent := t.TempDir()
	factory := Factory{Options: Options{
		Parent: parent, Capacity: 1, Backpressure: acquisition.BackpressureBlock, Now: time.Now,
	}}
	created, err := factory.New("histogram-session", acquisition.RunOptions{
		PersistHistograms: true,
		Histograms: acquisition.HistogramOptions{
			EnergyBins: 256, EnergyChannels: 8192, ToABins: 256, ToARebin: 2, ToTBins: 512,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := created.(*Session)
	session.sink.accumulateHistograms(1, 2, dt5202.Event{
		Kind:   dt5202.EventTiming,
		Timing: &dt5202.TimingEvent{Hits: []dt5202.Timing{{Channel: 3, ToA: 8, ToT: 4}}},
	})
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Finalize("later", "operator_stop"); err != nil {
		t.Fatal(err)
	}
	manifest, err := runstore.ReadManifest(session.Directory(), "histogram-session")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, artifact := range manifest.Artifacts {
		found = found || artifact.Kind == "histograms"
	}
	if !found {
		t.Fatalf("histogram artifact missing from %+v", manifest.Artifacts)
	}
	datasets, err := LoadPersistedHistograms(parent, "histogram-session", HistogramToA, []HistogramSelection{
		{Chain: 1, Node: 2, Channel: 3},
		{Chain: 1, Node: 2, Channel: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if datasets[0].Entries != 1 || datasets[0].Bins[4] != 1 || datasets[1].Entries != 0 || len(datasets[1].Bins) != 256 {
		t.Fatalf("persisted datasets = %+v", datasets)
	}
}
