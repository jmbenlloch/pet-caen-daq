package acquisition

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/transportjournal"
)

type CoordinatorHardware interface {
	Synchronize(context.Context) error
	ClearStream(context.Context) error
	ControlChain(context.Context, uint16, bool, uint32) error
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
	Concentrator           *dt5215.ConcentratorInfo
	Histograms             HistogramOptions
	PersistHistograms      bool
	HDF5SegmentSizeBytes   uint64
	HDF5Compression        string
}

type HistogramOptions struct {
	EnergyBins     int
	EnergyChannels int
	ToABins        int
	ToARebin       int
	ToAMinNS       float64
	ToTBins        int
}

type PipelineFactory func(runID string, options RunOptions) (RunPipeline, error)

type RunPipelineFinalizer interface {
	Finalize(completedAt, reason string) error
}

type RunPipelineAborter interface {
	Abort() error
}

type RunPipelineNotifier interface {
	Done() <-chan struct{}
	Err() error
}

type RunTransportJournal interface {
	TransportJournal() transportjournal.Sink
}

type StreamJournalHardware interface {
	SetStreamJournal(transportjournal.Sink, string, func() time.Time)
}

type activeRun struct {
	id              string
	ctx             context.Context
	cancel          context.CancelFunc
	done            chan struct{}
	pipeline        RunPipeline
	errMu           sync.Mutex
	readErr         error
	journalAttached bool
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
	synchronized   bool
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
	journalAttached := false
	if options.JournalTransport {
		provider, ok := pipeline.(RunTransportJournal)
		hardware, hardwareOK := c.hardware.(StreamJournalHardware)
		if !ok || !hardwareOK || provider.TransportJournal() == nil {
			return c.failStart(fmt.Errorf("transport journal requested but pipeline or stream hardware does not support attachment"), actor, pipeline)
		}
		hardware.SetStreamJournal(provider.TransportJournal(), "run-"+runID, nil)
		journalAttached = true
	}
	c.mu.Lock()
	synchronized := c.synchronized
	c.mu.Unlock()
	if !synchronized {
		if err = c.hardware.Synchronize(ctx); err != nil {
			return c.failStartAttached(fmt.Errorf("synchronize: %w", err), actor, pipeline, journalAttached)
		}
		c.mu.Lock()
		c.synchronized = true
		c.mu.Unlock()
	}
	if err = c.hardware.ClearStream(ctx); err != nil {
		return c.failStartAttached(fmt.Errorf("clear stream: %w", err), actor, pipeline, journalAttached)
	}
	for chain := 0; chain < c.expectedChains; chain++ {
		if err = c.hardware.ControlChain(ctx, uint16(chain), false, 0); err != nil {
			return c.failStartAttached(fmt.Errorf("disable chain %d readout train: %w", chain, err), actor, pipeline, journalAttached)
		}
	}
	if err = c.hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandResetTime, dt5215.TDLCommandDelay); err != nil {
		return c.failStartAttached(fmt.Errorf("reset acquisition time: %w", err), actor, pipeline, journalAttached)
	}
	if err = c.hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandResetPeriodic, dt5215.TDLCommandDelay); err != nil {
		return c.failStartAttached(fmt.Errorf("reset periodic trigger: %w", err), actor, pipeline, journalAttached)
	}
	if err = c.hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandAcquisitionStart, dt5215.TDLCommandDelay); err != nil {
		return c.failStartAttached(fmt.Errorf("start acquisition: %w", err), actor, pipeline, journalAttached)
	}
	for chain := 0; chain < c.expectedChains; chain++ {
		if err = c.hardware.ControlChain(ctx, uint16(chain), true, 0x100); err != nil {
			return c.failStartAttached(fmt.Errorf("enable chain %d readout train: %w", chain, err), actor, pipeline, journalAttached)
		}
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	run := &activeRun{id: runID, ctx: runCtx, cancel: cancel, done: make(chan struct{}), pipeline: pipeline, journalAttached: journalAttached}
	c.mu.Lock()
	c.active = run
	c.lastErr = nil
	c.mu.Unlock()
	if _, err = c.states.Move(StateRunning, actor); err != nil {
		cancel()
		c.detachJournal(journalAttached)
		_ = pipeline.Close()
		return c.recordFault(err, actor)
	}
	go c.readLoop(run)
	if notifier, ok := pipeline.(RunPipelineNotifier); ok {
		go c.watchPipeline(run, notifier)
	}
	go c.watch(run)
	return nil
}

// SetSynchronized records synchronization evidence for the current hardware
// session. A newly constructed coordinator remains conservative and performs
// SNT0 until discovery or a successful synchronization proves it unnecessary.
func (c *Coordinator) SetSynchronized(synchronized bool) {
	c.mu.Lock()
	c.synchronized = synchronized
	c.mu.Unlock()
}

func (c *Coordinator) watchPipeline(run *activeRun, notifier RunPipelineNotifier) {
	<-notifier.Done()
	if err := notifier.Err(); err != nil {
		run.setError(fmt.Errorf("run pipeline: %w", err))
		run.cancel()
	}
}

func (c *Coordinator) Stop(ctx context.Context, actor string) error {
	return c.StopWithReason(ctx, actor, "operator_stop")
}

