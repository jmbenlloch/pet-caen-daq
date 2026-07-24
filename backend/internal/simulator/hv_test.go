package simulator

import (
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

func TestHVRampUpAndDownExposeIntermediateVoltageAndStatus(t *testing.T) {
	const setpoint = uint32(454000)
	start := time.Unix(100, 0)
	duration := 2 * time.Second
	board := Board{
		Registers:   monitorRegisters(),
		HVRegisters: map[uint32]uint32{0x102: setpoint},
	}

	board.beginHVRamp(true, start, duration)
	assertHV(t, &board, 0, true, true)

	board.updateHVRamp(start.Add(time.Second), duration)
	assertHV(t, &board, setpoint/2, true, true)

	board.updateHVRamp(start.Add(duration), duration)
	assertHV(t, &board, setpoint, true, false)

	board.beginHVRamp(false, start.Add(3*time.Second), duration)
	assertHV(t, &board, setpoint, true, true)

	board.updateHVRamp(start.Add(4*time.Second), duration)
	assertHV(t, &board, setpoint/2, true, true)

	board.updateHVRamp(start.Add(5*time.Second), duration)
	assertHV(t, &board, 0, false, false)
}

func TestHVRampCanReverseFromIntermediateVoltage(t *testing.T) {
	const setpoint = uint32(400000)
	start := time.Unix(100, 0)
	duration := 2 * time.Second
	board := Board{
		Registers:   monitorRegisters(),
		HVRegisters: map[uint32]uint32{0x102: setpoint},
	}

	board.beginHVRamp(true, start, duration)
	board.updateHVRamp(start.Add(time.Second), duration)
	board.beginHVRamp(false, start.Add(time.Second), duration)
	board.updateHVRamp(start.Add(2*time.Second), duration)

	assertHV(t, &board, setpoint/4, true, true)
}

func assertHV(t *testing.T, board *Board, voltage uint32, on, ramping bool) {
	t.Helper()
	if got := board.Registers[uint32(dt5202.HVVoltageMonitor)]; got != voltage {
		t.Fatalf("Vmon = %d, want %d", got, voltage)
	}
	status := board.Registers[uint32(dt5202.HVStatus)]
	if got := status&(1<<26) != 0; got != on {
		t.Fatalf("on = %t, want %t (status %#x)", got, on, status)
	}
	if got := status&(1<<27) != 0; got != ramping {
		t.Fatalf("ramping = %t, want %t (status %#x)", got, ramping, status)
	}
}
