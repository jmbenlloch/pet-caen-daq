package acquisition

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
)

type HoldDelayHardware interface {
	ConfigurationHardware
	holddelay.Hardware
}

// HoldDelayCoordinator owns the spectroscopy stream while a scan is active
// and guarantees the same complete non-HV restoration used by staircases.
type HoldDelayCoordinator struct {
	mu       sync.Mutex
	states   *StateMachine
	hardware HoldDelayHardware
	targets  map[uint32]StaircaseTarget
}

func NewHoldDelayCoordinator(states *StateMachine, hardware HoldDelayHardware, targets []StaircaseTarget) (*HoldDelayCoordinator, error) {
	if states == nil || hardware == nil {
		return nil, fmt.Errorf("state machine and hardware are required")
	}
	coordinator := &HoldDelayCoordinator{states: states, hardware: hardware}
	coordinator.SetTargets(targets)
	return coordinator, nil
}

func (c *HoldDelayCoordinator) SetTargets(targets []StaircaseTarget) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.targets = make(map[uint32]StaircaseTarget, len(targets))
	for _, target := range targets {
		c.targets[target.Board] = target
	}
}

func (c *HoldDelayCoordinator) Run(ctx context.Context, request holddelay.Request, actor string, observe func(holddelay.Point) error) (restored bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	target, ok := c.targets[request.Board]
	if !ok {
		return false, fmt.Errorf("board %d is not configured", request.Board)
	}
	if !hasSpectroscopy(target.Plan.Pedestal.AcquisitionMode) {
		return false, fmt.Errorf("hold-delay scan requires SPECTROSCOPY acquisition mode")
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

	acquisitionControl := uint32(registerValue(target.Plan, dt5202.AcquisitionControl))
	acquisitionControl = acquisitionControl&^0x7&^(0x3<<12) | 1 | 0x3<<12
	for _, write := range []dt5202.RegisterWrite{
		{Address: dt5202.AcquisitionControl, Value: acquisitionControl},
		{Address: dt5202.TriggerMask, Value: uint32(registerValue(target.Plan, dt5202.TriggerMask))},
		{Address: dt5202.RunMask, Value: 1},
	} {
		if err = c.hardware.WriteRegister(ctx, target.Chain, target.Node, uint32(write.Address), write.Value); err != nil {
			return false, fmt.Errorf("prepare hold-delay register %#08x: %w", write.Address, err)
		}
	}
	range14 := acquisitionControl&(1<<21) != 0
	err = holddelay.Run(ctx, c.hardware, target.Chain, target.Node, request, range14, observe)
	return restored, err
}

func hasSpectroscopy(acquisitionMode uint32) bool {
	return acquisitionMode&1 != 0
}
