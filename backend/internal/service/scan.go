package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1/daqv1connect"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircasestore"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type StaircaseController interface {
	Run(context.Context, staircase.Request, string, func(staircase.Point) error) (bool, error)
	StateSnapshot() acquisition.StateSnapshot
}

type ScanService struct {
	daqv1connect.UnimplementedScanServiceHandler
	Controller StaircaseController
	Telemetry  SnapshotPublisher
	ScanParent string
	Now        func() time.Time

	mu     sync.Mutex
	active *activeScan
}

type activeScan struct {
	id     string
	cancel context.CancelFunc
	scan   *daqv1.StaircaseScan
}

func (s *ScanService) StartStaircase(ctx context.Context, request *connect.Request[daqv1.StartStaircaseRequest]) (*connect.Response[daqv1.StartStaircaseResponse], error) {
	if s.Controller == nil || s.Telemetry == nil || s.ScanParent == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("scan service is unavailable"))
	}
	message := request.Msg
	actor := strings.TrimSpace(message.GetRequestedBy())
	if actor == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("requested_by is required"))
	}
	domain := staircase.Request{
		Board: message.GetBoard(), Minimum: message.GetMinimumThreshold(), Maximum: message.GetMaximumThreshold(),
		Step: message.GetStep(), Dwell: time.Duration(message.GetDwellMilliseconds()) * time.Millisecond,
	}
	if err := domain.Validate(); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if state := s.Controller.StateSnapshot().State; state != acquisition.StateReady {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("staircase requires ready state, currently %s", state))
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	scanID, err := newScanID(now())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	writer, err := staircasestore.Create(s.ScanParent, staircasestore.NewManifest(scanID, domain.Board, actor, domain, now()))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	total := domain.PointCount()
	scan := &daqv1.StaircaseScan{
		Summary: &daqv1.ScanSummary{
			ScanId: scanID, Board: domain.Board, StartedAt: timestamppb.New(now()),
			State: daqv1.ScanState_SCAN_STATE_PREPARING, TotalPoints: total,
		},
		MinimumThreshold: domain.Minimum, MaximumThreshold: domain.Maximum, Step: domain.Step,
		DwellMilliseconds: uint32(domain.Dwell / time.Millisecond), ScansTdAndQd: true,
	}
	scanCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Lock()
	if s.active != nil {
		s.mu.Unlock()
		cancel()
		_ = writer.Finalize(now().UTC().Format(time.RFC3339Nano), "failed", "another scan is active", false)
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("another scan is active"))
	}
	s.active = &activeScan{id: scanID, cancel: cancel, scan: scan}
	s.mu.Unlock()
	s.publish(scan, daqv1.SystemState_SYSTEM_STATE_SCANNING, nil)
	responseScan := cloneScan(scan)
	go s.run(scanCtx, actor, domain, writer)
	return connect.NewResponse(&daqv1.StartStaircaseResponse{Scan: responseScan, Snapshot: s.Telemetry.Snapshot()}), nil
}

func (s *ScanService) run(ctx context.Context, actor string, request staircase.Request, writer *staircasestore.Writer) {
	s.update(func(scan *daqv1.StaircaseScan) { scan.Summary.State = daqv1.ScanState_SCAN_STATE_RUNNING })
	restored, runErr := s.Controller.Run(ctx, request, actor, func(point staircase.Point) error {
		if err := writer.Append(point); err != nil {
			return err
		}
		s.update(func(scan *daqv1.StaircaseScan) {
			scan.Points = append(scan.Points, protoPoint(point))
			scan.Summary.CompletedPoints = uint32(len(scan.Points))
		})
		return nil
	})
	s.update(func(scan *daqv1.StaircaseScan) { scan.Summary.State = daqv1.ScanState_SCAN_STATE_RESTORING })
	state, reason := daqv1.ScanState_SCAN_STATE_COMPLETED, "completed"
	if errors.Is(runErr, context.Canceled) {
		state, reason = daqv1.ScanState_SCAN_STATE_CANCELLED, "operator_cancel"
	} else if runErr != nil {
		state, reason = daqv1.ScanState_SCAN_STATE_FAILED, runErr.Error()
	}
	completedAt := time.Now().UTC()
	manifestState := strings.ToLower(strings.TrimPrefix(state.String(), "SCAN_STATE_"))
	if err := writer.Finalize(completedAt.Format(time.RFC3339Nano), manifestState, reason, restored); err != nil {
		state, reason = daqv1.ScanState_SCAN_STATE_FAILED, errors.Join(runErr, err).Error()
	}
	s.mu.Lock()
	scan := s.active.scan
	scan.Summary.State, scan.Summary.TerminationReason = state, reason
	scan.Summary.CompletedAt = timestamppb.New(completedAt)
	manifest, _, readErr := staircasestore.Read(s.ScanParent, scan.Summary.ScanId)
	if readErr == nil && manifest.Artifact != nil {
		scan.Summary.Artifact = &daqv1.Artifact{
			Kind: manifest.Artifact.Kind, Name: manifest.Artifact.Name,
			SizeBytes: manifest.Artifact.SizeBytes, Sha256: manifest.Artifact.SHA256,
		}
	}
	final := cloneScan(scan)
	s.active = nil
	s.mu.Unlock()
	s.publish(nil, stateAfterScan(runErr), final.Summary)
}

