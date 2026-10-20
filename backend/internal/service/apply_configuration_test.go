package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

func TestApplyConfigurationConfiguresWithoutStartingRun(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady}
	service := newRunService(t, controller)
	service.AllocateRunID = func(context.Context) (string, error) {
		t.Fatal("ApplyConfiguration allocated a run number")
		return "", nil
	}
	var configuredBy string
	var configured *janusconfig.Document
	service.Configure = func(_ context.Context, document *janusconfig.Document, actor string) (acquisition.ConfigurationResult, error) {
		configured, configuredBy = document, actor
		return acquisition.ConfigurationResult{Plans: []dt5202.ConfigurationPlan{{Board: 0}}, HVAuthorized: true}, nil
	}
	response, err := service.ApplyConfiguration(context.Background(), connect.NewRequest(&daqv1.ApplyConfigurationRequest{
		RequestedBy: "operator", JanusConfiguration: validTopology, ConfigurationRequestId: "apply-request",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if configured == nil || configuredBy != "operator" {
		t.Fatalf("configure document=%v actor=%q", configured, configuredBy)
	}
	if controller.active != "" || controller.options.RequestedConfiguration != "" || controller.state != acquisition.StateReady {
		t.Fatalf("acquisition started: active=%q options=%+v state=%s", controller.active, controller.options, controller.state)
	}
	applied := response.Msg.GetAppliedConfiguration()
	if applied.GetSource() != daqv1.AppliedConfigurationSource_APPLIED_CONFIGURATION_SOURCE_APPLY || applied.GetRequestedBy() != "operator" ||
		applied.GetConfigurationRequestId() != "apply-request" || applied.GetRunId() != "" || applied.GetBoardCount() != 1 || !applied.GetHvAuthorized() || !applied.GetAppliedAt().AsTime().Equal(service.now()) {
		t.Fatalf("applied configuration = %+v", applied)
	}
	snapshot := response.Msg.GetSnapshot()
	if snapshot.GetState() != daqv1.SystemState_SYSTEM_STATE_READY || snapshot.GetCurrentRun() != nil ||
		snapshot.GetAppliedConfiguration().GetSource() != daqv1.AppliedConfigurationSource_APPLIED_CONFIGURATION_SOURCE_APPLY {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if service.Telemetry.Snapshot().GetAppliedConfiguration().GetRequestedBy() != "operator" {
		t.Fatalf("published snapshot = %+v", service.Telemetry.Snapshot())
	}
}

func TestApplyConfigurationRefusedWhereStartRunIsRefused(t *testing.T) {
	for _, test := range []struct {
		name       string
		controller *fakeRunController
		diagnostic string
	}{
		{"running", &fakeRunController{state: acquisition.StateRunning, active: "41"}, "RUN_ALREADY_ACTIVE"},
		{"scanning", &fakeRunController{state: acquisition.StateScanning}, "SCAN_ACTIVE"},
		{"disconnected", &fakeRunController{state: acquisition.StateDisconnected}, "HARDWARE_DISCONNECTED"},
		{"fault", &fakeRunController{state: acquisition.StateFault}, "SYSTEM_NOT_READY"},
		{"draining", &fakeRunController{state: acquisition.StateDraining}, "SYSTEM_NOT_READY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := newRunService(t, test.controller)
			service.Configure = func(context.Context, *janusconfig.Document, string) (acquisition.ConfigurationResult, error) {
				t.Fatal("configuration was written in a refused state")
				return acquisition.ConfigurationResult{}, nil
			}
			_, err := service.ApplyConfiguration(context.Background(), connect.NewRequest(&daqv1.ApplyConfigurationRequest{
				RequestedBy: "operator", JanusConfiguration: validTopology,
			}))
			if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), test.diagnostic) {
				t.Fatalf("code=%v error=%v", connect.CodeOf(err), err)
			}
			if service.Telemetry.Snapshot().GetAppliedConfiguration() != nil {
				t.Fatal("refused application was recorded")
			}
		})
	}
}

func TestApplyConfigurationAcceptsIdleConnectedHardware(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateIdle}
	service := newRunService(t, controller)
	service.Configure = func(context.Context, *janusconfig.Document, string) (acquisition.ConfigurationResult, error) {
		controller.state = acquisition.StateReady
		return acquisition.ConfigurationResult{Plans: []dt5202.ConfigurationPlan{{Board: 0}}}, nil
	}
	if _, err := service.ApplyConfiguration(context.Background(), connect.NewRequest(&daqv1.ApplyConfigurationRequest{
		RequestedBy: "operator", JanusConfiguration: validTopology,
	})); err != nil {
		t.Fatal(err)
	}
}

