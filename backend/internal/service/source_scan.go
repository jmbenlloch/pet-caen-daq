package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1/daqv1connect"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/sourcestage"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type SourceScanService struct {
	daqv1connect.UnimplementedSourceScanServiceHandler
	Runs     *RunService
	Endpoint string
	Parent   string
	Open     func(context.Context, string) (sourcestage.Device, error)
	// Lifetime is the server lifetime, not the initiating HTTP request.
	Lifetime  context.Context
	operation sync.Mutex
	mu        sync.Mutex
	scan      *daqv1.SourceScan
	cancel    context.CancelFunc
	done      chan struct{}
}

func (s *SourceScanService) active() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.cancel != nil }

// Interceptor reserves all external hardware mutations for the scan, including
// between point runs. Read APIs and telemetry remain available. All handlers on
// the server use this same interceptor so checking and starting cannot race.
func (s *SourceScanService) Interceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			name := filepath.Base(req.Spec().Procedure)
			switch name {
			case "ApplyConfiguration", "StartRun", "StopRun", "ConnectHardware", "DisconnectHardware", "DiscoverHardware", "SetHighVoltage", "StartStaircase", "StartHoldDelayScan", "CancelScan", "StartSourceScan":
				s.operation.Lock()
				defer s.operation.Unlock()
				if s.active() {
					return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("source scan owns hardware; cancel the source scan first"))
				}
			}
			return next(ctx, req)
		}
	})
}

func (s *SourceScanService) StartSourceScan(ctx context.Context, req *connect.Request[daqv1.StartSourceScanRequest]) (*connect.Response[daqv1.StartSourceScanResponse], error) {
	if s.Runs == nil || s.Endpoint == "" || s.Parent == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("source stage is not configured on this server"))
	}
	request := proto.Clone(req.Msg).(*daqv1.StartSourceScanRequest)
	points, err := sourcePlan(request.Configuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if !request.OriginConfirmed || strings.TrimSpace(request.GetRun().GetRequestedBy()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("operator and confirmation of physical origin are required"))
	}
	if issues := ValidateJANUSConfiguration(request.GetRun().GetJanusConfiguration()); len(issues) > 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid PET configuration: %s", issues[0].Message))
	}
	if s.Runs.Controller.ActiveRunID() != "" || s.Runs.Controller.StateSnapshot().State != acquisition.StateReady {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("source scan requires ready hardware and no active run"))
	}
	snapshot := s.Runs.Telemetry.Snapshot()
	if snapshot.GetCurrentStaircase() != nil || snapshot.GetCurrentHoldDelayScan() != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("a diagnostic scan is active"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("a source scan is active"))
	}
	id, err := s.allocateSourceID(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	scan := &daqv1.SourceScan{ScanId: id, State: "preparing", Configuration: request.Configuration, Points: points, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), RequestedBy: request.Run.RequestedBy, JanusConfiguration: request.Run.JanusConfiguration, PositionBasis: "open_loop_steps_from_operator_confirmed_origin"}
	if err := s.persist(scan); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	parent := s.Lifetime
	if parent == nil {
		parent = context.Background()
	}
	scanCtx, cancel := context.WithCancel(parent)
	s.scan, s.cancel, s.done = scan, cancel, make(chan struct{})
	log.Printf("source_scan scan_id=%s operation=start points=%d requested_by=%q", scan.ScanId, len(points), scan.RequestedBy)
	response := proto.Clone(scan).(*daqv1.SourceScan)
	go s.execute(scanCtx, request)
	return connect.NewResponse(&daqv1.StartSourceScanResponse{Scan: response}), nil
}

func (s *SourceScanService) persist(scan *daqv1.SourceScan) error {
	data, err := protojson.MarshalOptions{Indent: "  "}.Marshal(scan)
	if err != nil {
		return err
	}
	dir := filepath.Join(s.Parent, "source-scans")
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".source-scan-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, scan.ScanId+".json"))
}

func (s *SourceScanService) update(fn func(*daqv1.SourceScan)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s.scan)
	return s.persist(s.scan)
}

