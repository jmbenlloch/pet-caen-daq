package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1/daqv1connect"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type RunController interface {
	Start(context.Context, string, string, acquisition.RunOptions) error
	Stop(context.Context, string) error
	ActiveRunID() string
	ActivePipeline() acquisition.RunPipeline
	StateSnapshot() acquisition.StateSnapshot
}

type ConfigurationApplier func(context.Context, *janusconfig.Document, string) (acquisition.ConfigurationResult, error)

type SnapshotPublisher interface {
	Snapshot() *daqv1.TelemetrySnapshot
	Publish(*daqv1.TelemetrySnapshot) *daqv1.TelemetrySnapshot
}

type RunService struct {
	daqv1connect.UnimplementedRunServiceHandler
	Controller     RunController
	Telemetry      SnapshotPublisher
	Now            func() time.Time
	Configure      ConfigurationApplier
	Boards         []configaudit.BoardEvidence
	HealthInterval time.Duration
	RunExists      func(string) (bool, error)
	RunParent      string

	opMu          sync.Mutex
	mu            sync.Mutex
	current       *daqv1.RunSummary
	completed     map[string]*daqv1.RunSummary
	monitorCancel context.CancelFunc
	monitorDone   chan error
	presetCancel  context.CancelFunc
	presetDone    chan struct{}
}

func (s *RunService) ListRuns(_ context.Context, request *connect.Request[daqv1.ListRunsRequest]) (*connect.Response[daqv1.ListRunsResponse], error) {
	limit := int(request.Msg.GetLimit())
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return nil, serviceError(connect.CodeInvalidArgument, "INVALID_RUN_LIMIT", fmt.Errorf("limit must be between 1 and 100"))
	}
	if s.RunParent == "" {
		return nil, serviceError(connect.CodeFailedPrecondition, "RUN_HISTORY_UNAVAILABLE", fmt.Errorf("run storage is not configured"))
	}
	manifests, err := runstore.ListManifests(s.RunParent, limit)
	if err != nil {
		return nil, serviceError(connect.CodeInternal, "RUN_HISTORY_INSPECTION_FAILED", err)
	}
	response := &daqv1.ListRunsResponse{Runs: make([]*daqv1.RunSummary, 0, len(manifests))}
	for _, manifest := range manifests {
		response.Runs = append(response.Runs, manifestSummary(s.RunParent, manifest))
	}
	return connect.NewResponse(response), nil
}

func (s *RunService) DownloadArtifact(_ context.Context, request *connect.Request[daqv1.DownloadArtifactRequest], stream *connect.ServerStream[daqv1.DownloadArtifactResponse]) error {
	runID, name := request.Msg.GetRunId(), request.Msg.GetArtifactName()
	if !validRunID.MatchString(runID) || name == "" || filepath.Base(name) != name {
		return serviceError(connect.CodeInvalidArgument, "INVALID_ARTIFACT_IDENTITY", fmt.Errorf("a valid run_id and artifact_name are required"))
	}
	if s.RunParent == "" {
		return serviceError(connect.CodeFailedPrecondition, "RUN_HISTORY_UNAVAILABLE", fmt.Errorf("run storage is not configured"))
	}
	file, _, err := runstore.OpenArtifact(s.RunParent, runID, name)
	if errors.Is(err, runstore.ErrRunNotFound) || errors.Is(err, runstore.ErrArtifactNotFound) {
		return serviceError(connect.CodeNotFound, "ARTIFACT_NOT_FOUND", err)
	}
	if err != nil {
		return serviceError(connect.CodeInternal, "ARTIFACT_OPEN_FAILED", err)
	}
	defer file.Close()
	buffer := make([]byte, 64<<10)
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			if err := stream.Send(&daqv1.DownloadArtifactResponse{Data: append([]byte(nil), buffer[:count]...)}); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return serviceError(connect.CodeInternal, "ARTIFACT_READ_FAILED", readErr)
		}
	}
}

