package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runpipeline"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/service"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/staircase"
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
	staircase     *acquisition.StaircaseCoordinator
	holdDelay     *acquisition.HoldDelayCoordinator
	hv            *service.NativeHVController
	monitorCancel context.CancelFunc
	topology      dt5215.Topology
	configuredFor *janusconfig.Document
	configured    acquisition.ConfigurationResult
}

// Six to eight enabled links take about seven seconds each to enumerate on
// real hardware. Allow enough time for the vendor-matched whole-chain retry
// cycles without cutting off a legitimate recovery attempt.
const topologyOperationTimeout = 10 * time.Minute

func (r *hardwareRuntime) Discover(ctx context.Context, actor string) error {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	r.mu.RLock()
	connected := r.client != nil
	r.mu.RUnlock()
	if connected {
		return fmt.Errorf("disconnect hardware before discovering cards")
	}
	r.publisher.Update(func(snapshot *daqv1.TelemetrySnapshot) {
		snapshot.State = daqv1.SystemState_SYSTEM_STATE_CONNECTING
	})
	discoveryCtx, cancel := context.WithTimeout(ctx, topologyOperationTimeout)
	defer cancel()
	client, err := dt5215.Dial(discoveryCtx, r.controlAddress, r.streamAddress)
	if err != nil {
		r.publishConnectFailure(err)
		return err
	}
	defer client.Close()
	startedAt := time.Now()
	var discoveryProgress *daqv1.DiscoveryProgress
	observeDiscovery := func(progress dt5215.DiscoveryProgress) {
		discoveryProgress = protobufDiscoveryProgress(progress, startedAt, time.Now())
		r.publisher.Update(func(snapshot *daqv1.TelemetrySnapshot) {
			snapshot.State = daqv1.SystemState_SYSTEM_STATE_CONNECTING
			snapshot.DiscoveryProgress = discoveryProgress
		})
	}
	topology, err := client.DiscoverEnabledTopologyWithObserver(discoveryCtx, observeDiscovery)
	if err != nil {
		r.publishConnectFailure(err)
		return err
	}
	firmwareCtx, cancelFirmware := context.WithTimeout(ctx, 15*time.Second)
	readHVModuleFirmware(firmwareCtx, client, &topology, r.output)
	cancelFirmware()
	printDiscoveredDevices(r.output, topology)
	snapshot := topologySnapshot(topology, nil)
	snapshot.State = daqv1.SystemState_SYSTEM_STATE_DISCONNECTED
	snapshot.DiscoveryProgress = discoveryProgress
	snapshot.Diagnostics = append(snapshot.Diagnostics, &daqv1.Diagnostic{
		Severity: daqv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_INFO,
		Code:     "HARDWARE_DISCOVERY_COMPLETE",
		Message:  fmt.Sprintf("discovered %d cards; requested by %s", len(topology.Boards), actor),
	})
	r.publisher.Publish(snapshot)
	return nil
}

