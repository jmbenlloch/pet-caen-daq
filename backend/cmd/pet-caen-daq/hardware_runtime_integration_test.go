//go:build integration

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/simulator"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

// TestRuntimeForcedConfigureRewritesAndRetargetsScans drives the production
// hardware runtime against the simulator: a forced configuration rewrites
// registers even when the document is unchanged, while the reusing path does
// not, and both scan coordinators restore the newly applied plan.
func TestRuntimeForcedConfigureRewritesAndRetargetsScans(t *testing.T) {
	server, err := simulator.Start("127.0.0.1:0", "127.0.0.1:0", simulator.ProductionTopology())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.EnablePeriodicEvents(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := telemetry.NewPublisher("runtime-test", &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_DISCONNECTED}, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &hardwareRuntime{
		controlAddress: server.ControlAddress(), streamAddress: server.StreamAddress(),
		runParent: t.TempDir(), capacity: 16, drainTimeout: time.Second,
		publisher: publisher, output: io.Discard,
	}
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := runtime.Connect(ctx, "integration", string(fixture)); err != nil {
		t.Fatal(err)
	}
	if source := publisher.Snapshot().GetAppliedConfiguration().GetSource(); source != daqv1.AppliedConfigurationSource_APPLIED_CONFIGURATION_SOURCE_CONNECT {
		t.Fatalf("applied configuration source after connect = %v", source)
	}
	connection := runtime.connections[0]
	chain, node := uint16(connection.Chain), uint16(connection.Node)
	threshold := func() uint32 {
		t.Helper()
		value, err := runtime.client.ReadRegister(ctx, chain, node, uint32(dt5202.TimeCoarseThreshold))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	tamper := func() {
		t.Helper()
		if err := runtime.client.WriteRegister(ctx, chain, node, uint32(dt5202.TimeCoarseThreshold), 5); err != nil {
			t.Fatal(err)
		}
	}
	if got := threshold(); got != 181 {
		t.Fatalf("connected threshold = %d, want 181", got)
	}

	text := strings.Replace(string(fixture), "TD_CoarseThreshold[0]              181", "TD_CoarseThreshold[0]              321", 1)
	document, err := janusconfig.Parse(bytes.NewBufferString(text))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ConfigureForced(ctx, document, "integration"); err != nil {
		t.Fatal(err)
	}
	if got := threshold(); got != 321 {
		t.Fatalf("applied threshold = %d, want 321", got)
	}

	// Something outside the DAQ changes the board. The reusing path trusts
	// its cache; the forced path puts the board back in a known state.
	tamper()
	if _, err := runtime.Configure(ctx, document, "integration"); err != nil {
		t.Fatal(err)
	}
	if got := threshold(); got != 5 {
		t.Fatalf("reusing configure wrote the board: threshold = %d", got)
	}
	if _, err := runtime.ConfigureForced(ctx, document, "integration"); err != nil {
		t.Fatal(err)
	}
	if got := threshold(); got != 321 {
		t.Fatalf("second forced apply did not rewrite: threshold = %d, want 321", got)
	}

	restored, err := runtime.Run(ctx, staircase.Request{Board: uint32(connection.Board), Minimum: 100, Maximum: 110, Step: 10, Dwell: time.Millisecond}, "integration", func(staircase.Point) error { return nil })
	if err != nil || !restored {
		t.Fatalf("staircase restored=%t error=%v", restored, err)
	}
	if got := threshold(); got != 321 {
		t.Fatalf("staircase restored threshold %d, want the applied 321", got)
	}
	restored, err = runtime.RunHoldDelay(ctx, holddelay.Request{
		Board: uint32(connection.Board), MinimumDelayNS: 0, MaximumDelayNS: 8, StepNS: 8, EventsPerDelay: 10, PointTimeout: 5 * time.Second,
	}, "integration", func(holddelay.Point) error { return nil })
	if err != nil || !restored {
		t.Fatalf("hold-delay restored=%t error=%v", restored, err)
	}
	if got := threshold(); got != 321 {
		t.Fatalf("hold-delay restored threshold %d, want the applied 321", got)
	}
}
