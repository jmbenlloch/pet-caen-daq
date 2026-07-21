package configaudit

import (
	"os"
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

func loadProduction(t *testing.T) (*janusconfig.Document, []dt5202.ConfigurationPlan) {
	t.Helper()
	file, err := os.Open("../../../test/fixtures/janus/config_same4_v3_good.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	doc, err := janusconfig.Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	var plans []dt5202.ConfigurationPlan
	for board := 0; board < 4; board++ {
		plan, err := dt5202.PlanProductionConfiguration(doc, board)
		if err != nil {
			t.Fatal(err)
		}
		plan, err = plan.WithPedestalCalibration(dt5202.PedestalCalibration{Source: "protected flash page 4"})
		if err != nil {
			t.Fatal(err)
		}
		plans = append(plans, plan)
	}
	return doc, plans
}

func TestProductionAuditAccountsForEveryAssignment(t *testing.T) {
	doc, plans := loadProduction(t)
	boards := []BoardEvidence{{0, 0x0800a707}, {1, 0x0800a707}, {2, 0x0800a707}, {3, 0x0800a707}}
	report, err := Build(doc, plans, boards)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatal("production audit is invalid")
	}
	if len(report.Settings) != 103 {
		t.Fatalf("settings = %d, want 103", len(report.Settings))
	}
	seenInactive := map[string]bool{}
	for _, setting := range report.Settings {
		if setting.Status == "" {
			t.Fatalf("unaccounted setting: %#v", setting)
		}
		if setting.Status == Applied && len(setting.Effective) == 0 {
			t.Fatalf("applied setting has no effective value: %#v", setting)
		}
		if setting.Status == Inactive {
			if setting.Reason == "" {
				t.Fatalf("inactive setting has no reason: %#v", setting)
			}
			seenInactive[setting.Name] = true
		}
	}
	for _, name := range []string{"TestPulseAmplitude", "TstampCoincWindow", "OF_ListBin", "DataAnalysis"} {
		if !seenInactive[name] {
			t.Errorf("%s was not explicitly inactive", name)
		}
	}
}

func TestAuditRejectsFirmwareBeforeDigitalProbePacking(t *testing.T) {
	doc, plans := loadProduction(t)
	boards := []BoardEvidence{{0, 0x04000000}, {1, 0x08000000}, {2, 0x08000000}, {3, 0x08000000}}
	report, err := Build(doc, plans, boards)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatal("firmware-4 report is valid")
	}
	for _, setting := range report.Settings {
		if setting.Name == "DigitalProbe0" && setting.Status == Rejected {
			return
		}
	}
	t.Fatal("missing rejected digital-probe record")
}
