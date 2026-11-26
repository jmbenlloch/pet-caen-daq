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

// BoardChargeChannel identifies a board-level PHA spectrum in histogram storage.
const BoardChargeChannel = dt5202.ChannelCount

const boardChargeUpperADC = 1 << 19

func (kind HistogramKind) IsPHA() bool {
	return kind == HistogramPHAHigh || kind == HistogramPHALow
}

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

func selectionHistogramSpec(options acquisition.HistogramOptions, kind HistogramKind, channel uint8) (int, float64, float64, error) {
	bins, minimum, width, err := histogramSpec(options, kind)
	if err == nil && bins > 0 && channel == BoardChargeChannel {
		// Keep the per-channel ADC bin width over the board charge range.
		bins = int(float64(boardChargeUpperADC) / width)
	}
	return bins, minimum, width, err
}

func (s *sink) histogram(key histogramKey) *histogramAccumulator {
	if histogram := s.histograms[key]; histogram != nil {
		return histogram
	}
	bins, minimum, width, err := selectionHistogramSpec(s.histogramOptions, key.kind, key.channel)
	if err != nil || bins <= 0 {
		return nil
	}
	histogram := &histogramAccumulator{minimum: minimum, width: width, bins: make([]uint32, bins)}
	s.histograms[key] = histogram
	return histogram
}

func (s *sink) incrementHistogram(key histogramKey, value float64) {
	histogram := s.histogram(key)
	if histogram == nil {
		return
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
		var high, low uint32
		var hasHigh, hasLow bool
		for _, energy := range event.Spectroscopy.Energies {
			if energy.HasHighGain {
				high += uint32(energy.HighGain)
				hasHigh = true
				s.incrementHistogram(histogramKey{chain, node, energy.Channel, HistogramPHAHigh}, float64(energy.HighGain))
			}
			if energy.HasLowGain {
				low += uint32(energy.LowGain)
				hasLow = true
				s.incrementHistogram(histogramKey{chain, node, energy.Channel, HistogramPHALow}, float64(energy.LowGain))
			}
		}
		highKey := histogramKey{chain, node, BoardChargeChannel, HistogramPHAHigh}
		lowKey := histogramKey{chain, node, BoardChargeChannel, HistogramPHALow}
		// Persist both spectra even if only one gain is present in this run.
		s.histogram(highKey)
		s.histogram(lowKey)
		if hasHigh {
			s.incrementHistogram(highKey, float64(high))
		}
		if hasLow {
			s.incrementHistogram(lowKey, float64(low))
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
	result := make([]HistogramDataset, 0, len(selections))
	for _, selection := range selections {
		if selection.Channel >= dt5202.ChannelCount && (selection.Channel != BoardChargeChannel || !kind.IsPHA()) {
			return nil, fmt.Errorf("channel %d outside range [0,63]", selection.Channel)
		}
		binCount, minimum, width, err := selectionHistogramSpec(s.histogramOptions, kind, selection.Channel)
		if err != nil || binCount <= 0 {
			return nil, fmt.Errorf("histogram %s is disabled", kind)
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
