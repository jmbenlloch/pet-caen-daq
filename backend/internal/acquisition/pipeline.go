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

// Pipeline preserves ordering within separate raw and decoded-event workers.
// An event batch waits for its raw batch to persist, while the raw worker may
// capture the next batch concurrently with decoding and event persistence.
type Pipeline struct {
	policy BackpressurePolicy
	sink   PipelineSink
	queue  chan PipelineBatch
	raw    chan *pipelineJob
	events chan *pipelineJob
	slots  chan struct{}
	done   chan struct{}
	stop   chan struct{}

	mu             sync.Mutex
	closed         bool
	err            error
	submitters     sync.WaitGroup
	stopOnce       sync.Once
	closeOnce      sync.Once
	accepted       atomic.Uint64
	rejected       atomic.Uint64
	received       atomic.Uint64
	receivedEvents atomic.Uint64
	rawPersisted   atomic.Uint64
	eventBatches   atomic.Uint64
	persisted      atomic.Uint64
	decoded        atomic.Uint64
	decodeFail     atomic.Uint64
	sinkFail       atomic.Uint64
}

type PipelineStats struct {
	Capacity              int
	QueueDepth            int
	RawQueueDepth         int
	EventQueueDepth       int
	ReceivedBatches       uint64
	ReceivedEvents        uint64
	AcceptedBatches       uint64
	RejectedBatches       uint64
	RawBatchesPersisted   uint64
	EventBatchesPersisted uint64
	DecodedEvents         uint64
	PersistedEvents       uint64
	DecodeFailures        uint64
	SinkFailures          uint64
}

type pipelineJob struct {
	batch   PipelineBatch
	rawDone chan struct{}
	rawErr  error
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
	p := &Pipeline{
		policy: policy, sink: sink,
		queue:  make(chan PipelineBatch, capacity),
		raw:    make(chan *pipelineJob, capacity),
		events: make(chan *pipelineJob, capacity),
		slots:  make(chan struct{}, capacity+1),
		done:   make(chan struct{}), stop: make(chan struct{}),
	}
	go p.run()
	return p, nil
}

func (p *Pipeline) Submit(ctx context.Context, batch PipelineBatch) (err error) {
	batch = cloneBatch(batch)
	p.received.Add(1)
	p.receivedEvents.Add(uint64(len(batch.Events)))
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
		case <-stop:
			return p.result()
		case p.slots <- struct{}{}:
		default:
			p.rejected.Add(1)
			return ErrPipelineFull
		}
		select {
		case queue <- batch:
			p.accepted.Add(1)
			return nil
		case <-stop:
			<-p.slots
			return p.result()
		}
	}
	select {
	case <-stop:
		return p.result()
	case <-ctx.Done():
		return ctx.Err()
	case p.slots <- struct{}{}:
	}
	select {
	case queue <- batch:
		p.accepted.Add(1)
		return nil
	case <-stop:
		<-p.slots
		return p.result()
	case <-ctx.Done():
		<-p.slots
		return ctx.Err()
	}
}

func (p *Pipeline) Stats() PipelineStats {
	queueDepth := len(p.slots)
	if queueDepth > 0 {
		queueDepth--
	}
	return PipelineStats{
		Capacity: cap(p.queue), QueueDepth: queueDepth,
		RawQueueDepth: len(p.raw), EventQueueDepth: len(p.events),
		ReceivedBatches: p.received.Load(), ReceivedEvents: p.receivedEvents.Load(),
		AcceptedBatches: p.accepted.Load(), RejectedBatches: p.rejected.Load(),
		RawBatchesPersisted: p.rawPersisted.Load(), EventBatchesPersisted: p.eventBatches.Load(),
		DecodedEvents: p.decoded.Load(), PersistedEvents: p.persisted.Load(),
		DecodeFailures: p.decodeFail.Load(), SinkFailures: p.sinkFail.Load(),
	}
}

func (p *Pipeline) Done() <-chan struct{} { return p.done }
func (p *Pipeline) Err() error            { return p.result() }

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
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		p.runRaw()
	}()
	go func() {
		defer workers.Done()
		p.runEvents()
	}()
	for batch := range p.queue {
		job := &pipelineJob{batch: batch, rawDone: make(chan struct{})}
		p.raw <- job
		p.events <- job
	}
	close(p.raw)
	close(p.events)
	workers.Wait()
}

func (p *Pipeline) closeQueueAfterSubmitters() {
	p.submitters.Wait()
	p.closeOnce.Do(func() { close(p.queue) })
}

func (p *Pipeline) runRaw() {
	for job := range p.raw {
		if !p.failed() {
			if err := p.sink.AppendRaw(job.batch.Raw); err != nil {
				p.sinkFail.Add(1)
				job.rawErr = fmt.Errorf("capture raw batch: %w", err)
				p.fail(job.rawErr)
			} else {
				p.rawPersisted.Add(1)
			}
		}
		close(job.rawDone)
	}
}

func (p *Pipeline) runEvents() {
	for job := range p.events {
		<-job.rawDone
		if job.rawErr != nil || p.failed() {
			<-p.slots
			continue
		}
		if err := p.processEvents(job.batch); err != nil {
			p.fail(err)
			<-p.slots
			continue
		}
		p.eventBatches.Add(1)
		<-p.slots
	}
}

func (p *Pipeline) processEvents(batch PipelineBatch) error {
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
		p.persisted.Add(1)
	}
	return nil
}

func (p *Pipeline) failed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err != nil
}

func (p *Pipeline) fail(err error) {
	p.mu.Lock()
	if p.err == nil {
		p.err = err
		p.closed = true
		p.stopOnce.Do(func() { close(p.stop) })
		go p.closeQueueAfterSubmitters()
	}
	p.mu.Unlock()
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
