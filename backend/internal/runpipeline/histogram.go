package runpipeline

import (
	"fmt"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

type HistogramKind string

const (
	HistogramPHAHigh HistogramKind = "pha_high"
	HistogramPHALow  HistogramKind = "pha_low"
	HistogramToA     HistogramKind = "toa"
	HistogramToT     HistogramKind = "tot"
)

type HistogramSelection struct {
	Chain, Node, Channel uint8
}

type HistogramDataset struct {
	HistogramSelection
	Minimum, BinWidth   float64
	Bins                []uint64
	Entries             uint64
	Underflow, Overflow uint64
}

type histogramKey struct {
	chain, node, channel uint8
	kind                 HistogramKind
}

type histogramAccumulator struct {
	minimum, width      float64
	bins                []uint64
	entries             uint64
	underflow, overflow uint64
}

func histogramSpec(options acquisition.HistogramOptions, kind HistogramKind) (int, float64, float64, error) {
	switch kind {
	case HistogramPHAHigh, HistogramPHALow:
		return options.EnergyBins, 0, float64(dt5202.MaxEnergy+1) / float64(options.EnergyBins), nil
	case HistogramToA:
		return options.ToABins, 0, float64(uint64(1)<<25) / float64(options.ToABins), nil
	case HistogramToT:
		return options.ToTBins, 0, 512 / float64(options.ToTBins), nil
	default:
		return 0, 0, 0, fmt.Errorf("unsupported histogram kind %q", kind)
	}
}

func (s *sink) incrementHistogram(key histogramKey, value float64) {
	bins, minimum, width, err := histogramSpec(s.histogramOptions, key.kind)
	if err != nil || bins <= 0 {
		return
	}
	histogram := s.histograms[key]
	if histogram == nil {
		histogram = &histogramAccumulator{minimum: minimum, width: width, bins: make([]uint64, bins)}
		s.histograms[key] = histogram
	}
	histogram.entries++
	index := int((value - histogram.minimum) / histogram.width)
	if value < histogram.minimum {
		histogram.underflow++
	} else if index < 0 || index >= len(histogram.bins) {
		histogram.overflow++
	} else {
		histogram.bins[index]++
	}
}

func (s *sink) accumulateHistograms(chain, node uint8, event dt5202.Event) {
	if event.Spectroscopy != nil {
		for _, energy := range event.Spectroscopy.Energies {
			if energy.HasHighGain {
				s.incrementHistogram(histogramKey{chain, node, energy.Channel, HistogramPHAHigh}, float64(energy.HighGain))
			}
			if energy.HasLowGain {
				s.incrementHistogram(histogramKey{chain, node, energy.Channel, HistogramPHALow}, float64(energy.LowGain))
			}
		}
		for _, timing := range event.Spectroscopy.Timings {
			s.accumulateTiming(chain, node, timing)
		}
	}
	if event.Timing != nil {
		for _, timing := range event.Timing.Hits {
			s.accumulateTiming(chain, node, timing)
		}
	}
}

func (s *sink) accumulateTiming(chain, node uint8, timing dt5202.Timing) {
	s.incrementHistogram(histogramKey{chain, node, timing.Channel, HistogramToA}, float64(timing.ToA))
	s.incrementHistogram(histogramKey{chain, node, timing.Channel, HistogramToT}, float64(timing.ToT))
}

func (s *sink) Histograms(kind HistogramKind, selections []HistogramSelection) ([]HistogramDataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binCount, minimum, width, err := histogramSpec(s.histogramOptions, kind)
	if err != nil || binCount <= 0 {
		return nil, fmt.Errorf("histogram %s is disabled", kind)
	}
	result := make([]HistogramDataset, 0, len(selections))
	for _, selection := range selections {
		if selection.Channel >= dt5202.ChannelCount {
			return nil, fmt.Errorf("channel %d outside range [0,63]", selection.Channel)
		}
		dataset := HistogramDataset{HistogramSelection: selection, Minimum: minimum, BinWidth: width, Bins: make([]uint64, binCount)}
		if histogram := s.histograms[histogramKey{selection.Chain, selection.Node, selection.Channel, kind}]; histogram != nil {
			copy(dataset.Bins, histogram.bins)
			dataset.Entries, dataset.Underflow, dataset.Overflow = histogram.entries, histogram.underflow, histogram.overflow
		}
		result = append(result, dataset)
	}
	return result, nil
}

func (s *Session) Histograms(kind HistogramKind, selections []HistogramSelection) ([]HistogramDataset, error) {
	return s.sink.Histograms(kind, selections)
}
