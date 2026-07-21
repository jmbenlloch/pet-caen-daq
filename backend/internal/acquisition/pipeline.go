package acquisition

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

var (
	ErrPipelineFull   = errors.New("acquisition pipeline is full")
	ErrPipelineClosed = errors.New("acquisition pipeline is closed")
)

type BackpressurePolicy uint8

const (
	BackpressureBlock BackpressurePolicy = iota
	BackpressureReject
)

type PipelineBatch struct {
	Raw    []byte
	Events []dt5215.StreamEvent
}

type PipelineSink interface {
	AppendRaw([]byte) error
	AppendEvent(dt5215.StreamEvent, dt5202.Event) error
}

// Pipeline preserves batch ordering with one consumer. Raw evidence is stored
// before any event in its batch is decoded, so decode failures cannot discard
// the bytes that produced them.
type Pipeline struct {
	policy BackpressurePolicy
	sink   PipelineSink
	queue  chan PipelineBatch
	done   chan struct{}
	stop   chan struct{}

	mu         sync.Mutex
	closed     bool
	err        error
	submitters sync.WaitGroup
	stopOnce   sync.Once
	closeOnce  sync.Once
	accepted   atomic.Uint64
	rejected   atomic.Uint64
	decoded    atomic.Uint64
	decodeFail atomic.Uint64
	sinkFail   atomic.Uint64
}

type PipelineStats struct {
	Capacity        int
	QueueDepth      int
	AcceptedBatches uint64
	RejectedBatches uint64
	DecodedEvents   uint64
	DecodeFailures  uint64
	SinkFailures    uint64
}

func NewPipeline(capacity int, policy BackpressurePolicy, sink PipelineSink) (*Pipeline, error) {
	if capacity < 1 {
		return nil, fmt.Errorf("pipeline capacity must be positive")
	}
	if policy != BackpressureBlock && policy != BackpressureReject {
		return nil, fmt.Errorf("unsupported backpressure policy %d", policy)
	}
	if sink == nil {
		return nil, fmt.Errorf("pipeline sink is required")
	}
	p := &Pipeline{policy: policy, sink: sink, queue: make(chan PipelineBatch, capacity), done: make(chan struct{}), stop: make(chan struct{})}
	go p.run()
	return p, nil
}

func (p *Pipeline) Submit(ctx context.Context, batch PipelineBatch) (err error) {
	batch = cloneBatch(batch)
	p.mu.Lock()
	if p.closed {
		err := p.resultLocked()
		p.mu.Unlock()
		return err
	}
	p.submitters.Add(1)
	queue := p.queue
	stop := p.stop
	p.mu.Unlock()
	defer p.submitters.Done()
	if p.policy == BackpressureReject {
		select {
		case queue <- batch:
			p.accepted.Add(1)
			return nil
		case <-stop:
			return p.result()
		default:
			p.rejected.Add(1)
			return ErrPipelineFull
		}
	}
	select {
	case queue <- batch:
		p.accepted.Add(1)
		return nil
	case <-stop:
		return p.result()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Pipeline) Stats() PipelineStats {
	return PipelineStats{
		Capacity: cap(p.queue), QueueDepth: len(p.queue),
		AcceptedBatches: p.accepted.Load(), RejectedBatches: p.rejected.Load(),
		DecodedEvents: p.decoded.Load(), DecodeFailures: p.decodeFail.Load(), SinkFailures: p.sinkFail.Load(),
	}
}

func (p *Pipeline) Close() error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
	}
	p.mu.Unlock()
	p.stopOnce.Do(func() { close(p.stop) })
	p.closeQueueAfterSubmitters()
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *Pipeline) run() {
	defer close(p.done)
	for batch := range p.queue {
		if err := p.process(batch); err != nil {
			p.mu.Lock()
			p.err = err
			p.closed = true
			p.mu.Unlock()
			p.stopOnce.Do(func() { close(p.stop) })
			go p.closeQueueAfterSubmitters()
			// Keep draining accepted work so blocked submitters cannot remain
			// stuck; no further sink calls occur after the first failure.
			for range p.queue {
			}
			return
		}
	}
}

func (p *Pipeline) closeQueueAfterSubmitters() {
	p.submitters.Wait()
	p.closeOnce.Do(func() { close(p.queue) })
}

func (p *Pipeline) process(batch PipelineBatch) error {
	if err := p.sink.AppendRaw(batch.Raw); err != nil {
		p.sinkFail.Add(1)
		return fmt.Errorf("capture raw batch: %w", err)
	}
	for _, wire := range batch.Events {
		decoded, err := dt5202.DecodeEvent(wire.Descriptor.Qualifier, wire.Descriptor.TriggerID, wire.Descriptor.Timestamp, wire.Payload)
		if err != nil {
			p.decodeFail.Add(1)
			return fmt.Errorf("decode chain %d node %d: %w", wire.Chain, wire.Descriptor.Node, err)
		}
		p.decoded.Add(1)
		if err := p.sink.AppendEvent(wire, decoded); err != nil {
			p.sinkFail.Add(1)
			return fmt.Errorf("store chain %d node %d event: %w", wire.Chain, wire.Descriptor.Node, err)
		}
	}
	return nil
}

func (p *Pipeline) result() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resultLocked()
}

func (p *Pipeline) resultLocked() error {
	if p.err != nil {
		return p.err
	}
	return ErrPipelineClosed
}

func cloneBatch(batch PipelineBatch) PipelineBatch {
	out := PipelineBatch{Raw: append([]byte(nil), batch.Raw...), Events: make([]dt5215.StreamEvent, len(batch.Events))}
	copy(out.Events, batch.Events)
	for i := range out.Events {
		out.Events[i].Payload = append([]byte(nil), batch.Events[i].Payload...)
	}
	return out
}
