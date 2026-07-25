package acquisition

import (
	"context"
	"errors"
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

func TestConfiguratorCachesPedestalCalibrationForHardwareSession(t *testing.T) {
	states, _ := NewStateMachine(StateIdle, nil)
	configurator, err := NewConfigurator(states, &coordinatorHardware{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	want := dt5202.PedestalFlashCalibration{Page: dt5202.PedestalFlashPage}
	configurator.loadPedestalCalibration = func(context.Context, dt5202.PedestalFlashReader, uint16, uint16) (dt5202.PedestalFlashCalibration, error) {
		calls++
		return want, nil
	}
	target := ConfigurationTarget{Board: 1, Chain: 1, Node: 0}
	first, reused, err := configurator.pedestalCalibration(context.Background(), target)
	if err != nil || reused || first.Page != want.Page {
		t.Fatalf("first calibration=%+v reused=%t error=%v", first, reused, err)
	}
	second, reused, err := configurator.pedestalCalibration(context.Background(), target)
	if err != nil || !reused || second.Page != want.Page || calls != 1 {
		t.Fatalf("second calibration=%+v reused=%t calls=%d error=%v", second, reused, calls, err)
	}
}

func TestConfiguratorDoesNotCacheFailedPedestalRead(t *testing.T) {
	states, _ := NewStateMachine(StateIdle, nil)
	configurator, err := NewConfigurator(states, &coordinatorHardware{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("flash read failed")
	calls := 0
	configurator.loadPedestalCalibration = func(context.Context, dt5202.PedestalFlashReader, uint16, uint16) (dt5202.PedestalFlashCalibration, error) {
		calls++
		if calls == 1 {
			return dt5202.PedestalFlashCalibration{}, sentinel
		}
		return dt5202.PedestalFlashCalibration{Page: dt5202.PedestalFlashPage}, nil
	}
	target := ConfigurationTarget{Board: 0, Chain: 0, Node: 0}
	if _, _, err := configurator.pedestalCalibration(context.Background(), target); !errors.Is(err, sentinel) {
		t.Fatalf("first error = %v", err)
	}
	if _, reused, err := configurator.pedestalCalibration(context.Background(), target); err != nil || reused || calls != 2 {
		t.Fatalf("retry reused=%t calls=%d error=%v", reused, calls, err)
	}
}

func TestConfiguratorCachesPedestalsPerTarget(t *testing.T) {
	states, _ := NewStateMachine(StateIdle, nil)
	configurator, err := NewConfigurator(states, &coordinatorHardware{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	configurator.loadPedestalCalibration = func(_ context.Context, _ dt5202.PedestalFlashReader, chain, _ uint16) (dt5202.PedestalFlashCalibration, error) {
		calls++
		return dt5202.PedestalFlashCalibration{Page: uint16(chain)}, nil
	}
	for chain := uint16(0); chain < 2; chain++ {
		target := ConfigurationTarget{Board: int(chain), Chain: chain}
		if _, reused, err := configurator.pedestalCalibration(context.Background(), target); err != nil || reused {
			t.Fatalf("chain %d reused=%t error=%v", chain, reused, err)
		}
	}
	if calls != 2 {
		t.Fatalf("loader calls = %d, want 2", calls)
	}
}
