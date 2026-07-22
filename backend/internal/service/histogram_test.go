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
	return []runpipeline.HistogramDataset{{HistogramSelection: selections[0], Minimum: 0, BinWidth: 4, Bins: []uint64{0, 2}, Entries: 2}}, nil
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
	controller.active = ""
	if _, err := service.GetHistograms(context.Background(), connect.NewRequest(&daqv1.GetHistogramsRequest{RunId: "42"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("inactive error = %v", err)
	}
}

func TestParseHistogramOptionsUsesJANUSBinsAndDisabledMode(t *testing.T) {
	document, _ := janusconfig.Parse(strings.NewReader("EHistoNbin 8K\nToAHistoNbin DISABLED\n"))
	options, err := parseHistogramOptions(document)
	if err != nil || options.EnergyBins != 8192 || options.ToABins != 0 || options.ToTBins != 512 {
		t.Fatalf("options=%+v error=%v", options, err)
	}
	document, _ = janusconfig.Parse(strings.NewReader("EHistoNbin 300\n"))
	if _, err := parseHistogramOptions(document); err == nil {
		t.Fatal("accepted non-power-of-two bin count")
	}
}
