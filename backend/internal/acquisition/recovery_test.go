package acquisition

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

type recoveryHardware struct {
	stopErr  error
	resetErr error
	status   uint32
	reads    int
}

func (h *recoveryHardware) SendCommand(_ context.Context, _, _ uint16, command, _ uint32) error {
	if command == uint32(dt5202.CommandAcquisitionStop) {
		return h.stopErr
	}
	if command == uint32(dt5202.CommandGlobalReset) {
		return h.resetErr
	}
	return nil
}
func (h *recoveryHardware) ReadRawStreamBatch(context.Context) ([]byte, []dt5215.StreamEvent, error) {
	if h.reads == 0 {
		h.reads++
		return []byte("completion"), []dt5215.StreamEvent{completion(0, true)}, nil
	}
	return nil, nil, errors.New("unexpected read")
}
func (h *recoveryHardware) ReadRegister(context.Context, uint16, uint16, uint32) (uint32, error) {
	return h.status, nil
}

func TestRecoverStartupStopsResetsAndReturnsToIdle(t *testing.T) {
	states, _ := NewStateMachine(StateIdle, nil)
	hardware := &recoveryHardware{status: uint32(dt5202.StatusReady)}
	result, err := RecoverStartup(context.Background(), states, hardware, []RecoveryBoard{{Status: uint32(dt5202.StatusRunning)}}, 1, time.Second, "restart")
	if err != nil || !result.Detected || result.Original == nil || states.Snapshot().State != StateIdle {
		t.Fatalf("result=%+v error=%v state=%s", result, err, states.Snapshot().State)
	}
	history := states.History()
	if len(history) != 3 || history[0].To != StateFault || history[1].To != StateRecovering || history[2].To != StateIdle {
		t.Fatalf("history=%+v", history)
	}
}

func TestRecoverStartupKeepsDetectionAndCleanupErrors(t *testing.T) {
	states, _ := NewStateMachine(StateIdle, nil)
	hardware := &recoveryHardware{stopErr: errors.New("stop failed"), resetErr: errors.New("reset failed"), status: uint32(dt5202.StatusRunning)}
	_, err := RecoverStartup(context.Background(), states, hardware, []RecoveryBoard{{Status: uint32(dt5202.StatusRunning)}}, 1, time.Second, "restart")
	if err == nil || states.Snapshot().State != StateDisconnected || !strings.Contains(err.Error(), "already running") || !strings.Contains(err.Error(), "stop failed") || !strings.Contains(err.Error(), "reset failed") || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("error=%v state=%s", err, states.Snapshot().State)
	}
}
