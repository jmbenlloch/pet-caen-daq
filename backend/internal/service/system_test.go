package service

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

type connectionControllerStub struct {
	connectActor    string
	disconnectActor string
	discoverActor   string
	connectErr      error
	connectConfig   string
}

func (s *connectionControllerStub) Connect(_ context.Context, actor, configuration string) error {
	s.connectActor = actor
	s.connectConfig = configuration
	return s.connectErr
}

func (s *connectionControllerStub) Disconnect(_ context.Context, actor string) error {
	s.disconnectActor = actor
	return nil
}

func (s *connectionControllerStub) Discover(_ context.Context, actor string) error {
	s.discoverActor = actor
	return nil
}

func TestGetSystemSnapshotIncludesCompatibleAndCompleteRepresentations(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", &daqv1.TelemetrySnapshot{
		State:  daqv1.SystemState_SYSTEM_STATE_READY,
		Chains: []*daqv1.Chain{{Index: 2, Enabled: true}},
	}, nil)
	service := &SystemService{Source: publisher}
	response, err := service.GetSystemSnapshot(context.Background(), connect.NewRequest(&daqv1.GetSystemSnapshotRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	message := response.Msg
	if message.InstanceId != "instance-a" || message.State != daqv1.SystemState_SYSTEM_STATE_READY || len(message.Chains) != 1 {
		t.Fatalf("legacy snapshot fields = %+v", message)
	}
	if message.Snapshot == nil || message.Snapshot.Sequence != 1 || message.Snapshot.InstanceId != "instance-a" {
		t.Fatalf("complete snapshot = %+v", message.Snapshot)
	}
}

func TestGetConfigurationTemplateReturnsExactStartupDocument(t *testing.T) {
	const configuration = "Open[0] usb:172.16.0.11:tdl:0:0\r\n"
	service := &SystemService{ConfigurationTemplate: configuration}
	response, err := service.GetConfigurationTemplate(context.Background(), connect.NewRequest(&daqv1.GetConfigurationTemplateRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetJanusConfiguration() != configuration {
		t.Fatalf("configuration = %q", response.Msg.GetJanusConfiguration())
	}
}

func TestHardwareConnectionCommandsRequireIdentityAndReturnSnapshot(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_DISCONNECTED}, nil)
	controller := &connectionControllerStub{}
	service := &SystemService{Source: publisher, Hardware: controller}
	if _, err := service.ConnectHardware(context.Background(), connect.NewRequest(&daqv1.ConnectHardwareRequest{})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("missing identity code = %v", connect.CodeOf(err))
	}
	publisher.Update(func(snapshot *daqv1.TelemetrySnapshot) { snapshot.State = daqv1.SystemState_SYSTEM_STATE_READY })
	response, err := service.ConnectHardware(context.Background(), connect.NewRequest(&daqv1.ConnectHardwareRequest{RequestedBy: "operator", JanusConfiguration: "Open[0] usb:host:tdl:0:0"}))
	if err != nil {
		t.Fatal(err)
	}
	if controller.connectActor != "operator" || controller.connectConfig == "" || response.Msg.GetSnapshot().GetState() != daqv1.SystemState_SYSTEM_STATE_READY {
		t.Fatalf("connect response = %+v actor=%q", response.Msg, controller.connectActor)
	}
	if _, err = service.DisconnectHardware(context.Background(), connect.NewRequest(&daqv1.DisconnectHardwareRequest{RequestedBy: "operator"})); err != nil || controller.disconnectActor != "operator" {
		t.Fatalf("disconnect error=%v actor=%q", err, controller.disconnectActor)
	}
}

func TestHardwareConnectionFailureIsFailedPrecondition(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", nil, nil)
	service := &SystemService{Source: publisher, Hardware: &connectionControllerStub{connectErr: errors.New("offline")}}
	_, err := service.ConnectHardware(context.Background(), connect.NewRequest(&daqv1.ConnectHardwareRequest{RequestedBy: "operator"}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("connect code = %v error=%v", connect.CodeOf(err), err)
	}
}

func TestHardwareDiscoveryRequiresIdentityAndReturnsSnapshot(t *testing.T) {
	publisher, _ := telemetry.NewPublisher("instance-a", &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_DISCONNECTED}, nil)
	controller := &connectionControllerStub{}
	service := &SystemService{Source: publisher, Discovery: controller}
	if _, err := service.DiscoverHardware(context.Background(), connect.NewRequest(&daqv1.DiscoverHardwareRequest{})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("missing identity code = %v", connect.CodeOf(err))
	}
	publisher.Update(func(snapshot *daqv1.TelemetrySnapshot) {
		snapshot.Chains = []*daqv1.Chain{{Index: 0, Enabled: true, Boards: []*daqv1.Board{{Node: 0}}}}
	})
	response, err := service.DiscoverHardware(context.Background(), connect.NewRequest(&daqv1.DiscoverHardwareRequest{RequestedBy: "operator"}))
	if err != nil {
		t.Fatal(err)
	}
	if controller.discoverActor != "operator" || len(response.Msg.GetSnapshot().GetChains()[0].GetBoards()) != 1 {
		t.Fatalf("discovery response = %+v actor=%q", response.Msg, controller.discoverActor)
	}
}
