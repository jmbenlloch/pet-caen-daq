package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
)

func parseHistogramOptions(document *janusconfig.Document) (acquisition.HistogramOptions, error) {
	values := map[string]string{"EHistoNbin": "4K", "ToAHistoNbin": "4K", "ToARebin": "1", "ToAHistoMin": "0 ns", "Range_14bit": "0"}
	for _, assignment := range document.Assignments {
		if assignment.Index == nil && assignment.Channel == nil {
			values[assignment.Name] = assignment.Value
		}
	}
	energy, err := parseHistogramBins(values["EHistoNbin"], 8192)
	if err != nil {
		return acquisition.HistogramOptions{}, fmt.Errorf("EHistoNbin: %w", err)
	}
	toa, err := parseHistogramBins(values["ToAHistoNbin"], 16384)
	if err != nil {
		return acquisition.HistogramOptions{}, fmt.Errorf("ToAHistoNbin: %w", err)
	}
	rebin, err := strconv.Atoi(strings.TrimSpace(values["ToARebin"]))
	if err != nil || rebin < 1 || rebin > 127 {
		return acquisition.HistogramOptions{}, fmt.Errorf("ToARebin: must be an integer from 1 to 127")
	}
	toaMinimum, err := parseHistogramTimeNS(values["ToAHistoMin"])
	if err != nil || toaMinimum < 0 || toaMinimum/0.5 > math.MaxUint32 {
		return acquisition.HistogramOptions{}, fmt.Errorf("ToAHistoMin: must be a non-negative time")
	}
	// JANUS converts the configured time to an integer number of 0.5 ns ticks.
	toaMinimum = math.Trunc(toaMinimum/0.5) * 0.5
	range14, err := strconv.ParseUint(strings.TrimSpace(values["Range_14bit"]), 0, 1)
	if err != nil {
		return acquisition.HistogramOptions{}, fmt.Errorf("Range_14bit: must be 0 or 1")
	}
	energyChannels := 1 << 13
	if range14 == 1 {
		energyChannels = 1 << 14
	}
	return acquisition.HistogramOptions{
		EnergyBins: energy, EnergyChannels: energyChannels,
		ToABins: toa, ToARebin: rebin, ToAMinNS: toaMinimum, ToTBins: 512,
	}, nil
}

func parseHistogramTimeNS(value string) (float64, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 || len(fields) > 2 {
		return 0, fmt.Errorf("invalid time")
	}
	number, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	unit := "ns"
	if len(fields) == 2 {
		unit = strings.ToLower(fields[1])
	}
	multiplier, ok := map[string]float64{"ns": 1, "us": 1e3, "ms": 1e6, "s": 1e9}[unit]
	if !ok {
		return 0, fmt.Errorf("unsupported time unit %q", unit)
	}
	return number * multiplier, nil
}

func parseHistogramBins(value string, maximum int) (int, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "DISABLED" {
		return 0, nil
	}
	multiplier := 1
	if strings.HasSuffix(value, "K") {
		multiplier, value = 1024, strings.TrimSuffix(value, "K")
	}
	base, err := strconv.Atoi(value)
	bins := base * multiplier
	if err != nil || bins < 256 || bins > maximum || bins&(bins-1) != 0 {
		return 0, fmt.Errorf("must be DISABLED or a power of two from 256 to %d", maximum)
	}
	return bins, nil
}

type histogramSource interface {
	Histograms(runpipeline.HistogramKind, []runpipeline.HistogramSelection) ([]runpipeline.HistogramDataset, error)
}

func (s *RunService) GetHistograms(_ context.Context, request *connect.Request[daqv1.GetHistogramsRequest]) (*connect.Response[daqv1.GetHistogramsResponse], error) {
	message := request.Msg
	if !validRunID.MatchString(message.GetRunId()) {
		return nil, serviceError(connect.CodeInvalidArgument, "INVALID_HISTOGRAM_RUN", fmt.Errorf("a valid run_id is required"))
	}
	if len(message.GetSelections()) == 0 || len(message.GetSelections()) > 64 {
		return nil, serviceError(connect.CodeInvalidArgument, "INVALID_HISTOGRAM_SELECTION", fmt.Errorf("select between 1 and 64 channels"))
	}
	kinds := map[daqv1.HistogramKind]runpipeline.HistogramKind{
		daqv1.HistogramKind_HISTOGRAM_KIND_PHA_HIGH_GAIN: runpipeline.HistogramPHAHigh,
		daqv1.HistogramKind_HISTOGRAM_KIND_PHA_LOW_GAIN:  runpipeline.HistogramPHALow,
		daqv1.HistogramKind_HISTOGRAM_KIND_TOA:           runpipeline.HistogramToA,
		daqv1.HistogramKind_HISTOGRAM_KIND_TOT:           runpipeline.HistogramToT,
	}
	kind, ok := kinds[message.GetKind()]
	if !ok {
		return nil, serviceError(connect.CodeInvalidArgument, "INVALID_HISTOGRAM_KIND", fmt.Errorf("histogram kind is required"))
	}
	selections := make([]runpipeline.HistogramSelection, 0, len(message.GetSelections()))
	for _, selection := range message.GetSelections() {
		if selection.GetChain() > 7 || selection.GetNode() > 15 || selection.GetChannel() > 63 {
			return nil, serviceError(connect.CodeInvalidArgument, "INVALID_HISTOGRAM_SELECTION", fmt.Errorf("selection %d/%d/%d is outside hardware range", selection.GetChain(), selection.GetNode(), selection.GetChannel()))
		}
		selections = append(selections, runpipeline.HistogramSelection{Chain: uint8(selection.GetChain()), Node: uint8(selection.GetNode()), Channel: uint8(selection.GetChannel())})
	}
	var datasets []runpipeline.HistogramDataset
	var err error
	if message.GetRunId() == s.Controller.ActiveRunID() {
		source, ok := s.Controller.ActivePipeline().(histogramSource)
		if !ok {
			return nil, serviceError(connect.CodeUnimplemented, "HISTOGRAMS_UNAVAILABLE", fmt.Errorf("active pipeline has no histogram accumulator"))
		}
		datasets, err = source.Histograms(kind, selections)
	} else {
		loader := s.LoadHistograms
		if loader == nil {
			loader = runpipeline.LoadPersistedHistograms
		}
		if s.RunParent == "" {
			return nil, serviceError(connect.CodeFailedPrecondition, "RUN_STORAGE_UNAVAILABLE", fmt.Errorf("run storage is not configured"))
		}
		datasets, err = loader(s.RunParent, message.GetRunId(), kind, selections)
	}
	if err != nil {
		return nil, serviceError(connect.CodeFailedPrecondition, "HISTOGRAM_UNAVAILABLE", err)
	}
	response := &daqv1.GetHistogramsResponse{RunId: message.GetRunId(), Kind: message.GetKind()}
	for _, dataset := range datasets {
		response.Datasets = append(response.Datasets, &daqv1.HistogramDataset{
			Chain: uint32(dataset.Chain), Node: uint32(dataset.Node), Channel: uint32(dataset.Channel),
			Minimum: dataset.Minimum, BinWidth: dataset.BinWidth, Bins: dataset.Bins, Entries: dataset.Entries,
			Underflow: dataset.Underflow, Overflow: dataset.Overflow,
		})
	}
	return connect.NewResponse(response), nil
}
