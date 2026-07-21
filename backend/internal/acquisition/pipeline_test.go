package acquisition

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

type pipelineSink struct {
	mu       sync.Mutex
	order    []string
	raw      [][]byte
	events   []dt5202.Event
	rawErr   error
	eventErr error
	blockRaw chan struct{}
	entered  chan struct{}
	once     sync.Once
}

func (s *pipelineSink) AppendRaw(raw []byte) error {
	if s.entered != nil {
		s.once.Do(func() { close(s.entered) })
	}
	if s.blockRaw != nil {
		<-s.blockRaw
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, "raw")
	s.raw = append(s.raw, append([]byte(nil), raw...))
	return s.rawErr
}

func (s *pipelineSink) AppendEvent(_ dt5215.StreamEvent, event dt5202.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, "event")
	s.events = append(s.events, event)
	return s.eventErr
}

func TestPipelineCapturesRawBeforeDecodeAndClonesInput(t *testing.T) {
	sink := &pipelineSink{}
	pipeline, err := NewPipeline(1, BackpressureBlock, sink)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte{1, 2, 3}
	payload := make([]byte, 20) // oversized test event, but captured first
	batch := PipelineBatch{Raw: raw, Events: []dt5215.StreamEvent{{Descriptor: dt5215.Descriptor{Qualifier: dt5202.QualifierTest}, Payload: payload}}}
	if err := pipeline.Submit(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	raw[0], payload[0] = 9, 9
	err = pipeline.Close()
	if err == nil {
		t.Fatal("expected decode failure")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.raw) != 1 || sink.raw[0][0] != 1 {
		t.Fatalf("captured raw = %v", sink.raw)
	}
	if len(sink.order) != 1 || sink.order[0] != "raw" {
		t.Fatalf("sink order = %v", sink.order)
	}
}

func TestPipelineRejectPolicyReportsFull(t *testing.T) {
	sink := &pipelineSink{blockRaw: make(chan struct{}), entered: make(chan struct{})}
	pipeline, _ := NewPipeline(1, BackpressureReject, sink)
	if err := pipeline.Submit(context.Background(), PipelineBatch{Raw: []byte{1}}); err != nil {
		t.Fatal(err)
	}
	<-sink.entered
	if err := pipeline.Submit(context.Background(), PipelineBatch{Raw: []byte{2}}); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Submit(context.Background(), PipelineBatch{Raw: []byte{3}}); !errors.Is(err, ErrPipelineFull) {
		t.Fatalf("third submit error = %v", err)
	}
	close(sink.blockRaw)
	if err := pipeline.Close(); err != nil {
		t.Fatalf("close error = %v", err)
	}
}

func TestPipelineBlockPolicyHonorsCancellation(t *testing.T) {
	sink := &pipelineSink{blockRaw: make(chan struct{}), entered: make(chan struct{})}
	pipeline, _ := NewPipeline(1, BackpressureBlock, sink)
	if err := pipeline.Submit(context.Background(), PipelineBatch{}); err != nil {
		t.Fatal(err)
	}
	<-sink.entered
	if err := pipeline.Submit(context.Background(), PipelineBatch{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pipeline.Submit(ctx, PipelineBatch{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("submit error = %v", err)
	}
	close(sink.blockRaw)
	_ = pipeline.Close()
}

func TestPipelineReturnsSinkFailure(t *testing.T) {
	sentinel := errors.New("disk full")
	sink := &pipelineSink{rawErr: sentinel}
	pipeline, _ := NewPipeline(1, BackpressureBlock, sink)
	if err := pipeline.Submit(context.Background(), PipelineBatch{Raw: []byte{1}}); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Close(); !errors.Is(err, sentinel) {
		t.Fatalf("close error = %v", err)
	}
	stats := pipeline.Stats()
	if stats.AcceptedBatches != 1 || stats.SinkFailures != 1 || stats.DecodedEvents != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestPipelineStatsCountAcceptanceRejectionAndDecodeFailure(t *testing.T) {
	sink := &pipelineSink{blockRaw: make(chan struct{}), entered: make(chan struct{})}
	pipeline, _ := NewPipeline(1, BackpressureReject, sink)
	if err := pipeline.Submit(context.Background(), PipelineBatch{Raw: []byte{1}}); err != nil {
		t.Fatal(err)
	}
	<-sink.entered
	bad := PipelineBatch{Raw: []byte{2}, Events: []dt5215.StreamEvent{{Descriptor: dt5215.Descriptor{Qualifier: 0x7e}}}}
	if err := pipeline.Submit(context.Background(), bad); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Submit(context.Background(), PipelineBatch{Raw: []byte{3}}); !errors.Is(err, ErrPipelineFull) {
		t.Fatalf("full submit = %v", err)
	}
	stats := pipeline.Stats()
	if stats.Capacity != 1 || stats.QueueDepth != 1 || stats.AcceptedBatches != 2 || stats.RejectedBatches != 1 {
		t.Fatalf("queued stats = %+v", stats)
	}
	close(sink.blockRaw)
	if err := pipeline.Close(); err == nil {
		t.Fatal("expected decode failure")
	}
	stats = pipeline.Stats()
	if stats.QueueDepth != 0 || stats.DecodeFailures != 1 || stats.DecodedEvents != 0 {
		t.Fatalf("final stats = %+v", stats)
	}
}
