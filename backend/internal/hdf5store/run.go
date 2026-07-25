//go:build hdf5

package hdf5store

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/rawcapture"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/transportjournal"
)

// RunWriter owns the production run directory and its HDF5 decoded artifact.
const DefaultSegmentSizeBytes uint64 = 500 << 20
const (
	bufferedAcquisitionBatches = 8
	bufferedEventPayloadBytes  = 16 << 20
)

type RunWriter struct {
	rawMu          sync.Mutex
	eventMu        sync.Mutex
	dir            string
	events         *Writer
	metadata       Metadata
	manifest       runstore.Manifest
	segmentSize    uint64
	segmentIndex   uint32
	segmentNames   []string
	histogramName  string
	raw            *rawcapture.Writer
	rawFile        *os.File
	rawEnabled     bool
	journal        *transportjournal.Writer
	journalFile    *os.File
	journalEnabled bool
	closed         bool
	pendingEvents  []EventRecord
	pendingBatches int
	pendingBytes   uint64
}

func CreateRun(parent string, manifest runstore.Manifest) (_ *RunWriter, err error) {
	if manifest.RunID == "" {
		return nil, errors.New("run ID is required")
	}
	manifest.SchemaVersion = runstore.SchemaVersion
	if manifest.HDF5SegmentSizeBytes == 0 {
		manifest.HDF5SegmentSizeBytes = DefaultSegmentSizeBytes
		manifest.ExecutionIdentity.Runtime.HDF5SegmentSizeBytes = DefaultSegmentSizeBytes
	}
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
	metadata := Metadata{
		RunID: manifest.RunID, RequestedConfiguration: []byte(manifest.RequestedConfiguration),
		AuditJSON: auditJSON, EffectiveJSON: effectiveJSON, MetadataJSON: metadataJSON,
		EffectiveConfiguration: manifest.EffectiveConfiguration, Boards: manifest.ExecutionIdentity.Topology.Boards,
	}
	writer := &RunWriter{
		dir: dir, metadata: metadata, manifest: manifest, segmentSize: manifest.HDF5SegmentSizeBytes,
	}
	if err := writer.openSegment(); err != nil {
		return nil, err
	}
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

func (w *RunWriter) SaveHistograms(histograms []runstore.HistogramDataset) error {
	if w.closed {
		return errors.New("run writer is closed")
	}
	if w.histogramName != "" {
		return errors.New("histograms are already saved")
	}
	name := fmt.Sprintf("run_%s.histograms.h5", w.manifest.RunID)
	temporary := filepath.Join(w.dir, name+".tmp")
	if err := saveHistograms(temporary, w.manifest.RunID, histograms, w.manifest.ExecutionIdentity.Storage.Compression); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, filepath.Join(w.dir, name)); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish histogram artifact: %w", err)
	}
	w.histogramName = name
	return nil
}

func (w *RunWriter) SaveStatistics(statistics runstore.RunStatistics) error {
	if w.closed {
		return errors.New("run writer is closed")
	}
	if w.manifest.Statistics != nil {
		return errors.New("run statistics are already saved")
	}
	copy := statistics
	copy.Boards = append([]runstore.BoardStatistics(nil), statistics.Boards...)
	w.manifest.Statistics = &copy
	return nil
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
	w.rawMu.Lock()
	defer w.rawMu.Unlock()
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
	w.eventMu.Lock()
	defer w.eventMu.Unlock()
	if w.closed {
		return errors.New("run writer is closed")
	}
	if err := w.flushEvents(); err != nil {
		return err
	}
	if w.events == nil {
		if err := w.openSegment(); err != nil {
			return err
		}
	}
	if err := w.events.AppendEvent(wire, event); err != nil {
		return err
	}
	w.manifest.EventCount++
	return w.rotateIfNeeded()
}

// AppendEvents buffers several decoded acquisition batches so HDF5 dataset
// extents and hyperslabs are updated in groups rather than once per event.
func (w *RunWriter) AppendEvents(events []acquisition.DecodedEvent) error {
	w.eventMu.Lock()
	defer w.eventMu.Unlock()
	if w.closed {
		return errors.New("run writer is closed")
	}
	for _, item := range events {
		w.pendingEvents = append(w.pendingEvents, EventRecord{Wire: item.Wire, Event: item.Event})
		w.pendingBytes += uint64(len(item.Wire.Payload))
	}
	w.pendingBatches++
	if w.pendingBatches < bufferedAcquisitionBatches && w.pendingBytes < bufferedEventPayloadBytes {
		return nil
	}
	return w.flushEvents()
}

func (w *RunWriter) flushEvents() error {
	if len(w.pendingEvents) == 0 {
		w.pendingBatches = 0
		return nil
	}
	if w.events == nil {
		if err := w.openSegment(); err != nil {
			return err
		}
	}
	if err := w.events.AppendEvents(w.pendingEvents); err != nil {
		return err
	}
	w.manifest.EventCount += uint64(len(w.pendingEvents))
	w.pendingEvents = w.pendingEvents[:0]
	w.pendingBatches = 0
	w.pendingBytes = 0
	return w.rotateIfNeeded()
}

func (w *RunWriter) rotateIfNeeded() error {
	info, err := os.Stat(filepath.Join(w.dir, w.currentSegmentName()))
	if err != nil {
		return fmt.Errorf("stat HDF5 segment: %w", err)
	}
	if uint64(info.Size()) >= w.segmentSize {
		if err := w.finalizeSegment(); err != nil {
			return fmt.Errorf("rotate HDF5 segment: %w", err)
		}
	}
	return nil
}

