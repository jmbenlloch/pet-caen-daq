package service

import (
	"regexp"
	"strconv"
	"strings"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

var lineErrorPattern = regexp.MustCompile(`^line ([0-9]+):`)

// ValidateJANUSConfiguration performs validation that does not require live
// board identity or calibration evidence: lossless syntax, explicit semantic
// ownership for every assignment, and the version-one production topology.
func ValidateJANUSConfiguration(configuration string) []*daqv1.ValidationIssue {
	if strings.TrimSpace(configuration) == "" {
		return []*daqv1.ValidationIssue{validationError("janus_configuration", 0, "configuration is required")}
	}
	document, err := janusconfig.Parse(strings.NewReader(configuration))
	if err != nil {
		return []*daqv1.ValidationIssue{issueFromError("janus_configuration", err)}
	}
	if _, err = document.Classify(); err != nil {
		return []*daqv1.ValidationIssue{issueFromError("janus_configuration", err)}
	}
	connections, err := document.Connections()
	if err != nil {
		return []*daqv1.ValidationIssue{issueFromError("Open", err)}
	}
	if err = janusconfig.ValidateProductionTopology(connections); err != nil {
		return []*daqv1.ValidationIssue{issueFromError("Open", err)}
	}
	return nil
}

func issueFromError(field string, err error) *daqv1.ValidationIssue {
	line := uint32(0)
	if match := lineErrorPattern.FindStringSubmatch(err.Error()); len(match) == 2 {
		value, parseErr := strconv.ParseUint(match[1], 10, 32)
		if parseErr == nil {
			line = uint32(value)
		}
	}
	return validationError(field, line, err.Error())
}

func validationError(field string, line uint32, message string) *daqv1.ValidationIssue {
	return &daqv1.ValidationIssue{
		Severity:   daqv1.ValidationSeverity_VALIDATION_SEVERITY_ERROR,
		Field:      field,
		SourceLine: line,
		Message:    message,
	}
}

func legacyErrors(issues []*daqv1.ValidationIssue) []string {
	errors := make([]string, 0, len(issues))
	for _, issue := range issues {
		if issue.GetSeverity() == daqv1.ValidationSeverity_VALIDATION_SEVERITY_ERROR {
			errors = append(errors, issue.GetMessage())
		}
	}
	return errors
}
