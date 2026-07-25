//go:build hdf5

package hdf5store

import (
	"path/filepath"
	"testing"
	"time"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestSaveHistogramsWritesIndependentUint32Artifact(t *testing.T) {
	t.Setenv(CompressionEnvironment, "")
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
	dataset, err := file.OpenDataset("histograms/toa/bins")
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
	spectra, err := file.OpenDataset("histograms/toa/spectra")
	if err != nil {
		t.Fatal(err)
	}
	defer spectra.Close()
	var rows [1]HistogramSpectrumRow
	if err := spectra.Read(&rows); err != nil {
		t.Fatal(err)
	}
	if got := rows[0]; got.Chain != 1 || got.Node != 2 || got.Channel != 3 ||
		got.BinOffset != 0 || got.BinCount != 3 || got.Entries != 18 ||
		got.Underflow != 1 || got.Overflow != 2 || got.Minimum != 10 || got.BinWidth != 2 {
		t.Fatalf("spectrum row = %+v", got)
	}
	if compression, err := histogramCompressionName(); err != nil || compression != CompressionBloscLZ4 {
		t.Fatalf("histogram compression = %q, %v", compression, err)
	}
}

func TestCompactHistogramWriterHandlesFullFourBoardSnapshot(t *testing.T) {
	t.Setenv(CompressionEnvironment, CompressionNone)
	var input []runstore.HistogramDataset
	for _, kind := range []struct {
		name string
		bins int
	}{{"pha_high", 4096}, {"pha_low", 4096}, {"toa", 4096}, {"tot", 512}} {
		for chain := uint8(0); chain < 4; chain++ {
			for channel := uint8(0); channel < 64; channel++ {
				input = append(input, runstore.HistogramDataset{
					Kind: kind.name, Chain: chain, Channel: channel,
					Bins: make([]uint32, kind.bins), Entries: 1,
				})
			}
		}
	}
	path := filepath.Join(t.TempDir(), "full-histograms.h5")
	started := time.Now()
	if err := SaveHistograms(path, "full", input); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d spectra in %s", len(input), time.Since(started))
	file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, kind := range []string{"pha_high", "pha_low", "toa", "tot"} {
		if !file.LinkExists("histograms/"+kind+"/bins") || !file.LinkExists("histograms/"+kind+"/spectra") {
			t.Fatalf("compact datasets for %s are missing", kind)
		}
	}
	if file.LinkExists("histograms/pha_high_0_0_0") {
		t.Fatal("legacy per-channel dataset was written")
	}
}

func TestHistogramCompressionCanBeExplicitlyDisabled(t *testing.T) {
	t.Setenv(CompressionEnvironment, CompressionNone)
	compression, err := histogramCompressionName()
	if err != nil {
		t.Fatal(err)
	}
	if compression != CompressionNone {
		t.Fatalf("histogram compression = %q", compression)
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
