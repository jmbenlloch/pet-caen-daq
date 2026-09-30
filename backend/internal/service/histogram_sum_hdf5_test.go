//go:build hdf5

package service

import (
	"connectrpc.com/connect"
	"context"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/hdf5store"
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
		if r.Msg.Datasets[0].BoardSumBins[7] != 65 || r.Msg.Datasets[0].Entries != 65 || r.Msg.Datasets[1].Bins[7] != 64 {
			t.Fatalf("wrong historical sum: %v", r.Msg)
		}
	}
}
