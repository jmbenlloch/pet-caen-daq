package acquisition

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

type CoordinatorHardware interface {
	Synchronize(context.Context) error
	ClearStream(context.Context) error
	SendCommand(context.Context, uint16, uint16, uint32, uint32) error
	ReadRawStreamBatch(context.Context) ([]byte, []dt5215.StreamEvent, error)
}

type RunPipeline interface {
	Submit(context.Context, PipelineBatch) error
	Close() error
}

type RunOptions struct {
	CaptureRaw             bool
	JournalTransport       bool
	RequestedBy            string
	RequestedConfiguration string
	EffectiveConfiguration []dt5202.ConfigurationPlan
	ConfigurationAudit     *configaudit.Report
}

type PipelineFactory func(runID string, options RunOptions) (RunPipeline, error)

type RunPipelineFinalizer interface {
	Finalize(completedAt, reason string) error
}

type RunPipelineAborter interface {
	Abort() error
}

type activeRun struct {
	id       string
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	pipeline RunPipeline
	readErr  error
}

// Coordinator owns one long-running acquisition and serializes start/stop.
// Configuration application remains a separate ready-state operation.
type Coordinator struct {
	opMu sync.Mutex
	mu   sync.Mutex

	states         *StateMachine
	hardware       CoordinatorHardware
	newPipeline    PipelineFactory
	expectedChains int
	drainTimeout   time.Duration
	active         *activeRun
	lastErr        error
	faultObserver  func(error)
}

func NewCoordinator(states *StateMachine, hardware CoordinatorHardware, newPipeline PipelineFactory, expectedChains int, drainTimeout time.Duration) (*Coordinator, error) {
	if states == nil || hardware == nil || newPipeline == nil {
		return nil, fmt.Errorf("state machine, hardware, and pipeline factory are required")
	}
	if expectedChains < 1 || expectedChains > dt5215.MaxChains {
		return nil, fmt.Errorf("expected chain count %d out of range", expectedChains)
	}
	if drainTimeout <= 0 {
		return nil, fmt.Errorf("drain timeout must be positive")
	}
	return &Coordinator{states: states, hardware: hardware, newPipeline: newPipeline, expectedChains: expectedChains, drainTimeout: drainTimeout}, nil
}

func (c *Coordinator) Start(ctx context.Context, runID, actor string, options RunOptions) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if runID == "" {
		return fmt.Errorf("run ID is required")
	}
	if _, err := c.states.Move(StateStarting, actor); err != nil {
		return err
	}
	pipeline, err := c.newPipeline(runID, options)
	if err != nil {
		return c.failStart(fmt.Errorf("create run pipeline: %w", err), actor, nil)
	}
	if err = c.hardware.Synchronize(ctx); err != nil {
		return c.failStart(fmt.Errorf("synchronize: %w", err), actor, pipeline)
	}
	if err = c.hardware.ClearStream(ctx); err != nil {
		return c.failStart(fmt.Errorf("clear stream: %w", err), actor, pipeline)
	}
	if err = c.hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandAcquisitionStart, 0); err != nil {
		return c.failStart(fmt.Errorf("start acquisition: %w", err), actor, pipeline)
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	run := &activeRun{id: runID, ctx: runCtx, cancel: cancel, done: make(chan struct{}), pipeline: pipeline}
	c.mu.Lock()
	c.active = run
	c.lastErr = nil
	c.mu.Unlock()
	if _, err = c.states.Move(StateRunning, actor); err != nil {
		cancel()
		_ = pipeline.Close()
		return c.recordFault(err, actor)
	}
	go c.readLoop(run)
	go c.watch(run)
	return nil
}

