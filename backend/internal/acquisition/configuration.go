package acquisition

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

// ConfigurationHardware is the complete, native DT5202 configuration I/O
// boundary. Loading pedestal calibration is read-only; HV writes are kept
// behind ConfigureOptions.AuthorizeHV.
type ConfigurationHardware interface {
	dt5202.ConfigurationHardware
	dt5202.PedestalFlashReader
	dt5202.HVHardware
}

type ConfigurationTarget struct {
	Board int
	Chain uint16
	Node  uint16
}

type ConfigureOptions struct {
	Actor       string
	Hard        bool
	AuthorizeHV bool
}

type ConfigurationStage string

const (
	ConfigurationPlanning ConfigurationStage = "planning"
	ConfigurationPedestal ConfigurationStage = "pedestal_calibration"
	ConfigurationWriting  ConfigurationStage = "writing_registers"
	ConfigurationCitiroc  ConfigurationStage = "configuring_citiroc"
	ConfigurationReadback ConfigurationStage = "reading_registers"
	ConfigurationHV       ConfigurationStage = "hv"
	ConfigurationComplete ConfigurationStage = "complete"
	ConfigurationFailed   ConfigurationStage = "failed"
)

type ConfigurationProgress struct {
	OperationID     string
	Stage           ConfigurationStage
	Target          *ConfigurationTarget
	BoardsCompleted int
	BoardsTotal     int
	Completed       int
	Total           int
	Unit            string
	Reused          bool
	Message         string
	Err             error
}

type ConfigurationObserver func(ConfigurationProgress)

type ConfigurationResult struct {
	Plans        []dt5202.ConfigurationPlan
	Calibrations []dt5202.PedestalFlashCalibration
	HVAuthorized bool
}

// Configurator serializes configuration with run control through the shared
// state machine. A partial hardware failure is a fault because the effective
// board state is no longer known to match either the old or requested plan.
type Configurator struct {
	mu                      sync.Mutex
	states                  *StateMachine
	hardware                ConfigurationHardware
	observe                 ConfigurationObserver
	calibrations            map[ConfigurationTarget]dt5202.PedestalFlashCalibration
	loadPedestalCalibration func(context.Context, dt5202.PedestalFlashReader, uint16, uint16) (dt5202.PedestalFlashCalibration, error)
	operationSequence       uint64
	operationID             string
	boardsCompleted         int
	boardsTotal             int
}

func NewConfigurator(states *StateMachine, hardware ConfigurationHardware, observe ConfigurationObserver) (*Configurator, error) {
	if states == nil || hardware == nil {
		return nil, fmt.Errorf("state machine and configuration hardware are required")
	}
	return &Configurator{
		states: states, hardware: hardware, observe: observe,
		calibrations:            make(map[ConfigurationTarget]dt5202.PedestalFlashCalibration),
		loadPedestalCalibration: dt5202.LoadPedestalCalibration,
	}, nil
}

