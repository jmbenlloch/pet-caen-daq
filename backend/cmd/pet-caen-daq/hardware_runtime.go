package main

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/service"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/telemetry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type hardwareRuntime struct {
	opMu sync.Mutex
	mu   sync.RWMutex

	controlAddress string
	streamAddress  string
	document       *janusconfig.Document
	connections    []janusconfig.Connection
	runParent      string
	capacity       int
	drainTimeout   time.Duration
	authorizeHV    bool
	publisher      *telemetry.Publisher
	output         io.Writer

	client        *dt5215.Client
	coordinator   *acquisition.Coordinator
	configurator  *acquisition.Configurator
	hv            *service.NativeHVController
	monitorCancel context.CancelFunc
	topology      dt5215.Topology
}

func (r *hardwareRuntime) Connect(ctx context.Context, actor string) error {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	r.mu.RLock()
	connected := r.client != nil
	r.mu.RUnlock()
	if connected {
		return fmt.Errorf("hardware is already connected")
	}
	r.publisher.Update(func(snapshot *daqv1.TelemetrySnapshot) {
		snapshot.State = daqv1.SystemState_SYSTEM_STATE_CONNECTING
	})
	connectCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	client, err := dt5215.Dial(connectCtx, r.controlAddress, r.streamAddress)
	if err != nil {
		r.publishConnectFailure(err)
		return err
	}
	failed := true
	defer func() {
		if failed {
			_ = client.Close()
		}
	}()
	topology, err := client.DiscoverProductionTopology(connectCtx, r.connections)
	if err != nil {
		r.publishConnectFailure(err)
		return err
	}
	firmwareCtx, cancelFirmware := context.WithTimeout(ctx, 5*time.Second)
	readHVModuleFirmware(firmwareCtx, client, &topology, r.output)
	cancelFirmware()
	printDiscoveredDevices(r.output, topology)

	states, _ := acquisition.NewStateMachine(acquisition.StateIdle, nil)
	factory := runpipeline.Factory{Options: runpipeline.Options{
		Parent: r.runParent, Capacity: r.capacity, Backpressure: acquisition.BackpressureBlock,
		ExecutionIdentity: executionIdentity(topology, r.connections, r.controlAddress, r.streamAddress),
	}}
	coordinator, err := acquisition.NewCoordinator(states, client, factory.New, 4, r.drainTimeout)
	if err != nil {
		r.publishConnectFailure(err)
		return err
	}
	coordinator.SetFaultObserver(func(fault error) {
		service.PublishCoordinatorFault(r.publisher, fault, nil)
		fmt.Fprintf(r.output, "acquisition fault: %v\n", fault)
	})
	recoveryBoards := make([]acquisition.RecoveryBoard, 0, len(topology.Boards))
	for _, board := range topology.Boards {
		recoveryBoards = append(recoveryBoards, acquisition.RecoveryBoard{Chain: board.Chain, Node: board.Node, Status: board.AcquisitionState})
	}
	recoveryResult, recoveryErr := acquisition.RecoverStartup(ctx, states, client, recoveryBoards, 4, r.drainTimeout, actor)
	r.publisher.Publish(topologySnapshot(topology))
	service.PublishStartupRecovery(r.publisher, recoveryResult, recoveryErr, time.Now())
	if recoveryErr != nil {
		err = fmt.Errorf("recover hardware after connection: %w", recoveryErr)
		r.publishConnectFailure(err)
		return err
	}
	configurator, err := acquisition.NewConfigurator(states, client, service.ConfigurationProgressPublisher(r.publisher, states, nil))
	if err != nil {
		r.publishConnectFailure(err)
		return err
	}
	targets := make([]acquisition.ConfigurationTarget, 0, len(r.connections))
	hvTargets := make([]service.HVTarget, 0, len(r.connections))
	for _, connection := range r.connections {
		targets = append(targets, acquisition.ConfigurationTarget{Board: connection.Board, Chain: uint16(connection.Chain), Node: uint16(connection.Node)})
		hvTargets = append(hvTargets, service.HVTarget{Board: connection.Board, Chain: uint16(connection.Chain), Node: uint16(connection.Node)})
	}
	configurationCtx, cancelConfiguration := context.WithTimeout(ctx, 30*time.Second)
	_, err = configurator.Configure(configurationCtx, r.document, targets, acquisition.ConfigureOptions{Actor: actor, Hard: true, AuthorizeHV: r.authorizeHV})
	cancelConfiguration()
	if err != nil {
		r.publishConnectFailure(fmt.Errorf("apply connection configuration: %w", err))
		return err
	}
	hv := &service.NativeHVController{Hardware: client, States: states, Publisher: r.publisher, Targets: hvTargets, Authorized: r.authorizeHV}
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	go func() {
		if monitorErr := hv.Run(monitorCtx, time.Second); monitorErr != nil {
			fmt.Fprintf(r.output, "HV monitor stopped: %v\n", monitorErr)
		}
	}()
	r.mu.Lock()
	r.client, r.coordinator, r.configurator, r.hv = client, coordinator, configurator, hv
	r.monitorCancel, r.topology = monitorCancel, topology
	r.mu.Unlock()
	failed = false
	fmt.Fprintf(r.output, "hardware connected requested_by=%s devices=%d\n", actor, len(topology.Boards))
	return nil
}

