// Package acquisition coordinates safe, storage-independent acquisition flows.
package acquisition

import (
	"context"
	"fmt"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

type TestPulseHardware interface {
	Synchronize(context.Context) error
	ClearStream(context.Context) error
	ControlChain(context.Context, uint16, bool, uint32) error
	SendCommand(context.Context, uint16, uint16, uint32, uint32) error
	ReadRawStreamBatch(context.Context) ([]byte, []dt5215.StreamEvent, error)
}
type TestPulseSink interface {
	AppendRaw([]byte) error
	AppendDecoded(dt5215.StreamEvent, dt5202.SpectroscopyEvent) error
}

// RunTestPulse performs one synchronized broadcast pulse and drains exactly one
// event batch per expected chain. Stop is always attempted after a successful
// start, and its error is joined without hiding the original failure.
func RunTestPulse(ctx context.Context, hardware TestPulseHardware, sink TestPulseSink, expectedChains int) (err error) {
	if expectedChains < 1 || expectedChains > dt5215.MaxChains {
		return fmt.Errorf("expected chain count %d out of range", expectedChains)
	}
	if err = hardware.Synchronize(ctx); err != nil {
		return fmt.Errorf("synchronize: %w", err)
	}
	if err = hardware.ClearStream(ctx); err != nil {
		return fmt.Errorf("clear stream: %w", err)
	}
	for chain := 0; chain < expectedChains; chain++ {
		if err = hardware.ControlChain(ctx, uint16(chain), false, 0); err != nil {
			return fmt.Errorf("disable chain %d readout train: %w", chain, err)
		}
	}
	if err = hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandResetTime, dt5215.TDLCommandDelay); err != nil {
		return fmt.Errorf("reset acquisition time: %w", err)
	}
	if err = hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandResetPeriodic, dt5215.TDLCommandDelay); err != nil {
		return fmt.Errorf("reset periodic trigger: %w", err)
	}
	if err = hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandAcquisitionStart, dt5215.TDLCommandDelay); err != nil {
		return fmt.Errorf("start acquisition: %w", err)
	}
	for chain := 0; chain < expectedChains; chain++ {
		if err = hardware.ControlChain(ctx, uint16(chain), true, 0x100); err != nil {
			return fmt.Errorf("enable chain %d readout train: %w", chain, err)
		}
	}
	defer func() {
		drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_, stopErr := StopAndDrain(drainCtx, hardware, expectedChains, func(raw []byte, events []dt5215.StreamEvent) error {
			if captureErr := sink.AppendRaw(raw); captureErr != nil {
				return fmt.Errorf("capture raw batch: %w", captureErr)
			}
			for _, wireEvent := range events {
				if wireEvent.Descriptor.Qualifier == dt5202.QualifierService {
					continue
				}
				decoded, decodeErr := dt5202.DecodeSpectroscopy(wireEvent.Descriptor.Qualifier, wireEvent.Descriptor.TriggerID, wireEvent.Descriptor.Timestamp, wireEvent.Payload)
				if decodeErr != nil {
					return fmt.Errorf("decode pending chain %d node %d: %w", wireEvent.Chain, wireEvent.Descriptor.Node, decodeErr)
				}
				if sinkErr := sink.AppendDecoded(wireEvent, decoded); sinkErr != nil {
					return fmt.Errorf("store pending chain %d event: %w", wireEvent.Chain, sinkErr)
				}
			}
			return nil
		})
		err = JoinStopError(err, stopErr)
	}()
	if err = hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandTestPulse, dt5215.TDLCommandDelay); err != nil {
		return fmt.Errorf("send test pulse: %w", err)
	}
	seen := make(map[uint8]bool, expectedChains)
	for len(seen) < expectedChains {
		raw, events, readErr := hardware.ReadRawStreamBatch(ctx)
		if readErr != nil {
			return fmt.Errorf("read stream batch: %w", readErr)
		}
		if captureErr := sink.AppendRaw(raw); captureErr != nil {
			return fmt.Errorf("capture raw batch: %w", captureErr)
		}
		for _, wireEvent := range events {
			if wireEvent.Descriptor.CRCError {
				return fmt.Errorf("chain %d node %d descriptor CRC error", wireEvent.Chain, wireEvent.Descriptor.Node)
			}
			if int(wireEvent.Chain) >= expectedChains {
				return fmt.Errorf("unexpected chain %d", wireEvent.Chain)
			}
			if seen[wireEvent.Chain] {
				return fmt.Errorf("duplicate event for chain %d", wireEvent.Chain)
			}
			decoded, decodeErr := dt5202.DecodeSpectroscopy(wireEvent.Descriptor.Qualifier, wireEvent.Descriptor.TriggerID, wireEvent.Descriptor.Timestamp, wireEvent.Payload)
			if decodeErr != nil {
				return fmt.Errorf("decode chain %d node %d: %w", wireEvent.Chain, wireEvent.Descriptor.Node, decodeErr)
			}
			if sinkErr := sink.AppendDecoded(wireEvent, decoded); sinkErr != nil {
				return fmt.Errorf("store chain %d event: %w", wireEvent.Chain, sinkErr)
			}
			seen[wireEvent.Chain] = true
		}
	}
	return nil
}
