package acquisition

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
)

type StaircaseTarget struct {
	Board       uint32
	Chain, Node uint16
	Plan        dt5202.ConfigurationPlan
}

// StaircaseCoordinator serializes a scan through the shared state machine and
// guarantees a complete non-HV production configuration restore.
type StaircaseCoordinator struct {
	mu       sync.Mutex
	states   *StateMachine
	hardware ConfigurationHardware
	targets  map[uint32]StaircaseTarget
}

func NewStaircaseCoordinator(states *StateMachine, hardware ConfigurationHardware, targets []StaircaseTarget) (*StaircaseCoordinator, error) {
	if states == nil || hardware == nil {
		return nil, fmt.Errorf("state machine and hardware are required")
	}
	coordinator := &StaircaseCoordinator{states: states, hardware: hardware}
	coordinator.SetTargets(targets)
	return coordinator, nil
}

func (c *StaircaseCoordinator) SetTargets(targets []StaircaseTarget) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.targets = make(map[uint32]StaircaseTarget, len(targets))
	for _, target := range targets {
		c.targets[target.Board] = target
	}
}

func (c *StaircaseCoordinator) Run(ctx context.Context, request staircase.Request, actor string, observe func(staircase.Point) error) (restored bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	target, ok := c.targets[request.Board]
	if !ok {
		return false, fmt.Errorf("board %d is not configured", request.Board)
	}
	if _, err = c.states.Move(StateScanning, actor); err != nil {
		return false, err
	}
	defer func() {
		restoreCtx := context.WithoutCancel(ctx)
		restoreErr := dt5202.ApplyConfiguration(restoreCtx, c.hardware, target.Chain, target.Node, target.Plan, true)
		restored = restoreErr == nil
		if restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("restore board %d configuration: %w", request.Board, restoreErr))
		}
		if restoreErr != nil || (err != nil && !errors.Is(err, context.Canceled)) {
			_, transitionErr := c.states.Move(StateFault, actor)
			err = errors.Join(err, transitionErr)
			return
		}
		_, transitionErr := c.states.Move(StateReady, actor)
		err = errors.Join(err, transitionErr)
	}()
	timeMask := registerValue(target.Plan, dt5202.TimeDiscriminatorMaskLow) |
		uint64(registerValue(target.Plan, dt5202.TimeDiscriminatorMaskHigh))<<32
	err = staircase.Run(ctx, c.hardware, target.Chain, target.Node, request, timeMask, observe)
	return restored, err
}

func registerValue(plan dt5202.ConfigurationPlan, address dt5202.Register) uint64 {
	for index := len(plan.Writes) - 1; index >= 0; index-- {
		if plan.Writes[index].Address == address {
			return uint64(plan.Writes[index].Value)
		}
	}
	return 0
}
