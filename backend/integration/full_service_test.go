//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1/daqv1connect"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/rawcapture"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/service"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/simulator"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

func TestGeneratedClientsCompleteSimulatedRunAndInspectArtifacts(t *testing.T) {
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

	states, _ := acquisition.NewStateMachine(acquisition.StateIdle, nil)
	publisher, _ := telemetry.NewPublisher("full-service", integrationTopologySnapshot(topology), nil)
	configurator, err := acquisition.NewConfigurator(states, hardware, service.ConfigurationProgressPublisher(publisher, states, nil))
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]acquisition.ConfigurationTarget, 0, len(connections))
	boards := make([]configaudit.BoardEvidence, 0, len(topology.Boards))
	for _, connection := range connections {
		targets = append(targets, acquisition.ConfigurationTarget{Board: connection.Board, Chain: uint16(connection.Chain), Node: uint16(connection.Node)})
	}
	for _, board := range topology.Boards {
		boards = append(boards, configaudit.BoardEvidence{Board: int(board.Chain), FirmwareRevision: board.FirmwareRevision})
	}
	parent := t.TempDir()
	factory := runpipeline.Factory{Options: runpipeline.Options{Parent: parent, Capacity: 16, Backpressure: acquisition.BackpressureBlock}}
	coordinator, err := acquisition.NewCoordinator(states, hardware, factory.New, 4, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	coordinator.SetFaultObserver(func(fault error) { service.PublishCoordinatorFault(publisher, fault, nil) })
	runService := &service.RunService{
		Controller: coordinator, Telemetry: publisher, Boards: boards, HealthInterval: 5 * time.Millisecond,
		Configure: func(ctx context.Context, requested *janusconfig.Document, actor string) (acquisition.ConfigurationResult, error) {
			return configurator.Configure(ctx, requested, targets, acquisition.ConfigureOptions{Actor: actor, Hard: true})
		},
	}
	mux := http.NewServeMux()
	systemPath, systemHandler := daqv1connect.NewSystemServiceHandler(&service.SystemService{Source: publisher})
	runPath, runHandler := daqv1connect.NewRunServiceHandler(runService)
	mux.Handle(systemPath, systemHandler)
	mux.Handle(runPath, runHandler)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	systemClient := daqv1connect.NewSystemServiceClient(httpServer.Client(), httpServer.URL)
	runClient := daqv1connect.NewRunServiceClient(httpServer.Client(), httpServer.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	validation, err := systemClient.ValidateConfiguration(ctx, connect.NewRequest(&daqv1.ValidateConfigurationRequest{JanusConfiguration: string(configuration)}))
	if err != nil || !validation.Msg.GetValid() || len(validation.Msg.GetIssues()) != 0 {
		t.Fatalf("validation=%+v error=%v", validation, err)
	}
	stream, err := systemClient.StreamTelemetry(ctx, connect.NewRequest(&daqv1.StreamTelemetryRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if !stream.Receive() {
		t.Fatalf("initial telemetry: %v", stream.Err())
	}
	start, err := runClient.StartRun(ctx, connect.NewRequest(&daqv1.StartRunRequest{
		RunId: "generated-client", RequestedBy: "integration", JanusConfiguration: string(configuration), CaptureRaw: true, JournalTransport: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if start.Msg.GetSnapshot().GetState() != daqv1.SystemState_SYSTEM_STATE_RUNNING {
		t.Fatalf("start=%+v", start.Msg)
	}
	if err := hardware.SendCommand(ctx, 0xff, 0xff, dt5215.CommandTestPulse, 0); err != nil {
		t.Fatal(err)
	}
	seenLive := false
	for stream.Receive() {
		snapshot := stream.Msg().GetSnapshot()
		if snapshot.GetState() == daqv1.SystemState_SYSTEM_STATE_RUNNING && snapshot.GetPipeline().GetDecodedEvents() >= 4 && snapshot.GetCurrentRun().GetEventCount() >= 4 {
			seenLive = true
			break
		}
	}
	if !seenLive {
		t.Fatalf("live telemetry not observed: %v", stream.Err())
	}
	stop, err := runClient.StopRun(ctx, connect.NewRequest(&daqv1.StopRunRequest{RunId: "generated-client", RequestedBy: "integration"}))
	if err != nil {
		t.Fatal(err)
	}
	if stop.Msg.GetSnapshot().GetState() != daqv1.SystemState_SYSTEM_STATE_READY || stop.Msg.GetRun().GetIncomplete() || stop.Msg.GetRun().GetEventCount() < 4 || stop.Msg.GetRun().GetRawBatchCount() < 4 || len(stop.Msg.GetRun().GetArtifacts()) != 3 {
		t.Fatalf("stop=%+v", stop.Msg)
	}

	directory := filepath.Join(parent, "run-generated-client")
	manifestData, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest runstore.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.RequestedConfiguration != string(configuration) || manifest.ConfigurationAudit == nil || !manifest.ConfigurationAudit.Valid || len(manifest.EffectiveConfiguration) != 4 || len(manifest.Artifacts) != 3 {
		t.Fatalf("manifest=%+v", manifest)
	}
	events, err := os.Open(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(events)
	lines := 0
	for scanner.Scan() {
		lines++
	}
	_ = events.Close()
	if err := scanner.Err(); err != nil || lines < 4 {
		t.Fatalf("JSONL lines=%d error=%v", lines, err)
	}
	rawFile, err := os.Open(filepath.Join(directory, "wire.raw"))
	if err != nil {
		t.Fatal(err)
	}
	rawReader, err := rawcapture.NewReader(rawFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawReader.Next(); err != nil {
		t.Fatal(err)
	}
	_ = rawFile.Close()
	for _, artifact := range stop.Msg.GetRun().GetArtifacts() {
		data, err := os.ReadFile(filepath.Join(directory, artifact.GetName()))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if artifact.GetSizeBytes() != uint64(len(data)) || artifact.GetSha256() != fmt.Sprintf("%x", digest) {
			t.Fatalf("artifact=%+v bytes=%d digest=%x", artifact, len(data), digest)
		}
	}
}

func integrationTopologySnapshot(topology dt5215.Topology) *daqv1.TelemetrySnapshot {
	snapshot := &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_IDLE}
	for chainIndex, chain := range topology.Chains {
		entry := &daqv1.Chain{Index: uint32(chainIndex), Enabled: chain.Status != 0, Health: daqv1.HealthStatus_HEALTH_STATUS_UNKNOWN}
		if entry.Enabled {
			entry.Health = daqv1.HealthStatus_HEALTH_STATUS_OK
		}
		for _, board := range topology.Boards {
			if int(board.Chain) == chainIndex {
				entry.Boards = append(entry.Boards, &daqv1.Board{Node: uint32(board.Node), ProductId: board.ProductID, FpgaFirmware: board.FirmwareRevision, Health: daqv1.HealthStatus_HEALTH_STATUS_OK})
			}
		}
		snapshot.Chains = append(snapshot.Chains, entry)
	}
	return snapshot
}
