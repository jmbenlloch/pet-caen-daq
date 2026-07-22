package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1/daqv1connect"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runcatalog"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/service"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/webui"
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
	frontendDirectory := flags.String("frontend-dir", "", "optional built frontend directory to serve on the same HTTP origin")
	runParent := flags.String("runs", "./runs", "parent directory for run artifacts")
	catalogPath := flags.String("catalog", "", "SQLite run catalog path (default: <runs>/catalog.sqlite3)")
	pipelineCapacity := flags.Int("pipeline-capacity", 32, "bounded stream-batch queue capacity")
	drainTimeout := flags.Duration("drain-timeout", 5*time.Second, "maximum orderly stop-and-drain duration")
	authorizeHV := flags.Bool("authorize-hv-config", false, "explicitly authorize applying configured DT5202 HV peripheral setpoints")
	inspectOnly := flags.Bool("inspect-only", false, "read and validate an already-ready topology, then exit without hardware writes")
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
	var listener net.Listener
	if !*inspectOnly {
		listener, err = listenHTTP(*listenAddress)
		if err != nil {
			return err
		}
		defer listener.Close()
		if err := os.MkdirAll(*runParent, 0o750); err != nil {
			return fmt.Errorf("create run storage parent: %w", err)
		}
	}
	if *catalogPath == "" {
		*catalogPath = filepath.Join(*runParent, "catalog.sqlite3")
	}
	// Real DT5215 link reset, four-chain enumeration, and synchronization take
	// about 36 seconds in capture-verified production hardware.
	discoveryCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	client, err := dt5215.Dial(discoveryCtx, *controlAddress, *streamAddress)
	if err != nil {
		cancel()
		return err
	}
	var topology dt5215.Topology
	if *inspectOnly {
		topology, err = client.InspectProductionTopology(discoveryCtx, connections)
	} else {
		topology, err = client.DiscoverProductionTopology(discoveryCtx, connections)
	}
	catalog, catalogErr := runcatalog.Open(*catalogPath)
	if catalogErr != nil {
		fmt.Fprintf(output, "run catalog unavailable path=%s error=%v\n", *catalogPath, catalogErr)
	} else {
		defer catalog.Close()
		report, reconcileErr := catalog.Reconcile(ctx, *runParent)
		if reconcileErr != nil {
			fmt.Fprintf(output, "run catalog reconciliation failed path=%s error=%v\n", *catalogPath, reconcileErr)
		} else {
			fmt.Fprintf(output, "run catalog reconciled indexed=%d unchanged=%d unavailable=%d problems=%d\n", report.Indexed, report.Unchanged, report.MarkedUnavailable, len(report.Problems))
			for _, problem := range report.Problems {
				fmt.Fprintf(output, "run catalog problem run_id=%s error=%s\n", problem.RunID, problem.Error)
			}
		}
	}
	cancel()
	if err != nil {
		client.Close()
		return err
	}
	printDiscoveredDevices(output, topology)
	defer client.Close()
	if *inspectOnly {
		fmt.Fprintln(output, "inspection complete mode=read-only hardware_writes=0")
		return nil
	}

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
	coordinator.SetFaultObserver(func(fault error) {
		service.PublishCoordinatorFault(publisher, fault, nil)
		fmt.Fprintf(output, "acquisition fault: %v\n", fault)
	})
	recoveryBoards := make([]acquisition.RecoveryBoard, 0, len(topology.Boards))
	for _, board := range topology.Boards {
		recoveryBoards = append(recoveryBoards, acquisition.RecoveryBoard{Chain: board.Chain, Node: board.Node, Status: board.AcquisitionState})
	}
	recoveryResult, recoveryErr := acquisition.RecoverStartup(ctx, states, client, recoveryBoards, 4, *drainTimeout, "backend_restart")
	service.PublishStartupRecovery(publisher, recoveryResult, recoveryErr, time.Now())
	if recoveryErr != nil {
		return fmt.Errorf("recover hardware after restart: %w", recoveryErr)
	}
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

	hvTargets := make([]service.HVTarget, 0, len(targets))
	for _, target := range targets {
		hvTargets = append(hvTargets, service.HVTarget{Board: target.Board, Chain: target.Chain, Node: target.Node})
	}
	hvController := &service.NativeHVController{
		Hardware: client, States: states, Publisher: publisher, Targets: hvTargets, Authorized: *authorizeHV,
	}
	systemService := &service.SystemService{Source: publisher, ConfigurationTemplate: string(configuration), HV: hvController}
	hvMonitorCtx, stopHVMonitor := context.WithCancel(ctx)
	defer stopHVMonitor()
	go func() {
		if monitorErr := hvController.Run(hvMonitorCtx, time.Second); monitorErr != nil {
			fmt.Fprintf(output, "HV monitor stopped: %v\n", monitorErr)
		}
	}()
	boards := make([]configaudit.BoardEvidence, 0, len(topology.Boards))
	for _, board := range topology.Boards {
		boards = append(boards, configaudit.BoardEvidence{Board: int(board.Chain), FirmwareRevision: board.FirmwareRevision})
	}
	runService := &service.RunService{
		Controller: coordinator, Telemetry: publisher, Boards: boards,
		RunExists: func(runID string) (bool, error) {
			_, err := os.Stat(filepath.Join(*runParent, "run-"+runID))
			if err == nil {
				return true, nil
			}
			if errors.Is(err, os.ErrNotExist) {
				return false, nil
			}
			return false, err
		},
		RunParent: *runParent,
		Configure: func(configureCtx context.Context, runDocument *janusconfig.Document, actor string) (acquisition.ConfigurationResult, error) {
			return configurator.Configure(configureCtx, runDocument, targets, acquisition.ConfigureOptions{Actor: actor, Hard: true, AuthorizeHV: *authorizeHV})
		},
	}
	if catalog != nil {
		runService.ReconcileCatalog = func(reconcileCtx context.Context, parent string) error {
			_, err := catalog.Reconcile(reconcileCtx, parent)
			return err
		}
		runService.CatalogError = func(err error) {
			fmt.Fprintf(output, "run catalog update failed: %v\n", err)
		}
	}
	mux := http.NewServeMux()
	systemPath, systemHandler := daqv1connect.NewSystemServiceHandler(systemService)
	runPath, runHandler := daqv1connect.NewRunServiceHandler(runService)
	mux.Handle(systemPath, systemHandler)
	mux.Handle(runPath, runHandler)
	if *frontendDirectory != "" {
		frontendHandler, frontendErr := webui.New(*frontendDirectory)
		if frontendErr != nil {
			return frontendErr
		}
		mux.Handle("/", frontendHandler)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

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
	fmt.Fprintf(output, "PET CAEN DAQ instance=%s listen=%s state=ready hv_authorized=%t frontend=%t\n", publisher.Snapshot().GetInstanceId(), listener.Addr(), *authorizeHV, *frontendDirectory != "")
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	stopServer()
	<-shutdownDone
	return err
}

func printDiscoveredDevices(output io.Writer, topology dt5215.Topology) {
	fmt.Fprintf(output, "devices found=%d\n", len(topology.Boards))
	for _, board := range topology.Boards {
		fmt.Fprintf(output, "device chain=%d node=%d product_id=%d fpga_firmware=%#08x acquisition_status=%#08x\n",
			board.Chain, board.Node, board.ProductID, board.FirmwareRevision, board.AcquisitionState)
	}
}

func listenHTTP(address string) (net.Listener, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("bind API listener %s: %w; stop the process using that address or select another one with -listen, for example -listen 127.0.0.1:8081", address, err)
	}
	return listener, nil
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
