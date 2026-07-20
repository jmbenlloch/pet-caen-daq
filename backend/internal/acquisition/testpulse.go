// Package acquisition coordinates safe, storage-independent acquisition flows.
package acquisition

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

type TestPulseHardware interface {
	Synchronize(context.Context) error
	ClearStream(context.Context) error
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
	if err = hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandAcquisitionStart, 0); err != nil {
		return fmt.Errorf("start acquisition: %w", err)
	}
	defer func() {
		stopErr := hardware.SendCommand(context.WithoutCancel(ctx), 0xff, 0xff, dt5215.CommandAcquisitionStop, 0)
		if stopErr != nil {
			err = errors.Join(err, fmt.Errorf("stop acquisition: %w", stopErr))
		}
	}()
	if err = hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandTestPulse, 0); err != nil {
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
