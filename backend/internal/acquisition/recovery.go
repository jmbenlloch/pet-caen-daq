package acquisition

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

type RecoveryBoard struct {
	Chain  uint16
	Node   uint16
	Status uint32
}

type RecoveryHardware interface {
	DrainHardware
	ReadRegister(context.Context, uint16, uint16, uint32) (uint32, error)
}

type StartupRecoveryResult struct {
	Detected        bool
	Boards          []RecoveryBoard
	Original        error
	CleanupWarnings error
}

func RecoverStartup(ctx context.Context, states *StateMachine, hardware RecoveryHardware, boards []RecoveryBoard, expectedChains int, timeout time.Duration, actor string) (StartupRecoveryResult, error) {
	result := StartupRecoveryResult{Boards: append([]RecoveryBoard(nil), boards...)}
	if states == nil || hardware == nil {
		return result, fmt.Errorf("startup recovery state machine and hardware are required")
	}
	if timeout <= 0 {
		return result, fmt.Errorf("startup recovery timeout must be positive")
	}
	var running []string
	for _, board := range boards {
		if dt5202.Status(board.Status).Has(dt5202.StatusRunning) {
			running = append(running, fmt.Sprintf("chain %d node %d status %#x", board.Chain, board.Node, board.Status))
		}
	}
	if len(running) == 0 {
		return result, nil
	}
	result.Detected = true
	result.Original = fmt.Errorf("hardware was already running after process restart: %s", strings.Join(running, "; "))
	if _, err := states.Move(StateFault, actor); err != nil {
		return result, errors.Join(result.Original, err)
	}
	if _, err := states.Move(StateRecovering, actor); err != nil {
		return result, errors.Join(result.Original, err)
	}

	drainCtx, cancelDrain := context.WithTimeout(ctx, timeout)
	if _, err := StopAndDrain(drainCtx, hardware, expectedChains, nil); err != nil {
		result.CleanupWarnings = errors.Join(result.CleanupWarnings, fmt.Errorf("bounded stop and drain: %w", err))
	}
	cancelDrain()
	resetCtx, cancelReset := context.WithTimeout(ctx, timeout)
	defer cancelReset()
	var cleanup error
	if err := hardware.SendCommand(resetCtx, 0xff, 0xff, uint32(dt5202.CommandGlobalReset), 0); err != nil {
		cleanup = errors.Join(cleanup, fmt.Errorf("broadcast global reset: %w", err))
	}
	for _, board := range boards {
		status, err := hardware.ReadRegister(resetCtx, board.Chain, board.Node, uint32(dt5202.AcquisitionStatus))
		if err != nil {
			cleanup = errors.Join(cleanup, fmt.Errorf("verify chain %d node %d status: %w", board.Chain, board.Node, err))
			continue
		}
		decoded := dt5202.Status(status)
		if decoded.Has(dt5202.StatusRunning) || !decoded.Has(dt5202.StatusReady) {
			cleanup = errors.Join(cleanup, fmt.Errorf("verify chain %d node %d status %#x is not ready", board.Chain, board.Node, status))
		}
	}
	if cleanup != nil {
		_, transitionErr := states.Move(StateDisconnected, actor)
		return result, errors.Join(result.Original, result.CleanupWarnings, cleanup, transitionErr)
	}
	if _, err := states.Move(StateIdle, actor); err != nil {
		return result, errors.Join(result.Original, err)
	}
	return result, nil
}
