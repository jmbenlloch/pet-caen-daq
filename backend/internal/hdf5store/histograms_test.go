//go:build hdf5

package hdf5store

import (
	"path/filepath"
	"testing"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestSaveHistogramsWritesIndependentUint32Artifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "histograms.h5")
	input := []runstore.HistogramDataset{{
		Kind: "toa", Chain: 1, Node: 2, Channel: 3,
		Minimum: 10, BinWidth: 2, Bins: []uint32{4, 5, 6},
		Entries: 18, Underflow: 1, Overflow: 2,
	}}
	if err := SaveHistograms(path, "42", input); err != nil {
		t.Fatal(err)
	}
	file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	dataset, err := file.OpenDataset("histograms/toa_1_2_3")
	if err != nil {
		t.Fatal(err)
	}
	defer dataset.Close()
	got := make([]uint32, 3)
	if err := dataset.Read(&got); err != nil {
		t.Fatal(err)
	}
	if got[0] != 4 || got[2] != 6 {
		t.Fatalf("bins = %v", got)
	}
}

func TestRunWriterCatalogsHistogramArtifact(t *testing.T) {
	writer, err := CreateRun(t.TempDir(), runstore.Manifest{RunID: "hist", StartedAt: "now", PersistHistograms: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.SaveHistograms([]runstore.HistogramDataset{{Kind: "tot", Bins: []uint32{1, 2}}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finalize("later", "operator_stop"); err != nil {
		t.Fatal(err)
	}
	manifest, err := runstore.ReadManifest(writer.Directory(), "hist")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == "histograms" && artifact.Name == "run_hist.histograms.h5" && artifact.SizeBytes > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("histogram artifact missing from %+v", manifest.Artifacts)
	}
}
