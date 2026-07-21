package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1/daqv1connect"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/service"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("pet-caen-daq", flag.ContinueOnError)
	flags.SetOutput(output)
	configurationPath := flags.String("config", "", "path to a JANUS configuration")
	controlAddress := flags.String("control", "172.16.0.11:9760", "DT5215 control address")
	streamAddress := flags.String("stream", "172.16.0.11:9000", "DT5215 stream address")
	listenAddress := flags.String("listen", "127.0.0.1:8080", "ConnectRPC HTTP listen address")
	runParent := flags.String("runs", "./runs", "parent directory for run artifacts")
	pipelineCapacity := flags.Int("pipeline-capacity", 32, "bounded stream-batch queue capacity")
	drainTimeout := flags.Duration("drain-timeout", 5*time.Second, "maximum orderly stop-and-drain duration")
	authorizeHV := flags.Bool("authorize-hv-config", false, "explicitly authorize applying configured DT5202 HV peripheral setpoints")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *configurationPath == "" {
		return fmt.Errorf("-config is required")
	}
	configuration, err := os.ReadFile(*configurationPath)
	if err != nil {
		return fmt.Errorf("read configuration: %w", err)
	}
	document, err := janusconfig.Parse(bytes.NewReader(configuration))
	if err != nil {
		return err
	}
	if _, err := document.Classify(); err != nil {
		return err
	}
	connections, err := document.Connections()
	if err != nil {
		return err
	}
	if err := janusconfig.ValidateProductionTopology(connections); err != nil {
		return err
	}
	if *pipelineCapacity <= 0 || *drainTimeout <= 0 {
		return fmt.Errorf("pipeline capacity and drain timeout must be positive")
	}
	if err := os.MkdirAll(*runParent, 0o750); err != nil {
		return fmt.Errorf("create run storage parent: %w", err)
	}

	discoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	client, err := dt5215.Dial(discoveryCtx, *controlAddress, *streamAddress)
	if err != nil {
		cancel()
		return err
	}
	topology, err := client.DiscoverProductionTopology(discoveryCtx, connections)
	cancel()
	if err != nil {
		client.Close()
		return err
	}
	defer client.Close()

	states, _ := acquisition.NewStateMachine(acquisition.StateIdle, nil)
	factory := runpipeline.Factory{Options: runpipeline.Options{
		Parent: *runParent, Capacity: *pipelineCapacity, Backpressure: acquisition.BackpressureBlock,
	}}
	coordinator, err := acquisition.NewCoordinator(states, client, factory.New, 4, *drainTimeout)
	if err != nil {
		return err
	}
	publisher, err := telemetry.NewPublisher(instanceID(), topologySnapshot(topology), nil)
	if err != nil {
		return err
	}
	coordinator.SetFaultObserver(func(fault error) { service.PublishCoordinatorFault(publisher, fault, nil) })
	incomplete, err := runstore.FindIncomplete(*runParent)
	if err != nil {
		return err
	}
	service.PublishRecoveryDiagnostics(publisher, incomplete, time.Now())
	configurator, err := acquisition.NewConfigurator(states, client, service.ConfigurationProgressPublisher(publisher, states, nil))
	if err != nil {
		return err
	}
	targets := make([]acquisition.ConfigurationTarget, 0, len(connections))
	for _, connection := range connections {
		targets = append(targets, acquisition.ConfigurationTarget{Board: connection.Board, Chain: uint16(connection.Chain), Node: uint16(connection.Node)})
	}
	configurationCtx, cancelConfiguration := context.WithTimeout(ctx, 30*time.Second)
	_, err = configurator.Configure(configurationCtx, document, targets, acquisition.ConfigureOptions{Actor: "backend_startup", Hard: true, AuthorizeHV: *authorizeHV})
	cancelConfiguration()
	if err != nil {
		return fmt.Errorf("apply startup configuration: %w", err)
	}

	systemService := &service.SystemService{Source: publisher}
	boards := make([]configaudit.BoardEvidence, 0, len(topology.Boards))
	for _, board := range topology.Boards {
		boards = append(boards, configaudit.BoardEvidence{Board: int(board.Chain), FirmwareRevision: board.FirmwareRevision})
	}
	runService := &service.RunService{
		Controller: coordinator, Telemetry: publisher, Boards: boards,
		Configure: func(configureCtx context.Context, runDocument *janusconfig.Document, actor string) (acquisition.ConfigurationResult, error) {
			return configurator.Configure(configureCtx, runDocument, targets, acquisition.ConfigureOptions{Actor: actor, Hard: true, AuthorizeHV: *authorizeHV})
		},
	}
	mux := http.NewServeMux()
	systemPath, systemHandler := daqv1connect.NewSystemServiceHandler(systemService)
	runPath, runHandler := daqv1connect.NewRunServiceHandler(runService)
	mux.Handle(systemPath, systemHandler)
	mux.Handle(runPath, runHandler)
	server := &http.Server{Addr: *listenAddress, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	serverCtx, stopServer := context.WithCancel(ctx)
	defer stopServer()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-serverCtx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(output, "PET CAEN DAQ instance=%s listen=%s state=ready hv_authorized=%t\n", publisher.Snapshot().GetInstanceId(), *listenAddress, *authorizeHV)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	stopServer()
	<-shutdownDone
	return err
}

func topologySnapshot(topology dt5215.Topology) *daqv1.TelemetrySnapshot {
	snapshot := &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_IDLE}
	boards := make(map[uint16][]*daqv1.Board)
	for _, board := range topology.Boards {
		boards[board.Chain] = append(boards[board.Chain], &daqv1.Board{
			Node: uint32(board.Node), ProductId: board.ProductID, FpgaFirmware: board.FirmwareRevision, Health: daqv1.HealthStatus_HEALTH_STATUS_OK,
		})
	}
	for index, chain := range topology.Chains {
		enabled := chain.Status != 0
		health := daqv1.HealthStatus_HEALTH_STATUS_UNKNOWN
		if enabled {
			health = daqv1.HealthStatus_HEALTH_STATUS_OK
		}
		snapshot.Chains = append(snapshot.Chains, &daqv1.Chain{Index: uint32(index), Enabled: enabled, Health: health, Boards: boards[uint16(index)]})
	}
	return snapshot
}

func instanceID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return host + "-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}
