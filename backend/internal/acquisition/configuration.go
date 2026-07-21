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
	ConfigurationFPGA     ConfigurationStage = "fpga_citiroc_probe_pedestal"
	ConfigurationHV       ConfigurationStage = "hv"
	ConfigurationComplete ConfigurationStage = "complete"
	ConfigurationFailed   ConfigurationStage = "failed"
)

type ConfigurationProgress struct {
	Stage   ConfigurationStage
	Target  *ConfigurationTarget
	Message string
	Err     error
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
	mu       sync.Mutex
	states   *StateMachine
	hardware ConfigurationHardware
	observe  ConfigurationObserver
}

func NewConfigurator(states *StateMachine, hardware ConfigurationHardware, observe ConfigurationObserver) (*Configurator, error) {
	if states == nil || hardware == nil {
		return nil, fmt.Errorf("state machine and configuration hardware are required")
	}
	return &Configurator{states: states, hardware: hardware, observe: observe}, nil
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
		c.publish(ConfigurationProgress{Stage: ConfigurationPlanning, Target: target, Message: "building production configuration plan"})
		plan, err := dt5202.PlanProductionConfiguration(document, target.Board)
		if err != nil {
			return ConfigurationResult{}, c.fail(target, fmt.Errorf("plan board %d: %w", target.Board, err), options.Actor)
		}
		c.publish(ConfigurationProgress{Stage: ConfigurationPedestal, Target: target, Message: "loading protected-flash pedestal calibration read-only"})
		calibration, err := dt5202.LoadPedestalCalibration(ctx, c.hardware, target.Chain, target.Node)
		if err != nil {
			return ConfigurationResult{}, c.fail(target, fmt.Errorf("load pedestal board %d chain %d node %d: %w", target.Board, target.Chain, target.Node, err), options.Actor)
		}
		plan, err = plan.WithPedestalCalibration(calibration.Calibration)
		if err != nil {
			return ConfigurationResult{}, c.fail(target, fmt.Errorf("complete pedestal plan board %d: %w", target.Board, err), options.Actor)
		}
		c.publish(ConfigurationProgress{Stage: ConfigurationFPGA, Target: target, Message: "applying and validating FPGA, Citiroc, probe, and pedestal registers"})
		if err = dt5202.ApplyConfiguration(ctx, c.hardware, target.Chain, target.Node, plan, options.Hard); err != nil {
			return ConfigurationResult{}, c.fail(target, err, options.Actor)
		}
		if options.AuthorizeHV {
			c.publish(ConfigurationProgress{Stage: ConfigurationHV, Target: target, Message: "applying explicitly authorized HV peripheral settings"})
			if err = dt5202.ApplyHVConfiguration(ctx, c.hardware, target.Chain, target.Node, plan.HV); err != nil {
				return ConfigurationResult{}, c.fail(target, fmt.Errorf("apply HV board %d chain %d node %d: %w", target.Board, target.Chain, target.Node, err), options.Actor)
			}
		}
		result.Plans = append(result.Plans, plan)
		result.Calibrations = append(result.Calibrations, calibration)
	}
	if _, err := c.states.Move(StateReady, options.Actor); err != nil {
		return ConfigurationResult{}, c.fail(nil, err, options.Actor)
	}
	c.publish(ConfigurationProgress{Stage: ConfigurationComplete, Message: fmt.Sprintf("configuration applied to %d boards; HV authorized=%t", len(targets), options.AuthorizeHV)})
	return result, nil
}

func (c *Configurator) fail(target *ConfigurationTarget, err error, actor string) error {
	if _, transitionErr := c.states.Move(StateFault, actor); transitionErr != nil {
		c.publish(ConfigurationProgress{Stage: ConfigurationFailed, Target: target, Message: err.Error(), Err: err})
		return fmt.Errorf("%w; transition to fault: %v", err, transitionErr)
	}
	c.publish(ConfigurationProgress{Stage: ConfigurationFailed, Target: target, Message: err.Error(), Err: err})
	return err
}

func (c *Configurator) publish(progress ConfigurationProgress) {
	if c.observe != nil {
		c.observe(progress)
	}
}