func (c *Configurator) Configure(ctx context.Context, document *janusconfig.Document, targets []ConfigurationTarget, options ConfigureOptions) (ConfigurationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if document == nil {
		return ConfigurationResult{}, fmt.Errorf("configuration document is required")
	}
	if options.Actor == "" {
		return ConfigurationResult{}, fmt.Errorf("configuration actor is required")
	}
	if len(targets) == 0 {
		return ConfigurationResult{}, fmt.Errorf("configuration targets are required")
	}
	if _, err := c.states.Move(StateConfiguring, options.Actor); err != nil {
		return ConfigurationResult{}, err
	}

	targets = append([]ConfigurationTarget(nil), targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].Board < targets[j].Board })
	c.operationSequence++
	c.operationID = fmt.Sprintf("configuration-%d", c.operationSequence)
	c.boardsCompleted, c.boardsTotal = 0, len(targets)
	result := ConfigurationResult{HVAuthorized: options.AuthorizeHV}
	seen := make(map[int]bool, len(targets))
	for i := range targets {
		target := &targets[i]
		if target.Board < 0 || seen[target.Board] {
			return ConfigurationResult{}, c.fail(target, fmt.Errorf("invalid or duplicate board target %d", target.Board), options.Actor)
		}
		seen[target.Board] = true
	}
	for i := range targets {
		target := &targets[i]
		c.publishProgress(ConfigurationPlanning, target, 0, 0, "", false, "building production configuration plan", nil)
		plan, err := dt5202.PlanProductionConfiguration(document, target.Board)
		if err != nil {
			return ConfigurationResult{}, c.fail(target, fmt.Errorf("plan board %d: %w", target.Board, err), options.Actor)
		}
		c.publishProgress(ConfigurationPedestal, target, 0, dt5202.PedestalFlashPageBytes, "bytes", false, "loading protected-flash pedestal calibration", nil)
		calibration, reused, err := c.pedestalCalibration(ctx, *target)
		if err != nil {
			return ConfigurationResult{}, c.fail(target, fmt.Errorf("load pedestal board %d chain %d node %d: %w", target.Board, target.Chain, target.Node, err), options.Actor)
		}
		message := "loaded protected-flash pedestal calibration read-only"
		if reused {
			message = "reusing protected-flash pedestal calibration from current hardware session"
		}
		c.publishProgress(ConfigurationPedestal, target, dt5202.PedestalFlashPageBytes, dt5202.PedestalFlashPageBytes, "bytes", reused, message, nil)
		plan, err = plan.WithPedestalCalibration(calibration.Calibration)
		if err != nil {
			return ConfigurationResult{}, c.fail(target, fmt.Errorf("complete pedestal plan board %d: %w", target.Board, err), options.Actor)
		}
		if err = dt5202.ApplyConfigurationWithProgress(ctx, c.hardware, target.Chain, target.Node, plan, options.Hard, func(progress dt5202.ConfigurationApplyProgress) {
			if progress.Completed != 0 && progress.Completed != progress.Total && progress.Completed%50 != 0 {
				return
			}
			stage, unit, message := ConfigurationWriting, "registers", "writing FPGA, probe, and pedestal registers"
			switch progress.Stage {
			case dt5202.ConfigurationApplyCitiroc:
				stage, unit, message = ConfigurationCitiroc, "chips", "configuring Citiroc chips"
			case dt5202.ConfigurationApplyReadback:
				stage, unit, message = ConfigurationReadback, "registers", "reading back and validating registers"
			}
			c.publishProgress(stage, target, progress.Completed, progress.Total, unit, false, message, nil)
		}); err != nil {
			return ConfigurationResult{}, c.fail(target, err, options.Actor)
		}
		if options.AuthorizeHV {
			c.publishProgress(ConfigurationHV, target, 0, 0, "", false, "applying explicitly authorized HV peripheral settings", nil)
			if err = dt5202.ApplyHVConfiguration(ctx, c.hardware, target.Chain, target.Node, plan.HV); err != nil {
				return ConfigurationResult{}, c.fail(target, fmt.Errorf("apply HV board %d chain %d node %d: %w", target.Board, target.Chain, target.Node, err), options.Actor)
			}
		}
		result.Plans = append(result.Plans, plan)
		result.Calibrations = append(result.Calibrations, calibration)
		c.boardsCompleted = i + 1
	}
	if _, err := c.states.Move(StateReady, options.Actor); err != nil {
		return ConfigurationResult{}, c.fail(nil, err, options.Actor)
	}
	c.publishProgress(ConfigurationComplete, nil, len(targets), len(targets), "boards", false, fmt.Sprintf("configuration applied to %d boards", len(targets)), nil)
	return result, nil
}

func (c *Configurator) publishProgress(stage ConfigurationStage, target *ConfigurationTarget, completed, total int, unit string, reused bool, message string, err error) {
	c.publish(ConfigurationProgress{
		OperationID: c.operationID, Stage: stage, Target: target,
		BoardsCompleted: c.boardsCompleted, BoardsTotal: c.boardsTotal,
		Completed: completed, Total: total, Unit: unit, Reused: reused,
		Message: message, Err: err,
	})
}

func (c *Configurator) pedestalCalibration(ctx context.Context, target ConfigurationTarget) (dt5202.PedestalFlashCalibration, bool, error) {
	if calibration, ok := c.calibrations[target]; ok {
		return calibration, true, nil
	}
	calibration, err := c.loadPedestalCalibration(ctx, c.hardware, target.Chain, target.Node)
	if err != nil {
		return dt5202.PedestalFlashCalibration{}, false, err
	}
	c.calibrations[target] = calibration
	return calibration, false, nil
}

func (c *Configurator) fail(target *ConfigurationTarget, err error, actor string) error {
	if _, transitionErr := c.states.Move(StateFault, actor); transitionErr != nil {
		c.publishProgress(ConfigurationFailed, target, 0, 0, "", false, err.Error(), err)
		return fmt.Errorf("%w; transition to fault: %v", err, transitionErr)
	}
	c.publishProgress(ConfigurationFailed, target, 0, 0, "", false, err.Error(), err)
	return err
}

func (c *Configurator) publish(progress ConfigurationProgress) {
	if c.observe != nil {
		c.observe(progress)
	}
}
