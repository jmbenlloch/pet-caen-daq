//go:build integration

package integration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/service"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/simulator"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

// TestApplyConfigurationRetargetsScansWithoutStartingRun wires the run
// service the way the backend command does: every successful Configure
// replaces the plan that staircase scans restore.
func TestApplyConfigurationRetargetsScansWithoutStartingRun(t *testing.T) {
	simulatorServer, err := simulator.Start("127.0.0.1:0", "127.0.0.1:0", simulator.ProductionTopology())
	if err != nil {
		t.Fatal(err)
	}
	defer simulatorServer.Close()
	configuration, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	document, err := janusconfig.Parse(bytes.NewReader(configuration))
	if err != nil {
		t.Fatal(err)
	}
	connections, err := document.Connections()
	if err != nil {
		t.Fatal(err)
	}
	hardware, err := dt5215.Dial(context.Background(), simulatorServer.ControlAddress(), simulatorServer.StreamAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer hardware.Close()
	topology, err := hardware.DiscoverProductionTopology(context.Background(), connections)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	states, _ := acquisition.NewStateMachine(acquisition.StateIdle, nil)
	publisher, _ := telemetry.NewPublisher("apply-configuration", integrationTopologySnapshot(topology), nil)
	configurator, err := acquisition.NewConfigurator(states, hardware, service.ConfigurationProgressPublisher(publisher, states, nil))
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]acquisition.ConfigurationTarget, 0, len(connections))
	for _, connection := range connections {
		targets = append(targets, acquisition.ConfigurationTarget{Board: connection.Board, Chain: uint16(connection.Chain), Node: uint16(connection.Node)})
	}
	boards := make([]configaudit.BoardEvidence, 0, len(topology.Boards))
	for _, board := range topology.Boards {
		boards = append(boards, configaudit.BoardEvidence{Board: int(board.Chain), FirmwareRevision: board.FirmwareRevision})
	}
	scanTargets := func(plans []dt5202.ConfigurationPlan) []acquisition.StaircaseTarget {
		result := make([]acquisition.StaircaseTarget, 0, len(plans))
		for _, plan := range plans {
			for _, target := range targets {
				if target.Board == plan.Board {
					result = append(result, acquisition.StaircaseTarget{Board: uint32(plan.Board), Chain: target.Chain, Node: target.Node, Plan: plan})
				}
			}
		}
		return result
	}
	connected, err := configurator.Configure(ctx, document, targets, acquisition.ConfigureOptions{Actor: "integration", Hard: true})
	if err != nil {
		t.Fatal(err)
	}
	staircaseCoordinator, err := acquisition.NewStaircaseCoordinator(states, hardware, scanTargets(connected.Plans))
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	factory := runpipeline.Factory{Options: runpipeline.Options{Parent: parent, Capacity: 16, Backpressure: acquisition.BackpressureBlock}}
	coordinator, err := acquisition.NewCoordinator(states, hardware, factory.New, 4, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	allocations := 0
	runService := &service.RunService{
		Controller: coordinator, Telemetry: publisher, Boards: boards,
		AllocateRunID: func(context.Context) (string, error) { allocations++; return "1", nil },
		Configure: func(ctx context.Context, requested *janusconfig.Document, actor string) (acquisition.ConfigurationResult, error) {
			result, err := configurator.Configure(ctx, requested, targets, acquisition.ConfigureOptions{Actor: actor, Hard: true})
			if err == nil {
				staircaseCoordinator.SetTargets(scanTargets(result.Plans))
			}
			return result, err
		},
	}

	board := targets[0]
	readThreshold := func() uint32 {
		t.Helper()
		value, err := hardware.ReadRegister(ctx, board.Chain, board.Node, uint32(dt5202.TimeCoarseThreshold))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	connectedThreshold := readThreshold()
	applied := strings.Replace(string(configuration), "TD_CoarseThreshold[0]              181", "TD_CoarseThreshold[0]              321", 1)
	if applied == string(configuration) || connectedThreshold == 321 {
		t.Fatalf("fixture does not set the expected board-0 threshold (register %d)", connectedThreshold)
	}
	response, err := runService.ApplyConfiguration(ctx, connect.NewRequest(&daqv1.ApplyConfigurationRequest{RequestedBy: "integration", JanusConfiguration: applied}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetSnapshot().GetState() != daqv1.SystemState_SYSTEM_STATE_READY ||
		response.Msg.GetAppliedConfiguration().GetSource() != daqv1.AppliedConfigurationSource_APPLIED_CONFIGURATION_SOURCE_APPLY ||
		response.Msg.GetAppliedConfiguration().GetBoardCount() != uint32(len(targets)) {
		t.Fatalf("apply response = %+v", response.Msg)
	}
	if state := states.Snapshot().State; state != acquisition.StateReady || coordinator.ActiveRunID() != "" || allocations != 0 {
		t.Fatalf("state=%s active=%q allocations=%d", state, coordinator.ActiveRunID(), allocations)
	}
	if entries, err := os.ReadDir(parent); err != nil || len(entries) != 0 {
		t.Fatalf("run files created: %v error=%v", entries, err)
	}
	if got := readThreshold(); got != 321 {
		t.Fatalf("applied threshold register = %d, want 321", got)
	}

	restored, err := staircaseCoordinator.Run(ctx, staircase.Request{Board: uint32(board.Board), Minimum: 100, Maximum: 110, Step: 10, Dwell: time.Millisecond}, "integration", func(staircase.Point) error { return nil })
	if err != nil || !restored {
		t.Fatalf("staircase restored=%t error=%v", restored, err)
	}
	if got := readThreshold(); got != 321 {
		t.Fatalf("staircase restored threshold %d, want the applied 321 (connect value %d)", got, connectedThreshold)
	}
}
