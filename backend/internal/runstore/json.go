// Package runstore provides the lightweight, streaming development run format.
package runstore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/configaudit"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/rawcapture"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/transportjournal"
)

const SchemaVersion = 1
const DefaultMaxRecordSize = 1 << 20

type Manifest struct {
	SchemaVersion      int                 `json:"schema_version"`
	RunID              string              `json:"run_id"`
	StartedAt          string              `json:"started_at"`
	CompletedAt        string              `json:"completed_at,omitempty"`
	TerminationReason  string              `json:"termination_reason,omitempty"`
	EventCount         uint64              `json:"event_count,string"`
	ConfigurationAudit *configaudit.Report `json:"configuration_audit,omitempty"`
}

type Envelope struct {
	SchemaVersion int             `json:"schema_version"`
	Kind          string          `json:"kind"`
	Sequence      uint64          `json:"sequence,string"`
	Payload       json.RawMessage `json:"payload"`
}

type Writer struct {
	dir      string
	events   *os.File
	manifest Manifest
	closed   bool
	raw      *rawcapture.Writer
	journal  *transportjournal.Writer
}

func Create(parent string, manifest Manifest) (*Writer, error) {
	if manifest.RunID == "" {
		return nil, errors.New("run ID is required")
	}
	manifest.SchemaVersion = SchemaVersion
	dir := filepath.Join(parent, "run-"+manifest.RunID)
	if err := os.Mkdir(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create run directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "incomplete"), []byte("run has not been finalized\n"), 0o640); err != nil {
		return nil, fmt.Errorf("create incomplete marker: %w", err)
	}
	w := &Writer{dir: dir, manifest: manifest}
	if err := w.writeManifest(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("create events file: %w", err)
	}
	w.events = f
	return w, nil
}
func (w *Writer) Directory() string { return w.dir }
func (w *Writer) EnableTransportJournal() error {
	if w.closed {
		return errors.New("run writer is closed")
	}
	if w.journal != nil {
		return errors.New("transport journal is already enabled")
	}
	file, err := os.OpenFile(filepath.Join(w.dir, "transport.journal"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("create transport journal: %w", err)
	}
	journal, err := transportjournal.NewWriter(file)
	if err != nil {
		file.Close()
		return err
	}
	w.journal = journal
	return nil
}
func (w *Writer) TransportJournal() transportjournal.Sink { return w.journal }
func (w *Writer) EnableRawCapture() error {
	if w.closed {
		return errors.New("run writer is closed")
	}
	if w.raw != nil {
		return errors.New("raw capture is already enabled")
	}
	f, err := os.OpenFile(filepath.Join(w.dir, "wire.raw"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("create raw capture: %w", err)
	}
	capture, err := rawcapture.NewWriter(f)
	if err != nil {
		f.Close()
		return err
	}
	w.raw = capture
	return nil
}
func (w *Writer) AppendRaw(batch []byte) error {
	if w.closed {
		return errors.New("run writer is closed")
	}
	if w.raw == nil {
		return errors.New("raw capture is not enabled")
	}
	return w.raw.Append(batch)
}
func (w *Writer) AppendDecoded(wire dt5215.StreamEvent, event dt5202.SpectroscopyEvent) error {
	payload, err := json.Marshal(struct {
		Chain     uint8                    `json:"chain"`
		Node      uint8                    `json:"node"`
		Qualifier uint8                    `json:"qualifier"`
		TriggerID uint64                   `json:"trigger_id,string"`
		Timestamp uint64                   `json:"timestamp,string"`
		Event     dt5202.SpectroscopyEvent `json:"event"`
	}{Chain: wire.Chain, Node: wire.Descriptor.Node, Qualifier: wire.Descriptor.Qualifier, TriggerID: wire.Descriptor.TriggerID, Timestamp: wire.Descriptor.Timestamp, Event: event})
	if err != nil {
		return fmt.Errorf("encode decoded event: %w", err)
	}
	return w.Append(Envelope{Kind: "spectroscopy_timing", Sequence: w.manifest.EventCount + 1, Payload: payload})
}

// AppendEvent persists any qualifier-dispatched project event while retaining
// the DT5215 descriptor identity required to correlate decoded and raw data.
func (w *Writer) AppendEvent(wire dt5215.StreamEvent, event dt5202.Event) error {
	if event.Kind == "" {
		return errors.New("decoded event kind is required")
	}
	payload, err := json.Marshal(struct {
		Chain     uint8        `json:"chain"`
		Node      uint8        `json:"node"`
		Qualifier uint8        `json:"qualifier"`
		TriggerID uint64       `json:"trigger_id,string"`
		Timestamp uint64       `json:"timestamp,string"`
		Event     dt5202.Event `json:"event"`
	}{Chain: wire.Chain, Node: wire.Descriptor.Node, Qualifier: wire.Descriptor.Qualifier, TriggerID: wire.Descriptor.TriggerID, Timestamp: wire.Descriptor.Timestamp, Event: event})
	if err != nil {
		return fmt.Errorf("encode decoded %s event: %w", event.Kind, err)
	}
	return w.Append(Envelope{Kind: string(event.Kind), Sequence: w.manifest.EventCount + 1, Payload: payload})
}
func (w *Writer) Append(e Envelope) error {
	if w.closed {
		return errors.New("run writer is closed")
	}
	if e.Kind == "" {
		return errors.New("event kind is required")
	}
	e.SchemaVersion = SchemaVersion
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	b = append(b, '\n')
	if _, err = w.events.Write(b); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	w.manifest.EventCount++
	return nil
}
func (w *Writer) Finalize(completedAt, reason string) error {
	if w.closed {
		return errors.New("run writer is closed")
	}
	w.closed = true
	if err := w.events.Sync(); err != nil {
		return fmt.Errorf("sync events: %w", err)
	}
	if err := w.events.Close(); err != nil {
		return fmt.Errorf("close events: %w", err)
	}
	if w.raw != nil {
		if err := w.raw.Close(); err != nil {
			return err
		}
	}
	if w.journal != nil {
		if err := w.journal.Close(); err != nil {
			return err
		}
	}
	w.manifest.CompletedAt = completedAt
	w.manifest.TerminationReason = reason
	if err := w.writeManifest(); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(w.dir, "incomplete")); err != nil {
		return fmt.Errorf("remove incomplete marker: %w", err)
	}
	return nil
}
func (w *Writer) Abort() error {
	if w.closed {
		return nil
	}
	w.closed = true
	eventsErr := w.events.Close()
	if w.raw != nil {
		if rawErr := w.raw.Close(); eventsErr == nil {
			eventsErr = rawErr
		}
	}
	if w.journal != nil {
		if journalErr := w.journal.Close(); eventsErr == nil {
			eventsErr = journalErr
		}
	}
	return eventsErr
}
func (w *Writer) writeManifest() error {
	b, err := json.MarshalIndent(w.manifest, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := filepath.Join(w.dir, "manifest.json.tmp")
	if err = os.WriteFile(tmp, b, 0o640); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err = os.Rename(tmp, filepath.Join(w.dir, "manifest.json")); err != nil {
		return fmt.Errorf("replace manifest: %w", err)
	}
	return nil
}

type Reader struct {
	r      *bufio.Reader
	max    int
	line   uint64
	offset int64
}

func NewReader(r io.Reader, maxRecordSize int) *Reader {
	if maxRecordSize <= 0 {
		maxRecordSize = DefaultMaxRecordSize
	}
	return &Reader{r: bufio.NewReaderSize(r, maxRecordSize+1), max: maxRecordSize}
}
func (r *Reader) Next() (Envelope, error) {
	start := r.offset
	b, err := r.r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return Envelope{}, fmt.Errorf("JSONL line %d at offset %d exceeds %d bytes", r.line+1, start, r.max)
	}
	if errors.Is(err, io.EOF) {
		if len(b) == 0 {
			return Envelope{}, io.EOF
		}
		return Envelope{}, fmt.Errorf("JSONL line %d at offset %d is truncated: %w", r.line+1, start, io.ErrUnexpectedEOF)
	}
	if err != nil {
		return Envelope{}, err
	}
	r.offset += int64(len(b))
	r.line++
	if len(b) > r.max {
		return Envelope{}, fmt.Errorf("JSONL line %d at offset %d exceeds %d bytes", r.line, start, r.max)
	}
	var e Envelope
	if err = json.Unmarshal(b, &e); err != nil {
		return Envelope{}, fmt.Errorf("JSONL line %d at offset %d: %w", r.line, start, err)
	}
	if e.SchemaVersion != SchemaVersion {
		return Envelope{}, fmt.Errorf("JSONL line %d: unsupported schema version %d", r.line, e.SchemaVersion)
	}
	if e.Kind == "" {
		return Envelope{}, fmt.Errorf("JSONL line %d: missing event kind", r.line)
	}
	return e, nil
}
