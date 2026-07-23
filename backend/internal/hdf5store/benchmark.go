//go:build hdf5

package hdf5store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

type BenchmarkResult struct {
	Compression           string  `json:"compression"`
	EventCount            uint64  `json:"event_count"`
	PreloadSeconds        float64 `json:"preload_seconds"`
	AppendSeconds         float64 `json:"append_seconds"`
	FinalizeSeconds       float64 `json:"finalize_seconds"`
	ValidationSeconds     float64 `json:"validation_seconds"`
	AppendEventsPerSecond float64 `json:"append_events_per_second"`
	OutputBytes           int64   `json:"output_bytes"`
	HeapBytesAfterPreload uint64  `json:"heap_bytes_after_preload"`
	TotalAllocBytes       uint64  `json:"total_alloc_bytes"`
}

type benchmarkEvent struct {
	wire  dt5215.StreamEvent
	event dt5202.Event
}

// BenchmarkJSONRun preloads already-decoded project events before timing the
// direct HDF5 writer. JSON parsing is deliberately excluded from append rate.
func BenchmarkJSONRun(directory, output string) (BenchmarkResult, error) {
	var result BenchmarkResult
	compression, err := compressionName()
	if err != nil {
		return result, err
	}
	result.Compression = compression
	preloadStarted := time.Now()
	events, err := preloadJSONEvents(directory)
	if err != nil {
		return result, err
	}
	result.PreloadSeconds = time.Since(preloadStarted).Seconds()
	result.EventCount = uint64(len(events))
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	result.HeapBytesAfterPreload = memory.HeapAlloc
	result.TotalAllocBytes = memory.TotalAlloc

	writer, err := CreateWithMetadata(output, Metadata{
		RunID:        "direct-writer-benchmark",
		MetadataJSON: []byte(`{"purpose":"direct HDF5 writer benchmark"}`),
	})
	if err != nil {
		return result, err
	}
	finished := false
	defer func() {
		if !finished {
			_ = writer.Close()
		}
	}()
	appendStarted := time.Now()
	for index, item := range events {
		if err := writer.AppendEvent(item.wire, item.event); err != nil {
			return result, fmt.Errorf("append benchmark event %d: %w", index+1, err)
		}
	}
	result.AppendSeconds = time.Since(appendStarted).Seconds()
	if result.AppendSeconds > 0 {
		result.AppendEventsPerSecond = float64(result.EventCount) / result.AppendSeconds
	}
	finalizeStarted := time.Now()
	manifest, err := json.Marshal(struct {
		SchemaVersion int    `json:"schema_version"`
		RunID         string `json:"run_id"`
		EventCount    uint64 `json:"event_count,string"`
	}{
		SchemaVersion: runstore.SchemaVersion,
		RunID:         "direct-writer-benchmark",
		EventCount:    result.EventCount,
	})
	if err != nil {
		return result, err
	}
	if err := writer.Finalize(manifest); err != nil {
		return result, err
	}
	finished = true
	result.FinalizeSeconds = time.Since(finalizeStarted).Seconds()
	validationStarted := time.Now()
	if err := Validate(output, true); err != nil {
		return result, err
	}
	result.ValidationSeconds = time.Since(validationStarted).Seconds()
	info, err := os.Stat(output)
	if err != nil {
		return result, err
	}
	result.OutputBytes = info.Size()
	return result, nil
}

func preloadJSONEvents(directory string) ([]benchmarkEvent, error) {
	runID := filepath.Base(directory)
	if len(runID) <= 4 || runID[:4] != "run-" {
		return nil, errors.New("source directory name must start with run-")
	}
	manifest, err := runstore.ReadManifest(directory, runID[4:])
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := runstore.NewReader(file, runstore.DefaultMaxRecordSize)
	maxInt := uint64(^uint(0) >> 1)
	if manifest.EventCount > maxInt {
		return nil, fmt.Errorf("event count %d exceeds addressable slice capacity", manifest.EventCount)
	}
	events := make([]benchmarkEvent, 0, int(manifest.EventCount))
	for {
		envelope, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
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
			return nil, fmt.Errorf("decode event %d: %w", len(events)+1, err)
		}
		events = append(events, benchmarkEvent{
			wire: dt5215.StreamEvent{Chain: payload.Chain, Descriptor: dt5215.Descriptor{
				Node: payload.Node, Qualifier: payload.Qualifier,
				TriggerID: payload.TriggerID, Timestamp: payload.Timestamp,
			}},
			event: payload.Event,
		})
	}
	if uint64(len(events)) != manifest.EventCount {
		return nil, fmt.Errorf("preloaded %d events, manifest records %d", len(events), manifest.EventCount)
	}
	return events, nil
}