func (s *SourceScanService) GetSourceScan(_ context.Context, req *connect.Request[daqv1.GetSourceScanRequest]) (*connect.Response[daqv1.GetSourceScanResponse], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := req.Msg.ScanId
	response := &daqv1.GetSourceScanResponse{DeviceConfigured: s.Endpoint != ""}
	if s.scan != nil && (id == "" || id == s.scan.ScanId) {
		response.Scan = proto.Clone(s.scan).(*daqv1.SourceScan)
		return connect.NewResponse(response), nil
	}
	if id == "" {
		return connect.NewResponse(response), nil
	}
	if !validRunID.MatchString(id) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid scan ID"))
	}
	data, err := os.ReadFile(filepath.Join(s.Parent, "source-scans", id+".json"))
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	response.Scan = &daqv1.SourceScan{}
	if err := protojson.Unmarshal(data, response.Scan); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if response.Scan.CompletedAt == "" {
		response.Scan.State = "failed"
		response.Scan.Error = "server interrupted scan; physical position must be re-established before starting another scan"
	}
	return connect.NewResponse(response), nil
}

func (s *SourceScanService) CancelSourceScan(_ context.Context, req *connect.Request[daqv1.CancelSourceScanRequest]) (*connect.Response[daqv1.CancelSourceScanResponse], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(req.Msg.RequestedBy) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("requested_by required"))
	}
	if s.cancel == nil || s.scan.ScanId != req.Msg.ScanId {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("active source scan not found"))
	}
	s.cancel()
	return connect.NewResponse(&daqv1.CancelSourceScanResponse{Scan: proto.Clone(s.scan).(*daqv1.SourceScan)}), nil
}

func (s *SourceScanService) Close() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (s *SourceScanService) execute(ctx context.Context, req *daqv1.StartSourceScanRequest) {
	err := s.acquirePoints(ctx, req)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scan.State = "completed"
	if err != nil {
		s.scan.State = "failed"
		s.scan.Error = err.Error()
	}
	if errors.Is(err, context.Canceled) && s.Runs.Controller.ActiveRunID() == "" && s.Runs.Controller.StateSnapshot().State == acquisition.StateReady {
		s.scan.State = "cancelled"
	}
	s.scan.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if persistErr := s.persist(s.scan); persistErr != nil {
		s.scan.State = "failed"
		s.scan.Error = errors.Join(err, persistErr).Error()
	}
	log.Printf("source_scan scan_id=%s operation=finish state=%s completed_points=%d error=%q", s.scan.ScanId, s.scan.State, s.scan.CompletedPoints, s.scan.Error)
	s.cancel()
	s.cancel = nil
	close(s.done)
}

