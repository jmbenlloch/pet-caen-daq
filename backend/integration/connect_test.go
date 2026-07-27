//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1/daqv1connect"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/service"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

type hardwareConnectionStub struct {
	connects    int
	disconnects int
}

func (s *hardwareConnectionStub) Connect(context.Context, string, string) error {
	s.connects++
	return nil
}

func (s *hardwareConnectionStub) Disconnect(context.Context, string) error {
	s.disconnects++
	return nil
}

func TestGeneratedClientSnapshotStreamAndReconnect(t *testing.T) {
	const configuration = "Open[0] usb:172.16.0.11:tdl:0:0\r\n"
	publisher, err := telemetry.NewPublisher("instance-http", &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_IDLE}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path, handler := daqv1connect.NewSystemServiceHandler(&service.SystemService{Source: publisher, ConfigurationTemplate: configuration})
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := daqv1connect.NewSystemServiceClient(server.Client(), server.URL)

	unary, err := client.GetSystemSnapshot(context.Background(), connect.NewRequest(&daqv1.GetSystemSnapshotRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if unary.Msg.Snapshot.GetInstanceId() != "instance-http" || unary.Msg.Snapshot.GetSequence() != 1 {
		t.Fatalf("unary snapshot = %+v", unary.Msg.Snapshot)
	}
	template, err := client.GetConfigurationTemplate(context.Background(), connect.NewRequest(&daqv1.GetConfigurationTemplateRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if template.Msg.GetJanusConfiguration() != configuration {
		t.Fatalf("configuration template = %q", template.Msg.GetJanusConfiguration())
	}

	streamCtx, cancelStream := context.WithCancel(context.Background())
	stream, err := client.StreamTelemetry(streamCtx, connect.NewRequest(&daqv1.StreamTelemetryRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Receive() || stream.Msg().Snapshot.GetSequence() != 1 {
		t.Fatalf("initial stream message=%+v error=%v", stream.Msg(), stream.Err())
	}
	publisher.Publish(&daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_RUNNING, CurrentRun: &daqv1.RunSummary{RunId: "42"}})
	if !stream.Receive() || stream.Msg().Snapshot.GetSequence() != 2 || stream.Msg().Snapshot.CurrentRun.GetRunId() != "42" {
		t.Fatalf("updated stream message=%+v error=%v", stream.Msg(), stream.Err())
	}
	cancelStream()
	_ = stream.Close()

	reconnected, err := client.StreamTelemetry(context.Background(), connect.NewRequest(&daqv1.StreamTelemetryRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	if !reconnected.Receive() || reconnected.Msg().Snapshot.GetSequence() != 2 || reconnected.Msg().Snapshot.GetState() != daqv1.SystemState_SYSTEM_STATE_RUNNING {
		t.Fatalf("reconnect message=%+v error=%v", reconnected.Msg(), reconnected.Err())
	}
}

func TestGeneratedHardwareConnectionRoutesAreRegistered(t *testing.T) {
	publisher, err := telemetry.NewPublisher("instance-http", &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_READY}, nil)
	if err != nil {
		t.Fatal(err)
	}
	hardware := &hardwareConnectionStub{}
	path, handler := daqv1connect.NewSystemServiceHandler(&service.SystemService{Source: publisher, Hardware: hardware})
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := daqv1connect.NewSystemServiceClient(server.Client(), server.URL)

	if _, err = client.DisconnectHardware(context.Background(), connect.NewRequest(&daqv1.DisconnectHardwareRequest{RequestedBy: "operator"})); err != nil {
		t.Fatalf("disconnect route: %v", err)
	}
	publisher.Update(func(snapshot *daqv1.TelemetrySnapshot) {
		snapshot.State = daqv1.SystemState_SYSTEM_STATE_DISCONNECTED
	})
	if _, err = client.ConnectHardware(context.Background(), connect.NewRequest(&daqv1.ConnectHardwareRequest{RequestedBy: "operator"})); err != nil {
		t.Fatalf("connect route: %v", err)
	}
	if hardware.connects != 1 || hardware.disconnects != 1 {
		t.Fatalf("hardware calls connect=%d disconnect=%d", hardware.connects, hardware.disconnects)
	}
}
