// Package runpipeline connects the acquisition pipeline to development run storage.
package runpipeline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
	Parent            string
	Capacity          int
	Backpressure      acquisition.BackpressurePolicy
	ExecutionIdentity runstore.ExecutionIdentity
	Now               func() time.Time
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
	identity := options.ExecutionIdentity
	identity.Storage = storageIdentity()
	identity.Runtime = runstore.RuntimeIdentity{
		PipelineCapacity: options.Capacity, BackpressurePolicy: backpressureName(options.Backpressure),
		CaptureRaw: runOptions.CaptureRaw, JournalTransport: runOptions.JournalTransport,
		EnergyHistogramBins: runOptions.Histograms.EnergyBins, ToAHistogramBins: runOptions.Histograms.ToABins,
		ToTHistogramBins:     runOptions.Histograms.ToTBins,
		HDF5SegmentSizeBytes: runOptions.HDF5SegmentSizeBytes,
	}
	configurationIdentity, err := configurationIdentity(runOptions)
	if err != nil {
		return nil, err
	}
	writer, err := createRunWriter(options.Parent, runstore.Manifest{
		RunID: runID, StartedAt: options.Now().UTC().Format(time.RFC3339Nano), RequestedBy: runOptions.RequestedBy,
		CaptureRaw: runOptions.CaptureRaw, JournalTransport: runOptions.JournalTransport,
		HDF5SegmentSizeBytes:   runOptions.HDF5SegmentSizeBytes,
		RequestedConfiguration: runOptions.RequestedConfiguration, EffectiveConfiguration: runOptions.EffectiveConfiguration,
		ConfigurationAudit: runOptions.ConfigurationAudit, ConfigurationIdentity: configurationIdentity, ExecutionIdentity: identity,
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
	sink := &sink{writer: writer, captureRaw: runOptions.CaptureRaw, boards: make(map[boardKey]BoardStats), now: options.Now, startedAt: options.Now(), histogramOptions: runOptions.Histograms, histograms: make(map[histogramKey]*histogramAccumulator)}
	pipeline, err := acquisition.NewPipeline(options.Capacity, options.Backpressure, sink)
	if err != nil {
		_ = writer.Abort()
		return nil, err
	}
	return &Session{pipeline: pipeline, writer: writer, sink: sink}, nil
}

func configurationIdentity(options acquisition.RunOptions) (runstore.ConfigurationIdentity, error) {
	effective, err := json.Marshal(options.EffectiveConfiguration)
	if err != nil {
		return runstore.ConfigurationIdentity{}, fmt.Errorf("encode effective configuration identity: %w", err)
	}
	audit, err := json.Marshal(options.ConfigurationAudit)
	if err != nil {
		return runstore.ConfigurationIdentity{}, fmt.Errorf("encode configuration audit identity: %w", err)
	}
	requestedHash := sha256.Sum256([]byte(options.RequestedConfiguration))
	effectiveHash := sha256.Sum256(effective)
	auditHash := sha256.Sum256(audit)
	auditVersion := 0
	if options.ConfigurationAudit != nil {
		auditVersion = options.ConfigurationAudit.SchemaVersion
	}
	return runstore.ConfigurationIdentity{
		ParserVersion: 1, AuditSchemaVersion: auditVersion,
		RequestedConfigurationSHA256: fmt.Sprintf("%x", requestedHash),
		EffectiveConfigurationSHA256: fmt.Sprintf("%x", effectiveHash),
		ConfigurationAuditSHA256:     fmt.Sprintf("%x", auditHash),
	}, nil
}

func backpressureName(policy acquisition.BackpressurePolicy) string {
	if policy == acquisition.BackpressureReject {
		return "reject"
	}
	return "block"
}

type Session struct {
	pipeline  *acquisition.Pipeline
	writer    runWriter
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
func (s *Session) Done() <-chan struct{}                    { return s.pipeline.Done() }
func (s *Session) Err() error                               { return s.pipeline.Err() }

func (s *Session) recordError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.lastErr = err
	s.mu.Unlock()
}

type sink struct {
	writer           runWriter
	captureRaw       bool
	events           atomic.Uint64
	rawBatches       atomic.Uint64
	mu               sync.Mutex
	boards           map[boardKey]BoardStats
	now              func() time.Time
	startedAt        time.Time
	histogramOptions acquisition.HistogramOptions
	histograms       map[histogramKey]*histogramAccumulator
}

type boardKey struct{ chain, node uint8 }

type BoardStats struct {
	Chain               uint8
	Node                uint8
	EventCount          uint64
	FPGATemperature     *float64
	BoardTemperature    *float64
	DetectorTemperature *float64
	HVTemperature       *float64
	HVVoltage           *float64
	HVCurrent           *float64
	HVOn                bool
	HVRamping           bool
	HVOverCurrent       bool
	HVOverVoltage       bool
	AcquisitionStatus   *uint16
	TelemetryObservedAt *time.Time
	Timestamp           uint64
	TriggerID           uint64
	TriggerCount        uint64
	LostTriggerCount    uint64
	EventBuildCount     uint64
	DataBytes           uint64
	ChannelTriggerCount [dt5202.ChannelCount]uint64
	TimestampCount      [dt5202.ChannelCount]uint64
	PHACount            [dt5202.ChannelCount]uint64
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
	board.DataBytes += uint64(len(wire.Payload))
	if event.Kind != dt5202.EventService {
		board.EventBuildCount++
		board.TriggerCount++
		if board.TriggerCount > 1 && wire.Descriptor.TriggerID > board.TriggerID+1 {
			board.LostTriggerCount += wire.Descriptor.TriggerID - board.TriggerID - 1
		}
		board.TriggerID = wire.Descriptor.TriggerID
		board.Timestamp = wire.Descriptor.Timestamp
	}
	accumulateChannels(&board, event)
	s.accumulateHistograms(key.chain, key.node, event)
	if service := event.Service; service != nil {
		observedAt := s.now()
		board.TelemetryObservedAt = &observedAt
		board.FPGATemperature = cloneFloat(service.FPGATemperature)
		board.BoardTemperature = cloneFloat(service.BoardTemperature)
		board.DetectorTemperature = cloneFloat(service.DetectorTemperature)
		board.HVTemperature = cloneFloat(service.HVTemperature)
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

func accumulateChannels(board *BoardStats, event dt5202.Event) {
	switch event.Kind {
	case dt5202.EventSpectroscopy:
		for _, energy := range event.Spectroscopy.Energies {
			board.PHACount[energy.Channel]++
			board.TimestampCount[energy.Channel]++
			if energy.Discriminator {
				board.ChannelTriggerCount[energy.Channel]++
			}
		}
	case dt5202.EventTiming:
		for _, hit := range event.Timing.Hits {
			board.ChannelTriggerCount[hit.Channel]++
			board.TimestampCount[hit.Channel]++
		}
	case dt5202.EventCounting:
		for _, count := range event.Counting.Counts {
			board.ChannelTriggerCount[count.Channel] += uint64(count.Value)
		}
	case dt5202.EventService:
		for _, counter := range event.Service.Counters {
			board.ChannelTriggerCount[counter.Channel] += uint64(counter.Value)
		}
	}
}

func (s *Session) StatisticsElapsed() time.Duration {
	return s.sink.now().Sub(s.sink.startedAt)
}

func (s *sink) BoardStats() []BoardStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]BoardStats, 0, len(s.boards))
	for _, board := range s.boards {
		board.TelemetryObservedAt = cloneTime(board.TelemetryObservedAt)
		board.FPGATemperature = cloneFloat(board.FPGATemperature)
		board.BoardTemperature = cloneFloat(board.BoardTemperature)
		board.DetectorTemperature = cloneFloat(board.DetectorTemperature)
		board.HVTemperature = cloneFloat(board.HVTemperature)
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

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
