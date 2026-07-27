package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
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
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runcatalog"
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
	if *inspectOnly {
		discoveryCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		client, dialErr := dt5215.Dial(discoveryCtx, *controlAddress, *streamAddress)
		if dialErr != nil {
			return dialErr
		}
		defer client.Close()
		topology, inspectErr := client.InspectProductionTopology(discoveryCtx, connections)
		if inspectErr != nil {
			return inspectErr
		}
		printDiscoveredDevices(output, topology)
		fmt.Fprintln(output, "inspection complete mode=read-only hardware_writes=0")
		return nil
	}

	catalog, catalogErr := runcatalog.Open(*catalogPath)
	if catalogErr != nil {
		fmt.Fprintf(output, "run catalog unavailable path=%s error=%v\n", *catalogPath, catalogErr)
	} else {
		defer catalog.Close()
		fmt.Fprintf(output, "run catalog opened path=%s reconciliation=manual\n", *catalogPath)
	}
	publisher, err := telemetry.NewPublisher(instanceID(), &daqv1.TelemetrySnapshot{State: daqv1.SystemState_SYSTEM_STATE_DISCONNECTED}, nil)
	if err != nil {
		return err
	}
	incomplete, err := runstore.FindIncomplete(*runParent)
	if err != nil {
		return err
	}
	service.PublishRecoveryDiagnostics(publisher, incomplete, time.Now())
	runtime := &hardwareRuntime{
		controlAddress: *controlAddress, streamAddress: *streamAddress,
		document: document, connections: connections, runParent: *runParent,
		capacity: *pipelineCapacity, drainTimeout: *drainTimeout, authorizeHV: *authorizeHV,
		publisher: publisher, output: output,
	}
	systemService := &service.SystemService{Source: publisher, ConfigurationTemplate: string(configuration), HV: runtime, Hardware: runtime, Discovery: runtime}
	runService := &service.RunService{
		Controller: runtime, Telemetry: publisher, HardwareMetadata: runtime, RunParent: *runParent,
		Configure: runtime.Configure,
	}
	scanService := &service.ScanService{
		Controller:          runtime,
		HoldDelayController: runtime,
		Telemetry:           publisher,
		ScanParent:          *runParent,
	}
	if catalog != nil {
		runService.Catalog = catalog
		runService.AllocateRunID = catalog.AllocateRunID
		scanService.AllocateRunID = catalog.AllocateRunID
		runService.IndexRun = catalog.IndexRun
		runService.CatalogError = func(err error) {
			fmt.Fprintf(output, "run catalog update failed: %v\n", err)
		}
	}
	defer runtime.Close()
	mux := http.NewServeMux()
	systemPath, systemHandler := daqv1connect.NewSystemServiceHandler(systemService)
	runPath, runHandler := daqv1connect.NewRunServiceHandler(runService)
	scanPath, scanHandler := daqv1connect.NewScanServiceHandler(scanService)
	mux.Handle(systemPath, systemHandler)
	mux.Handle(runPath, runHandler)
	mux.Handle(scanPath, scanHandler)
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
	// Serve the API immediately while the capture-verified discovery sequence
	// runs in the background. Failure is non-fatal and can be retried via RPC.
	go func() {
		if connectErr := runtime.Connect(ctx, "backend_startup"); connectErr != nil {
			fmt.Fprintf(output, "initial hardware connection failed: %v\n", connectErr)
		}
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	stopServer()
	<-shutdownDone
	return err
}

