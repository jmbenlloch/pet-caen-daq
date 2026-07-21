package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1/daqv1connect"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type RunController interface {
	Start(context.Context, string, string) error
	Stop(context.Context, string) error
	ActiveRunID() string
	StateSnapshot() acquisition.StateSnapshot
}

type SnapshotPublisher interface {
	Snapshot() *daqv1.TelemetrySnapshot
	Publish(*daqv1.TelemetrySnapshot) *daqv1.TelemetrySnapshot
}

type RunService struct {
	daqv1connect.UnimplementedRunServiceHandler
	Controller RunController
	Telemetry  SnapshotPublisher
	Now        func() time.Time

	mu      sync.Mutex
	current *daqv1.RunSummary
}

func (s *RunService) StartRun(ctx context.Context, request *connect.Request[daqv1.StartRunRequest]) (*connect.Response[daqv1.StartRunResponse], error) {
	message := request.Msg
	if message.GetRunId() == "" || message.GetRequestedBy() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("run_id and requested_by are required"))
	}
	if issues := ValidateJANUSConfiguration(message.GetJanusConfiguration()); len(issues) != 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("configuration is invalid: %s", issues[0].GetMessage()))
	}
	if err := s.Controller.Start(ctx, message.GetRunId(), message.GetRequestedBy()); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	run := &daqv1.RunSummary{RunId: message.GetRunId(), StartedAt: timestamppb.New(s.now()), Incomplete: true}
	s.mu.Lock()
	s.current = run
	s.mu.Unlock()
	snapshot := s.publish(run)
	return connect.NewResponse(&daqv1.StartRunResponse{Run: run, Snapshot: snapshot}), nil
}

func (s *RunService) StopRun(ctx context.Context, request *connect.Request[daqv1.StopRunRequest]) (*connect.Response[daqv1.StopRunResponse], error) {
	message := request.Msg
	if message.GetRunId() == "" || message.GetRequestedBy() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("run_id and requested_by are required"))
	}
	active := s.Controller.ActiveRunID()
	if active == "" || active != message.GetRunId() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("run %q is not active", message.GetRunId()))
	}
	if err := s.Controller.Stop(ctx, message.GetRequestedBy()); err != nil {
		s.publish(s.currentRun())
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	run := s.currentRun()
	if run == nil {
		run = &daqv1.RunSummary{RunId: message.GetRunId()}
	}
	run.CompletedAt = timestamppb.New(s.now())
	run.Incomplete = false
	run.TerminationReason = "operator_stop"
	s.mu.Lock()
	s.current = nil
	s.mu.Unlock()
	snapshot := s.publish(nil)
	return connect.NewResponse(&daqv1.StopRunResponse{Run: run, Snapshot: snapshot}), nil
}

func (s *RunService) publish(run *daqv1.RunSummary) *daqv1.TelemetrySnapshot {
	snapshot := s.Telemetry.Snapshot()
	snapshot.State = protobufState(s.Controller.StateSnapshot().State)
	snapshot.CurrentRun = run
	return s.Telemetry.Publish(snapshot)
}

func (s *RunService) currentRun() *daqv1.RunSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil
	}
	return proto.Clone(s.current).(*daqv1.RunSummary)
}

func (s *RunService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func protobufState(state acquisition.State) daqv1.SystemState {
	states := map[acquisition.State]daqv1.SystemState{
		acquisition.StateDisconnected: daqv1.SystemState_SYSTEM_STATE_DISCONNECTED,
		acquisition.StateConnecting:   daqv1.SystemState_SYSTEM_STATE_CONNECTING,
		acquisition.StateIdle:         daqv1.SystemState_SYSTEM_STATE_IDLE,
		acquisition.StateConfiguring:  daqv1.SystemState_SYSTEM_STATE_CONFIGURING,
		acquisition.StateReady:        daqv1.SystemState_SYSTEM_STATE_READY,
		acquisition.StateStarting:     daqv1.SystemState_SYSTEM_STATE_STARTING,
		acquisition.StateRunning:      daqv1.SystemState_SYSTEM_STATE_RUNNING,
		acquisition.StateStopping:     daqv1.SystemState_SYSTEM_STATE_STOPPING,
		acquisition.StateDraining:     daqv1.SystemState_SYSTEM_STATE_DRAINING,
		acquisition.StateFault:        daqv1.SystemState_SYSTEM_STATE_FAULT,
		acquisition.StateRecovering:   daqv1.SystemState_SYSTEM_STATE_RECOVERING,
	}
	return states[state]
}
