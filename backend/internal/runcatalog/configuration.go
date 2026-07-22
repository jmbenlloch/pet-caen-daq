package runcatalog

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

const (
	layerRequested = "requested"
	layerResolved  = "resolved"
)

// NormalizeConfiguration turns the lossless requested JANUS document and its
// configuration audit into scope-aware, typed catalog rows. Requested rows use
// the same last-assignment-wins rule as the configuration planner. Resolved
// rows are restricted to values which the audit says were actually applied.
func NormalizeConfiguration(configuration string, audit *configaudit.Report) ([]ConfigurationValue, error) {
	document, err := janusconfig.Parse(strings.NewReader(configuration))
	if err != nil {
		return nil, fmt.Errorf("parse requested configuration for catalog: %w", err)
	}

	rows := make(map[configurationKey]ConfigurationValue)
	assignments := make(map[assignmentKey]janusconfig.Assignment)
	for _, assignment := range document.Assignments {
		board, channel := scope(assignment.Index, assignment.Channel)
		row := typedConfigurationValue(layerRequested, assignment.Name, board, channel, assignment.Value, assignment.Line, false)
		rows[key(row)] = row
		assignments[assignmentKey{name: assignment.Name, board: board, channel: channel, line: assignment.Line}] = assignment
	}

	if audit != nil {
		for _, setting := range audit.Settings {
			if setting.Status != configaudit.Applied {
				continue
			}
			requestedBoard := -1
			if setting.Index != nil {
				requestedBoard = *setting.Index
			}
			requestedChannel := -1
			if assignment, ok := assignments[assignmentKey{name: setting.Name, board: requestedBoard, channel: -1, line: setting.Line}]; ok && assignment.Channel != nil {
				requestedChannel = *assignment.Channel
			}
			// Older audit manifests do not carry channel separately. Recover it by
			// source line, which is stable evidence from the parsed document.
			for candidateKey, assignment := range assignments {
				if candidateKey.name == setting.Name && candidateKey.line == setting.Line && assignment.Channel != nil {
					requestedChannel = *assignment.Channel
					break
				}
			}
			for _, effective := range setting.Effective {
				board := requestedBoard
				if effective.Board != nil {
					board = *effective.Board
				}
				inherited := requestedBoard == -1 && board >= 0
				row := typedConfigurationValue(layerResolved, setting.Name, board, requestedChannel, effective.Value, setting.Line, inherited)
				rows[key(row)] = row
			}
		}
	}

	result := make([]ConfigurationValue, 0, len(rows))
	for _, row := range rows {
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Layer != b.Layer {
			return a.Layer < b.Layer
		}
		if a.Parameter != b.Parameter {
			return a.Parameter < b.Parameter
		}
		if a.Board != b.Board {
			return a.Board < b.Board
		}
		return a.Channel < b.Channel
	})
	return result, nil
}

type configurationKey struct {
	layer, parameter string
	board, channel   int
}
type assignmentKey struct {
	name                 string
	board, channel, line int
}

func key(v ConfigurationValue) configurationKey {
	return configurationKey{v.Layer, v.Parameter, v.Board, v.Channel}
}

func scope(board, channel *int) (int, int) {
	b, c := -1, -1
	if board != nil {
		b = *board
	}
	if channel != nil {
		c = *channel
	}
	return b, c
}

var booleanParameters = map[string]bool{
	"EnableTempFeedback": true, "EnableCntZeroSuppr": true,
	"EnableServiceEvents": true, "EnableJobs": true, "RunNumber_AutoIncr": true,
	"OF_EnMaxSize": true, "OF_RawData": true, "OF_ListBin": true,
	"OF_ListAscii": true, "OF_ListCSV": true, "OF_Sync": true,
	"OF_ServiceInfo": true, "OF_RunInfo": true, "DataAnalysis": true,
	"EnableListZeroSuppr": true, "OF_SpectHisto": true, "OF_ToAHisto": true,
	"OF_ToTHisto": true, "OF_MCS": true, "OF_Staircase": true,
}

var nanosecondParameters = map[string]bool{
	"ChTrg_Width": true, "Tlogic_Width": true, "PtrgPeriod": true,
	"TrefWindow": true, "TrefDelay": true, "Hit_HoldOff": true,
	"TstampCoincWindow": true,
}

func typedConfigurationValue(layer, parameter string, board, channel int, raw string, line int, inherited bool) ConfigurationValue {
	raw = strings.TrimSpace(raw)
	v := ConfigurationValue{Layer: layer, Parameter: parameter, Board: board, Channel: channel, RawValue: raw, SourceLine: line, Inherited: inherited}
	if booleanParameters[parameter] {
		if parsed, err := strconv.ParseInt(raw, 0, 64); err == nil && (parsed == 0 || parsed == 1) {
			v.Type, v.Integer = ValueBoolean, &parsed
			return v
		}
	}
	if parameter == "PresetTime" {
		if value, ok := scaledNumber(raw, "s", map[string]float64{"ns": 1, "us": 1e3, "ms": 1e6, "s": 1e9}); ok {
			return numericValue(v, value, "ns")
		}
	}
	if nanosecondParameters[parameter] {
		if value, ok := scaledNumber(raw, "ns", map[string]float64{"ns": 1, "us": 1e3, "ms": 1e6, "s": 1e9}); ok {
			return numericValue(v, value, "ns")
		}
	}
	if parameter == "HV_Vbias" {
		if value, ok := scaledNumber(raw, "V", map[string]float64{"uV": 1e-6, "mV": 1e-3, "V": 1}); ok {
			return numericValue(v, value, "V")
		}
	}
	if parameter == "HV_Imax" {
		if value, ok := scaledNumber(raw, "mA", map[string]float64{"uA": 1e-3, "mA": 1, "A": 1e3}); ok {
			return numericValue(v, value, "mA")
		}
	}
	if parsed, err := strconv.ParseInt(raw, 0, 64); err == nil {
		v.Type, v.Integer = ValueInteger, &parsed
		return v
	}
	text := raw
	v.Type, v.Text = ValueText, &text
	return v
}

func scaledNumber(raw, defaultUnit string, multipliers map[string]float64) (float64, bool) {
	fields := strings.Fields(raw)
	if len(fields) < 1 || len(fields) > 2 {
		return 0, false
	}
	number, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	unit := defaultUnit
	if len(fields) == 2 {
		unit = fields[1]
	}
	multiplier, ok := multipliers[unit]
	return number * multiplier, ok
}

func numericValue(v ConfigurationValue, value float64, unit string) ConfigurationValue {
	v.CanonicalUnit = unit
	if value >= math.MinInt64 && value <= math.MaxInt64 && math.Trunc(value) == value {
		integer := int64(value)
		v.Type, v.Integer = ValueInteger, &integer
	} else {
		v.Type, v.Real = ValueReal, &value
	}
	return v
}
