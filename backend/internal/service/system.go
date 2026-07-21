// Package service adapts backend orchestration to the generated ConnectRPC API.
package service

import (
	"context"
	"fmt"
	"strings"

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
	Source                SnapshotSource
	ConfigurationTemplate string
	HV                    HVController
}

func (s *SystemService) SetHighVoltage(ctx context.Context, request *connect.Request[daqv1.SetHighVoltageRequest]) (*connect.Response[daqv1.SetHighVoltageResponse], error) {
	if s.HV == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, nil)
	}
	actor := strings.TrimSpace(request.Msg.GetRequestedBy())
	if actor == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requested_by is required"))
	}
	if err := s.HV.Set(ctx, request.Msg.GetBoards(), request.Msg.GetEnabled(), actor); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&daqv1.SetHighVoltageResponse{Snapshot: s.Source.Snapshot()}), nil
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

func (s *SystemService) GetConfigurationTemplate(_ context.Context, _ *connect.Request[daqv1.GetConfigurationTemplateRequest]) (*connect.Response[daqv1.GetConfigurationTemplateResponse], error) {
	return connect.NewResponse(&daqv1.GetConfigurationTemplateResponse{JanusConfiguration: s.ConfigurationTemplate}), nil
}

func (s *SystemService) ValidateConfiguration(_ context.Context, request *connect.Request[daqv1.ValidateConfigurationRequest]) (*connect.Response[daqv1.ValidateConfigurationResponse], error) {
	issues := ValidateJANUSConfiguration(request.Msg.GetJanusConfiguration())
	return connect.NewResponse(&daqv1.ValidateConfigurationResponse{
		Valid:  len(issues) == 0,
		Errors: legacyErrors(issues),
		Issues: issues,
	}), nil
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
