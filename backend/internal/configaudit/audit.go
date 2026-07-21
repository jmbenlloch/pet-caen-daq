// Package configaudit accounts for every parsed JANUS assignment at the
// boundary between requested configuration and the Phase 1 implementation.
package configaudit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

type Status string

const (
	Applied  Status = "applied"
	Inactive Status = "inactive"
	Rejected Status = "rejected"
)

type BoardEvidence struct {
	Board            int    `json:"board"`
	FirmwareRevision uint32 `json:"firmware_revision"`
}

type EffectiveValue struct {
	Board *int   `json:"board,omitempty"`
	Value string `json:"value"`
}

type Setting struct {
	Name      string            `json:"name"`
	Index     *int              `json:"index,omitempty"`
	Line      int               `json:"line"`
	Owner     janusconfig.Owner `json:"owner"`
	Requested string            `json:"requested"`
	Status    Status            `json:"status"`
	Effective []EffectiveValue  `json:"effective,omitempty"`
	Reason    string            `json:"reason,omitempty"`
}

type Report struct {
	SchemaVersion int             `json:"schema_version"`
	Valid         bool            `json:"valid"`
	Boards        []BoardEvidence `json:"boards"`
	Settings      []Setting       `json:"settings"`
}

// Build creates a complete, ordered audit. Hardware plans must already include
// calibration-dependent writes; an unresolved deferred setting is rejected.
func Build(doc *janusconfig.Document, plans []dt5202.ConfigurationPlan, boards []BoardEvidence) (Report, error) {
	classified, err := doc.Classify()
	if err != nil {
		return Report{}, err
	}
	if len(plans) == 0 {
		return Report{}, fmt.Errorf("configuration audit requires hardware plans")
	}
	planByBoard := make(map[int]dt5202.ConfigurationPlan, len(plans))
	for _, plan := range plans {
		if _, exists := planByBoard[plan.Board]; exists {
			return Report{}, fmt.Errorf("duplicate hardware plan for board %d", plan.Board)
		}
		planByBoard[plan.Board] = plan
	}
	firmware := make(map[int]uint32, len(boards))
	for _, board := range boards {
		firmware[board.Board] = board.FirmwareRevision
	}

	report := Report{SchemaVersion: 1, Valid: true, Boards: append([]BoardEvidence(nil), boards...)}
	sort.Slice(report.Boards, func(i, j int) bool { return report.Boards[i].Board < report.Boards[j].Board })
	for _, item := range classified {
		a := item.Assignment
		setting := Setting{Name: a.Name, Index: a.Index, Line: a.Line, Owner: item.Owner, Requested: strings.TrimSpace(a.Value), Status: Applied}
		switch item.Owner {
		case janusconfig.OwnerHardware:
			auditHardware(&setting, doc, planByBoard, firmware)
		case janusconfig.OwnerTopology:
			setting.Effective = []EffectiveValue{{Value: setting.Requested}}
		case janusconfig.OwnerRunControl, janusconfig.OwnerStorage, janusconfig.OwnerAnalysis:
			auditSoftwareSetting(&setting, doc)
		}
		if setting.Status == Rejected {
			report.Valid = false
		}
		report.Settings = append(report.Settings, setting)
	}
	return report, nil
}

func auditHardware(setting *Setting, doc *janusconfig.Document, plans map[int]dt5202.ConfigurationPlan, firmware map[int]uint32) {
	targets := make([]int, 0, len(plans))
	if setting.Index != nil {
		targets = append(targets, *setting.Index)
	} else {
		for board := range plans {
			targets = append(targets, board)
		}
		sort.Ints(targets)
	}
	var inactiveReasons []string
	for _, board := range targets {
		if setting.Index == nil && hasIndexedOverride(doc, setting.Name, board) {
			inactiveReasons = append(inactiveReasons, fmt.Sprintf("board %d: overridden by indexed assignment", board))
			continue
		}
		plan, ok := plans[board]
		if !ok {
			setting.Status, setting.Reason = Rejected, fmt.Sprintf("no hardware plan for board %d", board)
			return
		}
		if strings.HasPrefix(setting.Name, "DigitalProbe") && firmware[board]>>24 < 5 {
			setting.Status, setting.Reason = Rejected, fmt.Sprintf("board %d firmware %#08x does not support firmware-5 digital-probe packing", board, firmware[board])
			return
		}
		if contains(plan.Deferred, setting.Name) {
			setting.Status, setting.Reason = Rejected, fmt.Sprintf("board %d hardware setting remains deferred", board)
			return
		}
		if reason, ok := inactiveReason(plan.Inactive, setting.Name); ok {
			inactiveReasons = append(inactiveReasons, fmt.Sprintf("board %d: %s", board, reason))
			continue
		}
		b := board
		setting.Effective = append(setting.Effective, EffectiveValue{Board: &b, Value: setting.Requested})
	}
	if len(setting.Effective) == 0 && len(inactiveReasons) != 0 {
		setting.Status, setting.Reason = Inactive, strings.Join(inactiveReasons, "; ")
	} else if len(inactiveReasons) != 0 {
		setting.Reason = "inactive targets: " + strings.Join(inactiveReasons, "; ")
	}
}

