//go:build hdf5

package hdf5store

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/rawcapture"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/transportjournal"
)

// RunWriter owns the production run directory and its HDF5 decoded artifact.
type RunWriter struct {
	dir            string
	events         *Writer
	manifest       runstore.Manifest
	raw            *rawcapture.Writer
	rawFile        *os.File
	rawEnabled     bool
	journal        *transportjournal.Writer
	journalFile    *os.File
	journalEnabled bool
	closed         bool
}

func CreateRun(parent string, manifest runstore.Manifest) (_ *RunWriter, err error) {
	if manifest.RunID == "" {
		return nil, errors.New("run ID is required")
	}
	manifest.SchemaVersion = runstore.SchemaVersion
	dir := filepath.Join(parent, "run-"+manifest.RunID)
	if err := os.Mkdir(dir, 0o750); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w: %s", runstore.ErrRunExists, manifest.RunID)
		}
		return nil, fmt.Errorf("create run directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "incomplete"), []byte("run has not been finalized\n"), 0o640); err != nil {
		return nil, fmt.Errorf("create incomplete marker: %w", err)
	}
	auditJSON, err := json.Marshal(manifest.ConfigurationAudit)
	if err != nil {
		return nil, fmt.Errorf("encode configuration audit: %w", err)
	}
	effectiveJSON, err := json.Marshal(manifest.EffectiveConfiguration)
	if err != nil {
		return nil, fmt.Errorf("encode effective configuration: %w", err)
	}
	metadataJSON, err := json.Marshal(struct {
		SchemaVersion         int                            `json:"schema_version"`
		RunID                 string                         `json:"run_id"`
		RequestedBy           string                         `json:"requested_by,omitempty"`
		StartedAt             string                         `json:"started_at"`
		CaptureRaw            bool                           `json:"capture_raw"`
		JournalTransport      bool                           `json:"journal_transport"`
		ConfigurationIdentity runstore.ConfigurationIdentity `json:"configuration_identity"`
		ExecutionIdentity     runstore.ExecutionIdentity     `json:"execution_identity"`
	}{
		SchemaVersion: runstore.SchemaVersion, RunID: manifest.RunID, RequestedBy: manifest.RequestedBy,
		StartedAt: manifest.StartedAt, CaptureRaw: manifest.CaptureRaw, JournalTransport: manifest.JournalTransport,
		ConfigurationIdentity: manifest.ConfigurationIdentity, ExecutionIdentity: manifest.ExecutionIdentity,
	})
	if err != nil {
		return nil, fmt.Errorf("encode run metadata: %w", err)
	}
	events, err := CreateWithMetadata(filepath.Join(dir, "events.h5"), Metadata{
		RunID: manifest.RunID, RequestedConfiguration: []byte(manifest.RequestedConfiguration),
		AuditJSON: auditJSON, EffectiveJSON: effectiveJSON, MetadataJSON: metadataJSON,
		EffectiveConfiguration: manifest.EffectiveConfiguration, Boards: manifest.ExecutionIdentity.Topology.Boards,
	})
	if err != nil {
		return nil, err
	}
	writer := &RunWriter{dir: dir, events: events, manifest: manifest}
	if err := writer.writeManifest(); err != nil {
		_ = writer.Abort()
		return nil, err
	}
	return writer, nil
}

func (w *RunWriter) Directory() string { return w.dir }

func (w *RunWriter) Artifacts() []runstore.Artifact {
	return append([]runstore.Artifact(nil), w.manifest.Artifacts...)
}

