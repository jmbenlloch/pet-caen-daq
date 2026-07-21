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

func TestGeneratedClientSnapshotStreamAndReconnect(t *testing.T) {
	publisher, err := telemetry.NewPublisher("instance-http", &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_IDLE}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path, handler := daqv1connect.NewSystemServiceHandler(&service.SystemService{Source: publisher})
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
