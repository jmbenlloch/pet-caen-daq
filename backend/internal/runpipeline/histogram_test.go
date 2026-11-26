package runpipeline

import (
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

func TestHistogramAccumulatorStoresSelectedChannelSpectra(t *testing.T) {
	sink := &sink{
		histogramOptions: acquisition.HistogramOptions{EnergyBins: 256, EnergyChannels: 16384, ToABins: 256, ToTBins: 512},
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
	if _, err := sink.Histograms(HistogramPHAHigh, []HistogramSelection{{Channel: 65}}); err == nil {
		t.Fatal("accepted channel 65")
	}
}

func TestHistogramSpecUsesConfiguredEnergyRange(t *testing.T) {
	for _, test := range []struct {
		channels int
		width    float64
	}{
		{1 << 13, 2},
		{1 << 14, 4},
	} {
		_, _, width, err := histogramSpec(acquisition.HistogramOptions{
			EnergyBins: 4096, EnergyChannels: test.channels,
		}, HistogramPHAHigh)
		if err != nil || width != test.width {
			t.Fatalf("%d-channel energy width = %v, %v; want %v", test.channels, width, err, test.width)
		}
	}
}

func TestHistogramAccumulatorUsesJANUSToARebinAndMinimum(t *testing.T) {
	sink := &sink{
		histogramOptions: acquisition.HistogramOptions{ToABins: 256, ToARebin: 4, ToAMinNS: 10},
		histograms:       make(map[histogramKey]*histogramAccumulator),
	}
	key := histogramKey{chain: 1, node: 2, channel: 3, kind: HistogramToA}
	sink.incrementHistogram(key, 9.5)
	sink.incrementHistogram(key, 10)
	sink.incrementHistogram(key, 11.9)
	sink.incrementHistogram(key, 12)
	histogram := sink.histograms[key]
	if histogram.minimum != 10 || histogram.width != 2 || histogram.underflow != 1 ||
		histogram.bins[0] != 2 || histogram.bins[1] != 1 {
		t.Fatalf("rebinned ToA histogram = %+v", histogram)
	}
}

func TestHistogramUint32CounterSaturatesInsteadOfWrapping(t *testing.T) {
	key := histogramKey{kind: HistogramToT}
	sink := &sink{
		histogramOptions: acquisition.HistogramOptions{ToTBins: 512},
		histograms: map[histogramKey]*histogramAccumulator{
			key: {width: 1, bins: []uint32{^uint32(0)}},
		},
	}
	sink.incrementHistogram(key, 0)
	if sink.histograms[key].bins[0] != ^uint32(0) || sink.histograms[key].overflow != 1 {
		t.Fatalf("saturated histogram = %+v", sink.histograms[key])
	}
}

func TestBoardChargeAccumulatesOneTotalPerPresentGainAndEvent(t *testing.T) {
	s := &sink{
		histogramOptions: acquisition.HistogramOptions{EnergyBins: 8192, EnergyChannels: 8192},
		histograms:       make(map[histogramKey]*histogramAccumulator),
	}
	add := func(node uint8, energies ...dt5202.Energy) {
		s.accumulateHistograms(1, node, dt5202.Event{Spectroscopy: &dt5202.SpectroscopyEvent{Energies: energies}})
	}
	add(2,
		dt5202.Energy{Channel: 0, HighGain: 100, HasHighGain: true, LowGain: 12, HasLowGain: true},
		dt5202.Energy{Channel: 63, HighGain: 200, HasHighGain: true, LowGain: 30, HasLowGain: true},
	)
	add(2, dt5202.Energy{Channel: 0, HighGain: 9000, HasHighGain: true})
	add(2, dt5202.Energy{Channel: 0, HasLowGain: true})
	add(2)
	add(3, dt5202.Energy{Channel: 0, HighGain: 300, HasHighGain: true})
	selections := []HistogramSelection{{Chain: 1, Node: 2, Channel: BoardChargeChannel}, {Chain: 1, Node: 3, Channel: BoardChargeChannel}}
	high, err := s.Histograms(HistogramPHAHigh, selections)
	if err != nil {
		t.Fatal(err)
	}
	if high[0].Entries != 2 || high[0].Bins[300] != 1 || high[0].Bins[9000] != 1 || high[0].Bins[100] != 0 || high[0].Bins[200] != 0 || high[1].Entries != 1 || high[1].Bins[300] != 1 {
		t.Fatal("high-gain totals were not accumulated separately per board and event")
	}
	low, err := s.Histograms(HistogramPHALow, selections)
	if err != nil {
		t.Fatal(err)
	}
	if low[0].Entries != 2 || low[0].Bins[42] != 1 || low[0].Bins[0] != 1 || low[1].Entries != 0 {
		t.Fatal("low-gain totals must include reported zero and skip absent gains")
	}
}

func TestBoardChargeUpperEdgeOverflowsForBothGains(t *testing.T) {
	s := &sink{
		histogramOptions: acquisition.HistogramOptions{EnergyBins: 256, EnergyChannels: 8192},
		histograms:       make(map[histogramKey]*histogramAccumulator),
	}
	energies := make([]dt5202.Energy, 33)
	for i := range energies {
		energies[i] = dt5202.Energy{
			Channel: uint8(i), HighGain: 16383, HasHighGain: true, LowGain: 16383, HasLowGain: true,
		}
	}
	event := dt5202.Event{Spectroscopy: &dt5202.SpectroscopyEvent{Energies: energies}}
	energies[32].HighGain, energies[32].LowGain = 31, 31 // 524287 ADC total
	s.accumulateHistograms(0, 0, event)
	energies[32].HighGain, energies[32].LowGain = 32, 32 // 524288 ADC total
	s.accumulateHistograms(0, 0, event)

	for _, kind := range []HistogramKind{HistogramPHAHigh, HistogramPHALow} {
		datasets, err := s.Histograms(kind, []HistogramSelection{{Channel: BoardChargeChannel}})
		if err != nil {
			t.Fatal(err)
		}
		got := datasets[0]
		if got.Minimum != 0 || got.BinWidth != 32 || len(got.Bins) != 16384 ||
			got.Bins[16383] != 1 || got.Entries != 2 || got.Overflow != 1 {
			t.Fatalf("%s range or counts differ: min=%g width=%g bins=%d entries=%d overflow=%d",
				kind, got.Minimum, got.BinWidth, len(got.Bins), got.Entries, got.Overflow)
		}
	}
}
