package acquisition

import (
	"fmt"
	"sync"
	"time"
)

// State is the system-level acquisition lifecycle. Transitional states are
// explicit so service snapshots never have to infer an operation in progress.
type State string

const (
	StateDisconnected State = "disconnected"
	StateConnecting   State = "connecting"
	StateIdle         State = "idle"
	StateConfiguring  State = "configuring"
	StateReady        State = "ready"
	StateStarting     State = "starting"
	StateRunning      State = "running"
	StateStopping     State = "stopping"
	StateDraining     State = "draining"
	StateFault        State = "fault"
	StateRecovering   State = "recovering"
)

type Transition struct {
	Sequence uint64    `json:"sequence,string"`
	From     State     `json:"from"`
	To       State     `json:"to"`
	Actor    string    `json:"actor"`
	At       time.Time `json:"at"`
}

type StateSnapshot struct {
	State    State
	Sequence uint64
}

type Clock func() time.Time

// StateMachine serializes and records lifecycle transitions. Callers should
// keep hardware I/O outside Move; the explicit transitional states prevent a
// second operation from being admitted while that I/O is in progress.
type StateMachine struct {
	mu      sync.Mutex
	state   State
	seq     uint64
	now     Clock
	history []Transition
}

func NewStateMachine(initial State, now Clock) (*StateMachine, error) {
	if !validState(initial) {
		return nil, fmt.Errorf("invalid initial acquisition state %q", initial)
	}
	if now == nil {
		now = time.Now
	}
	return &StateMachine{state: initial, now: now}, nil
}

func (m *StateMachine) Snapshot() StateSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return StateSnapshot{State: m.state, Sequence: m.seq}
}

func (m *StateMachine) History() []Transition {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Transition(nil), m.history...)
}

func (m *StateMachine) Move(to State, actor string) (Transition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if actor == "" {
		return Transition{}, fmt.Errorf("transition %s -> %s requires an actor", m.state, to)
	}
	if !allowedTransition(m.state, to) {
		return Transition{}, fmt.Errorf("invalid acquisition transition %s -> %s", m.state, to)
	}
	m.seq++
	transition := Transition{Sequence: m.seq, From: m.state, To: to, Actor: actor, At: m.now()}
	m.state = to
	m.history = append(m.history, transition)
	return transition, nil
}

func validState(state State) bool {
	switch state {
	case StateDisconnected, StateConnecting, StateIdle, StateConfiguring, StateReady,
		StateStarting, StateRunning, StateStopping, StateDraining, StateFault, StateRecovering:
		return true
	default:
		return false
	}
}

func allowedTransition(from, to State) bool {
	if to == StateFault && from != StateFault && from != StateDisconnected {
		return true
	}
	switch from {
	case StateDisconnected:
		return to == StateConnecting
	case StateConnecting:
		return to == StateIdle || to == StateDisconnected
	case StateIdle:
		return to == StateConfiguring || to == StateDisconnected
	case StateConfiguring:
		return to == StateReady || to == StateIdle
	case StateReady:
		return to == StateStarting || to == StateConfiguring || to == StateDisconnected
	case StateStarting:
		return to == StateRunning || to == StateStopping
	case StateRunning:
		return to == StateStopping
	case StateStopping:
		return to == StateDraining
	case StateDraining:
		return to == StateReady
	case StateFault:
		return to == StateRecovering
	case StateRecovering:
		return to == StateIdle || to == StateDisconnected
	default:
		return false
	}
}
