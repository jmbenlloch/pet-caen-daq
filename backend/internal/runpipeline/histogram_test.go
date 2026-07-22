package runpipeline

import (
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

func TestHistogramAccumulatorStoresSelectedChannelSpectra(t *testing.T) {
	sink := &sink{
		histogramOptions: acquisition.HistogramOptions{EnergyBins: 256, ToABins: 256, ToTBins: 512},
		histograms:       make(map[histogramKey]*histogramAccumulator),
		boards:           make(map[boardKey]BoardStats),
	}
	event := dt5202.Event{Kind: dt5202.EventSpectroscopy, Spectroscopy: &dt5202.SpectroscopyEvent{
		Energies: []dt5202.Energy{{Channel: 3, HighGain: 128, LowGain: 64, HasHighGain: true, HasLowGain: true}},
		Timings:  []dt5202.Timing{{Channel: 3, ToA: 1024, ToT: 17}},
	}}
	sink.accumulateHistograms(1, 2, event)
	selection := []HistogramSelection{{Chain: 1, Node: 2, Channel: 3}, {Chain: 1, Node: 2, Channel: 4}}
	datasets, err := sink.Histograms(HistogramPHAHigh, selection)
	if err != nil {
		t.Fatal(err)
	}
	if len(datasets) != 2 || datasets[0].Entries != 1 || datasets[0].Bins[2] != 1 || datasets[1].Entries != 0 || len(datasets[1].Bins) != 256 {
		t.Fatalf("PHA datasets = %+v", datasets)
	}
	datasets[0].Bins[2] = 99
	again, _ := sink.Histograms(HistogramPHAHigh, selection[:1])
	if again[0].Bins[2] != 1 {
		t.Fatal("returned histogram aliases the live accumulator")
	}
	toa, _ := sink.Histograms(HistogramToA, selection[:1])
	tot, _ := sink.Histograms(HistogramToT, selection[:1])
	if toa[0].Entries != 1 || tot[0].Bins[17] != 1 {
		t.Fatalf("timing datasets: toa=%+v tot=%+v", toa[0], tot[0])
	}
}

func TestHistogramAccumulatorRejectsDisabledAndInvalidSelections(t *testing.T) {
	sink := &sink{histograms: make(map[histogramKey]*histogramAccumulator)}
	if _, err := sink.Histograms(HistogramPHAHigh, []HistogramSelection{{}}); err == nil {
		t.Fatal("accepted disabled histogram")
	}
	sink.histogramOptions.EnergyBins = 256
	if _, err := sink.Histograms(HistogramPHAHigh, []HistogramSelection{{Channel: 64}}); err == nil {
		t.Fatal("accepted channel 64")
	}
}