func (r *hardwareRuntime) Connect(ctx context.Context, actor, configuration string) error {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	r.mu.RLock()
	connected := r.client != nil
	r.mu.RUnlock()
	if connected {
		return fmt.Errorf("hardware is already connected")
	}
	if configuration != "" {
		document, err := janusconfig.Parse(bytes.NewBufferString(configuration))
		if err != nil {
			return err
		}
		if _, err = document.Classify(); err != nil {
			return err
		}
		connections, err := document.Connections()
		if err != nil {
			return err
		}
		if err = janusconfig.ValidateProductionTopology(connections); err != nil {
			return err
		}
		r.document, r.connections = document, connections
	}
	r.publisher.Update(func(snapshot *daqv1.TelemetrySnapshot) {
		snapshot.State = daqv1.SystemState_SYSTEM_STATE_CONNECTING
		snapshot.DiscoveryProgress = nil
	})
	connectCtx, cancel := context.WithTimeout(ctx, topologyOperationTimeout)
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
	activeChains := configuredChains(r.connections)
	coordinator, err := acquisition.NewCoordinatorForChains(states, client, factory.New, activeChains, r.drainTimeout)
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
	recoveryResult, recoveryErr := acquisition.RecoverStartupChains(ctx, states, client, recoveryBoards, activeChains, r.drainTimeout, actor)
	r.publisher.Publish(topologySnapshot(topology, r.connections))
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
	configurationTimeout := max(30*time.Second, time.Duration(len(r.connections))*8*time.Second)
	configurationCtx, cancelConfiguration := context.WithTimeout(ctx, configurationTimeout)
	configured, err := configurator.Configure(configurationCtx, r.document, targets, acquisition.ConfigureOptions{Actor: actor, Hard: true, AuthorizeHV: r.authorizeHV})
	cancelConfiguration()
	if err != nil {
		r.publishConnectFailure(fmt.Errorf("apply connection configuration: %w", err))
		return err
	}
	coordinator.SetSynchronized(topologySynchronized(topology))
	scanTargets := staircaseTargets(targets, configured.Plans)
	staircaseCoordinator, err := acquisition.NewStaircaseCoordinator(states, client, scanTargets)
	if err != nil {
		r.publishConnectFailure(err)
		return err
	}
	holdDelayCoordinator, err := acquisition.NewHoldDelayCoordinator(states, client, scanTargets)
	if err != nil {
		r.publishConnectFailure(err)
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
	r.client, r.coordinator, r.configurator, r.staircase, r.holdDelay, r.hv = client, coordinator, configurator, staircaseCoordinator, holdDelayCoordinator, hv
	r.monitorCancel, r.topology = monitorCancel, topology
	r.configuredFor, r.configured = cloneDocument(r.document), cloneConfigurationResult(configured)
	r.mu.Unlock()
	failed = false
	fmt.Fprintf(r.output, "hardware connected requested_by=%s devices=%d\n", actor, len(topology.Boards))
	return nil
}

func protobufDiscoveryProgress(progress dt5215.DiscoveryProgress, startedAt, updatedAt time.Time) *daqv1.DiscoveryProgress {
	structured := &daqv1.DiscoveryProgress{
		Stage:            protobufDiscoveryStage(progress.Stage),
		Active:           progress.Stage != dt5215.DiscoveryComplete && progress.Stage != dt5215.DiscoveryFailed,
		ChainsCompleted:  uint32(progress.ChainsCompleted),
		ChainsTotal:      uint32(progress.ChainsTotal),
		BoardsDiscovered: uint32(progress.BoardsDiscovered),
		BoardsTotal:      uint32(progress.BoardsTotal),
		Message:          progress.Message,
		StartedAt:        timestamppb.New(startedAt),
		UpdatedAt:        timestamppb.New(updatedAt),
	}
	if progress.Chain >= 0 {
		chain := uint32(progress.Chain)
		structured.Chain = &chain
	}
	if progress.Node >= 0 {
		node := uint32(progress.Node)
		structured.Node = &node
	}
	return structured
}

func protobufDiscoveryStage(stage dt5215.DiscoveryStage) daqv1.DiscoveryStage {
	switch stage {
	case dt5215.DiscoveryIdentity:
		return daqv1.DiscoveryStage_DISCOVERY_STAGE_IDENTITY
	case dt5215.DiscoveryScanning:
		return daqv1.DiscoveryStage_DISCOVERY_STAGE_SCANNING_LINKS
	case dt5215.DiscoveryResetting:
		return daqv1.DiscoveryStage_DISCOVERY_STAGE_RESETTING_LINKS
	case dt5215.DiscoveryEnumerating:
		return daqv1.DiscoveryStage_DISCOVERY_STAGE_ENUMERATING_LINKS
	case dt5215.DiscoverySynchronizing:
		return daqv1.DiscoveryStage_DISCOVERY_STAGE_SYNCHRONIZING_LINKS
	case dt5215.DiscoveryRecovering:
		return daqv1.DiscoveryStage_DISCOVERY_STAGE_RECOVERING_LINKS
	case dt5215.DiscoveryReadingBoards:
		return daqv1.DiscoveryStage_DISCOVERY_STAGE_READING_BOARDS
	case dt5215.DiscoveryComplete:
		return daqv1.DiscoveryStage_DISCOVERY_STAGE_COMPLETE
	case dt5215.DiscoveryFailed:
		return daqv1.DiscoveryStage_DISCOVERY_STAGE_FAILED
	default:
		return daqv1.DiscoveryStage_DISCOVERY_STAGE_UNSPECIFIED
	}
}

func configuredChains(connections []janusconfig.Connection) []uint16 {
	seen := make(map[int]struct{}, dt5215.MaxChains)
	chains := make([]uint16, 0, dt5215.MaxChains)
	for _, connection := range connections {
		if _, exists := seen[connection.Chain]; exists {
			continue
		}
		seen[connection.Chain] = struct{}{}
		chains = append(chains, uint16(connection.Chain))
	}
	return chains
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
	r.client, r.coordinator, r.configurator, r.staircase, r.holdDelay, r.hv, r.monitorCancel = nil, nil, nil, nil, nil, nil, nil
	r.topology = dt5215.Topology{}
	r.configuredFor, r.configured = nil, acquisition.ConfigurationResult{}
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
	r.client, r.coordinator, r.configurator, r.staircase, r.holdDelay, r.hv, r.monitorCancel = nil, nil, nil, nil, nil, nil, nil
	r.topology = dt5215.Topology{}
	r.configuredFor, r.configured = nil, acquisition.ConfigurationResult{}
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.configurator == nil {
		return acquisition.ConfigurationResult{}, fmt.Errorf("hardware is disconnected")
	}
	if sameConfiguration(r.configuredFor, document) {
		fmt.Fprintf(r.output, "hardware configuration reused requested_by=%s devices=%d\n", actor, len(r.configured.Plans))
		return cloneConfigurationResult(r.configured), nil
	}
	targets := make([]acquisition.ConfigurationTarget, 0, len(r.connections))
	for _, connection := range r.connections {
		targets = append(targets, acquisition.ConfigurationTarget{Board: connection.Board, Chain: uint16(connection.Chain), Node: uint16(connection.Node)})
	}
	result, err := r.configurator.Configure(ctx, document, targets, acquisition.ConfigureOptions{Actor: actor, Hard: true, AuthorizeHV: r.authorizeHV})
	if err == nil && r.staircase != nil {
		r.configuredFor, r.configured = cloneDocument(document), cloneConfigurationResult(result)
		scanTargets := staircaseTargets(targets, result.Plans)
		r.staircase.SetTargets(scanTargets)
		if r.holdDelay != nil {
			r.holdDelay.SetTargets(scanTargets)
		}
	}
	return result, err
}

func topologySynchronized(topology dt5215.Topology) bool {
	if len(topology.Boards) == 0 {
		return false
	}
	for _, board := range topology.Boards {
		if !dt5202.Status(board.AcquisitionState).Has(dt5202.StatusTDLinkSynchronized) {
			return false
		}
	}
	return true
}

func sameConfiguration(left, right *janusconfig.Document) bool {
	if left == nil || right == nil || len(left.Assignments) != len(right.Assignments) {
		return false
	}
	for index := range left.Assignments {
		a, b := left.Assignments[index], right.Assignments[index]
		if a.Name != b.Name || a.Value != b.Value ||
			!reflect.DeepEqual(a.Index, b.Index) || !reflect.DeepEqual(a.Channel, b.Channel) {
			return false
		}
	}
	return true
}

func cloneDocument(document *janusconfig.Document) *janusconfig.Document {
	if document == nil {
		return nil
	}
	clone := &janusconfig.Document{Assignments: make([]janusconfig.Assignment, len(document.Assignments))}
	for index, assignment := range document.Assignments {
		clone.Assignments[index] = assignment
		if assignment.Index != nil {
			value := *assignment.Index
			clone.Assignments[index].Index = &value
		}
		if assignment.Channel != nil {
			value := *assignment.Channel
			clone.Assignments[index].Channel = &value
		}
	}
	return clone
}

func cloneConfigurationResult(result acquisition.ConfigurationResult) acquisition.ConfigurationResult {
	result.Plans = append([]dt5202.ConfigurationPlan(nil), result.Plans...)
	result.Calibrations = append([]dt5202.PedestalFlashCalibration(nil), result.Calibrations...)
	return result
}

func (r *hardwareRuntime) Run(ctx context.Context, request staircase.Request, actor string, observe func(staircase.Point) error) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.staircase == nil {
		return false, fmt.Errorf("hardware is disconnected")
	}
	return r.staircase.Run(ctx, request, actor, observe)
}

