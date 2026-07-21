package acquisition

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

type coordinatorHardware struct {
	mu        sync.Mutex
	commands  []uint32
	readCount int
	readErr   error
	startErr  error
}

// The coordinator and configurator deliberately share the production client,
// while this test fake only exercises configurator validation before I/O.
func (h *coordinatorHardware) WriteRegister(context.Context, uint16, uint16, uint32, uint32) error {
	return nil
}
func (h *coordinatorHardware) ReadRegister(context.Context, uint16, uint16, uint32) (uint32, error) {
	return 0, nil
}

func (h *coordinatorHardware) Synchronize(context.Context) error { return nil }
func (h *coordinatorHardware) ClearStream(context.Context) error { return nil }
func (h *coordinatorHardware) SendCommand(_ context.Context, _, _ uint16, command, _ uint32) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commands = append(h.commands, command)
	if command == dt5215.CommandAcquisitionStart {
		return h.startErr
	}
	if command == dt5215.CommandAcquisitionStop && h.readErr != nil {
		return io.EOF
	}
	return nil
}
func (h *coordinatorHardware) ReadRawStreamBatch(ctx context.Context) ([]byte, []dt5215.StreamEvent, error) {
	h.mu.Lock()
	h.readCount++
	count, readErr := h.readCount, h.readErr
	h.mu.Unlock()
	if readErr != nil && count == 1 {
		return nil, nil, readErr
	}
	if count == 1 {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	return []byte("complete"), []dt5215.StreamEvent{completion(0, true)}, nil
}

type coordinatorPipeline struct {
	mu        sync.Mutex
	batches   []PipelineBatch
	closed    bool
	finalized bool
	aborted   bool
}

func (p *coordinatorPipeline) Submit(_ context.Context, batch PipelineBatch) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.batches = append(p.batches, batch)
	return nil
}
func (p *coordinatorPipeline) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}
func (p *coordinatorPipeline) Finalize(string, string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finalized = true
	return nil
}
func (p *coordinatorPipeline) Abort() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.aborted = true
	return nil
}

func readyCoordinator(t *testing.T, hardware *coordinatorHardware) (*Coordinator, *StateMachine, *coordinatorPipeline) {
	t.Helper()
	states, _ := NewStateMachine(StateReady, nil)
	pipeline := &coordinatorPipeline{}
	coordinator, err := NewCoordinator(states, hardware, func(string, RunOptions) (RunPipeline, error) { return pipeline, nil }, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return coordinator, states, pipeline
}

func TestCoordinatorStartsReadsAndDrainsToReady(t *testing.T) {
	hardware := &coordinatorHardware{}
	coordinator, states, pipeline := readyCoordinator(t, hardware)
	if err := coordinator.Start(context.Background(), "run-1", "operator", RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if states.Snapshot().State != StateRunning || coordinator.ActiveRunID() != "run-1" {
		t.Fatalf("state=%s run=%q", states.Snapshot().State, coordinator.ActiveRunID())
	}
	if err := coordinator.Stop(context.Background(), "operator"); err != nil {
		t.Fatal(err)
	}
	if states.Snapshot().State != StateReady || coordinator.ActiveRunID() != "" {
		t.Fatalf("state=%s run=%q", states.Snapshot().State, coordinator.ActiveRunID())
	}
	pipeline.mu.Lock()
	defer pipeline.mu.Unlock()
	if !pipeline.closed || !pipeline.finalized || pipeline.aborted || len(pipeline.batches) != 1 || string(pipeline.batches[0].Raw) != "complete" {
		t.Fatalf("pipeline=%+v", pipeline)
	}
}

func TestCoordinatorStartFailureMovesToFaultAndClosesPipeline(t *testing.T) {
	sentinel := errors.New("start rejected")
	hardware := &coordinatorHardware{startErr: sentinel}
	coordinator, states, pipeline := readyCoordinator(t, hardware)
	err := coordinator.Start(context.Background(), "run-1", "operator", RunOptions{})
	if !errors.Is(err, sentinel) || states.Snapshot().State != StateFault || !pipeline.closed || !pipeline.aborted || pipeline.finalized {
		t.Fatalf("error=%v state=%s pipeline=%+v", err, states.Snapshot().State, pipeline)
	}
}

func TestCoordinatorStreamFailureStopsAndRecordsPrimaryError(t *testing.T) {
	sentinel := errors.New("stream disconnected")
	hardware := &coordinatorHardware{readErr: sentinel}
	coordinator, states, _ := readyCoordinator(t, hardware)
	faults := make(chan error, 1)
	coordinator.SetFaultObserver(func(err error) { faults <- err })
	if err := coordinator.Start(context.Background(), "run-1", "operator", RunOptions{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for states.Snapshot().State != StateFault || coordinator.ActiveRunID() != "" {
		if time.Now().After(deadline) {
			t.Fatalf("state=%s run=%q error=%v", states.Snapshot().State, coordinator.ActiveRunID(), coordinator.LastError())
		}
		time.Sleep(time.Millisecond)
	}
	if !errors.Is(coordinator.LastError(), sentinel) {
		t.Fatalf("last error = %v", coordinator.LastError())
	}
	select {
	case observed := <-faults:
		if !errors.Is(observed, sentinel) {
			t.Fatalf("observed fault = %v", observed)
		}
	default:
		t.Fatal("coordinator fault was not published")
	}
}

func TestCoordinatorRejectsInvalidStartWithoutCreatingPipeline(t *testing.T) {
	states, _ := NewStateMachine(StateIdle, nil)
	created := false
	coordinator, _ := NewCoordinator(states, &coordinatorHardware{}, func(string, RunOptions) (RunPipeline, error) {
		created = true
		return &coordinatorPipeline{}, nil
	}, 1, time.Second)
	if err := coordinator.Start(context.Background(), "run-1", "operator", RunOptions{}); err == nil || created {
		t.Fatalf("error=%v created=%v", err, created)
	}
}

func TestConfiguratorRejectsDuplicateTargetAndPublishesFailure(t *testing.T) {
	states, _ := NewStateMachine(StateIdle, nil)
	var progress []ConfigurationProgress
	configurator, err := NewConfigurator(states, &coordinatorHardware{}, func(update ConfigurationProgress) {
		progress = append(progress, update)
	})
	if err != nil {
		t.Fatal(err)
	}
	document := &janusconfig.Document{}
	_, err = configurator.Configure(context.Background(), document, []ConfigurationTarget{{Board: 0}, {Board: 0}}, ConfigureOptions{Actor: "operator"})
	if err == nil || states.Snapshot().State != StateFault {
		t.Fatalf("error=%v state=%s", err, states.Snapshot().State)
	}
	if len(progress) != 1 || progress[0].Stage != ConfigurationFailed || progress[0].Err == nil {
		t.Fatalf("progress=%#v", progress)
	}
	history := states.History()
	if len(history) != 2 || history[0].From != StateIdle || history[0].To != StateConfiguring || history[1].To != StateFault {
		t.Fatalf("history=%#v", history)
	}
}
