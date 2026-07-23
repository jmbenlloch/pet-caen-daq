package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
	"google.golang.org/protobuf/proto"
)

type fakeRunController struct {
	active   string
	state    acquisition.State
	startErr error
	stopErr  error
	options  acquisition.RunOptions
	pipeline acquisition.RunPipeline
}

type serviceHealthPipeline struct{ events atomic.Uint64 }

func (*serviceHealthPipeline) Submit(context.Context, acquisition.PipelineBatch) error { return nil }
func (*serviceHealthPipeline) Close() error                                            { return nil }
func (*serviceHealthPipeline) PipelineStats() acquisition.PipelineStats {
	return acquisition.PipelineStats{Capacity: 4}
}
func (*serviceHealthPipeline) StorageStats() runpipeline.StorageStats {
	return runpipeline.StorageStats{Directory: "/runs/run-42"}
}
func (p *serviceHealthPipeline) Stats() runpipeline.StorageStats {
	return runpipeline.StorageStats{Directory: "/runs/run-42", EventCount: p.events.Load()}
}
func (*serviceHealthPipeline) Artifacts() []runstore.Artifact {
	return []runstore.Artifact{{Kind: "decoded_events", Name: "events.jsonl", SizeBytes: 123, SHA256: "abc"}}
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
	nextRunID := 42
	return &RunService{Controller: controller, Telemetry: publisher, AllocateRunID: func(context.Context) (string, error) {
		id := strconv.Itoa(nextRunID)
		nextRunID++
		return id, nil
	}, Configure: func(context.Context, *janusconfig.Document, string) (acquisition.ConfigurationResult, error) {
		return acquisition.ConfigurationResult{Plans: []dt5202.ConfigurationPlan{{Board: 0}}}, nil
	}, Now: func() time.Time {
		return time.Date(2026, 7, 21, 15, 0, 0, 0, time.UTC)
	}}
}

func TestRunServiceStartAndStopPublishesSnapshots(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady, pipeline: &serviceHealthPipeline{}}
	service := newRunService(t, controller)
	start, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: validTopology, CaptureRaw: true, JournalTransport: true}))
	if err != nil {
		t.Fatal(err)
	}
	if start.Msg.Snapshot.State != daqv1.SystemState_SYSTEM_STATE_RUNNING || start.Msg.Snapshot.CurrentRun.GetRunId() != "42" || !start.Msg.Run.Incomplete {
		t.Fatalf("start response = %+v", start.Msg)
	}
	if !controller.options.CaptureRaw || !controller.options.JournalTransport || controller.options.RequestedConfiguration != validTopology || controller.options.ConfigurationAudit == nil ||
		controller.options.HDF5SegmentSizeBytes != 500*bytesPerMiB {
		t.Fatalf("run options = %+v", controller.options)
	}
	stop, err := service.StopRun(context.Background(), connect.NewRequest(&daqv1.StopRunRequest{RunId: "42", RequestedBy: "operator"}))
	if err != nil {
		t.Fatal(err)
	}
	if stop.Msg.Snapshot.State != daqv1.SystemState_SYSTEM_STATE_READY || stop.Msg.Snapshot.CurrentRun != nil || stop.Msg.Run.Incomplete || stop.Msg.Run.TerminationReason != "operator_stop" {
		t.Fatalf("stop response = %+v", stop.Msg)
	}
	if len(stop.Msg.Run.Artifacts) != 1 || stop.Msg.Run.Artifacts[0].GetName() != "events.jsonl" || stop.Msg.Run.Artifacts[0].GetSizeBytes() != 123 || stop.Msg.Run.Artifacts[0].GetSha256() != "abc" {
		t.Fatalf("artifacts = %+v", stop.Msg.Run.Artifacts)
	}
}

func TestRunServiceAcceptsExplicitHDF5SegmentSize(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady}
	service := newRunService(t, controller)
	_, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{
		RequestedBy: "operator", JanusConfiguration: validTopology, Hdf5SegmentSizeMb: 17,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if controller.options.HDF5SegmentSizeBytes != 17*bytesPerMiB {
		t.Fatalf("segment size = %d", controller.options.HDF5SegmentSizeBytes)
	}
}

func TestRunServiceRejectsOversizedHDF5Segment(t *testing.T) {
	service := newRunService(t, &fakeRunController{state: acquisition.StateReady})
	_, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{
		RequestedBy: "operator", JanusConfiguration: validTopology,
		Hdf5SegmentSizeMb: maxHDF5SegmentSizeMB + 1,
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "INVALID_HDF5_SEGMENT_SIZE") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunServiceValidatesRequestAndActiveIdentity(t *testing.T) {
	service := newRunService(t, &fakeRunController{state: acquisition.StateReady})
	_, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator"}))
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
	_, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: validTopology}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || controller.active != "" {
		t.Fatalf("code=%v error=%v controller=%+v", connect.CodeOf(err), err, controller)
	}
}