func (s *ScanService) CancelScan(_ context.Context, request *connect.Request[daqv1.CancelScanRequest]) (*connect.Response[daqv1.CancelScanResponse], error) {
	if strings.TrimSpace(request.Msg.GetRequestedBy()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("requested_by is required"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil || s.active.id != request.Msg.GetScanId() {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("active scan not found"))
	}
	s.active.cancel()
	return connect.NewResponse(&daqv1.CancelScanResponse{Scan: cloneScan(s.active.scan), Snapshot: s.Telemetry.Snapshot()}), nil
}

func (s *ScanService) ListScans(_ context.Context, request *connect.Request[daqv1.ListScansRequest]) (*connect.Response[daqv1.ListScansResponse], error) {
	limit := int(request.Msg.GetLimit())
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("limit must be between 1 and 100"))
	}
	manifests, err := staircasestore.List(s.ScanParent, limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	response := &daqv1.ListScansResponse{}
	for _, manifest := range manifests {
		response.Scans = append(response.Scans, protoSummary(manifest))
	}
	return connect.NewResponse(response), nil
}

func (s *ScanService) GetStaircase(_ context.Context, request *connect.Request[daqv1.GetStaircaseRequest]) (*connect.Response[daqv1.GetStaircaseResponse], error) {
	manifest, points, err := staircasestore.Read(s.ScanParent, request.Msg.GetScanId())
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	scan := &daqv1.StaircaseScan{
		Summary: protoSummary(manifest), MinimumThreshold: manifest.Request.Minimum,
		MaximumThreshold: manifest.Request.Maximum, Step: manifest.Request.Step,
		DwellMilliseconds: uint32(manifest.Request.Dwell / time.Millisecond), ScansTdAndQd: true,
	}
	for _, point := range points {
		scan.Points = append(scan.Points, protoPoint(point))
	}
	return connect.NewResponse(&daqv1.GetStaircaseResponse{Scan: scan}), nil
}

func (s *ScanService) update(change func(*daqv1.StaircaseScan)) {
	s.mu.Lock()
	if s.active == nil {
		s.mu.Unlock()
		return
	}
	change(s.active.scan)
	scan := cloneScan(s.active.scan)
	s.mu.Unlock()
	s.publish(scan, daqv1.SystemState_SYSTEM_STATE_SCANNING, nil)
}

func (s *ScanService) publish(scan *daqv1.StaircaseScan, state daqv1.SystemState, completed *daqv1.ScanSummary) {
	s.Telemetry.Update(func(snapshot *daqv1.TelemetrySnapshot) {
		snapshot.State = state
		snapshot.CurrentStaircase = cloneScan(scan)
		if completed != nil {
			snapshot.LatestCompletedScan = proto.Clone(completed).(*daqv1.ScanSummary)
		}
	})
}

func stateAfterScan(err error) daqv1.SystemState {
	if err != nil && !errors.Is(err, context.Canceled) {
		return daqv1.SystemState_SYSTEM_STATE_FAULT
	}
	return daqv1.SystemState_SYSTEM_STATE_READY
}

func cloneScan(scan *daqv1.StaircaseScan) *daqv1.StaircaseScan {
	if scan == nil {
		return nil
	}
	return proto.Clone(scan).(*daqv1.StaircaseScan)
}

func protoPoint(point staircase.Point) *daqv1.StaircasePoint {
	counts := make([]uint64, staircase.ChannelCount)
	rates := make([]float64, staircase.ChannelCount)
	for index := range counts {
		counts[index], rates[index] = uint64(point.ChannelCounts[index]), point.ChannelRatesCPS[index]
	}
	return &daqv1.StaircasePoint{
		Threshold: point.Threshold, ElapsedNanoseconds: uint64(point.Elapsed),
		ChannelCounts: counts, ChannelRatesCps: rates, TOrCount: uint64(point.TORCount),
		QOrCount: uint64(point.QORCount), TOrRateCps: point.TORRateCPS, QOrRateCps: point.QORRateCPS,
	}
}

func protoSummary(manifest staircasestore.Manifest) *daqv1.ScanSummary {
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
	}
	if manifest.Artifact != nil {
		summary.Artifact = &daqv1.Artifact{
			Kind: manifest.Artifact.Kind, Name: manifest.Artifact.Name,
			SizeBytes: manifest.Artifact.SizeBytes, Sha256: manifest.Artifact.SHA256,
		}
	}
	return summary
}

func newScanID(now time.Time) (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("allocate scan ID: %w", err)
	}
	return now.UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(suffix[:]), nil
}
