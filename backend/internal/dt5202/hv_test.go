package dt5202

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBuildHVPlanMatchesProductionSourceSequence(t *testing.T) {
	plan := buildHVPlan(45.4, 1, [3]float64{0, 50, 0}, false, 35)
	if got, want := len(plan.Transactions), 15; got != want {
		t.Fatalf("transactions = %d, want %d", got, want)
	}
	want := []HVTransaction{
		{30, 2, 1}, {2, 1, 454000}, {2, 1, 454000}, {5, 1, 10000}, {5, 1, 10000},
		{7, 1, 0}, {8, 1, 500000}, {9, 1, 0}, {7, 1, 0}, {8, 1, 500000}, {9, 1, 0},
		{28, 1, 0xfffaa8d0}, {1, 0, 0}, {28, 1, 0xfffaa8d0}, {1, 0, 0},
	}
	for index := range want {
		if plan.Transactions[index] != want[index] {
			t.Errorf("transaction %d = %#v, want %#v", index, plan.Transactions[index], want[index])
		}
	}
}

type hvHardware struct {
	writes    []RegisterWrite
	status    uint32
	failWrite int
	reads     map[uint32]uint32
}

func (h *hvHardware) WriteRegister(_ context.Context, _, _ uint16, address, value uint32) error {
	h.writes = append(h.writes, RegisterWrite{Register(address), value})
	if len(h.writes) == h.failWrite {
		return errors.New("injected")
	}
	return nil
}

func (h *hvHardware) ReadRegister(_ context.Context, _, _ uint16, address uint32) (uint32, error) {
	if value, ok := h.reads[address]; ok {
		return value, nil
	}
	return h.status, nil
}
func (h *hvHardware) SendCommand(context.Context, uint16, uint16, uint32, uint32) error { return nil }

func TestApplyHVConfigurationWritesInitializedBusSequence(t *testing.T) {
	hardware := &hvHardware{}
	plan := buildHVPlan(45.4, 1, [3]float64{0, 50, 0}, false, 35)
	if err := ApplyHVConfiguration(context.Background(), hardware, 2, 0, plan); err != nil {
		t.Fatal(err)
	}
	if got, want := len(hardware.writes), 2*(1+len(plan.Transactions)); got != want {
		t.Fatalf("writes = %d, want %d", got, want)
	}
	if hardware.writes[0] != (RegisterWrite{HVRegisterAddress, 0x2001}) || hardware.writes[1] != (RegisterWrite{HVRegisterData, 0}) {
		t.Fatalf("initialization = %#v", hardware.writes[:2])
	}
	if hardware.writes[2] != (RegisterWrite{HVRegisterAddress, 0x21e}) || hardware.writes[3] != (RegisterWrite{HVRegisterData, 1}) {
		t.Fatalf("PID transaction = %#v", hardware.writes[2:4])
	}
}

func TestApplyHVConfigurationRejectsI2CFailure(t *testing.T) {
	hardware := &hvHardware{status: uint32(StatusI2CFailure)}
	err := ApplyHVConfiguration(context.Background(), hardware, 0, 0, HVPlan{})
	if err == nil || !strings.Contains(err.Error(), "I2C failure") {
		t.Fatalf("error = %v", err)
	}
	if len(hardware.writes) != 1 {
		t.Fatalf("writes after failure = %d, want 1", len(hardware.writes))
	}
}

func TestApplyHVConfigurationHonorsCancellationWhileBusy(t *testing.T) {
	hardware := &hvHardware{status: uint32(StatusI2CBusy)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ApplyHVConfiguration(ctx, hardware, 0, 0, HVPlan{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestBuildHVPlanClampsUnsupportedTenMilliampLimit(t *testing.T) {
	plan := buildHVPlan(45, 10, [3]float64{}, false, 0)
	if plan.CurrentLimitMA != 9.999 || plan.Transactions[3].Data != 99990 {
		t.Fatalf("effective current = %v, transaction = %#v", plan.CurrentLimitMA, plan.Transactions[3])
	}
}

func TestSetHVOnUsesPeripheralOnOffRegister(t *testing.T) {
	hardware := &hvHardware{}
	if err := SetHVOn(context.Background(), hardware, 1, 0, true); err != nil {
		t.Fatal(err)
	}
	want := []RegisterWrite{
		{HVRegisterAddress, 0x2001}, {HVRegisterData, 0},
		{HVRegisterAddress, 0x200}, {HVRegisterData, 1},
	}
	if len(hardware.writes) != len(want) {
		t.Fatalf("writes = %#v", hardware.writes)
	}
	for index := range want {
		if hardware.writes[index] != want[index] {
			t.Errorf("write %d = %#v, want %#v", index, hardware.writes[index], want[index])
		}
	}
}

func TestReadHVTelemetryDecodesDirectMonitorRegisters(t *testing.T) {
	hardware := &hvHardware{reads: map[uint32]uint32{
		uint32(FPGATemperature):  2500,
		uint32(BoardTemperature): 100,
		uint32(HVVoltageMonitor): 454000,
		uint32(HVCurrentMonitor): 10000,
		uint32(HVStatus):         1000 | 1200<<13 | 1<<26 | 1<<27 | 1<<28,
	}}
	got, err := ReadHVTelemetry(context.Background(), hardware, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.BoardTemperatureC != 25 || got.VoltageV != 45.4 || got.CurrentA != 0.001 ||
		got.DetectorTemperatureC != 25.6 || got.HVTemperatureC != 30.72 ||
		!got.On || !got.Ramping || !got.OverCurrent || got.OverVoltage {
		t.Fatalf("telemetry = %#v", got)
	}
}
