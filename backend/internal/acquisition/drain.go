package acquisition

import (
	"context"
	"errors"
	"fmt"
	"time"

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

const drainIdleTimeout = 100 * time.Millisecond

// StopAndDrain sends an idempotent broadcast stop and reads until every observed
// pending batch is delivered and the stream has remained silent for the
// capture/source-confirmed FERSlib NODATA_TIMEOUT. Service-ready events are
// retained as completion evidence, but do not end draining before stream
// silence: forward-version service payloads may not expose status, and accepted
// batches may still follow a ready event.
//
// The caller deadline bounds a stalled hardware drain before any batch arrives.
// After the first complete batch, draining continues only while complete
// batches remain immediately available and finishes at the first idle period.
// This prevents persistence time from consuming the following idle probe and
// prevents a total-duration deadline from interrupting a partially read batch.
func StopAndDrain(ctx context.Context, hardware DrainHardware, expectedChains int, handle BatchHandler) (DrainResult, error) {
	chains := make([]uint16, expectedChains)
	for chain := range chains {
		chains[chain] = uint16(chain)
	}
	return StopAndDrainChains(ctx, hardware, chains, handle)
}

// StopAndDrainChains drains an explicit set of active TDlinks. Unlike the
// compatibility count-based wrapper, it supports non-contiguous enabled links.
func StopAndDrainChains(ctx context.Context, hardware DrainHardware, activeChains []uint16, handle BatchHandler) (DrainResult, error) {
	chains, err := normalizeChains(activeChains)
	if err != nil {
		return DrainResult{}, err
	}
	expected := make(map[uint8]struct{}, len(chains))
	for _, chain := range chains {
		expected[uint8(chain)] = struct{}{}
	}
	if err := hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandAcquisitionStop, dt5215.TDLCommandDelay); err != nil {
		return DrainResult{}, fmt.Errorf("stop acquisition: %w", err)
	}
	completed := make(map[uint8]bool, len(chains))
	var result DrainResult
	for {
		if ctxErr := ctx.Err(); ctxErr != nil && (!errors.Is(ctxErr, context.DeadlineExceeded) || result.Batches == 0) {
			return result, fmt.Errorf("drain incomplete (%d/%d chains): %w", len(completed), len(chains), ctxErr)
		}
		idleDeadline := time.Now().Add(drainIdleTimeout)
		readParent := ctx
		progressDrain := result.Batches > 0
		if progressDrain {
			readParent = context.Background()
		}
		canDeclareIdle := progressDrain
		if !progressDrain {
			canDeclareIdle = true
			if parentDeadline, ok := ctx.Deadline(); ok && !parentDeadline.After(idleDeadline) {
				canDeclareIdle = false
			}
		}
		readCtx, cancelRead := context.WithTimeout(readParent, drainIdleTimeout)
		raw, events, err := hardware.ReadRawStreamBatch(readCtx)
		cancelRead()
		if err != nil {
			if canDeclareIdle && errors.Is(err, context.DeadlineExceeded) {
				result.CompletedChains = len(chains)
				return result, nil
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, fmt.Errorf("drain incomplete (%d/%d chains): %w", len(completed), len(chains), ctxErr)
			}
			return result, fmt.Errorf("drain incomplete (%d/%d chains): %w", len(completed), len(chains), err)
		}
		result.Batches++
		result.Events += len(events)
		if handle != nil {
			if err := handle(raw, events); err != nil {
				return result, fmt.Errorf("deliver drained batch: %w", err)
			}
		}
		for _, event := range events {
			if _, ok := expected[event.Chain]; !ok {
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
}

func normalizeChains(activeChains []uint16) ([]uint16, error) {
	if len(activeChains) < 1 || len(activeChains) > dt5215.MaxChains {
		return nil, fmt.Errorf("active chain count %d out of range", len(activeChains))
	}
	seen := make(map[uint16]struct{}, len(activeChains))
	chains := append([]uint16(nil), activeChains...)
	for _, chain := range chains {
		if chain >= dt5215.MaxChains {
			return nil, fmt.Errorf("active chain %d out of range", chain)
		}
		if _, exists := seen[chain]; exists {
			return nil, fmt.Errorf("duplicate active chain %d", chain)
		}
		seen[chain] = struct{}{}
	}
	return chains, nil
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
