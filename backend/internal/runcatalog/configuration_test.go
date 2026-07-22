package runcatalog

import (
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
)

func TestNormalizeConfigurationTypesScopesAndCanonicalUnits(t *testing.T) {
	configuration := "HV_Vbias 45400 mV\n" +
		"EnableJobs 1\n" +
		"PresetTime 1.5 s\n" +
		"AcquisitionMode SPECT_TIMING\n" +
		"TD_FineThreshold[2][17] 220\n"
	board := 2
	audit := &configaudit.Report{Settings: []configaudit.Setting{
		{Name: "HV_Vbias", Line: 1, Requested: "45400 mV", Status: configaudit.Applied, Effective: []configaudit.EffectiveValue{{Board: &board, Value: "45400 mV"}}},
		{Name: "TD_FineThreshold", Index: &board, Line: 5, Requested: "220", Status: configaudit.Applied, Effective: []configaudit.EffectiveValue{{Board: &board, Value: "220"}}},
	}}
	values, err := NormalizeConfiguration(configuration, audit)
	if err != nil {
		t.Fatal(err)
	}

	assertValue(t, values, layerRequested, "HV_Vbias", -1, -1, ValueReal, 0, 45.4, "V", false)
	assertValue(t, values, layerRequested, "EnableJobs", -1, -1, ValueBoolean, 1, 0, "", false)
	assertValue(t, values, layerRequested, "PresetTime", -1, -1, ValueInteger, 1500000000, 0, "ns", false)
	assertValue(t, values, layerRequested, "AcquisitionMode", -1, -1, ValueText, 0, 0, "", false)
	assertValue(t, values, layerResolved, "HV_Vbias", 2, -1, ValueReal, 0, 45.4, "V", true)
	assertValue(t, values, layerResolved, "TD_FineThreshold", 2, 17, ValueInteger, 220, 0, "", false)
}

func TestNormalizeConfigurationUsesLastAssignmentAtSameScope(t *testing.T) {
	values, err := NormalizeConfiguration("PresetCounts 10\nPresetCounts 25\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Integer == nil || *values[0].Integer != 25 || values[0].SourceLine != 2 || values[0].RawValue != "25" {
		t.Fatalf("last requested value was not retained: %+v", values)
	}
}

func TestNormalizeConfigurationIndexesInactiveOnlyAsRequested(t *testing.T) {
	audit := &configaudit.Report{Settings: []configaudit.Setting{{Name: "PresetCounts", Line: 1, Requested: "10", Status: configaudit.Inactive}}}
	values, err := NormalizeConfiguration("PresetCounts 10\n", audit)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Layer != layerRequested {
		t.Fatalf("inactive value became resolved: %+v", values)
	}
}

func assertValue(t *testing.T, values []ConfigurationValue, layer, parameter string, board, channel int, typ ValueType, integer int64, real float64, unit string, inherited bool) {
	t.Helper()
	for _, value := range values {
		if value.Layer != layer || value.Parameter != parameter || value.Board != board || value.Channel != channel {
			continue
		}
		if value.Type != typ || value.CanonicalUnit != unit || value.Inherited != inherited {
			t.Fatalf("value metadata = %+v", value)
		}
		if typ == ValueInteger || typ == ValueBoolean {
			if value.Integer == nil || *value.Integer != integer {
				t.Fatalf("integer value = %+v", value)
			}
		}
		if typ == ValueReal && (value.Real == nil || *value.Real != real) {
			t.Fatalf("real value = %+v", value)
		}
		return
	}
	t.Fatalf("missing %s/%s[%d][%d] in %+v", layer, parameter, board, channel, values)
}
