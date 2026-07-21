package dt5202

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type applyHardware struct {
	registers         map[uint32]uint32
	commands          []uint32
	writes, failWrite int
	corrupt           Register
}

func (h *applyHardware) WriteRegister(_ context.Context, _, _ uint16, address, value uint32) error {
	h.writes++
	if h.writes == h.failWrite {
		return errors.New("injected write failure")
	}
	if h.registers == nil {
		h.registers = make(map[uint32]uint32)
	}
	h.registers[address] = value
	return nil
}
func (h *applyHardware) ReadRegister(_ context.Context, _, _ uint16, address uint32) (uint32, error) {
	value := h.registers[address]
	if Register(address) == h.corrupt {
		value++
	}
	return value, nil
}
func (h *applyHardware) SendCommand(_ context.Context, _, _ uint16, command, _ uint32) error {
	h.commands = append(h.commands, command)
	if Command(command) == CommandGlobalReset {
		h.registers = make(map[uint32]uint32)
	}
	return nil
}

func TestApplyConfigurationWritesLoadsAndValidates(t *testing.T) {
	hardware := &applyHardware{}
	plan := ConfigurationPlan{Board: 3, Writes: []RegisterWrite{{TriggerMask, 0x41}, {RunMask, 1}}}
	if err := ApplyConfiguration(context.Background(), hardware, 3, 0, plan, true); err != nil {
		t.Fatal(err)
	}
	want := []uint32{uint32(CommandGlobalReset), uint32(CommandConfigureASIC), uint32(CommandConfigureASIC)}
	if len(hardware.commands) != len(want) {
		t.Fatalf("commands = %#v", hardware.commands)
	}
	for i := range want {
		if hardware.commands[i] != want[i] {
			t.Fatalf("commands = %#v, want %#v", hardware.commands, want)
		}
	}
}

func TestApplyConfigurationStopsAtWriteFailure(t *testing.T) {
	hardware := &applyHardware{failWrite: 2}
	plan := ConfigurationPlan{Board: 0, Writes: []RegisterWrite{{TriggerMask, 0x41}, {RunMask, 1}, {DwellTime, 2}}}
	err := ApplyConfiguration(context.Background(), hardware, 0, 0, plan, false)
	if err == nil || !strings.Contains(err.Error(), "write 1 register") {
		t.Fatalf("error = %v", err)
	}
	if len(hardware.commands) != 0 {
		t.Fatalf("commands after failure = %#v", hardware.commands)
	}
}

func TestApplyConfigurationDetectsReadbackMismatch(t *testing.T) {
	hardware := &applyHardware{corrupt: TriggerMask}
	plan := ConfigurationPlan{Board: 1, Writes: []RegisterWrite{{TriggerMask, 0x41}}}
	err := ApplyConfiguration(context.Background(), hardware, 1, 0, plan, false)
	if err == nil || !strings.Contains(err.Error(), "effective value") {
		t.Fatalf("error = %v", err)
	}
}
