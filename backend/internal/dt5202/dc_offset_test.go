package dt5202

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestAnalogDCOffsetsBuildIndependentOrderedPlan(t *testing.T) {
	original := ConfigurationPlan{Writes: []RegisterWrite{{TriggerMask, 1}}}
	plan, err := original.WithAnalogDCOffsets([4]uint16{100, 200, 300, 400})
	if err != nil {
		t.Fatal(err)
	}
	want := []RegisterWrite{{TriggerMask, 1}, {DCOffset, 100}, {DCOffset, 1<<14 | 200}, {DCOffset, 2<<14 | 300}, {DCOffset, 3<<14 | 400}}
	if !reflect.DeepEqual(plan.Writes, want) {
		t.Fatalf("writes %v, want %v", plan.Writes, want)
	}
	if len(original.Writes) != 1 {
		t.Fatal("mutated original plan")
	}
	again, err := plan.WithAnalogDCOffsets([4]uint16{100, 200, 300, 400})
	if err != nil || !reflect.DeepEqual(again.Writes, want) {
		t.Fatalf("repeat plan %v error %v", again.Writes, err)
	}
	for index := range 4 {
		for _, invalid := range []uint16{0, 4095, 65535} {
			offsets := [4]uint16{100, 200, 300, 400}
			offsets[index] = invalid
			if _, err := original.WithAnalogDCOffsets(offsets); err == nil {
				t.Fatalf("accepted DAC %d value %d", index, invalid)
			}
		}
	}
}

type dcOffsetHardware struct {
	applyHardware
	log    []RegisterWrite
	cancel context.CancelFunc
}

func (h *dcOffsetHardware) WriteRegister(ctx context.Context, chain, node uint16, address, value uint32) error {
	h.log = append(h.log, RegisterWrite{Register(address), value})
	if err := h.applyHardware.WriteRegister(ctx, chain, node, address, value); err != nil {
		return err
	}
	if Register(address) == DCOffset && h.cancel != nil {
		h.cancel()
	}
	return nil
}
func (h *dcOffsetHardware) ReadRegister(ctx context.Context, chain, node uint16, address uint32) (uint32, error) {
	if Register(address) == DCOffset {
		return 0, errors.New("DAC command port has no readback")
	}
	return h.applyHardware.ReadRegister(ctx, chain, node, address)
}
func TestAnalogDCOffsetsAppliedAfterHardResetWithoutReadback(t *testing.T) {
	plan, err := (ConfigurationPlan{Writes: []RegisterWrite{{TriggerMask, 1}}}).WithAnalogDCOffsets([4]uint16{100, 200, 300, 400})
	if err != nil {
		t.Fatal(err)
	}
	for _, hard := range []bool{true, false} {
		h := &dcOffsetHardware{}
		if err := ApplyConfiguration(context.Background(), h, 0, 0, plan, hard); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(h.log[:len(plan.Writes)], plan.Writes) {
			t.Fatalf("writes %v, want %v", h.log, plan.Writes)
		}
		if hard && h.commands[0] != uint32(CommandGlobalReset) {
			t.Fatal("missing reset")
		}
	}
}
func TestAnalogDCOffsetsFailureStopsConfiguration(t *testing.T) {
	plan, _ := (ConfigurationPlan{}).WithAnalogDCOffsets([4]uint16{100, 200, 300, 400})
	h := &dcOffsetHardware{applyHardware: applyHardware{failWrite: 2}}
	if err := ApplyConfiguration(context.Background(), h, 0, 0, plan, false); err == nil || !strings.Contains(err.Error(), "write 1 register") {
		t.Fatalf("error %v", err)
	}
	if len(h.log) != 2 || len(h.commands) != 0 {
		t.Fatalf("continued after DAC failure: %+v", h)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h = &dcOffsetHardware{cancel: cancel}
	if err := ApplyConfiguration(ctx, h, 0, 0, plan, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v", err)
	}
	if len(h.log) != 1 || len(h.commands) != 0 {
		t.Fatal("continued after canceled DAC settling")
	}
}
