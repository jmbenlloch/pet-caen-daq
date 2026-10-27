package acquisition

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
)

type staircaseChipLoad struct {
	chip, config uint32
}

// Record the registers present when each ASIC is loaded. This checks host
// command ordering, not the FPGA-generated stream or physical chip behavior.
type staircaseHardware struct {
	registers      map[dt5202.Register]uint32
	loads          []staircaseChipLoad
	configWrites   []uint32
	configReads    int
	restoring      bool
	scanError      error
	restoreError   error
	restoreFailure string
	onCounting     func()
}

func (h *staircaseHardware) WriteRegister(ctx context.Context, _, _ uint16, address, value uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	register := dt5202.Register(address)
	if h.restoring && h.restoreFailure == "write" && register == dt5202.CitirocConfig {
		return h.restoreError
	}
	h.registers[register] = value
	if register == dt5202.CitirocConfig {
		h.configWrites = append(h.configWrites, value)
	}
	return nil
}

func (h *staircaseHardware) ReadRegister(ctx context.Context, _, _ uint16, address uint32) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	register := dt5202.Register(address)
	if !h.restoring && register == dt5202.TimeORCount && h.scanError != nil {
		return 0, h.scanError
	}
	value := h.registers[register]
	if register == dt5202.CitirocConfig {
		h.configReads++
		if h.restoring && h.restoreFailure == "readback" {
			return value ^ 1, nil
		}
	}
	return value, nil
}

func (h *staircaseHardware) SendCommand(ctx context.Context, _, _ uint16, command, _ uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch dt5202.Command(command) {
	case dt5202.CommandGlobalReset:
		clear(h.registers)
		h.restoring = true
	case dt5202.CommandConfigureASIC:
		chip := (h.registers[dt5202.CitirocSlowControl] >> 9) & 1
		if h.restoring && h.restoreFailure == "reload" && chip == 1 {
			return h.restoreError
		}
		h.loads = append(h.loads, staircaseChipLoad{chip, h.registers[dt5202.CitirocConfig]})
	case dt5202.CommandResetPeriodicTrigger:
		h.onCounting()
	}
	return nil
}

