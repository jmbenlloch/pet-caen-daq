package service

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestRunHistory(t *testing.T) {
	parent := t.TempDir()
	writer, err := runstore.Create(parent, runstore.Manifest{RunID: "54", StartedAt: "2026-07-21T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(runstore.Envelope{Kind: "test", Payload: []byte(`{"value":54}`)}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finalize("2026-07-21T10:01:00Z", "operator_stop"); err != nil {
		t.Fatal(err)
	}

	service := &RunService{RunParent: parent}
	listed, err := service.ListRuns(context.Background(), connect.NewRequest(&daqv1.ListRunsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Msg.Runs) != 1 || listed.Msg.Runs[0].GetRunId() != "54" || listed.Msg.Runs[0].GetEventCount() != 1 || listed.Msg.Runs[0].GetIncomplete() {
		t.Fatalf("runs = %+v", listed.Msg.Runs)
	}

	_, err = service.ListRuns(context.Background(), connect.NewRequest(&daqv1.ListRunsRequest{Limit: 101}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("limit error = %v", err)
	}
}
