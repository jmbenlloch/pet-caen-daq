//go:build hdf5

package hdf5store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

// ConvertJSONRun streams a finalized development run's JSONL decoded events
// into a new HDF5 artifact. The source directory is read-only.
func ConvertJSONRun(directory, output string) (err error) {
	runID := filepath.Base(directory)
	if len(runID) <= 4 || runID[:4] != "run-" {
		return errors.New("source directory name must start with run-")
	}
	manifest, err := runstore.ReadManifest(directory, runID[4:])
	if err != nil {
		return err
	}
	manifestJSON, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read source manifest: %w", err)
	}
	auditJSON, err := json.Marshal(manifest.ConfigurationAudit)
	if err != nil {
		return fmt.Errorf("encode configuration audit: %w", err)
	}
	effectiveJSON, err := json.Marshal(manifest.EffectiveConfiguration)
	if err != nil {
		return fmt.Errorf("encode effective configuration: %w", err)
	}
	metadataJSON, err := json.Marshal(struct {
		SourceFormat          string                         `json:"source_format"`
		SourceRunID           string                         `json:"source_run_id"`
		StartedAt             string                         `json:"started_at"`
		CompletedAt           string                         `json:"completed_at,omitempty"`
		ConfigurationIdentity runstore.ConfigurationIdentity `json:"configuration_identity"`
		ExecutionIdentity     runstore.ExecutionIdentity     `json:"execution_identity"`
	}{
		SourceFormat: "pet-caen-daq-jsonl", SourceRunID: manifest.RunID,
		StartedAt: manifest.StartedAt, CompletedAt: manifest.CompletedAt,
		ConfigurationIdentity: manifest.ConfigurationIdentity, ExecutionIdentity: manifest.ExecutionIdentity,
	})
	if err != nil {
		return err
	}
	writer, err := CreateWithMetadata(output, Metadata{
		RunID: manifest.RunID, RequestedConfiguration: []byte(manifest.RequestedConfiguration),
		AuditJSON: auditJSON, EffectiveJSON: effectiveJSON, MetadataJSON: metadataJSON,
		EffectiveConfiguration: manifest.EffectiveConfiguration, Boards: manifest.ExecutionIdentity.Topology.Boards,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, writer.Close())
		}
	}()

	events, err := os.Open(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		return fmt.Errorf("open source events: %w", err)
	}
	defer events.Close()
	reader := runstore.NewReader(events, runstore.DefaultMaxRecordSize)
	var count uint64
	for {
		envelope, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nextErr
		}
		count++
		if envelope.Sequence != count {
			return fmt.Errorf("JSONL event sequence %d, want %d", envelope.Sequence, count)
		}
		var payload struct {
			Chain     uint8        `json:"chain"`
			Node      uint8        `json:"node"`
			Qualifier uint8        `json:"qualifier"`
			TriggerID uint64       `json:"trigger_id,string"`
			Timestamp uint64       `json:"timestamp,string"`
			Event     dt5202.Event `json:"event"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return fmt.Errorf("decode JSONL event %d payload: %w", count, err)
		}
		if envelope.Kind != string(payload.Event.Kind) {
			return fmt.Errorf("JSONL event %d envelope kind %q does not match payload kind %q", count, envelope.Kind, payload.Event.Kind)
		}
		wire := dt5215.StreamEvent{Chain: payload.Chain, Descriptor: dt5215.Descriptor{
			Node: payload.Node, Qualifier: payload.Qualifier,
			TriggerID: payload.TriggerID, Timestamp: payload.Timestamp,
		}}
		if err := writer.AppendEvent(wire, payload.Event); err != nil {
			return fmt.Errorf("convert JSONL event %d: %w", count, err)
		}
	}
	if count != manifest.EventCount {
		return fmt.Errorf("converted %d events, manifest records %d", count, manifest.EventCount)
	}
	if err := writer.Finalize(manifestJSON); err != nil {
		return err
	}
	return Validate(output, true)
}
