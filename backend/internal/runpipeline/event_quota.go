package runpipeline

import (
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

// SelectEvents enforces quotas before either batch or single-record storage.
// Counts include earlier accepted records in this batch, so queueing, mixed-board
// batches, and stop/drain latency cannot overshoot a board's target.
func (s *sink) SelectEvents(events []acquisition.DecodedEvent) []acquisition.DecodedEvent {
	if s.eventLimitPerBoard == 0 {
		return events
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[boardKey]uint64, len(s.quotaTargets))
	for key := range s.quotaTargets {
		counts[key] = s.boards[key].TriggerCount
	}
	selected := events[:0]
	for _, item := range events {
		key := boardKey{chain: item.Wire.Chain, node: item.Wire.Descriptor.Node}
		count, configured := counts[key]
		if !configured || count >= s.eventLimitPerBoard {
			continue
		}
		selected = append(selected, item)
		if item.Event.Kind != dt5202.EventService {
			counts[key]++
		}
	}
	return selected
}

// PerBoardQuotaReached only observes successfully persisted records. A silent
// configured board prevents completion; unconfigured boards cannot satisfy it.
func (s *Session) PerBoardQuotaReached() bool {
	s.sink.mu.Lock()
	defer s.sink.mu.Unlock()
	if s.sink.eventLimitPerBoard == 0 || len(s.sink.quotaTargets) == 0 {
		return false
	}
	for key := range s.sink.quotaTargets {
		if s.sink.boards[key].TriggerCount < s.sink.eventLimitPerBoard {
			return false
		}
	}
	return true
}
