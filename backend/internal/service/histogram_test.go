package service

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
)

type histogramPipeline struct{}

func (*histogramPipeline) Submit(context.Context, acquisition.PipelineBatch) error { return nil }
func (*histogramPipeline) Close() error                                            { return nil }
func (*histogramPipeline) Histograms(kind runpipeline.HistogramKind, selections []runpipeline.HistogramSelection) ([]runpipeline.HistogramDataset, error) {
	return []runpipeline.HistogramDataset{{HistogramSelection: selections[0], Minimum: 0, BinWidth: 4, Bins: []uint32{0, 2}, Entries: 2}}, nil
}

func TestRunServiceReturnsSelectedActiveHistogramData(t *testing.T) {
	controller := &fakeRunController{active: "42", state: acquisition.StateRunning, pipeline: &histogramPipeline{}}
	service := newRunService(t, controller)
	response, err := service.GetHistograms(context.Background(), connect.NewRequest(&daqv1.GetHistogramsRequest{
		RunId: "42", Kind: daqv1.HistogramKind_HISTOGRAM_KIND_PHA_HIGH_GAIN,
		Selections: []*daqv1.HistogramSelection{{Chain: 1, Node: 2, Channel: 3}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	dataset := response.Msg.Datasets[0]
	if dataset.GetChain() != 1 || dataset.GetChannel() != 3 || dataset.GetEntries() != 2 || dataset.GetBins()[1] != 2 {
		t.Fatalf("dataset = %+v", dataset)
	}
}

func TestRunServiceReturnsPersistedHistogramData(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady}
	service := newRunService(t, controller)
	service.RunParent = t.TempDir()
	called := false
	service.LoadHistograms = func(parent, runID string, kind runpipeline.HistogramKind, selections []runpipeline.HistogramSelection) ([]runpipeline.HistogramDataset, error) {
		called = parent == service.RunParent && runID == "41" && kind == runpipeline.HistogramToT
		return []runpipeline.HistogramDataset{{
			HistogramSelection: selections[0], BinWidth: 1, Bins: []uint32{0, 7}, Entries: 7,
		}}, nil
	}
	response, err := service.GetHistograms(context.Background(), connect.NewRequest(&daqv1.GetHistogramsRequest{
		RunId: "41", Kind: daqv1.HistogramKind_HISTOGRAM_KIND_TOT,
		Selections: []*daqv1.HistogramSelection{{Chain: 1, Node: 2, Channel: 3}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !called || response.Msg.Datasets[0].GetBins()[1] != 7 {
		t.Fatalf("called=%v response=%+v", called, response.Msg)
	}
}

func TestParseHistogramOptionsUsesJANUSBinsAndDisabledMode(t *testing.T) {
	document, _ := janusconfig.Parse(strings.NewReader("EHistoNbin 8K\nToAHistoNbin DISABLED\n"))
	options, err := parseHistogramOptions(document)
	if err != nil || options.EnergyBins != 8192 || options.EnergyChannels != 8192 || options.ToABins != 0 || options.ToTBins != 512 {
		t.Fatalf("options=%+v error=%v", options, err)
	}
	document, _ = janusconfig.Parse(strings.NewReader("EHistoNbin 4K\nRange_14bit 1\n"))
	options, err = parseHistogramOptions(document)
	if err != nil || options.EnergyChannels != 16384 {
		t.Fatalf("14-bit options=%+v error=%v", options, err)
	}
	document, _ = janusconfig.Parse(strings.NewReader("ToAHistoNbin 4K\nToARebin 8\nToAHistoMin 20 ns\n"))
	options, err = parseHistogramOptions(document)
	if err != nil || options.ToARebin != 8 || options.ToAMinNS != 20 {
		t.Fatalf("ToA options=%+v error=%v", options, err)
	}
	document, _ = janusconfig.Parse(strings.NewReader("EHistoNbin 300\n"))
	if _, err := parseHistogramOptions(document); err == nil {
		t.Fatal("accepted non-power-of-two bin count")
	}
}
