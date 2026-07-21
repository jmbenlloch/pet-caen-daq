package acquisition

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

// DrainHardware is the control/stream boundary needed for orderly shutdown.
type DrainHardware interface {
	SendCommand(context.Context, uint16, uint16, uint32, uint32) error
	ReadRawStreamBatch(context.Context) ([]byte, []dt5215.StreamEvent, error)
}

// BatchHandler receives every complete batch observed after stop, including
// the service batch that declares drain completion.
type BatchHandler func(raw []byte, events []dt5215.StreamEvent) error

type DrainResult struct {
	Batches         int
	Events          int
	CompletedChains int
}

// StopAndDrain sends an idempotent broadcast stop and reads until each expected
// chain emits a service event whose acquisition status is ready. Context
// cancellation or deadline is the authoritative bound for a stalled drain.
func StopAndDrain(ctx context.Context, hardware DrainHardware, expectedChains int, handle BatchHandler) (DrainResult, error) {
	if expectedChains < 1 || expectedChains > dt5215.MaxChains {
		return DrainResult{}, fmt.Errorf("expected chain count %d out of range", expectedChains)
	}
	if err := hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandAcquisitionStop, 0); err != nil {
		return DrainResult{}, fmt.Errorf("stop acquisition: %w", err)
	}
	completed := make(map[uint8]bool, expectedChains)
	var result DrainResult
	for len(completed) < expectedChains {
		raw, events, err := hardware.ReadRawStreamBatch(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, fmt.Errorf("drain incomplete (%d/%d chains): %w", len(completed), expectedChains, ctxErr)
			}
			return result, fmt.Errorf("drain incomplete (%d/%d chains): %w", len(completed), expectedChains, err)
		}
		result.Batches++
		result.Events += len(events)
		if handle != nil {
			if err := handle(raw, events); err != nil {
				return result, fmt.Errorf("deliver drained batch: %w", err)
			}
		}
		for _, event := range events {
			if int(event.Chain) >= expectedChains {
				return result, fmt.Errorf("drain completion from unexpected chain %d", event.Chain)
			}
			if event.Descriptor.Qualifier != dt5202.QualifierService {
				continue
			}
			service, err := dt5202.DecodeService(event.Descriptor.Timestamp, event.Payload)
			if err != nil {
				return result, fmt.Errorf("decode drain service chain %d node %d: %w", event.Chain, event.Descriptor.Node, err)
			}
			if service.Status != nil && dt5202.Status(*service.Status).Has(dt5202.StatusReady) {
				completed[event.Chain] = true
			}
		}
	}
	result.CompletedChains = len(completed)
	return result, nil
}

// JoinStopError retains the acquisition failure as the primary joined error
// when orderly stopping also fails.
func JoinStopError(acquisitionErr, stopErr error) error {
	if stopErr == nil {
		return acquisitionErr
	}
	if acquisitionErr == nil {
		return stopErr
	}
	return errors.Join(acquisitionErr, stopErr)
}
