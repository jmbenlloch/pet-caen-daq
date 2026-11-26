//go:build hdf5

package service

import (
	"connectrpc.com/connect"
	"context"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/hdf5store"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"testing"
	"time"
)

func TestBoardSumFromSparsePersistedHDF5(t *testing.T) {
	parent := t.TempDir()
	manifest := runstore.Manifest{SchemaVersion: 1, RunID: "sum-history", StartedAt: time.Now().UTC().Format(time.RFC3339Nano), PersistHistograms: true}
	manifest.ExecutionIdentity.Runtime.EnergyHistogramBins = 256
	manifest.ExecutionIdentity.Runtime.EnergyHistogramChannels = 8192
	manifest.ExecutionIdentity.Runtime.ToAHistogramBins = 256
	manifest.ExecutionIdentity.Runtime.ToAHistogramRebin = 2
	manifest.ExecutionIdentity.Runtime.ToTHistogramBins = 512
	w, err := hdf5store.CreateRun(parent, manifest)
	if err != nil {
		t.Fatal(err)
	}
	var spectra []runstore.HistogramDataset
	for _, kind := range []string{"pha_high", "pha_low", "toa", "tot"} {
		count, width := 256, 1.0
		if kind == "pha_high" || kind == "pha_low" {
			width = 32
			bins := make([]uint32, (1<<18)/32) // Range used by the first board-charge release.
			bins[9] = 1                        // An event with total charge 300 ADC.
			spectra = append(spectra, runstore.HistogramDataset{Kind: kind, Chain: 1, Node: 2, Channel: runpipeline.BoardChargeChannel, Bins: bins, BinWidth: width, Entries: 1})
		}
		if kind == "tot" {
			count = 512
		}
		for _, ch := range []uint8{0, 63} {
			bins := make([]uint32, count)
			bins[7] = uint32(ch) + 1
			spectra = append(spectra, runstore.HistogramDataset{Kind: kind, Chain: 1, Node: 2, Channel: ch, Bins: bins, BinWidth: width, Entries: uint64(ch) + 1})
		}
	}
	if err = w.SaveHistograms(spectra); err != nil {
		t.Fatal(err)
	}
	if err = w.Finalize(time.Now().UTC().Format(time.RFC3339Nano), "test"); err != nil {
		t.Fatal(err)
	}
	s := newRunService(t, &fakeRunController{state: acquisition.StateReady})
	s.RunParent = parent
	for _, kind := range []daqv1.HistogramKind{1, 2, 3, 4} {
		r, err := s.GetHistograms(context.Background(), connect.NewRequest(&daqv1.GetHistogramsRequest{RunId: "sum-history", Kind: kind, Selections: []*daqv1.HistogramSelection{{Chain: 1, Node: 2, BoardSum: true}, {Chain: 1, Node: 2, Channel: 63}}}))
		if err != nil {
			t.Fatal(err)
		}
		bin, count := 7, uint64(65)
		if kind == daqv1.HistogramKind_HISTOGRAM_KIND_PHA_HIGH_GAIN || kind == daqv1.HistogramKind_HISTOGRAM_KIND_PHA_LOW_GAIN {
			bin, count = 9, 1
			if got := r.Msg.Datasets[0]; len(got.BoardSumBins) != 8192 || got.BinWidth != 32 {
				t.Fatalf("wrong historical board-charge axis for %s", kind)
			}
		}
		if r.Msg.Datasets[0].BoardSumBins[bin] != count || r.Msg.Datasets[0].Entries != count || r.Msg.Datasets[1].Bins[7] != 64 {
			t.Fatalf("wrong historical sum for %s", kind)
		}

	}
	_, err = s.GetHistograms(context.Background(), connect.NewRequest(&daqv1.GetHistogramsRequest{
		RunId: "sum-history", Kind: daqv1.HistogramKind_HISTOGRAM_KIND_PHA_HIGH_GAIN,
		Selections: []*daqv1.HistogramSelection{{Chain: 1, Node: 3, BoardSum: true}},
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("board charge without a saved total should be unavailable, got %v", err)
	}
}