func TestApplyConfigurationValidatesRequest(t *testing.T) {
	service := newRunService(t, &fakeRunController{state: acquisition.StateReady})
	_, err := service.ApplyConfiguration(context.Background(), connect.NewRequest(&daqv1.ApplyConfigurationRequest{JanusConfiguration: validTopology}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "REQUIRED_IDENTITY") {
		t.Fatalf("missing identity code=%v error=%v", connect.CodeOf(err), err)
	}
	_, err = service.ApplyConfiguration(context.Background(), connect.NewRequest(&daqv1.ApplyConfigurationRequest{RequestedBy: "operator", JanusConfiguration: "Open[0] nonsense"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "INVALID_CONFIGURATION") {
		t.Fatalf("invalid configuration code=%v error=%v", connect.CodeOf(err), err)
	}
}

func TestApplyConfigurationFailureFaultClearsAppliedRecord(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady}
	service := newRunService(t, controller)
	if _, err := service.ApplyConfiguration(context.Background(), connect.NewRequest(&daqv1.ApplyConfigurationRequest{
		RequestedBy: "operator", JanusConfiguration: validTopology,
	})); err != nil {
		t.Fatal(err)
	}
	service.Configure = func(context.Context, *janusconfig.Document, string) (acquisition.ConfigurationResult, error) {
		controller.state = acquisition.StateFault
		return acquisition.ConfigurationResult{}, errors.New("readback mismatch")
	}
	_, err := service.ApplyConfiguration(context.Background(), connect.NewRequest(&daqv1.ApplyConfigurationRequest{
		RequestedBy: "operator", JanusConfiguration: validTopology,
	}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "CONFIGURATION_APPLICATION_FAILED") {
		t.Fatalf("code=%v error=%v", connect.CodeOf(err), err)
	}
	if service.Telemetry.Snapshot().GetAppliedConfiguration() != nil {
		t.Fatal("applied configuration survived a faulted write")
	}
}

func TestStartRunRecordsAppliedConfigurationWithRunNumber(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady}
	service := newRunService(t, controller)
	start, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: validTopology, ConfigurationRequestId: "run-request"}))
	if err != nil {
		t.Fatal(err)
	}
	applied := start.Msg.GetSnapshot().GetAppliedConfiguration()
	if applied.GetSource() != daqv1.AppliedConfigurationSource_APPLIED_CONFIGURATION_SOURCE_RUN || applied.GetRunId() != "42" || applied.GetRequestedBy() != "operator" || applied.GetConfigurationRequestId() != "run-request" {
		t.Fatalf("applied configuration = %+v", applied)
	}
	_, err = service.ApplyConfiguration(context.Background(), connect.NewRequest(&daqv1.ApplyConfigurationRequest{RequestedBy: "operator", JanusConfiguration: validTopology}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "RUN_ALREADY_ACTIVE") {
		t.Fatalf("apply during run code=%v error=%v", connect.CodeOf(err), err)
	}
}

// An audit rejection happens after the boards were written, so the record
// must describe what reached them even though the request fails.
func TestAuditRejectionStillRecordsTheWrittenConfiguration(t *testing.T) {
	rejected := validTopology + "HG_ShapingTime 50 ns\n"
	deferred := func(context.Context, *janusconfig.Document, string) (acquisition.ConfigurationResult, error) {
		return acquisition.ConfigurationResult{Plans: []dt5202.ConfigurationPlan{{Board: 0, Deferred: []string{"HG_ShapingTime"}}}}, nil
	}
	for _, test := range []struct {
		name   string
		call   func(*RunService) error
		source daqv1.AppliedConfigurationSource
	}{
		{"apply", func(service *RunService) error {
			_, err := service.ApplyConfiguration(context.Background(), connect.NewRequest(&daqv1.ApplyConfigurationRequest{RequestedBy: "operator", JanusConfiguration: rejected, ConfigurationRequestId: "rejected-request"}))
			return err
		}, daqv1.AppliedConfigurationSource_APPLIED_CONFIGURATION_SOURCE_APPLY},
		{"start run", func(service *RunService) error {
			_, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: rejected, ConfigurationRequestId: "rejected-request"}))
			return err
		}, daqv1.AppliedConfigurationSource_APPLIED_CONFIGURATION_SOURCE_RUN},
	} {
		t.Run(test.name, func(t *testing.T) {
			controller := &fakeRunController{state: acquisition.StateReady}
			service := newRunService(t, controller)
			service.Configure = deferred
			service.AllocateRunID = func(context.Context) (string, error) {
				t.Fatal("run number allocated after an audit rejection")
				return "", nil
			}
			err := test.call(service)
			if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "CONFIGURATION_AUDIT_FAILED") || !strings.Contains(err.Error(), "HG_ShapingTime") {
				t.Fatalf("code=%v error=%v", connect.CodeOf(err), err)
			}
			applied := service.Telemetry.Snapshot().GetAppliedConfiguration()
			if applied.GetSource() != test.source || applied.GetConfigurationRequestId() != "rejected-request" || applied.GetRunId() != "" || applied.GetRequestedBy() != "operator" || applied.GetBoardCount() != 1 {
				t.Fatalf("applied configuration = %+v", applied)
			}
			if controller.active != "" || controller.options.RequestedConfiguration != "" {
				t.Fatalf("acquisition started after an audit rejection: %+v", controller.options)
			}
		})
	}
}

func TestApplyConfigurationForcesTheWriteWhileStartRunMayReuse(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady}
	service := newRunService(t, controller)
	var reusable, forced int
	service.Configure = func(context.Context, *janusconfig.Document, string) (acquisition.ConfigurationResult, error) {
		reusable++
		return acquisition.ConfigurationResult{Plans: []dt5202.ConfigurationPlan{{Board: 0}}}, nil
	}
	service.ForceConfigure = func(context.Context, *janusconfig.Document, string) (acquisition.ConfigurationResult, error) {
		forced++
		return acquisition.ConfigurationResult{Plans: []dt5202.ConfigurationPlan{{Board: 0}}}, nil
	}
	for range 2 {
		if _, err := service.ApplyConfiguration(context.Background(), connect.NewRequest(&daqv1.ApplyConfigurationRequest{RequestedBy: "operator", JanusConfiguration: validTopology})); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: validTopology})); err != nil {
		t.Fatal(err)
	}
	if forced != 2 || reusable != 1 {
		t.Fatalf("forced=%d reusable=%d, want 2 forced applies and 1 run configuration", forced, reusable)
	}
}
