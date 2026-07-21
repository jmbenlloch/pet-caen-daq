package dt5202

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

func TestPlanProductionConfiguration(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := janusconfig.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	wantTD := []uint32{181, 183, 179, 178}
	for board := range 4 {
		plan, err := PlanProductionConfiguration(doc, board)
		if err != nil {
			t.Fatalf("board %d: %v", board, err)
		}
		if got, want := len(plan.Writes), 34+5*ChannelCount; got != want {
			t.Fatalf("board %d writes = %d, want %d", board, got, want)
		}
		writes := make(map[Register]uint32)
		for _, write := range plan.Writes {
			writes[write.Address] = write.Value
		}
		checks := map[Register]uint32{
			AcquisitionControl: 0x400c3003, RunMask: 1, TriggerMask: 0x41,
			TimeReferenceMask: 0x40, TimeReferenceWindow: 2000, TimeReferenceDelay: 0x000fffc2,
			DwellTime: 125000, TriggerLogicDefinition: 0x404, TimeCoarseThreshold: wantTD[board],
			ChargeCoarseThreshold: 250, LowGainShapingTime: 0, HighGainShapingTime: 0,
			IndividualRegister(LowGain, 63): 55, IndividualRegister(HighGain, 63): 55,
			IndividualRegister(HVIndividualAdjustment, 63): 0x100,
			DigitalProbe: 0xffff, CitirocProbe: 0,
		}
		for address, want := range checks {
			if got := writes[address]; got != want {
				t.Errorf("board %d register %#x = %#x, want %#x", board, address, got, want)
			}
		}
		if len(plan.Deferred) == 0 {
			t.Fatalf("board %d has no explicit deferred settings", board)
		}
		if plan.HV.VoltageV != 45.4 || plan.HV.CurrentLimitMA != 1 || plan.HV.TemperatureCoefficients != [3]float64{0, 50, 0} || plan.HV.TemperatureFeedback || len(plan.HV.Transactions) != 15 {
			t.Fatalf("board %d HV plan = %#v", board, plan.HV)
		}
		if plan.Pedestal.Common != 50 || plan.Pedestal.AcquisitionMode != 3 || plan.Pedestal.ZeroSuppressLowGain != 50 || plan.Pedestal.ZeroSuppressHighGain != 50 {
			t.Fatalf("board %d pedestal plan = %#v", board, plan.Pedestal)
		}
		if got, want := len(plan.Inactive), 7; got != want {
			t.Fatalf("board %d inactive settings = %d, want %d: %#v", board, got, want, plan.Inactive)
		}
		for chip := range 2 {
			stream, err := plan.Citiroc[chip].Stream()
			if err != nil {
				t.Fatalf("board %d chip %d: %v", board, chip, err)
			}
			for _, check := range []struct {
				start, width int
				want         uint32
			}{{0, 4, 0}, {128, 4, 0}, {331, 9, 0x100}, {619, 6, 55}, {625, 6, 55}, {1107, 10, 250}, {1117, 10, wantTD[board]}} {
				got, err := stream.Field(check.start, check.width)
				if err != nil || got != check.want {
					t.Errorf("board %d chip %d field %d = %d, %v; want %d", board, chip, check.start, got, err, check.want)
				}
			}
		}
	}
}

func TestPlanProductionConfigurationServiceEventModes(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, mode, setting string
		want                uint32
	}{
		{"default", "SPECT_TIMING", "", 3},
		{"disabled", "SPECT_TIMING", "DISABLED", 0},
		{"hv only", "SPECT_TIMING", "1", 1},
		{"counters only", "SPECT_TIMING", "2", 2},
		{"enabled", "SPECT_TIMING", "ENABLED", 3},
		{"counting masks counters", "COUNTING", "3", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			text := string(fixture) + "\nAcquisitionMode " + test.mode + "\n"
			if test.setting != "" {
				text += "EnableServiceEvents " + test.setting + "\n"
			}
			doc, parseErr := janusconfig.Parse(strings.NewReader(text))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			plan, planErr := PlanProductionConfiguration(doc, 0)
			if planErr != nil {
				t.Fatal(planErr)
			}
			var control uint32
			for _, write := range plan.Writes {
				if write.Address == AcquisitionControl {
					control = write.Value
				}
			}
			if got := (control >> 18) & 3; got != test.want {
				t.Fatalf("service-event mode = %d, want %d (acquisition control %#08x)", got, test.want, control)
			}
		})
	}
}