func manifestSummary(parent string, manifest runstore.Manifest) *daqv1.RunSummary {
	run := &daqv1.RunSummary{
		RunId: manifest.RunID, TerminationReason: manifest.TerminationReason,
		EventCount: manifest.EventCount, RawBatchCount: manifest.RawBatchCount,
	}
	if value, err := time.Parse(time.RFC3339Nano, manifest.StartedAt); err == nil {
		run.StartedAt = timestamppb.New(value)
	}
	if value, err := time.Parse(time.RFC3339Nano, manifest.CompletedAt); err == nil {
		run.CompletedAt = timestamppb.New(value)
	}
	_, err := os.Lstat(filepath.Join(parent, "run-"+manifest.RunID, "incomplete"))
	run.Incomplete = err == nil
	for _, artifact := range manifest.Artifacts {
		run.Artifacts = append(run.Artifacts, &daqv1.Artifact{Kind: artifact.Kind, Name: artifact.Name, SizeBytes: artifact.SizeBytes, Sha256: artifact.SHA256})
	}
	return run
}

func (s *RunService) StartRun(ctx context.Context, request *connect.Request[daqv1.StartRunRequest]) (*connect.Response[daqv1.StartRunResponse], error) {
	if !s.opMu.TryLock() {
		return nil, serviceError(connect.CodeAborted, "CONCURRENT_OPERATION", fmt.Errorf("another run-control operation is in progress"))
	}
	defer s.opMu.Unlock()
	message := request.Msg
	if message.GetRunId() == "" || message.GetRequestedBy() == "" {
		return nil, serviceError(connect.CodeInvalidArgument, "REQUIRED_IDENTITY", fmt.Errorf("run_id and requested_by are required"))
	}
	if !validRunID.MatchString(message.GetRunId()) {
		return nil, serviceError(connect.CodeInvalidArgument, "INVALID_RUN_ID", fmt.Errorf("run_id must match %s", validRunID.String()))
	}
	if s.Controller.ActiveRunID() != "" {
		return nil, serviceError(connect.CodeFailedPrecondition, "RUN_ALREADY_ACTIVE", fmt.Errorf("run %q is already active", s.Controller.ActiveRunID()))
	}
	if s.completedRun(message.GetRunId()) != nil {
		return nil, serviceError(connect.CodeAlreadyExists, "DUPLICATE_RUN_ID", fmt.Errorf("run %q was already completed", message.GetRunId()))
	}
	if s.RunExists != nil {
		exists, err := s.RunExists(message.GetRunId())
		if err != nil {
			return nil, serviceError(connect.CodeInternal, "RUN_STORAGE_INSPECTION_FAILED", err)
		}
		if exists {
			return nil, serviceError(connect.CodeAlreadyExists, "RUN_DIRECTORY_EXISTS", fmt.Errorf("run directory for %q already exists", message.GetRunId()))
		}
	}
	if issues := ValidateJANUSConfiguration(message.GetJanusConfiguration()); len(issues) != 0 {
		return nil, serviceError(connect.CodeInvalidArgument, "INVALID_CONFIGURATION", fmt.Errorf("configuration is invalid: %s", issues[0].GetMessage()))
	}
	if s.Configure == nil {
		return nil, serviceError(connect.CodeFailedPrecondition, "CONFIGURATION_UNAVAILABLE", fmt.Errorf("run configuration application is unavailable"))
	}
	document, err := janusconfig.Parse(bytes.NewBufferString(message.GetJanusConfiguration()))
	if err != nil {
		return nil, serviceError(connect.CodeInvalidArgument, "INVALID_CONFIGURATION", err)
	}
	stopPolicy, err := parsePresetStop(document)
	if err != nil {
		return nil, serviceError(connect.CodeInvalidArgument, "INVALID_STOP_POLICY", err)
	}
	histogramOptions, err := parseHistogramOptions(document)
	if err != nil {
		return nil, serviceError(connect.CodeInvalidArgument, "INVALID_HISTOGRAM_CONFIGURATION", err)
	}
	configured, err := s.Configure(ctx, document, message.GetRequestedBy())
	if err != nil {
		return nil, serviceError(connect.CodeFailedPrecondition, "CONFIGURATION_APPLICATION_FAILED", fmt.Errorf("apply run configuration: %w", err))
	}
	if state := s.Controller.StateSnapshot().State; state != acquisition.StateReady {
		return nil, serviceError(connect.CodeFailedPrecondition, "SYSTEM_NOT_READY", fmt.Errorf("configuration completed in state %s, want ready", state))
	}
	audit, err := configaudit.Build(document, configured.Plans, s.Boards)
	if err != nil || !audit.Valid {
		if err == nil {
			err = fmt.Errorf("effective configuration audit rejected one or more settings")
		}
		return nil, serviceError(connect.CodeFailedPrecondition, "CONFIGURATION_AUDIT_FAILED", err)
	}
	options := acquisition.RunOptions{
		CaptureRaw: message.GetCaptureRaw(), JournalTransport: message.GetJournalTransport(), RequestedBy: message.GetRequestedBy(),
		RequestedConfiguration: message.GetJanusConfiguration(), EffectiveConfiguration: configured.Plans, ConfigurationAudit: &audit,
		Histograms: histogramOptions,
	}
	if err := s.Controller.Start(ctx, message.GetRunId(), message.GetRequestedBy(), options); err != nil {
		if errors.Is(err, runstore.ErrRunExists) {
			return nil, serviceError(connect.CodeAlreadyExists, "RUN_DIRECTORY_EXISTS", err)
		}
		return nil, serviceError(connect.CodeFailedPrecondition, "RUN_START_FAILED", err)
	}
	run := &daqv1.RunSummary{
		RunId: message.GetRunId(), StartedAt: timestamppb.New(s.now()), Incomplete: true,
		StopMode: stopPolicy.mode, PresetTimeMilliseconds: uint64(stopPolicy.duration.Milliseconds()), PresetEventCount: stopPolicy.eventCount,
	}
	s.mu.Lock()
	s.current = run
	s.mu.Unlock()
	snapshot := s.publish(run)
	s.startMonitor()
	s.startPresetMonitor(message.GetRunId(), stopPolicy)
	return connect.NewResponse(&daqv1.StartRunResponse{Run: run, Snapshot: snapshot}), nil
}

