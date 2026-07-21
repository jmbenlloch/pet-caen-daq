// Package runpipeline connects the acquisition pipeline to development run storage.
package runpipeline

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/transportjournal"
)

type Options struct {
	Parent       string
	Capacity     int
	Backpressure acquisition.BackpressurePolicy
	Now          func() time.Time
}

type Factory struct{ Options Options }

func (f Factory) New(runID string, runOptions acquisition.RunOptions) (acquisition.RunPipeline, error) {
	options := f.Options
	if options.Parent == "" {
		return nil, fmt.Errorf("run storage parent is required")
	}
	if options.Capacity <= 0 {
		return nil, fmt.Errorf("run pipeline capacity must be positive")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	writer, err := runstore.Create(options.Parent, runstore.Manifest{
		RunID: runID, StartedAt: options.Now().UTC().Format(time.RFC3339Nano), RequestedBy: runOptions.RequestedBy,
		CaptureRaw: runOptions.CaptureRaw, JournalTransport: runOptions.JournalTransport,
		RequestedConfiguration: runOptions.RequestedConfiguration, EffectiveConfiguration: runOptions.EffectiveConfiguration,
		ConfigurationAudit: runOptions.ConfigurationAudit,
	})
	if err != nil {
		return nil, err
	}
	if runOptions.CaptureRaw {
		if err := writer.EnableRawCapture(); err != nil {
			_ = writer.Abort()
			return nil, err
		}
	}
	if runOptions.JournalTransport {
		if err := writer.EnableTransportJournal(); err != nil {
			_ = writer.Abort()
			return nil, err
		}
	}
	sink := &sink{writer: writer, captureRaw: runOptions.CaptureRaw, boards: make(map[boardKey]BoardStats)}
	pipeline, err := acquisition.NewPipeline(options.Capacity, options.Backpressure, sink)
	if err != nil {
		_ = writer.Abort()
		return nil, err
	}
	return &Session{pipeline: pipeline, writer: writer, sink: sink}, nil
}

type Session struct {
	pipeline  *acquisition.Pipeline
	writer    *runstore.Writer
	sink      *sink
	mu        sync.Mutex
	lastErr   error
	finalized bool
}

type StorageStats struct {
	Directory    string
	BytesWritten uint64
	EventCount   uint64
	RawBatches   uint64
	Finalized    bool
	LastError    string
}

func (s *Session) Submit(ctx context.Context, batch acquisition.PipelineBatch) error {
	err := s.pipeline.Submit(ctx, batch)
	s.recordError(err)
	return err
}

// Close drains and closes event processing; the coordinator then explicitly
// chooses Finalize or Abort so a processing failure cannot remove incomplete.
func (s *Session) Close() error {
	err := s.pipeline.Close()
	s.recordError(err)
	return err
}

func (s *Session) Finalize(completedAt, reason string) error {
	err := s.writer.Finalize(completedAt, reason)
	s.mu.Lock()
	if err != nil {
		s.lastErr = err
	} else {
		s.finalized = true
	}
	s.mu.Unlock()
	return err
}

func (s *Session) Abort() error {
	err := s.writer.Abort()
	s.recordError(err)
	return err
}

func (s *Session) Directory() string { return s.writer.Directory() }

func (s *Session) Stats() StorageStats {
	stats := StorageStats{Directory: s.Directory(), EventCount: s.sink.events.Load(), RawBatches: s.sink.rawBatches.Load()}
	_ = filepath.WalkDir(stats.Directory, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr == nil && info.Mode().IsRegular() {
			stats.BytesWritten += uint64(info.Size())
		}
		return nil
	})
	s.mu.Lock()
	stats.Finalized = s.finalized
	if s.lastErr != nil {
		stats.LastError = s.lastErr.Error()
	}
	s.mu.Unlock()
	return stats
}

func (s *Session) PipelineStats() acquisition.PipelineStats { return s.pipeline.Stats() }
func (s *Session) StorageStats() StorageStats               { return s.Stats() }
func (s *Session) BoardStats() []BoardStats                 { return s.sink.BoardStats() }
func (s *Session) TransportJournal() transportjournal.Sink  { return s.writer.TransportJournal() }
func (s *Session) Artifacts() []runstore.Artifact           { return s.writer.Artifacts() }

func (s *Session) recordError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.lastErr = err
	s.mu.Unlock()
}

type sink struct {
	writer     *runstore.Writer
	captureRaw bool
	events     atomic.Uint64
	rawBatches atomic.Uint64
	mu         sync.Mutex
	boards     map[boardKey]BoardStats
}

type boardKey struct{ chain, node uint8 }

type BoardStats struct {
	Chain               uint8
	Node                uint8
	EventCount          uint64
	FPGATemperature     *float64
	BoardTemperature    *float64
	DetectorTemperature *float64
	HVVoltage           *float64
	HVCurrent           *float64
	HVOn                bool
	HVRamping           bool
	HVOverCurrent       bool
	HVOverVoltage       bool
	AcquisitionStatus   *uint16
}

func (s *sink) AppendRaw(raw []byte) error {
	if s.captureRaw {
		if err := s.writer.AppendRaw(raw); err != nil {
			return err
		}
	}
	s.rawBatches.Add(1)
	return nil
}

func (s *sink) AppendEvent(wire dt5215.StreamEvent, event dt5202.Event) error {
	if err := s.writer.AppendEvent(wire, event); err != nil {
		return err
	}
	s.events.Add(1)
	s.mu.Lock()
	key := boardKey{chain: wire.Chain, node: wire.Descriptor.Node}
	board := s.boards[key]
	board.Chain, board.Node, board.EventCount = key.chain, key.node, board.EventCount+1
	if service := event.Service; service != nil {
		board.FPGATemperature = cloneFloat(service.FPGATemperature)
		board.BoardTemperature = cloneFloat(service.BoardTemperature)
		board.DetectorTemperature = cloneFloat(service.DetectorTemperature)
		board.HVVoltage = cloneFloat(service.HVVoltage)
		board.HVCurrent = cloneFloat(service.HVCurrent)
		board.HVOn, board.HVRamping = service.HVOn, service.HVRamping
		board.HVOverCurrent, board.HVOverVoltage = service.HVOverCurrent, service.HVOverVoltage
		if service.Status != nil {
			status := *service.Status
			board.AcquisitionStatus = &status
		}
	}
	s.boards[key] = board
	s.mu.Unlock()
	return nil
}

func (s *sink) BoardStats() []BoardStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]BoardStats, 0, len(s.boards))
	for _, board := range s.boards {
		board.FPGATemperature = cloneFloat(board.FPGATemperature)
		board.BoardTemperature = cloneFloat(board.BoardTemperature)
		board.DetectorTemperature = cloneFloat(board.DetectorTemperature)
		board.HVVoltage = cloneFloat(board.HVVoltage)
		board.HVCurrent = cloneFloat(board.HVCurrent)
		result = append(result, board)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Chain == result[j].Chain {
			return result[i].Node < result[j].Node
		}
		return result[i].Chain < result[j].Chain
	})
	return result
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
