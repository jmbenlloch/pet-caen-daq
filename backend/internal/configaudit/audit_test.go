package configaudit

import (
	"os"
	"strings"
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
	boards := []BoardEvidence{
		{Board: 0, FirmwareRevision: 0x0800a707},
		{Board: 1, FirmwareRevision: 0x0800a707},
		{Board: 2, FirmwareRevision: 0x0800a707},
		{Board: 3, FirmwareRevision: 0x0800a707},
	}
	report, err := Build(doc, plans, boards)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatal("production audit is invalid")
	}
	if len(report.Settings) != 104 {
		t.Fatalf("settings = %d, want 104", len(report.Settings))
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
	for index := range doc.Assignments {
		if doc.Assignments[index].Name == "DigitalProbe0" {
			doc.Assignments[index].Value = "Q_OR"
		}
	}
	boards := []BoardEvidence{
		{Board: 0, FirmwareRevision: 0x04000000},
		{Board: 1, FirmwareRevision: 0x08000000},
		{Board: 2, FirmwareRevision: 0x08000000},
		{Board: 3, FirmwareRevision: 0x08000000},
	}
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

func TestAuditAllowsDisabledDigitalProbesOnOlderFirmware(t *testing.T) {
	document, err := janusconfig.Parse(strings.NewReader("Open[0] usb:host:tdl:0:0\nDigitalProbe0 OFF\nDigitalProbe1 OFF\n"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := Build(document, []dt5202.ConfigurationPlan{{Board: 0}}, []BoardEvidence{{
		Board: 0, FirmwareRevision: 0x04000000,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("disabled digital probes must be valid on older firmware: %+v", report.Settings)
	}
}

func TestAuditAppliesPeakDetectorMode(t *testing.T) {
	fixture, err := os.ReadFile("../../../test/fixtures/janus/config_same4_v3_good.txt")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := janusconfig.Parse(strings.NewReader(string(fixture) + "\nPeakDetectorMode TRACK&HOLD\n"))
	if err != nil {
		t.Fatal(err)
	}
	var plans []dt5202.ConfigurationPlan
	var boards []BoardEvidence
	for board := range 4 {
		plan, err := dt5202.PlanProductionConfiguration(doc, board)
		if err != nil {
			t.Fatal(err)
		}
		plan, err = plan.WithPedestalCalibration(dt5202.PedestalCalibration{Source: "protected flash page 4"})
		if err != nil {
			t.Fatal(err)
		}
		plans = append(plans, plan)
		boards = append(boards, BoardEvidence{Board: board, FirmwareRevision: 0x0800a707})
	}
	report, err := Build(doc, plans, boards)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatal("audit with PeakDetectorMode is invalid")
	}
	for _, setting := range report.Settings {
		if setting.Name != "PeakDetectorMode" || setting.Requested != "TRACK&HOLD" {
			continue
		}
		if setting.Status != Applied || len(setting.Effective) != 4 || setting.Effective[0].Value != "TRACK&HOLD" {
			t.Fatalf("PeakDetectorMode audit = %#v", setting)
		}
		return
	}
	t.Fatal("PeakDetectorMode missing from the audit")
}

func TestAuditMarksInactiveJANUS501RunSettings(t *testing.T) {
	document, err := janusconfig.Parse(strings.NewReader("Open[0] eth:192.168.50.125:tdl:1:0\nStartRunMode TDL\nExtRunSource LEMO_RA\nGPSTimeUTC 2030-06-20 16:30:00\n"))
	if err != nil {
		t.Fatal(err)
	}
	plan := dt5202.ConfigurationPlan{Board: 0}
	report, err := Build(document, []dt5202.ConfigurationPlan{plan}, []BoardEvidence{{Board: 0}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ExtRunSource", "GPSTimeUTC"} {
		found := false
		for _, setting := range report.Settings {
			if setting.Name == name {
				found = setting.Status == Inactive && setting.Reason != ""
			}
		}
		if !found {
			t.Errorf("%s was not explicitly audited as inactive", name)
		}
	}
}
