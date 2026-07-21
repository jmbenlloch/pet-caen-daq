package dt5202

import (
	"context"
	"fmt"
)

type ConfigurationHardware interface {
	CitirocHardware
	ReadRegister(context.Context, uint16, uint16, uint32) (uint32, error)
}

// ApplyConfiguration executes one board's ordered plan and validates every
// final register value by readback. A hard apply begins with the same global
// reset used by JANUS CFG_HARD. No later step runs after an error.
func ApplyConfiguration(ctx context.Context, hardware ConfigurationHardware, chain, node uint16, plan ConfigurationPlan, hard bool) error {
	if hard {
		if err := hardware.SendCommand(ctx, chain, node, uint32(CommandGlobalReset), 0); err != nil {
			return fmt.Errorf("board %d chain %d node %d reset: %w", plan.Board, chain, node, err)
		}
	}
	for index, write := range plan.Writes {
		if err := hardware.WriteRegister(ctx, chain, node, uint32(write.Address), write.Value); err != nil {
			return fmt.Errorf("board %d chain %d node %d write %d register %#08x: %w", plan.Board, chain, node, index, write.Address, err)
		}
	}
	if err := ConfigureCitirocAutomatic(ctx, hardware, chain, node); err != nil {
		return fmt.Errorf("board %d chain %d node %d: %w", plan.Board, chain, node, err)
	}
	readback := make(map[Register]uint32, len(plan.Writes))
	seen := make(map[Register]bool, len(plan.Writes))
	for _, write := range plan.Writes {
		if seen[write.Address] {
			continue
		}
		seen[write.Address] = true
		value, err := hardware.ReadRegister(ctx, chain, node, uint32(write.Address))
		if err != nil {
			return fmt.Errorf("board %d chain %d node %d readback register %#08x: %w", plan.Board, chain, node, write.Address, err)
		}
		readback[write.Address] = value
	}
	if err := plan.ValidateReadback(readback); err != nil {
		return fmt.Errorf("chain %d node %d: %w", chain, node, err)
	}
	return nil
}
