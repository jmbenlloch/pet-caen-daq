package service

import (
	"connectrpc.com/connect"
	"context"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"math"
	"testing"
)

type sumPipeline struct{ histogramPipeline }

func (*sumPipeline) Histograms(_ runpipeline.HistogramKind, selections []runpipeline.HistogramSelection) ([]runpipeline.HistogramDataset, error) {
	out := make([]runpipeline.HistogramDataset, 0, len(selections))
	for _, s := range selections {
		value := uint32(s.Channel) + uint32(s.Node) + 1
		out = append(out, runpipeline.HistogramDataset{HistogramSelection: s, Minimum: 2, BinWidth: 0.5, Bins: []uint32{math.MaxUint32, value}, Entries: uint64(value) + math.MaxUint32 + 3, Underflow: 1, Overflow: 2})
	}
	return out, nil
}
func TestBoardSumsAllKindsLiveAndHistorical(t *testing.T) {
	for _, live := range []bool{true, false} {
		for _, kind := range []daqv1.HistogramKind{1, 2, 3, 4} {
			p := &sumPipeline{}
			controller := &fakeRunController{state: acquisition.StateReady}
			if live {
				controller.active = "42"
				controller.pipeline = p
			}
			s := newRunService(t, controller)
			s.RunParent = t.TempDir()
			calls := 0
			s.LoadHistograms = func(_, _ string, k runpipeline.HistogramKind, selections []runpipeline.HistogramSelection) ([]runpipeline.HistogramDataset, error) {
				calls++
				return p.Histograms(k, selections)
			}
			resp, err := s.GetHistograms(context.Background(), connect.NewRequest(&daqv1.GetHistogramsRequest{RunId: "42", Kind: kind, Selections: []*daqv1.HistogramSelection{
				{Chain: 1, Node: 2, Channel: 0}, {Chain: 1, Node: 2, BoardSum: true}, {Chain: 1, Node: 3, BoardSum: true},
			}}))
			if err != nil {
				t.Fatal(err)
			}
			a, b, c := resp.Msg.Datasets[0], resp.Msg.Datasets[1], resp.Msg.Datasets[2]
			if a.BoardSum || a.Bins[1] != 3 || !b.BoardSum || len(b.Bins) != 0 || b.BoardSumBins[0] != 64*uint64(math.MaxUint32) || b.BoardSumBins[1] != 2208 || c.BoardSumBins[1] != 2272 || b.Underflow != 64 || b.Overflow != 128 || b.Minimum != 2 || b.BinWidth != 0.5 || b.Entries != b.BoardSumBins[0]+b.BoardSumBins[1]+192 {
				t.Fatalf("invalid sum: %v %v %v", a, b, c)
			}
			if !live && calls != 1 {
				t.Fatal("historical query must read once")
			}
		}
	}
}
func TestSumRejectsMismatchedAxesAndCounterOverflow(t *testing.T) {
	p := &sumPipeline{}
	sels := make([]runpipeline.HistogramSelection, 64)
	for i := range sels {
		sels[i].Channel = uint8(i)
	}
	data, _ := p.Histograms(runpipeline.HistogramToA, sels)
	index := map[runpipeline.HistogramSelection]runpipeline.HistogramDataset{}
	for _, d := range data {
		index[d.HistogramSelection] = d
	}
	bad := index[sels[63]]
	bad.BinWidth = 2
	index[sels[63]] = bad
	if _, err := sumBoardHistograms(sels[0], index); err == nil {
		t.Fatal("accepted different axes")
	}
	bad.BinWidth = 0.5
	bad.Entries = math.MaxUint64
	index[sels[63]] = bad
	if _, err := sumBoardHistograms(sels[0], index); err == nil {
		t.Fatal("accepted counter overflow")
	}
}
func TestBoardSumSelectionValidation(t *testing.T) {
	s := newRunService(t, &fakeRunController{state: acquisition.StateReady})
	cases := [][]*daqv1.HistogramSelection{{nil}, {{BoardSum: true, Channel: 1}}, {{BoardSum: true, Node: 16}}}
	many := make([]*daqv1.HistogramSelection, 66)
	for i := range many {
		many[i] = &daqv1.HistogramSelection{}
	}
	cases = append(cases, many, []*daqv1.HistogramSelection{{BoardSum: true, Node: 0}, {BoardSum: true, Node: 1}, {BoardSum: true, Node: 2}, {BoardSum: true, Node: 3}, {BoardSum: true, Node: 4}})
	for _, sels := range cases {
		_, err := s.GetHistograms(context.Background(), connect.NewRequest(&daqv1.GetHistogramsRequest{RunId: "42", Kind: 1, Selections: sels}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("expected validation error, got %v", err)
		}
	}
}

func TestAllChannelsAndBoardSumTogether(t *testing.T) {
	controller := &fakeRunController{active: "42", state: acquisition.StateRunning, pipeline: &sumPipeline{}}
	s := newRunService(t, controller)
	selections := make([]*daqv1.HistogramSelection, 0, 65)
	for i := uint32(0); i < 64; i++ {
		selections = append(selections, &daqv1.HistogramSelection{Channel: i})
	}
	selections = append(selections, &daqv1.HistogramSelection{BoardSum: true})
	r, err := s.GetHistograms(context.Background(), connect.NewRequest(&daqv1.GetHistogramsRequest{RunId: "42", Kind: 1, Selections: selections}))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Msg.Datasets) != 65 || !r.Msg.Datasets[64].BoardSum || r.Msg.Datasets[64].BoardSumBins[1] != 2080 {
		t.Fatalf("all channels + sum: %v", r.Msg)
	}
}
