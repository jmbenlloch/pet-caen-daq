package dt5202

import (
	"os"
	"path/filepath"
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
		if got, want := len(plan.Writes), 29+4*ChannelCount; got != want {
			t.Fatalf("board %d writes = %d, want %d", board, got, want)
		}
		writes := make(map[Register]uint32)
		for _, write := range plan.Writes {
			writes[write.Address] = write.Value
		}
		checks := map[Register]uint32{
			AcquisitionControl: 0x40003003, RunMask: 1, TriggerMask: 0x41,
			TimeReferenceMask: 0x40, TimeReferenceWindow: 2000, TimeReferenceDelay: 0xffffffc2,
			DwellTime: 125000, TriggerLogicDefinition: 0x404, TimeCoarseThreshold: wantTD[board],
			ChargeCoarseThreshold: 250, LowGainShapingTime: 0, HighGainShapingTime: 0,
			IndividualRegister(LowGain, 63): 55, IndividualRegister(HighGain, 63): 55,
		}
		for address, want := range checks {
			if got := writes[address]; got != want {
				t.Errorf("board %d register %#x = %#x, want %#x", board, address, got, want)
			}
		}
		if len(plan.Deferred) == 0 {
			t.Fatalf("board %d has no explicit deferred settings", board)
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
