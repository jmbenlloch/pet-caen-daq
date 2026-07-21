package service

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

type HVTarget struct {
	Board int
	Chain uint16
	Node  uint16
}

type HVController interface {
	Set(context.Context, []uint32, bool, string) error
}

// NativeHVController owns safe live HV switching and idle monitor polling.
// Configuration setpoints remain owned by the configuration orchestrator.
type NativeHVController struct {
	Hardware   dt5202.HVHardware
	States     *acquisition.StateMachine
	Publisher  SnapshotPublisher
	Targets    []HVTarget
	Authorized bool
	mu         sync.Mutex
}

func (c *NativeHVController) Set(ctx context.Context, requested []uint32, enabled bool, actor string) error {
	if actor == "" {
		return fmt.Errorf("HV action requires requested_by")
	}
	if enabled && !c.Authorized {
		return fmt.Errorf("HV enable requires backend --authorize-hv-config")
	}
	if c.States == nil || c.States.Snapshot().State != acquisition.StateReady {
		return fmt.Errorf("HV switching is allowed only while system is ready")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	targets, err := c.selectTargets(requested)
	if err != nil {
		return err
	}
	completed := make([]HVTarget, 0, len(targets))
	for _, target := range targets {
		if err := dt5202.SetHVOn(ctx, c.Hardware, target.Chain, target.Node, enabled); err != nil {
			if enabled {
				for _, rollback := range completed {
					_ = dt5202.SetHVOn(ctx, c.Hardware, rollback.Chain, rollback.Node, false)
				}
			}
			return fmt.Errorf("board %d HV on=%t requested by %s: %w", target.Board, enabled, actor, err)
		}
		completed = append(completed, target)
	}
	c.refresh(ctx, targets)
	return nil
}

func (c *NativeHVController) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("HV monitor interval must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	c.refresh(ctx, c.Targets)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.refresh(ctx, c.Targets)
		}
	}
}

func (c *NativeHVController) selectTargets(requested []uint32) ([]HVTarget, error) {
	if len(requested) == 0 {
		return append([]HVTarget(nil), c.Targets...), nil
	}
	available := make(map[uint32]HVTarget, len(c.Targets))
	for _, target := range c.Targets {
		available[uint32(target.Board)] = target
	}
	seen := make(map[uint32]bool, len(requested))
	result := make([]HVTarget, 0, len(requested))
	for _, board := range requested {
		if seen[board] {
			return nil, fmt.Errorf("duplicate HV board %d", board)
		}
		target, ok := available[board]
		if !ok {
			return nil, fmt.Errorf("unknown HV board %d", board)
		}
		seen[board] = true
		result = append(result, target)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Board < result[j].Board })
	return result, nil
}

func (c *NativeHVController) refresh(ctx context.Context, targets []HVTarget) {
	if c.Publisher == nil || c.Hardware == nil {
		return
	}
	snapshot := c.Publisher.Snapshot()
	for _, target := range targets {
		reading, err := dt5202.ReadHVTelemetry(ctx, c.Hardware, target.Chain, target.Node)
		board := findBoard(snapshot, uint32(target.Chain), uint32(target.Node))
		if board == nil {
			continue
		}
		if err != nil {
			board.Health = daqv1.HealthStatus_HEALTH_STATUS_DEGRADED
			continue
		}
		board.FpgaTemperatureC = reading.FPGATemperatureC
		board.BoardTemperatureC = reading.BoardTemperatureC
		board.DetectorTemperatureC = reading.DetectorTemperatureC
		board.HvTemperatureC = reading.HVTemperatureC
		board.HvVoltageV = reading.VoltageV
		board.HvCurrentA = reading.CurrentA
		board.HvOn = reading.On
		board.HvRamping = reading.Ramping
		board.HvOverCurrent = reading.OverCurrent
		board.HvOverVoltage = reading.OverVoltage
		if reading.OverCurrent || reading.OverVoltage {
			board.Health = daqv1.HealthStatus_HEALTH_STATUS_FAULT
		} else {
			board.Health = daqv1.HealthStatus_HEALTH_STATUS_OK
		}
	}
	c.Publisher.Publish(snapshot)
}

func findBoard(snapshot *daqv1.TelemetrySnapshot, chain, node uint32) *daqv1.Board {
	for _, candidateChain := range snapshot.GetChains() {
		if candidateChain.GetIndex() != chain {
			continue
		}
		for _, board := range candidateChain.GetBoards() {
			if board.GetNode() == node {
				return board
			}
		}
	}
	return nil
}
