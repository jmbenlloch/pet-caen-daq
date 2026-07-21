// Package telemetry publishes immutable, independently usable system snapshots.
package telemetry

import (
	"context"
	"errors"
	"sync"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrInstanceIDRequired = errors.New("telemetry instance ID is required")

type Clock func() time.Time

// Publisher retains the authoritative current snapshot. Subscriber queues have
// capacity one: a slow consumer receives the newest complete snapshot instead
// of forcing acquisition to wait for obsolete telemetry samples.
type Publisher struct {
	mu          sync.Mutex
	instanceID  string
	sequence    uint64
	now         Clock
	current     *daqv1.TelemetrySnapshot
	nextID      uint64
	subscribers map[uint64]chan *daqv1.TelemetrySnapshot
}

func NewPublisher(instanceID string, initial *daqv1.TelemetrySnapshot, now Clock) (*Publisher, error) {
	if instanceID == "" {
		return nil, ErrInstanceIDRequired
	}
	if now == nil {
		now = time.Now
	}
	p := &Publisher{instanceID: instanceID, now: now, subscribers: make(map[uint64]chan *daqv1.TelemetrySnapshot)}
	p.current = p.prepare(initial)
	return p, nil
}

// Publish replaces the complete authoritative snapshot and returns an
// immutable copy with publisher-owned identity, sequence, and observation time.
func (p *Publisher) Publish(snapshot *daqv1.TelemetrySnapshot) *daqv1.TelemetrySnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = p.prepare(snapshot)
	for _, subscriber := range p.subscribers {
		value := clone(p.current)
		select {
		case subscriber <- value:
		default:
			select {
			case <-subscriber:
			default:
			}
			subscriber <- value
		}
	}
	return clone(p.current)
}

func (p *Publisher) Snapshot() *daqv1.TelemetrySnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return clone(p.current)
}

// Subscribe immediately queues the current complete snapshot. The channel is
// intentionally not closed on cancellation; consumers must select on ctx.Done,
// avoiding close/send races with concurrent publication.
func (p *Publisher) Subscribe(ctx context.Context) <-chan *daqv1.TelemetrySnapshot {
	p.mu.Lock()
	p.nextID++
	id := p.nextID
	updates := make(chan *daqv1.TelemetrySnapshot, 1)
	updates <- clone(p.current)
	p.subscribers[id] = updates
	p.mu.Unlock()
	go func() {
		<-ctx.Done()
		p.mu.Lock()
		delete(p.subscribers, id)
		p.mu.Unlock()
	}()
	return updates
}

func (p *Publisher) prepare(snapshot *daqv1.TelemetrySnapshot) *daqv1.TelemetrySnapshot {
	if snapshot == nil {
		snapshot = &daqv1.TelemetrySnapshot{}
	}
	value := clone(snapshot)
	p.sequence++
	value.InstanceId = p.instanceID
	value.Sequence = p.sequence
	value.ObservedAt = timestamppb.New(p.now())
	return value
}

func clone(snapshot *daqv1.TelemetrySnapshot) *daqv1.TelemetrySnapshot {
	return proto.Clone(snapshot).(*daqv1.TelemetrySnapshot)
}

// IsStale applies the frontend/server health convention to one observation.
// Missing or invalid timestamps are stale; future timestamps are not.
func IsStale(snapshot *daqv1.TelemetrySnapshot, now time.Time, maximumAge time.Duration) bool {
	if snapshot == nil || snapshot.ObservedAt == nil || snapshot.ObservedAt.CheckValid() != nil || maximumAge <= 0 {
		return true
	}
	age := now.Sub(snapshot.ObservedAt.AsTime())
	return age > maximumAge
}