func TestRunServiceOwnsHealthMonitorThroughStop(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady, pipeline: &serviceHealthPipeline{}}
	service := newRunService(t, controller)
	service.HealthInterval = time.Hour
	_, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: validTopology}))
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

func TestRunServicePresetTimeStopsAndPublishesCompletion(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady, pipeline: &serviceHealthPipeline{}}
	service := newRunService(t, controller)
	service.HealthInterval = time.Hour
	configuration := validTopology + "StopRunMode PRESET_TIME\nPresetTime 20 ms\n"
	start, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: configuration}))
	if err != nil {
		t.Fatal(err)
	}
	if start.Msg.Run.GetStopMode() != "PRESET_TIME" || start.Msg.Run.GetPresetTimeMilliseconds() != 20 {
		t.Fatalf("run policy = %+v", start.Msg.Run)
	}
	deadline := time.Now().Add(time.Second)
	for service.Telemetry.Snapshot().GetLatestCompletedRun() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	snapshot := service.Telemetry.Snapshot()
	if controller.ActiveRunID() != "" || snapshot.GetState() != daqv1.SystemState_SYSTEM_STATE_READY || snapshot.GetLatestCompletedRun().GetTerminationReason() != "preset_time" {
		t.Fatalf("controller=%+v snapshot=%+v", controller, snapshot)
	}
}

func TestRunServicePresetCountsStopsAtAuthoritativeEventCount(t *testing.T) {
	pipeline := &serviceHealthPipeline{}
	controller := &fakeRunController{state: acquisition.StateReady, pipeline: pipeline}
	service := newRunService(t, controller)
	service.HealthInterval = time.Millisecond
	configuration := validTopology + "StopRunMode PRESET_COUNTS\nPresetCounts 3\n"
	if _, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: configuration})); err != nil {
		t.Fatal(err)
	}
	pipeline.events.Store(3)
	deadline := time.Now().Add(time.Second)
	for service.Telemetry.Snapshot().GetLatestCompletedRun() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	completed := service.Telemetry.Snapshot().GetLatestCompletedRun()
	if controller.ActiveRunID() != "" || completed.GetTerminationReason() != "preset_counts" || completed.GetEventCount() != 3 {
		t.Fatalf("controller=%+v completed=%+v", controller, completed)
	}
}

func TestRunServiceStopIsIdempotentAndNextStartUsesNextID(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady}
	service := newRunService(t, controller)
	request := connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: validTopology})
	if _, err := service.StartRun(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	stopRequest := connect.NewRequest(&daqv1.StopRunRequest{RunId: "42", RequestedBy: "operator"})
	first, err := service.StopRun(context.Background(), stopRequest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.StopRun(context.Background(), stopRequest)
	if err != nil || !proto.Equal(first.Msg.GetRun(), second.Msg.GetRun()) {
		t.Fatalf("first=%+v second=%+v error=%v", first.Msg, second, err)
	}
	next, err := service.StartRun(context.Background(), request)
	if err != nil || next.Msg.GetRun().GetRunId() != "43" {
		t.Fatalf("next=%+v error=%v", next, err)
	}
}

func TestRunServiceRejectsInvalidOrFailedAllocatedID(t *testing.T) {
	service := newRunService(t, &fakeRunController{state: acquisition.StateReady})
	service.AllocateRunID = func(context.Context) (string, error) { return "../escape", nil }
	_, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: validTopology}))
	if connect.CodeOf(err) != connect.CodeInternal || !strings.Contains(err.Error(), "[INVALID_ALLOCATED_RUN_ID]") {
		t.Fatalf("code=%v error=%v", connect.CodeOf(err), err)
	}
	service.AllocateRunID = func(context.Context) (string, error) { return "", errors.New("database unavailable") }
	_, err = service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: validTopology}))
	if connect.CodeOf(err) != connect.CodeInternal || !strings.Contains(err.Error(), "[RUN_ID_ALLOCATION_FAILED]") {
		t.Fatalf("code=%v error=%v", connect.CodeOf(err), err)
	}
}

func TestRunServiceRejectsConcurrentOperation(t *testing.T) {
	controller := &fakeRunController{state: acquisition.StateReady}
	service := newRunService(t, controller)
	entered, release := make(chan struct{}), make(chan struct{})
	service.Configure = func(context.Context, *janusconfig.Document, string) (acquisition.ConfigurationResult, error) {
		close(entered)
		<-release
		return acquisition.ConfigurationResult{Plans: []dt5202.ConfigurationPlan{{Board: 0}}}, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := service.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: validTopology}))
		done <- err
	}()
	<-entered
	_, err := service.StopRun(context.Background(), connect.NewRequest(&daqv1.StopRunRequest{RunId: "42", RequestedBy: "operator"}))
	if connect.CodeOf(err) != connect.CodeAborted || !strings.Contains(err.Error(), "[CONCURRENT_OPERATION]") {
		t.Fatalf("code=%v error=%v", connect.CodeOf(err), err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
