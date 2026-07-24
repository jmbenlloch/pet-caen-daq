package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelaystore"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *ScanService) StartHoldDelayScan(ctx context.Context, request *connect.Request[daqv1.StartHoldDelayScanRequest]) (*connect.Response[daqv1.StartHoldDelayScanResponse], error) {
	if s.HoldDelayController == nil || s.Telemetry == nil || s.ScanParent == "" || s.AllocateRunID == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("hold-delay scan service is unavailable"))
	}
	message := request.Msg
	actor := strings.TrimSpace(message.GetRequestedBy())
	if actor == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("requested_by is required"))
	}
	domain := holddelay.Request{
		Board: message.GetBoard(), MinimumDelayNS: message.GetMinimumDelayNs(), MaximumDelayNS: message.GetMaximumDelayNs(),
		StepNS: message.GetStepNs(), EventsPerDelay: message.GetEventsPerDelay(),
		PointTimeout: time.Duration(message.GetPointTimeoutSeconds()) * time.Second,
	}
	if err := domain.Validate(); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if state := s.HoldDelayController.StateSnapshot().State; state != acquisition.StateReady {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("hold-delay scan requires ready state, currently %s", state))
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	scanID, err := s.AllocateRunID(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("allocate hold-delay scan run number: %w", err))
	}
	if !validRunID.MatchString(scanID) {
		return nil, connect.NewError(connect.CodeInternal, errors.New("allocated hold-delay scan run number is invalid"))
	}
	startedAt := now()
	writer, err := holddelaystore.Create(s.ScanParent, holddelaystore.NewManifest(scanID, domain.Board, actor, domain, startedAt))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	scan := &daqv1.HoldDelayScan{
		Summary: &daqv1.ScanSummary{
			ScanId: scanID, Board: domain.Board, StartedAt: timestamppb.New(startedAt),
			State: daqv1.ScanState_SCAN_STATE_PREPARING, TotalPoints: domain.PointCount(),
			ScanType: daqv1.ScanType_SCAN_TYPE_HOLD_DELAY,
		},
		MinimumDelayNs: domain.MinimumDelayNS, MaximumDelayNs: domain.MaximumDelayNS, StepNs: domain.StepNS,
		EventsPerDelay: domain.EventsPerDelay, PointTimeoutSeconds: uint32(domain.PointTimeout / time.Second),
	}
	scanCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Lock()
	if s.active != nil {
		s.mu.Unlock()
		cancel()
		_ = writer.Finalize(now().UTC().Format(time.RFC3339Nano), "failed", "another scan is active", false)
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("another scan is active"))
	}
	s.active = &activeScan{id: scanID, cancel: cancel, holdDelay: scan}
	s.mu.Unlock()
	s.publishHoldDelay(scan, daqv1.SystemState_SYSTEM_STATE_SCANNING, nil)
	response := cloneHoldDelayScan(scan)
	go s.runHoldDelay(scanCtx, actor, domain, writer)
	return connect.NewResponse(&daqv1.StartHoldDelayScanResponse{Scan: response, Snapshot: s.Telemetry.Snapshot()}), nil
}

func (s *ScanService) runHoldDelay(ctx context.Context, actor string, request holddelay.Request, writer *holddelaystore.Writer) {
	s.updateHoldDelay(func(scan *daqv1.HoldDelayScan) { scan.Summary.State = daqv1.ScanState_SCAN_STATE_RUNNING })
	restored, runErr := s.HoldDelayController.RunHoldDelay(ctx, request, actor, func(point holddelay.Point) error {
		if err := writer.Append(point); err != nil {
			return err
		}
		s.updateHoldDelay(func(scan *daqv1.HoldDelayScan) {
			scan.Points = append(scan.Points, protoHoldDelayPoint(point))
			scan.Summary.CompletedPoints = uint32(len(scan.Points))
		})
		return nil
	})
	s.updateHoldDelay(func(scan *daqv1.HoldDelayScan) { scan.Summary.State = daqv1.ScanState_SCAN_STATE_RESTORING })
	state, reason := daqv1.ScanState_SCAN_STATE_COMPLETED, "completed"
	if errors.Is(runErr, context.Canceled) {
		state, reason = daqv1.ScanState_SCAN_STATE_CANCELLED, "operator_cancel"
	} else if runErr != nil {
		state, reason = daqv1.ScanState_SCAN_STATE_FAILED, runErr.Error()
	}
	completedAt := time.Now().UTC()
	manifestState := strings.ToLower(strings.TrimPrefix(state.String(), "SCAN_STATE_"))
	finalErr := runErr
	if err := writer.Finalize(completedAt.Format(time.RFC3339Nano), manifestState, reason, restored); err != nil {
		finalErr = errors.Join(runErr, err)
		state, reason = daqv1.ScanState_SCAN_STATE_FAILED, finalErr.Error()
	}
	s.mu.Lock()
	if s.active == nil || s.active.holdDelay == nil {
		s.mu.Unlock()
		return
	}
	scan := s.active.holdDelay
	scan.Summary.State, scan.Summary.TerminationReason = state, reason
	scan.Summary.CompletedAt = timestamppb.New(completedAt)
	manifest, _, readErr := holddelaystore.Read(s.ScanParent, scan.Summary.ScanId)
	if readErr == nil && manifest.Artifact != nil {
		scan.Summary.Artifact = &daqv1.Artifact{Kind: manifest.Artifact.Kind, Name: manifest.Artifact.Name, SizeBytes: manifest.Artifact.SizeBytes, Sha256: manifest.Artifact.SHA256}
	}
	final := cloneHoldDelayScan(scan)
	s.active = nil
	s.mu.Unlock()
	s.publishHoldDelay(nil, stateAfterScan(finalErr), final.Summary)
}