func (r *hardwareRuntime) Disconnect(_ context.Context, actor string) error {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	r.mu.Lock()
	if r.client == nil {
		r.mu.Unlock()
		return fmt.Errorf("hardware is already disconnected")
	}
	if runID := r.coordinator.ActiveRunID(); runID != "" {
		r.mu.Unlock()
		return fmt.Errorf("stop active run %q before disconnecting hardware", runID)
	}
	client, cancel := r.client, r.monitorCancel
	r.client, r.coordinator, r.configurator, r.hv, r.monitorCancel = nil, nil, nil, nil, nil
	r.topology = dt5215.Topology{}
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	err := client.Close()
	r.publisher.Update(func(snapshot *daqv1.TelemetrySnapshot) {
		snapshot.State = daqv1.SystemState_SYSTEM_STATE_DISCONNECTED
		snapshot.Chains = nil
		snapshot.Concentrator = nil
	})
	fmt.Fprintf(r.output, "hardware disconnected requested_by=%s\n", actor)
	return err
}

func (r *hardwareRuntime) Close() error {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	r.mu.Lock()
	client, cancel := r.client, r.monitorCancel
	r.client, r.coordinator, r.configurator, r.hv, r.monitorCancel = nil, nil, nil, nil, nil
	r.topology = dt5215.Topology{}
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if client != nil {
		return client.Close()
	}
	return nil
}

func (r *hardwareRuntime) publishConnectFailure(err error) {
	r.publisher.Update(func(snapshot *daqv1.TelemetrySnapshot) {
		snapshot.State = daqv1.SystemState_SYSTEM_STATE_DISCONNECTED
		snapshot.Chains = nil
		snapshot.Concentrator = nil
		snapshot.Diagnostics = append(snapshot.Diagnostics, &daqv1.Diagnostic{
			Severity: daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_ERROR,
			Code:     "HARDWARE_CONNECTION_FAILED", Message: err.Error(), ObservedAt: timestamppb.Now(),
		})
	})
}

func (r *hardwareRuntime) controller() (*acquisition.Coordinator, error) {
	if r.coordinator == nil {
		return nil, fmt.Errorf("hardware is disconnected")
	}
	return r.coordinator, nil
}

func (r *hardwareRuntime) Start(ctx context.Context, runID, actor string, options acquisition.RunOptions) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	controller, err := r.controller()
	if err != nil {
		return err
	}
	return controller.Start(ctx, runID, actor, options)
}

func (r *hardwareRuntime) Stop(ctx context.Context, actor string) error {
	return r.StopWithReason(ctx, actor, "operator_stop")
}

func (r *hardwareRuntime) StopWithReason(ctx context.Context, actor, reason string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	controller, err := r.controller()
	if err != nil {
		return err
	}
	return controller.StopWithReason(ctx, actor, reason)
}

func (r *hardwareRuntime) ActiveRunID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.coordinator == nil {
		return ""
	}
	return r.coordinator.ActiveRunID()
}

func (r *hardwareRuntime) ActivePipeline() acquisition.RunPipeline {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.coordinator == nil {
		return nil
	}
	return r.coordinator.ActivePipeline()
}

func (r *hardwareRuntime) StateSnapshot() acquisition.StateSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.coordinator == nil {
		return acquisition.StateSnapshot{State: acquisition.StateDisconnected}
	}
	return r.coordinator.StateSnapshot()
}

func (r *hardwareRuntime) Configure(ctx context.Context, document *janusconfig.Document, actor string) (acquisition.ConfigurationResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.configurator == nil {
		return acquisition.ConfigurationResult{}, fmt.Errorf("hardware is disconnected")
	}
	targets := make([]acquisition.ConfigurationTarget, 0, len(r.connections))
	for _, connection := range r.connections {
		targets = append(targets, acquisition.ConfigurationTarget{Board: connection.Board, Chain: uint16(connection.Chain), Node: uint16(connection.Node)})
	}
	return r.configurator.Configure(ctx, document, targets, acquisition.ConfigureOptions{Actor: actor, Hard: true, AuthorizeHV: r.authorizeHV})
}

func (r *hardwareRuntime) Set(ctx context.Context, boards []uint32, enabled bool, actor string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.hv == nil {
		return fmt.Errorf("hardware is disconnected")
	}
	return r.hv.Set(ctx, boards, enabled, actor)
}

func (r *hardwareRuntime) HardwareMetadata() ([]configaudit.BoardEvidence, *dt5215.ConcentratorInfo) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.client == nil {
		return nil, nil
	}
	boards := make([]configaudit.BoardEvidence, 0, len(r.topology.Boards))
	for _, board := range r.topology.Boards {
		boards = append(boards, configaudit.BoardEvidence{
			Board: int(board.Chain), FirmwareRevision: board.FirmwareRevision,
			HVModuleFirmwareRaw: board.HVModuleFirmwareRaw, HVModuleFirmwareVersion: board.HVModuleFirmwareVersion,
			HVModuleFirmwareAvailable: board.HVModuleFirmwareAvailable,
		})
	}
	concentrator := r.topology.Concentrator
	return boards, &concentrator
}
