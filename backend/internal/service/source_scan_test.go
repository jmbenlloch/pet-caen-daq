package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1/daqv1connect"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/sourcestage"
	"google.golang.org/protobuf/proto"
)

func sourceConfig() *daqv1.SourceScanConfiguration {
	return &daqv1.SourceScanConfiguration{XMinMm: -1, XMaxMm: 1, YMinMm: -1, YMaxMm: 1, PitchMm: 1, MmPerTurn: 0.5, FullStepsPerTurn: 200, Microsteps: 1, PulseHighUs: 20, PulseLowUs: 3000, DwellMilliseconds: 1, PointTimeoutSeconds: 10}
}

func TestSourcePlan(t *testing.T) {
	c := sourceConfig()
	points, err := sourcePlan(c)
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]int32{{-400, 400}, {0, 400}, {400, 400}, {400, 0}, {0, 0}, {-400, 0}, {-400, -400}, {0, -400}, {400, -400}}
	for i, p := range points {
		if p.XSteps != want[i][0] || p.YSteps != want[i][1] || p.Index != uint32(i) {
			t.Fatalf("point %d = %v", i, p)
		}
	}
	for _, change := range []func(*daqv1.SourceScanConfiguration){
		func(c *daqv1.SourceScanConfiguration) { c.PitchMm = 0 },
		func(c *daqv1.SourceScanConfiguration) { c.PitchMm = 0.3 },
		func(c *daqv1.SourceScanConfiguration) { c.Microsteps = 0 },
		func(c *daqv1.SourceScanConfiguration) { c.MmPerTurn = 1e-310 },
		func(c *daqv1.SourceScanConfiguration) { c.EventCount = 1 },
		func(c *daqv1.SourceScanConfiguration) { c.PointTimeoutSeconds = 1 },
		func(c *daqv1.SourceScanConfiguration) { c.PitchMm = 0.00001 },
		func(c *daqv1.SourceScanConfiguration) { c.XMaxMm = 1e20 },
	} {
		bad := proto.Clone(c).(*daqv1.SourceScanConfiguration)
		change(bad)
		if _, err := sourcePlan(bad); err == nil {
			t.Fatalf("accepted invalid config %v", bad)
		}
	}
	c.XMinMm = 0.001
	c.XMaxMm = 0.001
	c.YMinMm = 0
	c.YMaxMm = 0
	points, err = sourcePlan(c)
	if err != nil {
		t.Fatal(err)
	}
	if points[0].RequestedXMm != 0.001 || points[0].CalculatedXMm != 0 || points[0].XSteps != 0 {
		t.Fatal(points[0])
	}
}

type testStage struct {
	exchange func(context.Context, string, string) error
}

func (d *testStage) Exchange(ctx context.Context, c, e string) error { return d.exchange(ctx, c, e) }
func (*testStage) Close() error                                      { return nil }

func sourceTestService(t *testing.T) (*SourceScanService, *fakeRunController) {
	controller := &fakeRunController{state: acquisition.StateReady, pipeline: &serviceHealthPipeline{}}
	runs := newRunService(t, controller)
	return &SourceScanService{Runs: runs, Endpoint: "tcp://test", Parent: t.TempDir()}, controller
}