func hasIndexedOverride(doc *janusconfig.Document, name string, board int) bool {
	for _, assignment := range doc.Assignments {
		if assignment.Name == name && assignment.Index != nil && *assignment.Index == board {
			return true
		}
	}
	return false
}

func auditSoftwareSetting(setting *Setting, doc *janusconfig.Document) {
	inactive := func(reason string) { setting.Status, setting.Reason = Inactive, reason }
	switch setting.Owner {
	case janusconfig.OwnerRunControl:
		switch setting.Name {
		case "TstampCoincWindow":
			if value(doc, "EventBuildingMode") == "DISABLED" {
				inactive("event building is disabled")
			}
		case "PresetCounts":
			if value(doc, "StopRunMode") != "PRESET_COUNTS" {
				inactive("stop mode does not use preset counts")
			}
		case "PresetTime":
			if value(doc, "StopRunMode") != "PRESET_TIME" {
				inactive("stop mode does not use preset time")
			}
		case "JobFirstRun", "JobLastRun", "RunSleep":
			if value(doc, "EnableJobs") == "0" {
				inactive("JANUS job sequencing is disabled")
			}
		}
	case janusconfig.OwnerStorage:
		switch setting.Name {
		case "DataFilePath":
			inactive("the runstore parent directory is supplied by the service, not the imported JANUS path")
		case "OF_ListBin":
			if setting.Requested == "1" {
				inactive("JANUS binary-list output is replaced by the Phase 1 JSON Lines event stream")
			} else {
				inactive("JANUS binary-list output is disabled")
			}
		case "OF_RawData", "OF_ListAscii", "OF_ListCSV", "OF_Sync", "OF_ServiceInfo":
			if setting.Requested == "0" {
				inactive("output is disabled by configuration")
			}
		case "OF_OutFileUnit":
			inactive("no enabled text output consumes the JANUS output unit")
		case "OF_EnMaxSize", "OF_MaxSize":
			inactive("Phase 1 runstore rotation by JANUS maximum size is not enabled")
		}
	case janusconfig.OwnerAnalysis:
		switch setting.Name {
		case "DataAnalysis":
			inactive("Phase 1 persists decoded events without an online analysis pipeline")
		case "EnableListZeroSuppr":
			if setting.Requested == "0" {
				inactive("list-output zero suppression is disabled")
			}
		case "OF_SpectHisto", "OF_ToAHisto", "OF_ToTHisto", "OF_MCS", "OF_Staircase":
			if setting.Requested == "0" {
				inactive("analysis output is disabled by configuration")
			}
		default:
			inactive("the corresponding online histogram output is disabled")
		}
	}
	if setting.Status == Applied {
		setting.Effective = []EffectiveValue{{Value: setting.Requested}}
	}
}

func value(doc *janusconfig.Document, name string) string {
	for i := len(doc.Assignments) - 1; i >= 0; i-- {
		if doc.Assignments[i].Name == name && doc.Assignments[i].Index == nil {
			return strings.TrimSpace(doc.Assignments[i].Value)
		}
	}
	return ""
}
func contains(values []string, name string) bool {
	for _, value := range values {
		if value == name {
			return true
		}
	}
	return false
}
func inactiveReason(values []dt5202.InactiveSetting, name string) (string, bool) {
	for _, value := range values {
		if value.Name == name {
			return value.Reason, true
		}
	}
	return "", false
}