func (s *RunService) StopRun(ctx context.Context, request *connect.Request[daqv1.StopRunRequest]) (*connect.Response[daqv1.StopRunResponse], error) {
	if !s.opMu.TryLock() {
		return nil, serviceError(connect.CodeAborted, "CONCURRENT_OPERATION", fmt.Errorf("another run-control operation is in progress"))
	}
	defer s.opMu.Unlock()
	message := request.Msg
	if message.GetRunId() == "" || message.GetRequestedBy() == "" {
		return nil, serviceError(connect.CodeInvalidArgument, "REQUIRED_IDENTITY", fmt.Errorf("run_id and requested_by are required"))
	}
	active := s.Controller.ActiveRunID()
	if active == "" {
		if completed := s.completedRun(message.GetRunId()); completed != nil {
			return connect.NewResponse(&daqv1.StopRunResponse{Run: completed, Snapshot: s.Telemetry.Snapshot()}), nil
		}
		return nil, serviceError(connect.CodeFailedPrecondition, "RUN_NOT_ACTIVE", fmt.Errorf("run %q is not active", message.GetRunId()))
	}
	if active != message.GetRunId() {
		return nil, serviceError(connect.CodeFailedPrecondition, "ACTIVE_RUN_MISMATCH", fmt.Errorf("active run is %q, not %q", active, message.GetRunId()))
	}
	return s.stopActive(ctx, message.GetRunId(), message.GetRequestedBy(), "operator_stop")
}