func TestStaircaseRestoresProductionCitirocModes(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	scanError := errors.New("injected counter read failure")
	restoreError := errors.New("injected restoration failure")
	for _, mode := range []struct {
		name, settings string
		register       uint32
	}{
		{"LG_4.5", "\nFastShaperInput LG-PA\nHV_Adjust_Range 4.5\n", 0x00070fa1},
		{"HG_2.5", "\nFastShaperInput HG-PA\nHV_Adjust_Range 2.5\n", 0x00070e21},
		{"LG_4.5_track_hold", "\nFastShaperInput LG-PA\nHV_Adjust_Range 4.5\nPeakDetectorMode TRACK&HOLD\n", 0x00070fad},
		{"HG_2.5_track_hold", "\nFastShaperInput HG-PA\nHV_Adjust_Range 2.5\nPeakDetectorMode TRACK&HOLD\n", 0x00070e2d},
	} {
		for _, scenario := range []struct {
			name           string
			cancel         bool
			scanError      error
			restoreFailure string
		}{
			{name: "completed"},
			{name: "cancelled", cancel: true},
			{name: "counter_read_failed", scanError: scanError},
			{name: "restore_write_failed", restoreFailure: "write"},
			{name: "restore_second_asic_failed", restoreFailure: "reload"},
			{name: "restore_readback_mismatch", restoreFailure: "readback"},
			{name: "scan_and_restore_failed", scanError: scanError, restoreFailure: "write"},
		} {
			t.Run(mode.name+"/"+scenario.name, func(t *testing.T) {
				document, err := janusconfig.Parse(strings.NewReader(string(fixture) + mode.settings))
				if err != nil {
					t.Fatal(err)
				}
				plan, err := dt5202.PlanProductionConfiguration(document, 0)
				if err != nil {
					t.Fatal(err)
				}
				hardware := &staircaseHardware{registers: make(map[dt5202.Register]uint32)}
				if err := dt5202.ApplyConfiguration(context.Background(), hardware, 1, 2, plan, true); err != nil {
					t.Fatal(err)
				}
				productionLoads := []staircaseChipLoad{{0, mode.register}, {0, mode.register}, {1, mode.register}, {1, mode.register}}
				initialLoads := []staircaseChipLoad{{0, mode.register}, {0, mode.register}, {1, mode.register}, {1, mode.register}}
				if !slices.Equal(hardware.loads, initialLoads) {
					t.Fatalf("initial ASIC loads = %#v, want %#v", hardware.loads, initialLoads)
				}
				hardware.loads = nil
				hardware.configWrites = nil
				hardware.configReads = 0
				hardware.restoring = false
				hardware.scanError = scenario.scanError
				hardware.restoreError = restoreError
				hardware.restoreFailure = scenario.restoreFailure

				states, err := NewStateMachine(StateReady, nil)
				if err != nil {
					t.Fatal(err)
				}
				coordinator, err := NewStaircaseCoordinator(states, hardware, []StaircaseTarget{
					{Board: 0, Chain: 1, Node: 2, Plan: plan},
				})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				scanLoads := []staircaseChipLoad{{0, 0x00070f20}, {1, 0x00070f20}}
				countingStarted := false
				hardware.onCounting = func() {
					countingStarted = true
					if state := states.Snapshot().State; state != StateScanning {
						t.Errorf("state during counting = %s, want scanning", state)
					}
					// Both ASICs must receive latch=0 and channel triggers=1
					// before counting starts, regardless of production modes.
					if !slices.Equal(hardware.loads, scanLoads) {
						t.Errorf("ASIC loads before counting = %#v, want %#v", hardware.loads, scanLoads)
					}
					if scenario.cancel {
						cancel()
					}
				}
				points := 0
				restored, runErr := coordinator.Run(ctx, staircase.Request{
					Board: 0, Minimum: 100, Maximum: 100, Step: 1, Dwell: time.Millisecond,
				}, "test", func(staircase.Point) error {
					points++
					return nil
				})
				if !countingStarted {
					t.Fatal("scan never started counting")
				}
				wantPoints := 0
				if !scenario.cancel && scenario.scanError == nil {
					wantPoints = 1
				}
				if points != wantPoints {
					t.Errorf("points = %d, want %d", points, wantPoints)
				}
				if scenario.cancel && !errors.Is(runErr, context.Canceled) {
					t.Errorf("cancellation error = %v", runErr)
				}
				if scenario.scanError != nil && !errors.Is(runErr, scenario.scanError) {
					t.Errorf("original scan error lost: %v", runErr)
				}
				if scenario.restoreFailure == "" && !scenario.cancel && scenario.scanError == nil && runErr != nil {
					t.Errorf("completed scan error = %v", runErr)
				}
				wantRestored := scenario.restoreFailure == ""
				if restored != wantRestored {
					t.Errorf("restored = %t, want %t: %v", restored, wantRestored, runErr)
				}
				wantState := StateReady
				if scenario.scanError != nil || !wantRestored {
					wantState = StateFault
				}
				if state := states.Snapshot().State; state != wantState {
					t.Errorf("final state = %s, want %s", state, wantState)
				}
				wantWrites := []uint32{0x00070f20}
				wantLoads := slices.Clone(scanLoads)
				switch scenario.restoreFailure {
				case "write":
					if !errors.Is(runErr, restoreError) {
						t.Errorf("restoration write error lost: %v", runErr)
					}
				case "reload":
					wantWrites = append(wantWrites, mode.register)
					wantLoads = append(wantLoads, productionLoads[:2]...)
					if !errors.Is(runErr, restoreError) {
						t.Errorf("restoration ASIC error lost: %v", runErr)
					}
				default:
					wantWrites = append(wantWrites, mode.register)
					wantLoads = append(wantLoads, productionLoads...)
					if hardware.configReads != 1 {
						t.Errorf("CitirocConfig readbacks = %d, want 1", hardware.configReads)
					}
					if scenario.restoreFailure == "readback" && (runErr == nil || !strings.Contains(runErr.Error(), "effective value")) {
						t.Errorf("missing readback mismatch: %v", runErr)
					}
				}
				if !slices.Equal(hardware.configWrites, wantWrites) {
					t.Errorf("CitirocConfig writes = %#v, want %#v", hardware.configWrites, wantWrites)
				}
				if !slices.Equal(hardware.loads, wantLoads) {
					t.Errorf("ASIC loads = %#v, want %#v", hardware.loads, wantLoads)
				}
				if restored {
					if err := plan.ValidateReadback(hardware.registers); err != nil {
						t.Errorf("production configuration not restored: %v", err)
					}
				}
			})
		}
	}
}
