package service

import (
	"context"
	"strings"
	"testing"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

type hvServiceHardware struct{ writes []dt5202.RegisterWrite }

func (h *hvServiceHardware) WriteRegister(_ context.Context, _, _ uint16, address, value uint32) error {
	h.writes = append(h.writes, dt5202.RegisterWrite{Address: dt5202.Register(address), Value: value})
	return nil
}
func (h *hvServiceHardware) ReadRegister(_ context.Context, _, _ uint16, address uint32) (uint32, error) {
	switch dt5202.Register(address) {
	case dt5202.FPGATemperature:
		return 2500, nil
	case dt5202.BoardTemperature:
		return 100, nil
	case dt5202.HVStatus:
		return 1 << 26, nil
	case dt5202.HVVoltageMonitor:
		return 454000, nil
	default:
		return 0, nil
	}
}

func TestNativeHVControllerEnforcesAuthorizationAndReadyState(t *testing.T) {
	states, err := acquisition.NewStateMachine(acquisition.StateReady, nil)
	if err != nil {
		t.Fatal(err)
	}
	hardware := &hvServiceHardware{}
	publisher, err := telemetry.NewPublisher("hv-test", &daqv1.TelemetrySnapshot{Chains: []*daqv1.Chain{{Index: 0, Boards: []*daqv1.Board{{Node: 0}}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	controller := &NativeHVController{
		Hardware: hardware, States: states, Publisher: publisher,
		Targets: []HVTarget{{Board: 0, Chain: 0, Node: 0}},
		Now:     func() time.Time { return time.Date(2026, 7, 22, 18, 0, 0, 0, time.UTC) },
	}
	if err := controller.Set(context.Background(), []uint32{0}, true, "operator"); err == nil || !strings.Contains(err.Error(), "authorize") {
		t.Fatalf("unauthorized error = %v", err)
	}
	controller.Authorized = true
	if err := controller.Set(context.Background(), []uint32{0}, true, "operator"); err != nil {
		t.Fatal(err)
	}
	board := publisher.Snapshot().GetChains()[0].GetBoards()[0]
	if !board.GetHvOn() || board.GetHvVoltageV() != 45.4 || len(hardware.writes) != 4 || board.GetTelemetryObservedAt() == nil || board.GetTelemetryObservedAt().AsTime() != controller.Now() {
		t.Fatalf("board = %#v, writes = %#v", board, hardware.writes)
	}
}
