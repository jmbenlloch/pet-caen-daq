package runpipeline

import (
	"fmt"
	"sort"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
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
	Bins                []uint32
	Entries             uint64
	Underflow, Overflow uint64
}

type histogramKey struct {
	chain, node, channel uint8
	kind                 HistogramKind
}

type histogramAccumulator struct {
	minimum, width      float64
	bins                []uint32
	entries             uint64
	underflow, overflow uint64
}

func histogramSpec(options acquisition.HistogramOptions, kind HistogramKind) (int, float64, float64, error) {
	switch kind {
	case HistogramPHAHigh, HistogramPHALow:
		if options.EnergyChannels != 1<<13 && options.EnergyChannels != 1<<14 {
			return 0, 0, 0, fmt.Errorf("unsupported energy range %d", options.EnergyChannels)
		}
		return options.EnergyBins, 0, float64(options.EnergyChannels) / float64(options.EnergyBins), nil
	case HistogramToA:
		rebin := options.ToARebin
		if rebin == 0 {
			rebin = 1
		}
		if rebin < 1 {
			return 0, 0, 0, fmt.Errorf("unsupported ToA rebin factor %d", options.ToARebin)
		}
		return options.ToABins, options.ToAMinNS, 0.5 * float64(rebin), nil
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
		histogram = &histogramAccumulator{minimum: minimum, width: width, bins: make([]uint32, bins)}
		s.histograms[key] = histogram
	}
	histogram.entries++
	index := int((value - histogram.minimum) / histogram.width)
	if value < histogram.minimum {
		histogram.underflow++
	} else if index < 0 || index >= len(histogram.bins) {
		histogram.overflow++
	} else {
		if histogram.bins[index] < ^uint32(0) {
			histogram.bins[index]++
		} else {
			histogram.overflow++
		}
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
	// JANUS defines one decoded ToA tick as 0.5 ns, subtracts ToAHistoMin,
	// and integer-divides the result by ToARebin.
	s.incrementHistogram(histogramKey{chain, node, timing.Channel, HistogramToA}, float64(timing.ToA)*0.5)
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
		dataset := HistogramDataset{HistogramSelection: selection, Minimum: minimum, BinWidth: width, Bins: make([]uint32, binCount)}
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

func (s *sink) HistogramSnapshot() []runstore.HistogramDataset {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]runstore.HistogramDataset, 0, len(s.histograms))
	for key, histogram := range s.histograms {
		result = append(result, runstore.HistogramDataset{
			Kind: string(key.kind), Chain: key.chain, Node: key.node, Channel: key.channel,
			Minimum: histogram.minimum, BinWidth: histogram.width, Bins: append([]uint32(nil), histogram.bins...),
			Entries: histogram.entries, Underflow: histogram.underflow, Overflow: histogram.overflow,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Chain != right.Chain {
			return left.Chain < right.Chain
		}
		if left.Node != right.Node {
			return left.Node < right.Node
		}
		return left.Channel < right.Channel
	})
	return result
}