func (r *hardwareRuntime) RunHoldDelay(ctx context.Context, request holddelay.Request, actor string, observe func(holddelay.Point) error) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.holdDelay == nil {
		return false, fmt.Errorf("hardware is disconnected")
	}
	return r.holdDelay.Run(ctx, request, actor, observe)
}

func staircaseTargets(targets []acquisition.ConfigurationTarget, plans []dt5202.ConfigurationPlan) []acquisition.StaircaseTarget {
	byBoard := make(map[int]acquisition.ConfigurationTarget, len(targets))
	for _, target := range targets {
		byBoard[target.Board] = target
	}
	result := make([]acquisition.StaircaseTarget, 0, len(plans))
	for _, plan := range plans {
		target, ok := byBoard[plan.Board]
		if ok {
			result = append(result, acquisition.StaircaseTarget{Board: uint32(plan.Board), Chain: target.Chain, Node: target.Node, Plan: plan})
		}
	}
	return result
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
	boardNumbers := make(map[[2]uint16]int, len(r.connections))
	for _, connection := range r.connections {
		boardNumbers[[2]uint16{uint16(connection.Chain), uint16(connection.Node)}] = connection.Board
	}
	boards := make([]configaudit.BoardEvidence, 0, len(r.topology.Boards))
	for _, board := range r.topology.Boards {
		boards = append(boards, configaudit.BoardEvidence{
			Board: boardNumbers[[2]uint16{board.Chain, board.Node}], FirmwareRevision: board.FirmwareRevision,
			HVModuleFirmwareRaw: board.HVModuleFirmwareRaw, HVModuleFirmwareVersion: board.HVModuleFirmwareVersion,
			HVModuleFirmwareAvailable: board.HVModuleFirmwareAvailable,
		})
	}
	concentrator := r.topology.Concentrator
	return boards, &concentrator
}