func (w *RunWriter) EnableRawCapture() error {
	if w.closed {
		return errors.New("run writer is closed")
	}
	if w.raw != nil {
		return errors.New("raw capture is already enabled")
	}
	file, err := os.OpenFile(filepath.Join(w.dir, "wire.raw"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("create raw capture: %w", err)
	}
	w.raw, err = rawcapture.NewWriter(file)
	if err != nil {
		file.Close()
	} else {
		w.rawFile = file
		w.rawEnabled = true
	}
	return err
}

func (w *RunWriter) AppendRaw(batch []byte) error {
	if w.closed {
		return errors.New("run writer is closed")
	}
	if w.raw == nil {
		return errors.New("raw capture is not enabled")
	}
	if err := w.raw.Append(batch); err != nil {
		return err
	}
	w.manifest.RawBatchCount++
	return nil
}

func (w *RunWriter) EnableTransportJournal() error {
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
	w.journal, err = transportjournal.NewWriter(file)
	if err != nil {
		file.Close()
	} else {
		w.journalFile = file
		w.journalEnabled = true
	}
	return err
}

func (w *RunWriter) TransportJournal() transportjournal.Sink { return w.journal }

func (w *RunWriter) AppendEvent(wire dt5215.StreamEvent, event dt5202.Event) error {
	if w.closed {
		return errors.New("run writer is closed")
	}
	if err := w.events.AppendEvent(wire, event); err != nil {
		return err
	}
	w.manifest.EventCount++
	return nil
}

func (w *RunWriter) Finalize(completedAt, reason string) (err error) {
	if w.closed {
		return errors.New("run writer is closed")
	}
	w.closed = true
	defer func() {
		if err != nil {
			err = errors.Join(err, w.closeOpenArtifacts())
		}
	}()
	if err = w.closeRaw(); err != nil {
		return err
	}
	if err = w.closeJournal(); err != nil {
		return err
	}
	w.manifest.CompletedAt = completedAt
	w.manifest.TerminationReason = reason
	internalManifest, err := json.Marshal(w.manifest)
	if err != nil {
		return fmt.Errorf("encode internal manifest: %w", err)
	}
	if err := w.events.Finalize(internalManifest); err != nil {
		return err
	}
	if err := Validate(filepath.Join(w.dir, "events.h5"), true); err != nil {
		return fmt.Errorf("validate finalized HDF5 artifact: %w", err)
	}
	w.manifest.Artifacts, err = w.finalizedArtifacts()
	if err != nil {
		return err
	}
	if err := w.writeManifest(); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(w.dir, "incomplete")); err != nil {
		return fmt.Errorf("remove incomplete marker: %w", err)
	}
	return nil
}

func (w *RunWriter) finalizedArtifacts() ([]runstore.Artifact, error) {
	names := []struct{ name, kind string }{{"events.h5", "decoded_events"}}
	if w.rawEnabled {
		names = append(names, struct{ name, kind string }{"wire.raw", "raw_capture"})
	}
	if w.journalEnabled {
		names = append(names, struct{ name, kind string }{"transport.journal", "transport_journal"})
	}
	artifacts := make([]runstore.Artifact, 0, len(names))
	for _, candidate := range names {
		file, err := os.Open(filepath.Join(w.dir, candidate.name))
		if err != nil {
			return nil, fmt.Errorf("open artifact %s: %w", candidate.name, err)
		}
		hash := sha256.New()
		size, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("hash artifact %s: %w", candidate.name, copyErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close artifact %s: %w", candidate.name, closeErr)
		}
		artifacts = append(artifacts, runstore.Artifact{
			Kind: candidate.kind, Name: candidate.name, SizeBytes: uint64(size), SHA256: fmt.Sprintf("%x", hash.Sum(nil)),
		})
	}
	return artifacts, nil
}

func (w *RunWriter) Abort() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.closeOpenArtifacts()
}

func (w *RunWriter) closeOpenArtifacts() error {
	return errors.Join(w.events.Close(), w.closeRaw(), w.closeJournal())
}

func (w *RunWriter) closeRaw() error {
	if w.raw == nil && w.rawFile == nil {
		return nil
	}
	var writerErr error
	if w.raw != nil {
		writerErr = w.raw.Close()
		w.raw = nil
	}
	fileErr := closeOSFile(w.rawFile)
	w.rawFile = nil
	return errors.Join(writerErr, fileErr)
}

func (w *RunWriter) closeJournal() error {
	if w.journal == nil && w.journalFile == nil {
		return nil
	}
	var writerErr error
	if w.journal != nil {
		writerErr = w.journal.Close()
		w.journal = nil
	}
	fileErr := closeOSFile(w.journalFile)
	w.journalFile = nil
	return errors.Join(writerErr, fileErr)
}

func closeOSFile(file *os.File) error {
	if file == nil {
		return nil
	}
	err := file.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

func (w *RunWriter) writeManifest() error {
	data, err := json.MarshalIndent(w.manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary := filepath.Join(w.dir, "manifest.json.tmp")
	if err := os.WriteFile(temporary, data, 0o640); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := os.Rename(temporary, filepath.Join(w.dir, "manifest.json")); err != nil {
		return fmt.Errorf("replace manifest: %w", err)
	}
	return nil
}