func (s *ScanService) GetHoldDelayScan(_ context.Context, request *connect.Request[daqv1.GetHoldDelayScanRequest]) (*connect.Response[daqv1.GetHoldDelayScanResponse], error) {
	scanID := request.Msg.GetScanId()
	if s.ScanParent == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("scan history is unavailable"))
	}
	if !validRunID.MatchString(scanID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a valid scan_id is required"))
	}
	manifest, points, err := holddelaystore.Read(s.ScanParent, scanID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	scan := &daqv1.HoldDelayScan{
		Summary: protoHoldDelaySummary(manifest), MinimumDelayNs: manifest.Request.MinimumDelayNS,
		MaximumDelayNs: manifest.Request.MaximumDelayNS, StepNs: manifest.Request.StepNS,
		EventsPerDelay: manifest.Request.EventsPerDelay, PointTimeoutSeconds: uint32(manifest.Request.PointTimeout / time.Second),
	}
	for _, point := range points {
		scan.Points = append(scan.Points, protoHoldDelayPoint(point))
	}
	return connect.NewResponse(&daqv1.GetHoldDelayScanResponse{Scan: scan}), nil
}

func (s *ScanService) updateHoldDelay(change func(*daqv1.HoldDelayScan)) {
	s.mu.Lock()
	if s.active == nil || s.active.holdDelay == nil {
		s.mu.Unlock()
		return
	}
	change(s.active.holdDelay)
	scan := cloneHoldDelayScan(s.active.holdDelay)
	s.mu.Unlock()
	s.publishHoldDelay(scan, daqv1.SystemState_SYSTEM_STATE_SCANNING, nil)
}

func (s *ScanService) publishHoldDelay(scan *daqv1.HoldDelayScan, state daqv1.SystemState, completed *daqv1.ScanSummary) {
	s.Telemetry.Update(func(snapshot *daqv1.TelemetrySnapshot) {
		snapshot.State = state
		snapshot.CurrentHoldDelayScan = cloneHoldDelayScan(scan)
		snapshot.CurrentStaircase = nil
		if completed != nil {
			snapshot.LatestCompletedScan = proto.Clone(completed).(*daqv1.ScanSummary)
		}
	})
}

func cloneHoldDelayScan(scan *daqv1.HoldDelayScan) *daqv1.HoldDelayScan {
	if scan == nil {
		return nil
	}
	return proto.Clone(scan).(*daqv1.HoldDelayScan)
}

func protoHoldDelayPoint(point holddelay.Point) *daqv1.HoldDelayPoint {
	out := &daqv1.HoldDelayPoint{
		DelayNs: point.DelayNS, EffectiveDelayNs: point.EffectiveDelay,
		EventsCollected: point.EventsCollected, ElapsedNanoseconds: uint64(point.Elapsed),
	}
	for channel := 0; channel < holddelay.ChannelCount; channel++ {
		bins := make([]uint32, holddelay.BinCount)
		copy(bins, point.HighGainBins[channel][:])
		out.Channels = append(out.Channels, &daqv1.HoldDelayChannelHistogram{
			Channel: uint32(channel), HighGainBins: bins, MissingEvents: point.MissingChannels[channel],
		})
	}
	return out
}

func protoHoldDelaySummary(manifest holddelaystore.Manifest) *daqv1.ScanSummary {
	started, _ := time.Parse(time.RFC3339Nano, manifest.StartedAt)
	completed, _ := time.Parse(time.RFC3339Nano, manifest.CompletedAt)
	state := daqv1.ScanState_SCAN_STATE_FAILED
	switch manifest.State {
	case "completed":
		state = daqv1.ScanState_SCAN_STATE_COMPLETED
	case "cancelled":
		state = daqv1.ScanState_SCAN_STATE_CANCELLED
	}
	summary := &daqv1.ScanSummary{
		ScanId: manifest.ScanID, Board: manifest.Board, StartedAt: timestamppb.New(started),
		CompletedAt: timestamppb.New(completed), State: state, TerminationReason: manifest.TerminationReason,
		CompletedPoints: manifest.CompletedPoints, TotalPoints: manifest.Request.PointCount(),
		ScanType: daqv1.ScanType_SCAN_TYPE_HOLD_DELAY,
	}
	if manifest.Artifact != nil {
		summary.Artifact = &daqv1.Artifact{Kind: manifest.Artifact.Kind, Name: manifest.Artifact.Name, SizeBytes: manifest.Artifact.SizeBytes, Sha256: manifest.Artifact.SHA256}
	}
	return summary
}