func TestPlanProductionConfigurationAppliesPerChannelOverrides(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := janusconfig.Parse(strings.NewReader(string(fixture) + "\n" +
		"TD_FineThreshold[0][7] 9\n" +
		"QD_FineThreshold[0][7] 8\n" +
		"HG_Gain[0][7] 42\n" +
		"LG_Gain[0][7] 41\n" +
		"HV_IndivAdj[0][7] 17\n" +
		"ZS_Threshold_HG[0][7] 77\n" +
		"TD_FineThreshold[1][7] 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanProductionConfiguration(doc, 0)
	if err != nil {
		t.Fatal(err)
	}
	writes := make(map[Register]uint32)
	for _, write := range plan.Writes {
		writes[write.Address] = write.Value
	}
	for address, want := range map[Register]uint32{
		IndividualRegister(TimeFineThreshold, 7):      9,
		IndividualRegister(ChargeFineThreshold, 7):    8,
		IndividualRegister(HighGain, 7):               42,
		IndividualRegister(LowGain, 7):                41,
		IndividualRegister(HVIndividualAdjustment, 7): 0x111,
		IndividualRegister(TimeFineThreshold, 8):      0,
	} {
		if got := writes[address]; got != want {
			t.Errorf("register %#x = %d, want %d", address, got, want)
		}
	}
	if got := plan.Pedestal.ZeroSuppressHighGainChannels[7]; got != 77 {
		t.Fatalf("channel 7 HG zero suppression = %d", got)
	}
}

func TestPlanProductionConfigurationRejectsInvalidServiceEventMode(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := janusconfig.Parse(strings.NewReader(string(fixture) + "\nEnableServiceEvents 4\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = PlanProductionConfiguration(doc, 0); err == nil || !strings.Contains(err.Error(), "value from 0 to 3") {
		t.Fatalf("PlanProductionConfiguration() error = %v", err)
	}
}

func TestPlanProductionConfigurationRejectsOutOfRangeOperatorValues(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, overrides, want string
	}{
		{"majority", "MajorityLevel 65\n", "MajorityLevel 65 outside production range [1,64]"},
		{"channel width step", "ChTrg_Width 10 ns\n", "8 ns increment"},
		{"coarse threshold", "TD_CoarseThreshold 2048\n", "range [0,2047]"},
		{"fine threshold", "QD_FineThreshold 16\n", "range [0,15]"},
		{"gain", "HG_Gain 64\n", "range [1,63]"},
		{"test pulse DAC", "TestPulseAmplitude 4096\n", "range [0,4095]"},
		{"active probe channel", "AnalogProbe1 FAST\nProbeChannel1 31\n", "range [32,63]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, parseErr := janusconfig.Parse(strings.NewReader(string(fixture) + "\n" + test.overrides))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if _, planErr := PlanProductionConfiguration(doc, 0); planErr == nil || !strings.Contains(planErr.Error(), test.want) {
				t.Fatalf("PlanProductionConfiguration() error = %v, want %q", planErr, test.want)
			}
		})
	}
}

func TestEncodeTimeReferenceDelayUsesHardwareTwentyBitField(t *testing.T) {
	for _, test := range []struct {
		name string
		ns   float64
		want uint32
	}{
		{"captured negative delay", -500, 0x000fffc2},
		{"zero", 0, 0},
		{"largest positive", ((1 << 19) - 1) * 8, 0x0007ffff},
		{"smallest negative", -(1 << 19) * 8, 0x00080000},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := encodeTimeReferenceDelay(test.ns)
			if err != nil || got != test.want {
				t.Fatalf("encodeTimeReferenceDelay(%g) = %#x, %v; want %#x", test.ns, got, err, test.want)
			}
		})
	}
	for _, ns := range []float64{-(1<<19)*8 - 8, (1 << 19) * 8} {
		if _, err := encodeTimeReferenceDelay(ns); err == nil {
			t.Fatalf("encodeTimeReferenceDelay(%g) accepted an out-of-range value", ns)
		}
	}
}

func TestPlanProductionConfigurationPacksEnabledProbes(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := janusconfig.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	doc.Assignments = append(doc.Assignments,
		janusconfig.Assignment{Name: "AnalogProbe0", Value: "FAST", Line: 1001},
		janusconfig.Assignment{Name: "ProbeChannel0", Value: "12", Line: 1002},
		janusconfig.Assignment{Name: "DigitalProbe0", Value: "Q_OR", Line: 1003},
		janusconfig.Assignment{Name: "AnalogProbe1", Value: "PREAMP_LG", Line: 1004},
		janusconfig.Assignment{Name: "ProbeChannel1", Value: "33", Line: 1005},
		janusconfig.Assignment{Name: "DigitalProbe1", Value: "PEAK_HG", Line: 1006})
	plan, err := PlanProductionConfiguration(doc, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantTail := []RegisterWrite{{CitirocSlowControl, 0}, {CitirocProbe, 0x8c}, {CitirocSlowControl, 0x200}, {CitirocProbe, 0x8a1}, {DigitalProbe, 0x1105}}
	got := plan.Writes[29:34]
	if !reflect.DeepEqual(got, wantTail) {
		t.Fatalf("probe writes = %#v, want %#v", got, wantTail)
	}
	for _, inactive := range plan.Inactive {
		if inactive.Name == "ProbeChannel0" || inactive.Name == "ProbeChannel1" {
			t.Fatalf("enabled probe marked inactive: %#v", inactive)
		}
	}
}

func TestPlanProductionConfigurationRejectsInvalidValue(t *testing.T) {
	doc, err := janusconfig.Parse(strings.NewReader("AcquisitionMode SURPRISE\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = PlanProductionConfiguration(doc, 0)
	if err == nil {
		t.Fatal("expected invalid acquisition mode error")
	}
}

func TestConfigurationPlanValidatesRequestedVersusEffective(t *testing.T) {
	plan := ConfigurationPlan{Board: 2, Writes: []RegisterWrite{{TriggerMask, 0x41}, {TriggerMask, 0x21}, {RunMask, 1}}}
	actual := map[Register]uint32{TriggerMask: 0x21, RunMask: 1}
	if err := plan.ValidateReadback(actual); err != nil {
		t.Fatalf("ValidateReadback() error = %v", err)
	}
	actual[TriggerMask] = 0x41
	err := plan.ValidateReadback(actual)
	if err == nil || !strings.Contains(err.Error(), "effective value 0x00000041, requested 0x00000021") {
		t.Fatalf("ValidateReadback() error = %v", err)
	}
	actual[TriggerMask] = 0x21
	delete(actual, RunMask)
	if err := plan.ValidateReadback(actual); err == nil || !strings.Contains(err.Error(), "missing from readback") {
		t.Fatalf("ValidateReadback() missing error = %v", err)
	}
}