func (s *SourceScanService) acquirePoints(ctx context.Context, req *daqv1.StartSourceScanRequest) (result error) {
	open := s.Open
	if open == nil {
		open = sourcestage.Open
	}
	setupCtx, setupCancel := context.WithTimeout(ctx, 10*time.Second)
	defer setupCancel()
	device, err := open(setupCtx, s.Endpoint)
	if err != nil {
		return err
	}
	defer device.Close()
	// On any exit request a stop when the transport is still usable. Firmware also
	// stops within one second when MOVE heartbeats disappear.
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		result = errors.Join(result, device.Exchange(stopCtx, "S", "STOPPED"))
	}()
	if !strings.HasPrefix(s.Endpoint, "tcp://") {
		if err := sourcestage.ResetWait(setupCtx); err != nil {
			return err
		}
	}
	if err := device.Exchange(setupCtx, "HELLO", "PETXY 1"); err != nil {
		return err
	}
	c := req.Configuration
	config := fmt.Sprintf("CONFIG %d %d %d %d", c.PulseHighUs, c.PulseLowUs, boolInt(c.XPositiveDirectionHigh), boolInt(c.YPositiveDirectionHigh))
	if err := device.Exchange(setupCtx, config, "CONFIGURED"); err != nil {
		return err
	}
	if err := device.Exchange(setupCtx, "ZERO", "ZEROED"); err != nil {
		return err
	}
	s.mu.Lock()
	scanID := s.scan.ScanId
	points := proto.Clone(s.scan).(*daqv1.SourceScan).Points
	s.mu.Unlock()
	configJSON, _ := protojson.Marshal(c)
	for _, p := range points {
		if err := ctx.Err(); err != nil {
			return err
		}
		log.Printf("source_scan scan_id=%s operation=move point=%d x_steps=%d y_steps=%d", scanID, p.Index, p.XSteps, p.YSteps)
		pointCtx, cancel := context.WithTimeout(ctx, time.Duration(c.PointTimeoutSeconds)*time.Second)
		err := func() error {
			if err := s.update(func(v *daqv1.SourceScan) { v.State = "moving"; v.CurrentPoint = p.Index }); err != nil {
				return err
			}
			if err := device.Exchange(pointCtx, fmt.Sprintf("MOVE %d %d", p.XSteps, p.YSteps), fmt.Sprintf("READY %d %d", p.XSteps, p.YSteps)); err != nil {
				return err
			}
			if err := s.update(func(v *daqv1.SourceScan) { v.State = "settling" }); err != nil {
				return err
			}
			if err := waitSource(pointCtx, time.Duration(c.SettleMilliseconds)*time.Millisecond); err != nil {
				return err
			}
			metadata := &runstore.SourcePosition{ScanID: scanID, Index: p.Index, Row: p.Row, Column: p.Column, RequestedXMM: p.RequestedXMm, RequestedYMM: p.RequestedYMm, XSteps: p.XSteps, YSteps: p.YSteps, CalculatedXMM: p.CalculatedXMm, CalculatedYMM: p.CalculatedYMm, PositionBasis: "open_loop_steps_from_operator_confirmed_origin", Configuration: string(configJSON)}
			started, err := s.Runs.startRun(pointCtx, connect.NewRequest(req.Run), metadata)
			if err != nil {
				return err
			}
			runID := started.Msg.Run.RunId
			pointErr := s.update(func(v *daqv1.SourceScan) { v.State = "acquiring"; v.Points[p.Index].RunId = runID })
			if pointErr == nil {
				pointErr = s.waitAcquisition(pointCtx, runID, c)
			}
			stateErr := s.update(func(v *daqv1.SourceScan) { v.State = "stopping" })
			// Always stop and fully drain before issuing another MOVE, including on
			// cancellation. A fresh context keeps cleanup independent of the point timer.
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer stopCancel()
			s.Runs.opMu.Lock()
			reason := "source_scan_time"
			if c.EventCount > 0 {
				reason = "source_scan_events"
			}
			if pointErr != nil {
				reason = "source_scan_interrupted"
			}
			_, stopErr := s.Runs.stopActive(stopCtx, runID, req.Run.RequestedBy, reason)
			s.Runs.opMu.Unlock()
			if err := errors.Join(pointErr, stateErr, stopErr); err != nil {
				return err
			}
			return s.update(func(v *daqv1.SourceScan) { v.Points[p.Index].Completed = true; v.CompletedPoints++ })
		}()
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *SourceScanService) waitAcquisition(ctx context.Context, runID string, c *daqv1.SourceScanConfiguration) error {
	started := time.Now()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.Runs.Controller.ActiveRunID() != runID || s.Runs.Controller.StateSnapshot().State != acquisition.StateRunning {
			return fmt.Errorf("point acquisition ended unexpectedly")
		}
		if c.DwellMilliseconds > 0 && time.Since(started) >= time.Duration(c.DwellMilliseconds)*time.Millisecond {
			return nil
		}
		if c.EventCount > 0 && pipelineEventCount(s.Runs.Controller.ActivePipeline()) >= c.EventCount {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitSource(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func sourcePositionProto(p *runstore.SourcePosition) *daqv1.SourcePositionMetadata {
	if p == nil {
		return nil
	}
	return &daqv1.SourcePositionMetadata{ScanId: p.ScanID, Index: p.Index, Row: p.Row, Column: p.Column, RequestedXMm: p.RequestedXMM, RequestedYMm: p.RequestedYMM, XSteps: p.XSteps, YSteps: p.YSteps, CalculatedXMm: p.CalculatedXMM, CalculatedYMm: p.CalculatedYMM, PositionBasis: p.PositionBasis, ConfigurationJson: p.Configuration}
}