func executionIdentity(topology dt5215.Topology, connections []janusconfig.Connection, controlAddress, streamAddress string) runstore.ExecutionIdentity {
	boardNumbers := make(map[[2]uint16]int, len(connections))
	for _, connection := range connections {
		boardNumbers[[2]uint16{uint16(connection.Chain), uint16(connection.Node)}] = connection.Board
	}
	boards := make([]runstore.BoardIdentity, 0, len(topology.Boards))
	for _, board := range topology.Boards {
		boards = append(boards, runstore.BoardIdentity{
			Board: boardNumbers[[2]uint16{board.Chain, board.Node}], Chain: board.Chain, Node: board.Node, ProductID: board.ProductID,
			FirmwareRevision: board.FirmwareRevision, AcquisitionState: board.AcquisitionState,
			IdentityEvidence: "hardware-register-read", FirmwareEvidence: "hardware-register-read",
		})
	}
	return runstore.ExecutionIdentity{
		Topology: runstore.TopologyIdentity{
			Concentrator: runstore.ConcentratorIdentity{
				ControlAddress: controlAddress, StreamAddress: streamAddress,
				IdentityEvidence: "unknown-not-queried", FirmwareRevisionEvidence: "unknown-not-queried",
			},
			Boards: boards,
		},
		Software: runstore.CurrentSoftwareIdentity(),
	}
}

func printDiscoveredDevices(output io.Writer, topology dt5215.Topology) {
	fmt.Fprintf(output, "concentrator product_id=%d software_revision=%s fpga_revision=%s\n",
		topology.Concentrator.ProductID, topology.Concentrator.SoftwareRevision, topology.Concentrator.FPGARevision)
	fmt.Fprintf(output, "devices found=%d\n", len(topology.Boards))
	for _, board := range topology.Boards {
		hvFirmware := "unavailable"
		if board.HVModuleFirmwareAvailable {
			hvFirmware = strconv.FormatFloat(float64(board.HVModuleFirmwareVersion), 'f', 1, 32)
		}
		fmt.Fprintf(output, "device chain=%d node=%d product_id=%d fpga_firmware=%#08x hv_module_firmware=%s hv_module_firmware_raw=%#08x acquisition_status=%#08x\n",
			board.Chain, board.Node, board.ProductID, board.FirmwareRevision, hvFirmware, board.HVModuleFirmwareRaw, board.AcquisitionState)
	}
}

func readHVModuleFirmware(ctx context.Context, hardware dt5202.HVHardware, topology *dt5215.Topology, output io.Writer) {
	for index := range topology.Boards {
		board := &topology.Boards[index]
		raw, version, err := dt5202.ReadHVModuleFirmware(ctx, hardware, board.Chain, board.Node)
		board.HVModuleFirmwareRaw = raw
		if err != nil {
			fmt.Fprintf(output, "device warning chain=%d node=%d hv_module_firmware=unavailable error=%v\n", board.Chain, board.Node, err)
			continue
		}
		if math.IsNaN(float64(version)) {
			fmt.Fprintf(output, "device warning chain=%d node=%d hv_module_firmware=unavailable raw=%#08x\n", board.Chain, board.Node, raw)
			continue
		}
		board.HVModuleFirmwareVersion = version
		board.HVModuleFirmwareAvailable = true
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
	snapshot := &daqv1.TelemetrySnapshot{
		State: daqv1.SystemState_SYSTEM_STATE_IDLE,
		Concentrator: &daqv1.Concentrator{
			SoftwareRevision: topology.Concentrator.SoftwareRevision,
			FpgaRevision:     topology.Concentrator.FPGARevision,
			ProductId:        topology.Concentrator.ProductID,
		},
	}
	boards := make(map[uint16][]*daqv1.Board)
	for _, board := range topology.Boards {
		boards[board.Chain] = append(boards[board.Chain], &daqv1.Board{
			Node: uint32(board.Node), ProductId: board.ProductID, FpgaFirmware: board.FirmwareRevision,
			HvModuleFirmwareRaw: board.HVModuleFirmwareRaw, HvModuleFirmwareVersion: board.HVModuleFirmwareVersion,
			HvModuleFirmwareAvailable: board.HVModuleFirmwareAvailable, Health: daqv1.HealthStatus_HEALTH_STATUS_OK,
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
