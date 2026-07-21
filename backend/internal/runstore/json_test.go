package runstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/rawcapture"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/transportjournal"
)

func TestRunLifecycleAndReplay(t *testing.T) {
	audit := &configaudit.Report{SchemaVersion: 1, Valid: true, Settings: []configaudit.Setting{{Name: "OF_RunInfo", Status: configaudit.Applied, Requested: "1"}}}
	w, err := Create(t.TempDir(), Manifest{RunID: "54-replay", StartedAt: "2026-07-17T11:06:37Z", ConfigurationAudit: audit})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.EnableRawCapture(); err != nil {
		t.Fatal(err)
	}
	if err = w.EnableTransportJournal(); err != nil {
		t.Fatal(err)
	}
	if err = w.TransportJournal().AppendRecord(transportjournal.Record{Kind: transportjournal.Data, ConnectionID: "stream", Stage: "header", Data: []byte{9, 8, 7}}); err != nil {
		t.Fatal(err)
	}
	if err = w.AppendRaw([]byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"board":0,"trigger_id":"42"}`)
	if err = w.Append(Envelope{Kind: "spectroscopy_timing", Sequence: 1, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(w.Directory(), "incomplete")); err != nil {
		t.Fatal(err)
	}
	if err = w.Finalize("2026-07-17T11:06:52Z", "completed"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(w.Directory(), "incomplete")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker still present: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(w.Directory(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	r := NewReader(bytes.NewReader(data), 0)
	e, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if e.Sequence != 1 || e.Kind != "spectroscopy_timing" || !bytes.Equal(e.Payload, payload) {
		t.Fatalf("event = %#v", e)
	}
	if _, err = r.Next(); err != io.EOF {
		t.Fatalf("end = %v", err)
	}
	manifestData, err := os.ReadFile(filepath.Join(w.Directory(), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err = json.Unmarshal(manifestData, &m); err != nil {
		t.Fatal(err)
	}
	if m.EventCount != 1 || m.TerminationReason != "completed" {
		t.Fatalf("manifest = %#v", m)
	}
	if m.ConfigurationAudit == nil || len(m.ConfigurationAudit.Settings) != 1 || m.ConfigurationAudit.Settings[0].Name != "OF_RunInfo" {
		t.Fatalf("configuration audit was not preserved: %#v", m.ConfigurationAudit)
	}
	if len(m.Artifacts) != 3 {
		t.Fatalf("artifacts = %#v", m.Artifacts)
	}
	for _, artifact := range m.Artifacts {
		artifactData, err := os.ReadFile(filepath.Join(w.Directory(), artifact.Name))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(artifactData)
		if artifact.SizeBytes != uint64(len(artifactData)) || artifact.SHA256 != fmt.Sprintf("%x", digest) || len(artifact.SHA256) != 64 {
			t.Fatalf("artifact=%+v bytes=%d digest=%x", artifact, len(artifactData), digest)
		}
	}
	rawFile, err := os.Open(filepath.Join(w.Directory(), "wire.raw"))
	if err != nil {
		t.Fatal(err)
	}
	defer rawFile.Close()
	rawReader, err := rawcapture.NewReader(rawFile)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := rawReader.Next()
	if err != nil || !bytes.Equal(raw, []byte{1, 2, 3, 4}) {
		t.Fatalf("raw = %x, %v", raw, err)
	}
	journalFile, err := os.Open(filepath.Join(w.Directory(), "transport.journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer journalFile.Close()
	journalReader, err := transportjournal.NewReader(journalFile)
	if err != nil {
		t.Fatal(err)
	}
	journalData, failures, err := transportjournal.Replay(journalReader, "stream")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(journalData, []byte{9, 8, 7}) || len(failures) != 0 {
		t.Fatalf("journal data=%x failures=%#v", journalData, failures)
	}
}
func TestWriterPersistsEveryTypedEventKind(t *testing.T) {
	w, err := Create(t.TempDir(), Manifest{RunID: "all-kinds"})
	if err != nil {
		t.Fatal(err)
	}
	kinds := []dt5202.EventKind{dt5202.EventSpectroscopy, dt5202.EventTiming, dt5202.EventCounting, dt5202.EventWaveform, dt5202.EventService, dt5202.EventTest}
	for i, kind := range kinds {
		wire := dt5215.StreamEvent{Chain: uint8(i % 4), Descriptor: dt5215.Descriptor{Qualifier: uint8(i + 1), TriggerID: uint64(100 + i), Timestamp: uint64(200 + i)}}
		if err := w.AppendEvent(wire, dt5202.Event{Kind: kind, Qualifier: uint8(i + 1)}); err != nil {
			t.Fatalf("append %s: %v", kind, err)
		}
	}
	if err := w.Finalize("2026-07-21T15:01:00Z", "complete"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(w.Directory(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	r := NewReader(bytes.NewReader(data), 0)
	for i, want := range kinds {
		envelope, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if envelope.Kind != string(want) || envelope.Sequence != uint64(i+1) {
			t.Fatalf("envelope %d = %+v", i, envelope)
		}
		var payload struct {
			TriggerID uint64       `json:"trigger_id,string"`
			Timestamp uint64       `json:"timestamp,string"`
			Event     dt5202.Event `json:"event"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Event.Kind != want || payload.TriggerID != uint64(100+i) || payload.Timestamp != uint64(200+i) {
			t.Fatalf("payload %d = %+v", i, payload)
		}
	}
}
func TestWriterRejectsUntypedEvent(t *testing.T) {
	w, err := Create(t.TempDir(), Manifest{RunID: "missing-kind"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Abort()
	if err := w.AppendEvent(dt5215.StreamEvent{}, dt5202.Event{}); err == nil {
		t.Fatal("accepted event without kind")
	}
}
func TestAbortLeavesIncompleteMarker(t *testing.T) {
	w, err := Create(t.TempDir(), Manifest{RunID: "interrupted"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(w.Directory(), "incomplete")); err != nil {
		t.Fatal(err)
	}
}
func TestReaderReportsTruncationAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
		max              int
	}{{"truncated", `{"schema_version":1}`, "truncated", 100}, {"oversize", strings.Repeat("x", 20) + "\n", "exceeds", 10}} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewReader(strings.NewReader(tc.data), tc.max).Next()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
