// Package holddelay implements the source-confirmed JANUS hold-delay scan.
package holddelay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

const (
	ChannelCount  = 64
	BinCount      = 512
	MaxPointCount = 64
)

type Hardware interface {
	WriteRegister(context.Context, uint16, uint16, uint32, uint32) error
	ReadRegister(context.Context, uint16, uint16, uint32) (uint32, error)
	SendCommand(context.Context, uint16, uint16, uint32, uint32) error
	ReadRawStreamBatch(context.Context) ([]byte, []dt5215.StreamEvent, error)
	ClearStream(context.Context) error
}

type Request struct {
	Board, MinimumDelayNS, MaximumDelayNS, StepNS uint32
	EventsPerDelay                                uint32
	PointTimeout                                  time.Duration
}

type Point struct {
	DelayNS         uint32                         `json:"delay_ns"`
	EffectiveDelay  uint32                         `json:"effective_delay_ns"`
	EventsCollected uint32                         `json:"events_collected"`
	Elapsed         time.Duration                  `json:"elapsed"`
	HighGainBins    [ChannelCount][BinCount]uint32 `json:"high_gain_bins"`
	MissingChannels [ChannelCount]uint32           `json:"missing_channels"`
}

func (r Request) Validate() error {
	if r.MinimumDelayNS > r.MaximumDelayNS {
		return fmt.Errorf("minimum delay must not exceed maximum delay")
	}
	if r.StepNS < 8 || r.StepNS%8 != 0 {
		return fmt.Errorf("step must be a positive multiple of 8 ns")
	}
	if r.MinimumDelayNS%8 != 0 || r.MaximumDelayNS%8 != 0 {
		return fmt.Errorf("delay bounds must be multiples of 8 ns")
	}
	if r.EventsPerDelay < 10 || r.EventsPerDelay > 100000 {
		return fmt.Errorf("events per delay must be between 10 and 100000")
	}
	if r.PointTimeout < time.Second || r.PointTimeout > 10*time.Minute {
		return fmt.Errorf("point timeout must be between 1 s and 10 min")
	}
	if count := r.PointCount(); count == 0 || count > MaxPointCount {
		return fmt.Errorf("scan must contain between 1 and %d delay points", MaxPointCount)
	}
	return nil
}

func (r Request) PointCount() uint32 {
	if r.StepNS == 0 || r.MinimumDelayNS > r.MaximumDelayNS {
		return 0
	}
	return (r.MaximumDelayNS-r.MinimumDelayNS)/r.StepNS + 1
}

func EnergyBin(value uint16, range14 bool) uint16 {
	shift := 4
	if range14 {
		shift = 5
	}
	bin := value >> shift
	if bin >= BinCount {
		return BinCount - 1
	}
	return bin
}

func Run(ctx context.Context, hardware Hardware, chain, node uint16, request Request, range14 bool, observe func(Point) error) (err error) {
	if hardware == nil || observe == nil {
		return fmt.Errorf("hardware and point observer are required")
	}
	if err := request.Validate(); err != nil {
		return err
	}
	for delay := request.MinimumDelayNS; delay <= request.MaximumDelayNS; delay += request.StepNS {
		point, pointErr := scanPoint(ctx, hardware, chain, node, request, delay, range14)
		if pointErr != nil {
			return pointErr
		}
		if err := observe(point); err != nil {
			return err
		}
		if request.MaximumDelayNS-delay < request.StepNS {
			break
		}
	}
	return nil
}

func scanPoint(ctx context.Context, hardware Hardware, chain, node uint16, request Request, delay uint32, range14 bool) (point Point, err error) {
	if err := hardware.ClearStream(ctx); err != nil {
		return point, fmt.Errorf("delay %d ns clear stream: %w", delay, err)
	}
	registerValue := delay / 8
	if err := hardware.WriteRegister(ctx, chain, node, uint32(dt5202.HoldDelay), registerValue); err != nil {
		return point, fmt.Errorf("delay %d ns write: %w", delay, err)
	}
	readback, err := hardware.ReadRegister(ctx, chain, node, uint32(dt5202.HoldDelay))
	if err != nil {
		return point, fmt.Errorf("delay %d ns readback: %w", delay, err)
	}
	if readback != registerValue {
		return point, fmt.Errorf("delay %d ns readback %d does not match %d", delay, readback, registerValue)
	}
	if err := hardware.SendCommand(ctx, chain, node, dt5215.CommandAcquisitionStart, 0); err != nil {
		return point, fmt.Errorf("delay %d ns start acquisition: %w", delay, err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		stopErr := hardware.SendCommand(stopCtx, chain, node, dt5215.CommandAcquisitionStop, 0)
		if stopErr != nil {
			stopErr = fmt.Errorf("delay %d ns stop acquisition: %w", delay, stopErr)
		}
		err = errors.Join(err, stopErr)
	}()

	point.DelayNS, point.EffectiveDelay = delay, readback*8
	started := time.Now()
	pointCtx, cancel := context.WithTimeout(ctx, request.PointTimeout)
	defer cancel()
	for point.EventsCollected < request.EventsPerDelay {
		_, events, readErr := hardware.ReadRawStreamBatch(pointCtx)
		if readErr != nil {
			return point, fmt.Errorf("delay %d ns collected %d/%d events: %w", delay, point.EventsCollected, request.EventsPerDelay, readErr)
		}
		for _, wire := range events {
			if wire.Chain != uint8(chain) || wire.Descriptor.Node != uint8(node) || wire.Descriptor.Qualifier == dt5202.QualifierService {
				continue
			}
			if wire.Descriptor.CRCError {
				return point, fmt.Errorf("delay %d ns descriptor CRC error", delay)
			}
			event, decodeErr := dt5202.DecodeSpectroscopy(wire.Descriptor.Qualifier, wire.Descriptor.TriggerID, wire.Descriptor.Timestamp, wire.Payload)
			if decodeErr != nil {
				return point, fmt.Errorf("delay %d ns decode spectroscopy: %w", delay, decodeErr)
			}
			seen := [ChannelCount]bool{}
			for _, energy := range event.Energies {
				if !energy.HasHighGain {
					continue
				}
				seen[energy.Channel] = true
				point.HighGainBins[energy.Channel][EnergyBin(energy.HighGain, range14)]++
			}
			for channel := range seen {
				if !seen[channel] {
					point.MissingChannels[channel]++
				}
			}
			point.EventsCollected++
			if point.EventsCollected == request.EventsPerDelay {
				break
			}
		}
	}
	point.Elapsed = time.Since(started)
	return point, nil
}
