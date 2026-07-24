package staircase

import (
	"context"
	"fmt"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

const ChannelCount = 64

type Hardware interface {
	WriteRegister(context.Context, uint16, uint16, uint32, uint32) error
	ReadRegister(context.Context, uint16, uint16, uint32) (uint32, error)
	SendCommand(context.Context, uint16, uint16, uint32, uint32) error
}

type Request struct {
	Board, Minimum, Maximum, Step uint32
	Dwell                         time.Duration
}

type Point struct {
	Threshold       uint32                `json:"threshold"`
	Elapsed         time.Duration         `json:"elapsed"`
	ChannelCounts   [ChannelCount]uint32  `json:"channel_counts"`
	ChannelRatesCPS [ChannelCount]float64 `json:"channel_rates_cps"`
	TORCount        uint32                `json:"t_or_count"`
	QORCount        uint32                `json:"q_or_count"`
	TORRateCPS      float64               `json:"t_or_rate_cps"`
	QORRateCPS      float64               `json:"q_or_rate_cps"`
}

func (r Request) Validate() error {
	if r.Step == 0 {
		return fmt.Errorf("step must be positive")
	}
	if r.Minimum > r.Maximum {
		return fmt.Errorf("minimum threshold must not exceed maximum threshold")
	}
	if r.Maximum > 2047 {
		return fmt.Errorf("threshold must be in range [0,2047]")
	}
	if r.Dwell < time.Millisecond || r.Dwell > 34*time.Second {
		return fmt.Errorf("dwell must be between 1 ms and 34 s")
	}
	if r.PointCount() > 4096 {
		return fmt.Errorf("scan contains too many points")
	}
	return nil
}

func (r Request) PointCount() uint32 {
	if r.Step == 0 || r.Minimum > r.Maximum {
		return 0
	}
	return (r.Maximum-r.Minimum)/r.Step + 1
}

// Run performs the source-confirmed JANUS staircase sequence without the
// undocumented max+step warm-up pass. restore is owned by the caller.
func Run(ctx context.Context, hardware Hardware, chain, node uint16, request Request, timeMask uint64, observe func(Point) error) error {
	if hardware == nil || observe == nil {
		return fmt.Errorf("hardware and point observer are required")
	}
	if err := request.Validate(); err != nil {
		return err
	}
	dwellCycles := uint32(request.Dwell / (8 * time.Nanosecond))
	setup := []dt5202.RegisterWrite{
		{Address: dt5202.AcquisitionControl, Value: 4},
		{Address: dt5202.DwellTime, Value: dwellCycles},
		{Address: dt5202.TimeDiscriminatorMaskLow, Value: uint32(timeMask)},
		{Address: dt5202.TimeDiscriminatorMaskHigh, Value: uint32(timeMask >> 32)},
		{Address: dt5202.CitirocConfig, Value: 0x00070f20},
		{Address: dt5202.LowGainShapingTime, Value: 5},
		{Address: dt5202.HighGainShapingTime, Value: 5},
		{Address: dt5202.TriggerMask, Value: 1},
		{Address: dt5202.T1OutputMask, Value: 0x10},
		{Address: dt5202.T0OutputMask, Value: 0x04},
	}
	for _, write := range setup {
		if err := hardware.WriteRegister(ctx, chain, node, uint32(write.Address), write.Value); err != nil {
			return fmt.Errorf("prepare register %#08x: %w", write.Address, err)
		}
	}
	for threshold := request.Maximum; ; {
		if err := scanPoint(ctx, hardware, chain, node, request, threshold, observe); err != nil {
			return err
		}
		if threshold < request.Minimum+request.Step {
			break
		}
		threshold -= request.Step
	}
	return nil
}

func scanPoint(ctx context.Context, hardware Hardware, chain, node uint16, request Request, threshold uint32, observe func(Point) error) error {
	for _, register := range []dt5202.Register{dt5202.ChargeCoarseThreshold, dt5202.TimeCoarseThreshold} {
		if err := hardware.WriteRegister(ctx, chain, node, uint32(register), threshold); err != nil {
			return fmt.Errorf("threshold %d write register %#08x: %w", threshold, register, err)
		}
	}
	for _, chip := range []uint32{0, 0x200} {
		if err := hardware.WriteRegister(ctx, chain, node, uint32(dt5202.CitirocSlowControl), chip); err != nil {
			return fmt.Errorf("threshold %d select Citiroc: %w", threshold, err)
		}
		if err := hardware.SendCommand(ctx, chain, node, uint32(dt5202.CommandConfigureASIC), 0); err != nil {
			return fmt.Errorf("threshold %d configure Citiroc: %w", threshold, err)
		}
	}
	if err := hardware.WriteRegister(ctx, chain, node, uint32(dt5202.TriggerMask), 0x20); err != nil {
		return fmt.Errorf("threshold %d enable periodic trigger: %w", threshold, err)
	}
	started := time.Now()
	if err := hardware.SendCommand(ctx, chain, node, uint32(dt5202.CommandResetPeriodicTrigger), 0); err != nil {
		return fmt.Errorf("threshold %d reset periodic trigger: %w", threshold, err)
	}
	timer := time.NewTimer(request.Dwell + 200*time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	point := Point{Threshold: threshold, Elapsed: time.Since(started)}
	var err error
	if point.TORCount, err = readCounter(ctx, hardware, chain, node, dt5202.TimeORCount); err != nil {
		return fmt.Errorf("threshold %d T-OR: %w", threshold, err)
	}
	if point.QORCount, err = readCounter(ctx, hardware, chain, node, dt5202.ChargeORCount); err != nil {
		return fmt.Errorf("threshold %d Q-OR: %w", threshold, err)
	}
	seconds := request.Dwell.Seconds()
	point.TORRateCPS = float64(point.TORCount) / seconds
	point.QORRateCPS = float64(point.QORCount) / seconds
	for channel := uint8(0); channel < ChannelCount; channel++ {
		count, readErr := readCounter(ctx, hardware, chain, node, dt5202.IndividualRegister(dt5202.HitCounter, channel))
		if readErr != nil {
			return fmt.Errorf("threshold %d channel %d: %w", threshold, channel, readErr)
		}
		point.ChannelCounts[channel] = count
		point.ChannelRatesCPS[channel] = float64(count) / seconds
	}
	return observe(point)
}

func readCounter(ctx context.Context, hardware Hardware, chain, node uint16, register dt5202.Register) (uint32, error) {
	return hardware.ReadRegister(ctx, chain, node, uint32(register))
}