func (c *Coordinator) StopWithReason(ctx context.Context, actor, reason string) error {
	stopStarted := time.Now()
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
	log.Printf("run_timing run_id=%s stage=stop_requested reason=%s", run.id, reason)
	stageStarted := time.Now()
	run.cancel()
	select {
	case <-run.done:
	case <-ctx.Done():
		return c.finishFault(run, ctx.Err(), actor)
	}
	log.Printf("run_timing run_id=%s stage=read_loop_stop duration_ms=%d", run.id, time.Since(stageStarted).Milliseconds())
	if _, err := c.states.Move(StateDraining, actor); err != nil {
		return c.finishFault(run, err, actor)
	}
	drainCtx, cancel := boundedContext(ctx, c.drainTimeout)
	// Once a complete batch has been read from the hardware, preserve it even
	// if the stream-drain deadline expires while the bounded persistence queue
	// catches up. Pipeline.Close below already waits for accepted storage work,
	// and pipeline failures independently unblock Submit.
	deliveryCtx := context.WithoutCancel(ctx)
	stageStarted = time.Now()
	drainResult, drainErr := StopAndDrain(drainCtx, c.hardware, c.expectedChains, func(raw []byte, events []dt5215.StreamEvent) error {
		return run.pipeline.Submit(deliveryCtx, PipelineBatch{Raw: raw, Events: events})
	})
	cancel()
	log.Printf("run_timing run_id=%s stage=hardware_drain duration_ms=%d batches=%d events=%d error=%t", run.id, time.Since(stageStarted).Milliseconds(), drainResult.Batches, drainResult.Events, drainErr != nil)
	result := JoinStopError(run.error(), drainErr)
	c.detachJournal(run.journalAttached)
	stageStarted = time.Now()
	result = JoinStopError(result, run.pipeline.Close())
	if provider, ok := run.pipeline.(interface{ PipelineStats() PipelineStats }); ok {
		stats := provider.PipelineStats()
		log.Printf("run_timing run_id=%s stage=pipeline_close duration_ms=%d received_batches=%d accepted_batches=%d decoded_events=%d persisted_events=%d raw_persisted=%d event_batches_persisted=%d", run.id, time.Since(stageStarted).Milliseconds(), stats.ReceivedBatches, stats.AcceptedBatches, stats.DecodedEvents, stats.PersistedEvents, stats.RawBatchesPersisted, stats.EventBatchesPersisted)
	} else {
		log.Printf("run_timing run_id=%s stage=pipeline_close duration_ms=%d", run.id, time.Since(stageStarted).Milliseconds())
	}
	if result == nil {
		if finalizer, ok := run.pipeline.(RunPipelineFinalizer); ok {
			stageStarted = time.Now()
			result = finalizer.Finalize(time.Now().UTC().Format(time.RFC3339Nano), reason)
			log.Printf("run_timing run_id=%s stage=run_finalize duration_ms=%d error=%t", run.id, time.Since(stageStarted).Milliseconds(), result != nil)
		}
	}
	if result != nil {
		result = JoinStopError(result, abortPipeline(run.pipeline))
		return c.finishFault(run, result, actor)
	}
	c.clearActive(run, nil)
	_, err := c.states.Move(StateReady, actor)
	log.Printf("run_timing run_id=%s stage=ready total_stop_ms=%d error=%t", run.id, time.Since(stopStarted).Milliseconds(), err != nil)
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
			run.setError(fmt.Errorf("read stream batch: %w", err))
			return
		}
		if err = run.pipeline.Submit(run.ctx, PipelineBatch{Raw: raw, Events: events}); err != nil {
			if run.ctx.Err() != nil {
				return
			}
			run.setError(fmt.Errorf("submit stream batch: %w", err))
			return
		}
	}
}

func (c *Coordinator) watch(run *activeRun) {
	<-run.done
	readErr := run.error()
	if readErr == nil {
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
	_ = c.recordFault(readErr, "backend")
	cleanupCtx, cancel := context.WithTimeout(context.Background(), c.drainTimeout)
	deliveryCtx := context.WithoutCancel(cleanupCtx)
	_, cleanupErr := StopAndDrain(cleanupCtx, c.hardware, c.expectedChains, func(raw []byte, events []dt5215.StreamEvent) error {
		return run.pipeline.Submit(deliveryCtx, PipelineBatch{Raw: raw, Events: events})
	})
	cancel()
	err := JoinStopError(readErr, cleanupErr)
	c.detachJournal(run.journalAttached)
	err = JoinStopError(err, run.pipeline.Close())
	err = JoinStopError(err, abortPipeline(run.pipeline))
	c.clearActive(run, err)
}

func (r *activeRun) setError(err error) {
	r.errMu.Lock()
	if r.readErr == nil {
		r.readErr = err
	}
	r.errMu.Unlock()
}

func (r *activeRun) error() error {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	return r.readErr
}

func (c *Coordinator) failStart(primary error, actor string, pipeline RunPipeline) error {
	if pipeline != nil {
		primary = JoinStopError(primary, pipeline.Close())
		primary = JoinStopError(primary, abortPipeline(pipeline))
	}
	return c.recordFault(primary, actor)
}

func (c *Coordinator) failStartAttached(primary error, actor string, pipeline RunPipeline, attached bool) error {
	c.detachJournal(attached)
	return c.failStart(primary, actor, pipeline)
}

func (c *Coordinator) detachJournal(attached bool) {
	if !attached {
		return
	}
	if hardware, ok := c.hardware.(StreamJournalHardware); ok {
		hardware.SetStreamJournal(nil, "", nil)
	}
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