func startSource(t *testing.T, s *SourceScanService, c *daqv1.SourceScanConfiguration) *daqv1.SourceScan {
	t.Helper()
	response, err := s.StartSourceScan(context.Background(), connect.NewRequest(&daqv1.StartSourceScanRequest{Configuration: c, Run: &daqv1.StartRunRequest{RequestedBy: "operator", JanusConfiguration: validTopology}, OriginConfirmed: true}))
	if err != nil {
		t.Fatal(err)
	}
	return response.Msg.Scan
}
func awaitSource(t *testing.T, s *SourceScanService) *daqv1.SourceScan {
	t.Helper()
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		s.Close()
		t.Fatal("scan timeout")
	}
	response, err := s.GetSourceScan(context.Background(), connect.NewRequest(&daqv1.GetSourceScanRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	return response.Msg.Scan
}

func TestSourceScanRunsAndMetadata(t *testing.T) {
	for _, events := range []bool{false, true} {
		t.Run(fmt.Sprint(events), func(t *testing.T) {
			s, controller := sourceTestService(t)
			controller.pipeline.(*serviceHealthPipeline).events.Store(5)
			moves := 0
			s.Open = func(context.Context, string) (sourcestage.Device, error) {
				return &testStage{exchange: func(_ context.Context, c, _ string) error {
					if strings.HasPrefix(c, "MOVE ") {
						if controller.active != "" {
							return errors.New("motion before run drained")
						}
						moves++
					}
					return nil
				}}, nil
			}
			c := sourceConfig()
			if events {
				c.DwellMilliseconds = 0
				c.EventCount = 5
			}
			initial := startSource(t, s, c)
			final := awaitSource(t, s)
			if final.State != "completed" || final.CompletedPoints != 9 || moves != 9 {
				t.Fatalf("scan %v moves %d", final, moves)
			}
			if initial.State != "preparing" {
				t.Fatal("response aliased mutable state")
			}
			for _, p := range final.Points {
				if p.RunId == "" || !p.Completed {
					t.Fatal(p)
				}
			}
			metadata := controller.options.SourcePosition
			if metadata == nil || metadata.ScanID != final.ScanId || metadata.XSteps != 400 || metadata.Row != 2 || metadata.Configuration == "" {
				t.Fatal(metadata)
			}
			// A new service can load durable scan/point/run relationships after restart.
			restarted := &SourceScanService{Parent: s.Parent}
			stored, err := restarted.GetSourceScan(context.Background(), connect.NewRequest(&daqv1.GetSourceScanRequest{ScanId: final.ScanId}))
			if err != nil || !proto.Equal(stored.Msg.Scan, final) {
				t.Fatalf("reload %v %v", stored, err)
			}
		})
	}
}

func TestSourceScanFaultDoesNotAcquire(t *testing.T) {
	s, controller := sourceTestService(t)
	s.Open = func(context.Context, string) (sourcestage.Device, error) {
		return &testStage{exchange: func(_ context.Context, c, _ string) error {
			if strings.HasPrefix(c, "MOVE ") {
				return errors.New("wrong READY coordinates")
			}
			return nil
		}}, nil
	}
	startSource(t, s, sourceConfig())
	final := awaitSource(t, s)
	if final.State != "failed" || final.CompletedPoints != 0 || controller.options.SourcePosition != nil {
		t.Fatal(final)
	}
}

func TestSourceScanCancelAndExclusiveAPI(t *testing.T) {
	s, controller := sourceTestService(t)
	s.Open = func(context.Context, string) (sourcestage.Device, error) {
		return &testStage{exchange: func(context.Context, string, string) error { return nil }}, nil
	}
	c := sourceConfig()
	c.DwellMilliseconds = 0
	c.EventCount = 100
	initial := startSource(t, s, c)
	mux := http.NewServeMux()
	path, handler := daqv1connect.NewRunServiceHandler(s.Runs, connect.WithInterceptors(s.Interceptor()))
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := daqv1connect.NewRunServiceClient(server.Client(), server.URL)
	_, err := client.StartRun(context.Background(), connect.NewRequest(&daqv1.StartRunRequest{RequestedBy: "intruder", JanusConfiguration: validTopology}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("interleaved start: %v", err)
	}
	// Wait for an actual acquisition to test stop/drain cancellation, not just a
	// cancellation before the asynchronous job starts.
	deadline := time.Now().Add(time.Second)
	for {
		res, _ := s.GetSourceScan(context.Background(), connect.NewRequest(&daqv1.GetSourceScanRequest{}))
		if res.Msg.Scan.State == "acquiring" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no acquisition")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := s.CancelSourceScan(context.Background(), connect.NewRequest(&daqv1.CancelSourceScanRequest{ScanId: initial.ScanId, RequestedBy: "operator"})); err != nil {
		t.Fatal(err)
	}
	final := awaitSource(t, s)
	if final.State != "cancelled" || final.CompletedPoints != 0 || controller.active != "" {
		t.Fatal(final)
	}
}

func TestSourceScanPointTimeout(t *testing.T) {
	s, controller := sourceTestService(t)
	s.Open = func(context.Context, string) (sourcestage.Device, error) {
		return &testStage{exchange: func(context.Context, string, string) error { return nil }}, nil
	}
	c := sourceConfig()
	c.XMinMm = 0
	c.XMaxMm = 0
	c.YMinMm = 0
	c.YMaxMm = 0
	c.EventCount = 1
	c.DwellMilliseconds = 0
	c.PointTimeoutSeconds = 1
	startSource(t, s, c)
	final := awaitSource(t, s)
	if final.State != "failed" || controller.active != "" || !strings.Contains(final.Error, "deadline") {
		t.Fatal(final)
	}
}
