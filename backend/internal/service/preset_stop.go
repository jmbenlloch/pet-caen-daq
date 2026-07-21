package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
)

type presetStopPolicy struct {
	mode       string
	duration   time.Duration
	eventCount uint64
}

func parsePresetStop(document *janusconfig.Document) (presetStopPolicy, error) {
	values := make(map[string]string)
	for _, assignment := range document.Assignments {
		if assignment.Index == nil && assignment.Channel == nil {
			values[assignment.Name] = assignment.Value
		}
	}
	policy := presetStopPolicy{mode: strings.ToUpper(strings.TrimSpace(values["StopRunMode"]))}
	if policy.mode == "" {
		policy.mode = "MANUAL"
	}
	switch policy.mode {
	case "MANUAL":
		return policy, nil
	case "PRESET_TIME":
		duration, err := parsePresetDuration(values["PresetTime"])
		if err != nil || duration <= 0 {
			return presetStopPolicy{}, fmt.Errorf("PresetTime must be a positive duration: %w", err)
		}
		policy.duration = duration
		return policy, nil
	case "PRESET_COUNTS":
		count, err := strconv.ParseUint(strings.TrimSpace(values["PresetCounts"]), 10, 64)
		if err != nil || count == 0 {
			return presetStopPolicy{}, fmt.Errorf("PresetCounts must be a positive integer")
		}
		policy.eventCount = count
		return policy, nil
	default:
		return presetStopPolicy{}, fmt.Errorf("unsupported StopRunMode %q", policy.mode)
	}
}

func parsePresetDuration(value string) (time.Duration, error) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 || len(fields) > 2 {
		return 0, fmt.Errorf("invalid duration %q", value)
	}
	number, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("invalid duration %q", value)
	}
	unit := "s"
	if len(fields) == 2 {
		unit = strings.ToLower(fields[1])
	}
	multipliers := map[string]time.Duration{"s": time.Second, "ms": time.Millisecond, "us": time.Microsecond, "ns": time.Nanosecond}
	multiplier, ok := multipliers[unit]
	if !ok {
		return 0, fmt.Errorf("unsupported duration unit %q", unit)
	}
	return time.Duration(number * float64(multiplier)), nil
}

func pipelineEventCount(pipeline any) uint64 {
	if source, ok := pipeline.(interface {
		Stats() runpipeline.StorageStats
	}); ok {
		return source.Stats().EventCount
	}
	return 0
}
