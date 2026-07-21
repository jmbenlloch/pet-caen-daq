package service

import (
	"strings"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

func TestParsePresetStopModesAndUnits(t *testing.T) {
	tests := []struct {
		configuration string
		mode          string
		duration      time.Duration
		count         uint64
	}{
		{"StopRunMode MANUAL\n", "MANUAL", 0, 0},
		{"StopRunMode PRESET_TIME\nPresetTime 1.5 s\n", "PRESET_TIME", 1500 * time.Millisecond, 0},
		{"StopRunMode PRESET_TIME\nPresetTime 250 ms\n", "PRESET_TIME", 250 * time.Millisecond, 0},
		{"StopRunMode PRESET_COUNTS\nPresetCounts 42\n", "PRESET_COUNTS", 0, 42},
	}
	for _, test := range tests {
		document, err := janusconfig.Parse(strings.NewReader(test.configuration))
		if err != nil {
			t.Fatal(err)
		}
		policy, err := parsePresetStop(document)
		if err != nil || policy.mode != test.mode || policy.duration != test.duration || policy.eventCount != test.count {
			t.Fatalf("configuration %q: policy=%+v error=%v", test.configuration, policy, err)
		}
	}
}

func TestParsePresetStopRejectsZeroAndUnknownPolicies(t *testing.T) {
	for _, configuration := range []string{
		"StopRunMode PRESET_TIME\nPresetTime 0\n",
		"StopRunMode PRESET_COUNTS\nPresetCounts 0\n",
		"StopRunMode LATER\n",
	} {
		document, _ := janusconfig.Parse(strings.NewReader(configuration))
		if _, err := parsePresetStop(document); err == nil {
			t.Fatalf("accepted %q", configuration)
		}
	}
}
