// Package service adapts backend orchestration to the generated ConnectRPC API.
package service

import (
	"context"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1/daqv1connect"
)

type SnapshotSource interface {
	Snapshot() *daqv1.TelemetrySnapshot
	Subscribe(context.Context) <-chan *daqv1.TelemetrySnapshot
}

type SystemService struct {
	daqv1connect.UnimplementedSystemServiceHandler
	Source SnapshotSource
}

func (s *SystemService) GetSystemSnapshot(_ context.Context, _ *connect.Request[daqv1.GetSystemSnapshotRequest]) (*connect.Response[daqv1.GetSystemSnapshotResponse], error) {
	snapshot := s.Source.Snapshot()
	response := &daqv1.GetSystemSnapshotResponse{
		InstanceId: snapshot.GetInstanceId(),
		State:      snapshot.GetState(),
		Chains:     snapshot.GetChains(),
		Snapshot:   snapshot,
	}
	return connect.NewResponse(response), nil
}

func (s *SystemService) StreamTelemetry(ctx context.Context, _ *connect.Request[daqv1.StreamTelemetryRequest], stream *connect.ServerStream[daqv1.StreamTelemetryResponse]) error {
	updates := s.Source.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case snapshot := <-updates:
			if err := stream.Send(&daqv1.StreamTelemetryResponse{Snapshot: snapshot}); err != nil {
				return err
			}
		}
	}
}