func (c *Coordinator) Stop(ctx context.Context, actor string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.mu.Lock()
	run := c.active
	c.mu.Unlock()
	if run == nil {
		if c.states.Snapshot().State == StateReady {
			return nil
		}
		return fmt.Errorf("no active run to stop in state %s", c.states.Snapshot().State)
	}
	if _, err := c.states.Move(StateStopping, actor); err != nil {
		return err
	}
	run.cancel()
	select {
	case <-run.done:
	case <-ctx.Done():
		return c.finishFault(run, ctx.Err(), actor)
	}
	if _, err := c.states.Move(StateDraining, actor); err != nil {
		return c.finishFault(run, err, actor)
	}
	drainCtx, cancel := boundedContext(ctx, c.drainTimeout)
	_, drainErr := StopAndDrain(drainCtx, c.hardware, c.expectedChains, func(raw []byte, events []dt5215.StreamEvent) error {
		return run.pipeline.Submit(drainCtx, PipelineBatch{Raw: raw, Events: events})
	})
	cancel()
	result := JoinStopError(run.readErr, drainErr)
	result = JoinStopError(result, run.pipeline.Close())
	if result == nil {
		if finalizer, ok := run.pipeline.(RunPipelineFinalizer); ok {
			result = finalizer.Finalize(time.Now().UTC().Format(time.RFC3339Nano), "operator_stop")
		}
	}
	if result != nil {
		result = JoinStopError(result, abortPipeline(run.pipeline))
		return c.finishFault(run, result, actor)
	}
	c.clearActive(run, nil)
	_, err := c.states.Move(StateReady, actor)
	return err
}

func (c *Coordinator) ActiveRunID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil {
		return ""
	}
	return c.active.id
}

func (c *Coordinator) StateSnapshot() StateSnapshot { return c.states.Snapshot() }

func (c *Coordinator) LastError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

func (c *Coordinator) ActivePipeline() RunPipeline {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil {
		return nil
	}
	return c.active.pipeline
}

func (c *Coordinator) SetFaultObserver(observer func(error)) {
	c.mu.Lock()
	c.faultObserver = observer
	c.mu.Unlock()
}

func (c *Coordinator) readLoop(run *activeRun) {
	defer close(run.done)
	for {
		raw, events, err := c.hardware.ReadRawStreamBatch(run.ctx)
		if err != nil {
			if run.ctx.Err() != nil {
				return
			}
			run.readErr = fmt.Errorf("read stream batch: %w", err)
			return
		}
		if err = run.pipeline.Submit(run.ctx, PipelineBatch{Raw: raw, Events: events}); err != nil {
			if run.ctx.Err() != nil {
				return
			}
			run.readErr = fmt.Errorf("submit stream batch: %w", err)
			return
		}
	}
}

func (c *Coordinator) watch(run *activeRun) {
	<-run.done
	if run.readErr == nil {
		return
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.mu.Lock()
	current := c.active == run
	c.mu.Unlock()
	if !current {
		return
	}
	_ = c.recordFault(run.readErr, "backend")
	cleanupCtx, cancel := context.WithTimeout(context.Background(), c.drainTimeout)
	_, cleanupErr := StopAndDrain(cleanupCtx, c.hardware, c.expectedChains, func(raw []byte, events []dt5215.StreamEvent) error {
		return run.pipeline.Submit(cleanupCtx, PipelineBatch{Raw: raw, Events: events})
	})
	cancel()
	err := JoinStopError(run.readErr, cleanupErr)
	err = JoinStopError(err, run.pipeline.Close())
	err = JoinStopError(err, abortPipeline(run.pipeline))
	c.clearActive(run, err)
}

func (c *Coordinator) failStart(primary error, actor string, pipeline RunPipeline) error {
	if pipeline != nil {
		primary = JoinStopError(primary, pipeline.Close())
		primary = JoinStopError(primary, abortPipeline(pipeline))
	}
	return c.recordFault(primary, actor)
}

func abortPipeline(pipeline RunPipeline) error {
	if aborter, ok := pipeline.(RunPipelineAborter); ok {
		return aborter.Abort()
	}
	return nil
}

func (c *Coordinator) finishFault(run *activeRun, err error, actor string) error {
	_ = c.recordFault(err, actor)
	c.clearActive(run, err)
	return err
}

func (c *Coordinator) recordFault(err error, actor string) error {
	if _, transitionErr := c.states.Move(StateFault, actor); transitionErr != nil {
		return errors.Join(err, transitionErr)
	}
	c.mu.Lock()
	c.lastErr = err
	observer := c.faultObserver
	c.mu.Unlock()
	if observer != nil {
		observer(err)
	}
	return err
}

func (c *Coordinator) clearActive(run *activeRun, err error) {
	c.mu.Lock()
	if c.active == run {
		c.active = nil
	}
	if err != nil {
		c.lastErr = err
	}
	c.mu.Unlock()
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) <= timeout {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}
