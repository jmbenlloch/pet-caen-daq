package dt5202

import (
	"context"
	"fmt"
	"time"
)

type HVTransaction struct {
	Register uint8
	DataType uint8
	Data     uint32
}

type HVPlan struct {
	VoltageV                float64
	CurrentLimitMA          float64
	TemperatureCoefficients [3]float64
	TemperatureFeedback     bool
	FeedbackMVPerC          float64
	Transactions            []HVTransaction
}

type HVHardware interface {
	WriteRegister(context.Context, uint16, uint16, uint32, uint32) error
	ReadRegister(context.Context, uint16, uint16, uint32) (uint32, error)
}

func fixedPoint(value float64) uint32 { return uint32(int32(value * 10000)) }

func buildHVPlan(voltage, current float64, coefficients [3]float64, feedback bool, feedbackCoefficient float64) HVPlan {
	if current >= 10 {
		current = 9.999
	}
	plan := HVPlan{VoltageV: voltage, CurrentLimitMA: current, TemperatureCoefficients: coefficients, TemperatureFeedback: feedback, FeedbackMVPerC: feedbackCoefficient}
	appendTwice := func(register, dataType uint8, data uint32) {
		plan.Transactions = append(plan.Transactions, HVTransaction{register, dataType, data}, HVTransaction{register, dataType, data})
	}
	plan.Transactions = append(plan.Transactions, HVTransaction{30, 2, 1})
	appendTwice(2, 1, fixedPoint(voltage))
	appendTwice(5, 1, fixedPoint(current))
	for range 2 {
		plan.Transactions = append(plan.Transactions, HVTransaction{7, 1, fixedPoint(coefficients[2])}, HVTransaction{8, 1, fixedPoint(coefficients[1])}, HVTransaction{9, 1, fixedPoint(coefficients[0])})
	}
	enable := uint32(0)
	if feedback {
		enable = 2
	}
	for range 2 {
		plan.Transactions = append(plan.Transactions, HVTransaction{28, 1, fixedPoint(-feedbackCoefficient)}, HVTransaction{1, 0, enable})
	}
	return plan
}

// ApplyHVConfiguration is intentionally separate from ApplyConfiguration:
// changing an HV setpoint is a safety-relevant operation and must be explicit.
func ApplyHVConfiguration(ctx context.Context, hardware HVHardware, chain, node uint16, plan HVPlan) error {
	if err := writeHVBusPair(ctx, hardware, chain, node, 0x2001, 0); err != nil {
		return fmt.Errorf("initialize HV bus: %w", err)
	}
	for index, transaction := range plan.Transactions {
		selector := uint32(transaction.DataType)<<8 | uint32(transaction.Register)
		if err := writeHVBusPair(ctx, hardware, chain, node, selector, transaction.Data); err != nil {
			return fmt.Errorf("HV transaction %d register %d type %d: %w", index, transaction.Register, transaction.DataType, err)
		}
	}
	return nil
}

func writeHVBusPair(ctx context.Context, hardware HVHardware, chain, node uint16, selector, data uint32) error {
	if err := hardware.WriteRegister(ctx, chain, node, uint32(HVRegisterAddress), selector); err != nil {
		return fmt.Errorf("write selector %#x: %w", selector, err)
	}
	if err := waitHVBus(ctx, hardware, chain, node); err != nil {
		return err
	}
	if err := hardware.WriteRegister(ctx, chain, node, uint32(HVRegisterData), data); err != nil {
		return fmt.Errorf("write data %#x: %w", data, err)
	}
	return waitHVBus(ctx, hardware, chain, node)
}

func waitHVBus(ctx context.Context, hardware HVHardware, chain, node uint16) error {
	for attempt := 0; attempt < 50; attempt++ {
		status, err := hardware.ReadRegister(ctx, chain, node, uint32(AcquisitionStatus))
		if err != nil {
			return fmt.Errorf("read I2C status: %w", err)
		}
		if Status(status).Has(StatusI2CFailure) {
			return fmt.Errorf("HV I2C failure status %#x", status)
		}
		if !Status(status).Has(StatusI2CBusy) {
			return nil
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("HV I2C remained busy after 50 polls")
}
