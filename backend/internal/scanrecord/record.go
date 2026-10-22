// Package scanrecord defines storage-independent scan provenance and telemetry.
package scanrecord

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

// Configuration is the applied baseline, before temporary scan overrides.
// Assignments retain order, indices and source lines, but not source comments.
type Configuration struct {
	RequestedAssignments   []janusconfig.Assignment   `json:"requested_assignments"`
	EffectiveConfiguration []dt5202.ConfigurationPlan `json:"effective_configuration"`
	Audit                  *configaudit.Report        `json:"configuration_audit,omitempty"`
	Topology               dt5215.Topology            `json:"topology"`
	Software               runstore.SoftwareIdentity  `json:"software"`
}

type Record struct {
	RawPayload        []byte                    `json:"raw_payload,omitempty"`
	SchemaVersion     int                       `json:"schema_version"`
	Type              string                    `json:"type"`
	ObservedAt        time.Time                 `json:"observed_at"`
	Board             uint32                    `json:"board"`
	Chain             uint16                    `json:"chain"`
	Node              uint16                    `json:"node"`
	PointIndex        *uint32                   `json:"point_index,omitempty"`
	Phase             string                    `json:"phase,omitempty"`
	Configuration     *Configuration            `json:"configuration,omitempty"`
	BaselinePlan      *dt5202.ConfigurationPlan `json:"baseline_plan,omitempty"`
	Writes            []dt5202.RegisterWrite    `json:"writes,omitempty"`
	Service           *dt5202.ServiceEvent      `json:"service,omitempty"`
	Monitor           *dt5202.HVTelemetry       `json:"monitor,omitempty"`
	HardwareTimestamp *uint64                   `json:"hardware_timestamp,omitempty"`
	CRCError          bool                      `json:"crc_error,omitempty"`
	Restored          *bool                     `json:"restored,omitempty"`
	Error             string                    `json:"error,omitempty"`
}

// ErrPersistence distinguishes evidence loss from an operator cancellation.
var ErrPersistence = errors.New("scan evidence persistence failed")

type Sink func(Record) error

func Emit(sink Sink, record Record) error {
	if sink == nil {
		return nil
	}
	record.SchemaVersion = 1
	if record.ObservedAt.IsZero() {
		record.ObservedAt = time.Now().UTC()
	}
	if err := sink(record); err != nil {
		return fmt.Errorf("%w: %w", ErrPersistence, err)
	}
	return nil
}

// SampleMonitor reads only between scan points, never during a counting dwell.
// A read failure is evidence, not a zero-valued successful measurement.
func SampleMonitor(ctx context.Context, hardware dt5202.HVHardware, sink Sink, board uint32, chain, node uint16, phase string, point *uint32) error {
	if sink == nil {
		return nil
	}
	sampleCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	reading, err := dt5202.ReadHVTelemetry(sampleCtx, hardware, chain, node)
	record := Record{Type: "monitor", Board: board, Chain: chain, Node: node, Phase: phase, PointIndex: point}
	if err != nil {
		record.Error = err.Error()
	} else {
		record.Monitor = &reading
	}
	return Emit(sink, record)
}
