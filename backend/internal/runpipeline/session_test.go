package runpipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

func TestSessionFinalizesTypedEventsAndRawCapture(t *testing.T) {
	parent := t.TempDir()
	factory := Factory{Options: Options{Parent: parent, Capacity: 2, Backpressure: acquisition.BackpressureBlock, Now: func() time.Time { return time.Unix(100, 0) }}}
	audit := &configaudit.Report{SchemaVersion: 1, Valid: true}
	created, err := factory.New("42", acquisition.RunOptions{
		CaptureRaw: true, JournalTransport: true, RequestedBy: "operator",
		RequestedConfiguration: "Open 0=0", EffectiveConfiguration: []dt5202.ConfigurationPlan{{Board: 0}}, ConfigurationAudit: audit,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := created.(*Session)
	event := dt5215.StreamEvent{Chain: 0, Descriptor: dt5215.Descriptor{Qualifier: dt5202.QualifierTest, TriggerID: 7, Timestamp: 8}, Payload: []byte{1, 0, 0, 0}}
	if err := session.Submit(context.Background(), acquisition.PipelineBatch{Raw: []byte{9}, Events: []dt5215.StreamEvent{event}}); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Finalize("2026-07-21T16:00:00Z", "operator_stop"); err != nil {
		t.Fatal(err)
	}
	stats := session.Stats()
	if stats.Directory != session.Directory() || stats.BytesWritten == 0 || stats.EventCount != 1 || stats.RawBatches != 1 || !stats.Finalized || stats.LastError != "" {
		t.Fatalf("storage stats = %+v", stats)
	}
	for _, name := range []string{"manifest.json", "events.jsonl", "wire.raw", "transport.journal"} {
		if _, err := os.Stat(filepath.Join(session.Directory(), name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	manifestData, err := os.ReadFile(filepath.Join(session.Directory(), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		RequestedBy            string                     `json:"requested_by"`
		CaptureRaw             bool                       `json:"capture_raw"`
		JournalTransport       bool                       `json:"journal_transport"`
		RequestedConfiguration string                     `json:"requested_configuration"`
		EffectiveConfiguration []dt5202.ConfigurationPlan `json:"effective_configuration"`
		ConfigurationAudit     *configaudit.Report        `json:"configuration_audit"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.RequestedBy != "operator" || !manifest.CaptureRaw || !manifest.JournalTransport || manifest.RequestedConfiguration != "Open 0=0" || len(manifest.EffectiveConfiguration) != 1 || manifest.ConfigurationAudit == nil {
		t.Fatalf("manifest = %+v", manifest)
	}
	if _, err := os.Stat(filepath.Join(session.Directory(), "incomplete")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete marker remains: %v", err)
	}
}

func TestSessionAbortRetainsIncompleteMarker(t *testing.T) {
	factory := Factory{Options: Options{Parent: t.TempDir(), Capacity: 1, Backpressure: acquisition.BackpressureBlock}}
	created, err := factory.New("failed", acquisition.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	session := created.(*Session)
	bad := dt5215.StreamEvent{Descriptor: dt5215.Descriptor{Qualifier: 0x7e}}
	if err := session.Submit(context.Background(), acquisition.PipelineBatch{Raw: []byte{1}, Events: []dt5215.StreamEvent{bad}}); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err == nil {
		t.Fatal("expected decode failure")
	}
	if err := session.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(session.Directory(), "incomplete")); err != nil {
		t.Fatalf("incomplete marker missing: %v", err)
	}
	stats := session.Stats()
	if stats.Finalized || stats.LastError == "" || stats.EventCount != 0 {
		t.Fatalf("failed storage stats = %+v", stats)
	}
}

func TestSessionRetainsLatestBoardServiceTelemetry(t *testing.T) {
	factory := Factory{Options: Options{Parent: t.TempDir(), Capacity: 1, Backpressure: acquisition.BackpressureBlock}}
	created, err := factory.New("service", acquisition.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	session := created.(*Session)
	temperature, voltage := 38.25, 49.5
	status := uint16(7)
	event := dt5202.Event{Kind: dt5202.EventService, Service: &dt5202.ServiceEvent{
		FPGATemperature: &temperature, HVVoltage: &voltage, HVOn: true, HVOverVoltage: true, Status: &status,
	}}
	if err := session.sink.AppendEvent(dt5215.StreamEvent{Chain: 2, Descriptor: dt5215.Descriptor{Node: 3}}, event); err != nil {
		t.Fatal(err)
	}
	boards := session.BoardStats()
	if len(boards) != 1 || boards[0].Chain != 2 || boards[0].Node != 3 || boards[0].EventCount != 1 || boards[0].FPGATemperature == nil || *boards[0].FPGATemperature != temperature || boards[0].HVVoltage == nil || *boards[0].HVVoltage != voltage || !boards[0].HVOverVoltage || boards[0].AcquisitionStatus == nil || *boards[0].AcquisitionStatus != status {
		t.Fatalf("boards = %+v", boards)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Abort(); err != nil {
		t.Fatal(err)
	}
}
