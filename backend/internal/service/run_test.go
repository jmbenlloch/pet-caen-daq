package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

type fakeRunController struct {
	active   string
	state    acquisition.State
	startErr error
	stopErr  error
	options  acquisition.RunOptions
	pipeline acquisition.RunPipeline
}

type serviceHealthPipeline struct{}

func (*serviceHealthPipeline) Submit(context.Context, acquisition.PipelineBatch) error { return nil }
func (*serviceHealthPipeline) Close() error                                            { return nil }
func (*serviceHealthPipeline) PipelineStats() acquisition.PipelineStats {
	return acquisition.PipelineStats{Capacity: 4}
}
func (*serviceHealthPipeline) StorageStats() runpipeline.StorageStats {
	return runpipeline.StorageStats{Directory: "/runs/run-42"}
}

func (c *fakeRunController) Start(_ context.Context, runID, _ string, options acquisition.RunOptions) error {
	if c.startErr != nil {
		return c.startErr
	}
	c.active, c.state, c.options = runID, acquisition.StateRunning, options
	return nil
}
func (c *fakeRunController) Stop(context.Context, string) error {
	if c.stopErr != nil {
		c.state = acquisition.StateFault
		return c.stopErr
	}
	c.active, c.state = "", acquisition.StateReady
	return nil
}
func (c *fakeRunController) ActiveRunID() string                     { return c.active }
func (c *fakeRunController) ActivePipeline() acquisition.RunPipeline { return c.pipeline }
func (c *fakeRunController) StateSnapshot() acquisition.StateSnapshot {
	return acquisition.StateSnapshot{State: c.state}
}

func newRunService(t *testing.T, controller *fakeRunController) *RunService {
	t.Helper()
	publisher, err := telemetry.NewPublisher("instance-a", &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_READY}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &RunService{Controller: controller, Telemetry: publisher, Configure: func(context.Context, *janusconfig.Document, string) (acquisition.ConfigurationResult, error) {
		return acquisition.ConfigurationResult{Plans: []dt5202.ConfigurationPlan{{Board: 0}}}, nil
	}, Now: func() time.Time {
		return time.Date(2026, 7, 21, 15, 0, 0, 0, time.UTC)
	}}
}

func TestRunServiceStartAndStopPublishesSnapshots(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady}
	service := newRunService(t, controller)
	start, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RunId: "42", RequestedBy: "operator", JanusConfiguration: validTopology, CaptureRaw: true, JournalTransport: true}))
	if err != nil {
		t.Fatal(err)
	}
	if start.Msg.Snapshot.State != daqv1.SystemState_SYSTEM_STATE_RUNNING || start.Msg.Snapshot.CurrentRun.GetRunId() != "42" || !start.Msg.Run.Incomplete {
		t.Fatalf("start response = %+v", start.Msg)
	}
	if !controller.options.CaptureRaw || !controller.options.JournalTransport || controller.options.RequestedConfiguration != validTopology || controller.options.ConfigurationAudit == nil {
		t.Fatalf("run options = %+v", controller.options)
	}
	stop, err := service.StopRun(context.Background(), connect.NewRequest(&daqv1.StopRunRequest{RunId: "42", RequestedBy: "operator"}))
	if err != nil {
		t.Fatal(err)
	}
	if stop.Msg.Snapshot.State != daqv1.SystemState_SYSTEM_STATE_READY || stop.Msg.Snapshot.CurrentRun != nil || stop.Msg.Run.Incomplete || stop.Msg.Run.TerminationReason != "operator_stop" {
		t.Fatalf("stop response = %+v", stop.Msg)
	}
}

func TestRunServiceValidatesRequestAndActiveIdentity(t *testing.T) {
	service := newRunService(t, &fakeRunController{state: acquisition.StateReady})
	_, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RunId: "42", RequestedBy: "operator"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("start code = %v error=%v", connect.CodeOf(err), err)
	}
	_, err = service.StopRun(context.Background(), connect.NewRequest(&daqv1.StopRunRequest{RunId: "other", RequestedBy: "operator"}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("stop code = %v error=%v", connect.CodeOf(err), err)
	}
}

func TestRunServicePublishesFaultAfterStopFailure(t *testing.T) {
	controller := &fakeRunController{active: "42", state: acquisition.StateRunning, stopErr: errors.New("drain failed")}
	service := newRunService(t, controller)
	service.current = &daqv1.RunSummary{RunId: "42", Incomplete: true}
	_, err := service.StopRun(context.Background(), connect.NewRequest(&daqv1.StopRunRequest{RunId: "42", RequestedBy: "operator"}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || service.Telemetry.Snapshot().State != daqv1.SystemState_SYSTEM_STATE_FAULT {
		t.Fatalf("code=%v snapshot=%+v", connect.CodeOf(err), service.Telemetry.Snapshot())
	}
}

func TestRunServiceRejectsStartWhenConfigurationDoesNotReachReady(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateConfiguring}
	service := newRunService(t, controller)
	_, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RunId: "42", RequestedBy: "operator", JanusConfiguration: validTopology}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || controller.active != "" {
		t.Fatalf("code=%v error=%v controller=%+v", connect.CodeOf(err), err, controller)
	}
}

func TestRunServiceOwnsHealthMonitorThroughStop(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady, pipeline: &serviceHealthPipeline{}}
	service := newRunService(t, controller)
	service.HealthInterval = time.Hour
	_, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RunId: "42", RequestedBy: "operator", JanusConfiguration: validTopology}))
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	running := service.monitorCancel != nil && service.monitorDone != nil
	service.mu.Unlock()
	if !running {
		t.Fatal("health monitor did not start")
	}
	if _, err := service.StopRun(context.Background(), connect.NewRequest(&daqv1.StopRunRequest{RunId: "42", RequestedBy: "operator"})); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	stopped := service.monitorCancel == nil && service.monitorDone == nil
	service.mu.Unlock()
	if !stopped {
		t.Fatal("health monitor did not stop")
	}
}