func (s *RunService) stopActive(ctx context.Context, runID, requestedBy, reason string) (*connect.Response[daqv1.StopRunResponse], error) {
	pipeline := s.Controller.ActivePipeline()
	var err error
	if controller, ok := s.Controller.(interface {
		StopWithReason(context.Context, string, string) error
	}); ok {
		err = controller.StopWithReason(ctx, requestedBy, reason)
	} else {
		err = s.Controller.Stop(ctx, requestedBy)
	}
	if err != nil {
		s.stopMonitor()
		s.stopPresetMonitor()
		s.publish(s.currentRun())
		return nil, serviceError(connect.CodeFailedPrecondition, "RUN_STOP_FAILED", err)
	}
	s.stopMonitor()
	s.stopPresetMonitor()
	run := s.currentRun()
	if run == nil {
		run = &daqv1.RunSummary{RunId: runID}
	}
	run.CompletedAt = timestamppb.New(s.now())
	run.Incomplete = false
	run.TerminationReason = reason
	if source, ok := pipeline.(interface {
		Stats() runpipeline.StorageStats
	}); ok {
		stats := source.Stats()
		run.EventCount = stats.EventCount
		run.RawBatchCount = stats.RawBatches
	}
	if source, ok := pipeline.(interface{ Artifacts() []runstore.Artifact }); ok {
		for _, artifact := range source.Artifacts() {
			run.Artifacts = append(run.Artifacts, &daqv1.Artifact{Kind: artifact.Kind, Name: artifact.Name, SizeBytes: artifact.SizeBytes, Sha256: artifact.SHA256})
		}
	}
	s.mu.Lock()
	s.current = nil
	if s.completed == nil {
		s.completed = make(map[string]*daqv1.RunSummary)
	}
	s.completed[run.GetRunId()] = proto.Clone(run).(*daqv1.RunSummary)
	s.mu.Unlock()
	snapshot := s.publish(nil)
	snapshot.LatestCompletedRun = proto.Clone(run).(*daqv1.RunSummary)
	snapshot = s.Telemetry.Publish(snapshot)
	return connect.NewResponse(&daqv1.StopRunResponse{Run: run, Snapshot: snapshot}), nil
}

var validRunID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func serviceError(code connect.Code, diagnostic string, err error) error {
	return connect.NewError(code, fmt.Errorf("[%s] %w", diagnostic, err))
}

func (s *RunService) completedRun(runID string) *daqv1.RunSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.completed == nil || s.completed[runID] == nil {
		return nil
	}
	return proto.Clone(s.completed[runID]).(*daqv1.RunSummary)
}

func (s *RunService) startMonitor() {
	source, ok := s.Controller.ActivePipeline().(RunHealthSource)
	if !ok {
		return
	}
	interval := s.HealthInterval
	if interval <= 0 {
		interval = time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	s.mu.Lock()
	s.monitorCancel, s.monitorDone = cancel, done
	s.mu.Unlock()
	go func() { done <- (&HealthMonitor{Publisher: s.Telemetry, Source: source, Interval: interval}).Run(ctx) }()
}

func (s *RunService) stopMonitor() {
	s.mu.Lock()
	cancel, done := s.monitorCancel, s.monitorDone
	s.monitorCancel, s.monitorDone = nil, nil
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func (s *RunService) startPresetMonitor(runID string, policy presetStopPolicy) {
	if policy.mode == "MANUAL" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.mu.Lock()
	s.presetCancel, s.presetDone = cancel, done
	s.mu.Unlock()
	go func() {
		defer close(done)
		trigger := false
		if policy.mode == "PRESET_TIME" {
			timer := time.NewTimer(policy.duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				trigger = true
			}
		} else {
			interval := s.HealthInterval
			if interval <= 0 || interval > 100*time.Millisecond {
				interval = 100 * time.Millisecond
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for !trigger {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					trigger = pipelineEventCount(s.Controller.ActivePipeline()) >= policy.eventCount
				}
			}
		}
		if trigger {
			go s.automaticStop(runID, policy.mode)
		}
	}()
}

func (s *RunService) automaticStop(runID, mode string) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.Controller.ActiveRunID() != runID {
		return
	}
	reason := "preset_time"
	if mode == "PRESET_COUNTS" {
		reason = "preset_counts"
	}
	_, _ = s.stopActive(context.Background(), runID, "preset-stop", reason)
}

func (s *RunService) stopPresetMonitor() {
	s.mu.Lock()
	cancel, done := s.presetCancel, s.presetDone
	s.presetCancel, s.presetDone = nil, nil
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
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
