package acquisition

import (
	"sync"
	"testing"
	"time"
)

func TestStateMachineRunLifecycle(t *testing.T) {
	stamp := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	machine, err := NewStateMachine(StateDisconnected, func() time.Time { return stamp })
	if err != nil {
		t.Fatal(err)
	}
	states := []State{StateConnecting, StateIdle, StateConfiguring, StateReady, StateStarting, StateRunning, StateStopping, StateDraining, StateReady}
	for _, state := range states {
		transition, err := machine.Move(state, "test")
		if err != nil {
			t.Fatalf("move to %s: %v", state, err)
		}
		if transition.At != stamp {
			t.Fatalf("transition time = %v", transition.At)
		}
	}
	snapshot := machine.Snapshot()
	if snapshot.State != StateReady || snapshot.Sequence != uint64(len(states)) {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if got := machine.History(); len(got) != len(states) || got[0].From != StateDisconnected {
		t.Fatalf("history = %+v", got)
	}
}

func TestStateMachineRejectsInvalidTransitionWithoutMutation(t *testing.T) {
	machine, _ := NewStateMachine(StateIdle, nil)
	if _, err := machine.Move(StateRunning, "operator"); err == nil {
		t.Fatal("accepted idle -> running")
	}
	if got := machine.Snapshot(); got.State != StateIdle || got.Sequence != 0 {
		t.Fatalf("snapshot after rejection = %+v", got)
	}
	if _, err := machine.Move(StateConfiguring, ""); err == nil {
		t.Fatal("accepted empty actor")
	}
}

func TestStateMachineFaultAndRecovery(t *testing.T) {
	machine, _ := NewStateMachine(StateRunning, nil)
	for _, state := range []State{StateFault, StateRecovering, StateDisconnected} {
		if _, err := machine.Move(state, "backend"); err != nil {
			t.Fatalf("move to %s: %v", state, err)
		}
	}
	if _, err := machine.Move(StateFault, "backend"); err == nil {
		t.Fatal("accepted disconnected -> fault")
	}
}

func TestStateMachineSerializesConcurrentMoves(t *testing.T) {
	machine, _ := NewStateMachine(StateReady, nil)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := machine.Move(StateStarting, "operator")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var successes int
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent transitions = %d", successes)
	}
}
