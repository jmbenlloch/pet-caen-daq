package dt5202

import (
	"context"
	"fmt"
	"time"
)

type ConfigurationHardware interface {
	CitirocHardware
	ReadRegister(context.Context, uint16, uint16, uint32) (uint32, error)
}

type ConfigurationApplyStage string

const (
	ConfigurationApplyWriting  ConfigurationApplyStage = "writing_registers"
	ConfigurationApplyCitiroc  ConfigurationApplyStage = "configuring_citiroc"
	ConfigurationApplyReadback ConfigurationApplyStage = "reading_registers"
)

type ConfigurationApplyProgress struct {
	Stage     ConfigurationApplyStage
	Completed int
	Total     int
}

// ApplyConfiguration executes one board's ordered plan and validates every
// final readable register value by readback. DCOffset is a DAC command port.
// A hard apply begins with the same global reset used by JANUS CFG_HARD. No later step runs after an error.
func ApplyConfiguration(ctx context.Context, hardware ConfigurationHardware, chain, node uint16, plan ConfigurationPlan, hard bool) error {
	return ApplyConfigurationWithProgress(ctx, hardware, chain, node, plan, hard, nil)
}

func ApplyConfigurationWithProgress(ctx context.Context, hardware ConfigurationHardware, chain, node uint16, plan ConfigurationPlan, hard bool, observe func(ConfigurationApplyProgress)) error {
	publish := func(stage ConfigurationApplyStage, completed, total int) {
		if observe != nil {
			observe(ConfigurationApplyProgress{Stage: stage, Completed: completed, Total: total})
		}
	}
	if hard {
		if err := hardware.SendCommand(ctx, chain, node, uint32(CommandGlobalReset), TDLConfigurationCommandDelay); err != nil {
			return fmt.Errorf("board %d chain %d node %d reset: %w", plan.Board, chain, node, err)
		}
	}
	publish(ConfigurationApplyWriting, 0, len(plan.Writes))
	for index, write := range plan.Writes {
		if err := hardware.WriteRegister(ctx, chain, node, uint32(write.Address), write.Value); err != nil {
			return fmt.Errorf("board %d chain %d node %d write %d register %#08x: %w", plan.Board, chain, node, index, write.Address, err)
		}
		if write.Address == DCOffset {
			// FERSlib waits 1 ms after each of the four DAC commands.
			timer := time.NewTimer(time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("board %d chain %d node %d settle DC offset: %w", plan.Board, chain, node, ctx.Err())
			case <-timer.C:
			}
		}
		publish(ConfigurationApplyWriting, index+1, len(plan.Writes))
	}
	publish(ConfigurationApplyCitiroc, 0, 2)
	if err := ConfigureCitirocAutomaticReadback(ctx, hardware, chain, node); err != nil {
		return fmt.Errorf("board %d chain %d node %d: %w", plan.Board, chain, node, err)
	}
	seen := make(map[Register]bool, len(plan.Writes))
	for _, write := range plan.Writes {
		if write.Address != DCOffset {
			seen[write.Address] = true
		}
	}
	readbackTotal := len(seen)
	publish(ConfigurationApplyCitiroc, 2, 2)
	publish(ConfigurationApplyReadback, 0, readbackTotal)
	readback := make(map[Register]uint32, readbackTotal)
	clear(seen)
	for _, write := range plan.Writes {
		if write.Address == DCOffset || seen[write.Address] {
			continue
		}
		seen[write.Address] = true
		value, err := hardware.ReadRegister(ctx, chain, node, uint32(write.Address))
		if err != nil {
			return fmt.Errorf("board %d chain %d node %d readback register %#08x: %w", plan.Board, chain, node, write.Address, err)
		}
		readback[write.Address] = value
		publish(ConfigurationApplyReadback, len(readback), readbackTotal)
	}
	if err := plan.ValidateReadback(readback); err != nil {
		return fmt.Errorf("chain %d node %d: %w", chain, node, err)
	}
	return nil
}
