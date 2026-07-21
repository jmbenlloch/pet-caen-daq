package service

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
)

const validTopology = `
Open[0] usb:172.16.0.11:tdl:0:0
Open[1] usb:172.16.0.11:tdl:1:0
Open[2] usb:172.16.0.11:tdl:2:0
Open[3] usb:172.16.0.11:tdl:3:0
`

func TestValidateConfigurationAcceptsOwnedProductionTopology(t *testing.T) {
	issues := ValidateJANUSConfiguration(validTopology + "AcquisitionMode SPECT_TIMING\n")
	if len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestValidateConfigurationReturnsStructuredSourceLine(t *testing.T) {
	issues := ValidateJANUSConfiguration(validTopology + "MysterySetting 1\n")
	if len(issues) != 1 {
		t.Fatalf("issues = %+v", issues)
	}
	issue := issues[0]
	if issue.Severity != daqv1.ValidationSeverity_VALIDATION_SEVERITY_ERROR || issue.SourceLine != 6 || !strings.Contains(issue.Message, "unsupported JANUS setting") {
		t.Fatalf("issue = %+v", issue)
	}
}

func TestValidateConfigurationRejectsWrongTopology(t *testing.T) {
	issues := ValidateJANUSConfiguration(strings.Replace(validTopology, ":tdl:3:0", ":tdl:4:0", 1))
	if len(issues) != 1 || issues[0].Field != "Open" || !strings.Contains(issues[0].Message, "expected board 3") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestValidationRPCPopulatesNewAndLegacyErrors(t *testing.T) {
	service := &SystemService{}
	response, err := service.ValidateConfiguration(context.Background(), connect.NewRequest(&daqv1.ValidateConfigurationRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.Valid || len(response.Msg.Issues) != 1 || len(response.Msg.Errors) != 1 || response.Msg.Errors[0] != response.Msg.Issues[0].Message {
		t.Fatalf("response = %+v", response.Msg)
	}
}