func (w *RunWriter) Finalize(completedAt, reason string) (err error) {
	finalizeStarted := time.Now()
	if w.closed {
		return errors.New("run writer is closed")
	}
	w.closed = true
	defer func() {
		if err != nil {
			err = errors.Join(err, w.closeOpenArtifacts())
		}
	}()
	w.eventMu.Lock()
	stageStarted := time.Now()
	err = w.flushEvents()
	w.eventMu.Unlock()
	w.logTiming("flush_pending_events", stageStarted, "events", w.manifest.EventCount)
	if err != nil {
		return err
	}
	stageStarted = time.Now()
	if err = w.closeRaw(); err != nil {
		return err
	}
	w.logTiming("close_raw", stageStarted, "batches", w.manifest.RawBatchCount)
	stageStarted = time.Now()
	if err = w.closeJournal(); err != nil {
		return err
	}
	w.logTiming("close_journal", stageStarted)
	w.manifest.CompletedAt = completedAt
	w.manifest.TerminationReason = reason
	if w.events != nil {
		stageStarted = time.Now()
		if err := w.finalizeSegment(); err != nil {
			return err
		}
		w.logTiming("finalize_hdf5_segment", stageStarted, "segments", len(w.segmentNames))
	}
	stageStarted = time.Now()
	w.manifest.Artifacts, err = w.finalizedArtifacts()
	if err != nil {
		return err
	}
	w.logTiming("hash_artifacts", stageStarted, "artifacts", len(w.manifest.Artifacts))
	stageStarted = time.Now()
	if err := w.writeManifest(); err != nil {
		return err
	}
	w.logTiming("write_manifest", stageStarted)
	if err := os.Remove(filepath.Join(w.dir, "incomplete")); err != nil {
		return fmt.Errorf("remove incomplete marker: %w", err)
	}
	w.logTiming("hdf5_run_finalize", finalizeStarted, "events", w.manifest.EventCount, "batches", w.manifest.RawBatchCount, "artifacts", len(w.manifest.Artifacts))
	return nil
}

func (w *RunWriter) finalizedArtifacts() ([]runstore.Artifact, error) {
	names := make([]struct{ name, kind string }, 0, len(w.segmentNames)+2)
	for _, name := range w.segmentNames {
		names = append(names, struct{ name, kind string }{name, "decoded_events"})
	}
	if w.histogramName != "" {
		names = append(names, struct{ name, kind string }{w.histogramName, "histograms"})
	}
	if w.rawEnabled {
		names = append(names, struct{ name, kind string }{"wire.raw", "raw_capture"})
	}
	if w.journalEnabled {
		names = append(names, struct{ name, kind string }{"transport.journal", "transport_journal"})
	}
	artifacts := make([]runstore.Artifact, 0, len(names))
	for _, candidate := range names {
		started := time.Now()
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
		w.logTiming("hash_artifact", started, "artifact", candidate.name, "bytes", size)
	}
	return artifacts, nil
}

func (w *RunWriter) logTiming(stage string, started time.Time, fields ...any) {
	message := fmt.Sprintf("run_timing run_id=%s stage=%s duration_ms=%d", w.manifest.RunID, stage, time.Since(started).Milliseconds())
	for index := 0; index+1 < len(fields); index += 2 {
		message += fmt.Sprintf(" %v=%v", fields[index], fields[index+1])
	}
	log.Print(message)
}

func (w *RunWriter) Abort() error {
	if w.closed {
		return nil
	}
	w.closed = true
	w.eventMu.Lock()
	flushErr := w.flushEvents()
	w.eventMu.Unlock()
	if flushErr != nil {
		return errors.Join(flushErr, w.closeOpenArtifacts())
	}
	return w.closeOpenArtifacts()
}

func (w *RunWriter) closeOpenArtifacts() error {
	var eventsErr error
	if w.events != nil {
		eventsErr = w.events.Close()
		w.events = nil
	}
	return errors.Join(eventsErr, w.closeRaw(), w.closeJournal())
}

func (w *RunWriter) currentSegmentName() string {
	return fmt.Sprintf("run_%s.%04d.h5", w.manifest.RunID, w.segmentIndex)
}

func (w *RunWriter) openSegment() error {
	if w.events != nil {
		return errors.New("HDF5 segment is already open")
	}
	metadata := w.metadata
	metadata.SegmentIndex = w.segmentIndex
	metadata.EventSequenceBase = w.manifest.EventCount
	metadata.Compression = w.manifest.ExecutionIdentity.Storage.Compression
	events, err := CreateWithMetadata(filepath.Join(w.dir, w.currentSegmentName()), metadata)
	if err != nil {
		return fmt.Errorf("create HDF5 segment %04d: %w", w.segmentIndex, err)
	}
	w.events = events
	return nil
}

func (w *RunWriter) finalizeSegment() error {
	if w.events == nil {
		return nil
	}
	internalManifest, err := json.Marshal(w.manifest)
	if err != nil {
		return fmt.Errorf("encode internal manifest: %w", err)
	}
	name := w.currentSegmentName()
	if err := w.events.Finalize(internalManifest); err != nil {
		w.events = nil
		return err
	}
	w.events = nil
	w.segmentNames = append(w.segmentNames, name)
	w.segmentIndex++
	return nil
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
