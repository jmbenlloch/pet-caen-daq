// Package runpipeline connects the acquisition pipeline to development run storage.
package runpipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

type Options struct {
	Parent       string
	CaptureRaw   bool
	Capacity     int
	Backpressure acquisition.BackpressurePolicy
	Now          func() time.Time
}

type Factory struct{ Options Options }

func (f Factory) New(runID string) (acquisition.RunPipeline, error) {
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
	writer, err := runstore.Create(options.Parent, runstore.Manifest{RunID: runID, StartedAt: options.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return nil, err
	}
	if options.CaptureRaw {
		if err := writer.EnableRawCapture(); err != nil {
			_ = writer.Abort()
			return nil, err
		}
	}
	sink := &sink{writer: writer, captureRaw: options.CaptureRaw}
	pipeline, err := acquisition.NewPipeline(options.Capacity, options.Backpressure, sink)
	if err != nil {
		_ = writer.Abort()
		return nil, err
	}
	return &Session{pipeline: pipeline, writer: writer}, nil
}

type Session struct {
	pipeline *acquisition.Pipeline
	writer   *runstore.Writer
}

func (s *Session) Submit(ctx context.Context, batch acquisition.PipelineBatch) error {
	return s.pipeline.Submit(ctx, batch)
}

// Close drains and closes event processing; the coordinator then explicitly
// chooses Finalize or Abort so a processing failure cannot remove incomplete.
func (s *Session) Close() error { return s.pipeline.Close() }

func (s *Session) Finalize(completedAt, reason string) error {
	return s.writer.Finalize(completedAt, reason)
}

func (s *Session) Abort() error { return s.writer.Abort() }

func (s *Session) Directory() string { return s.writer.Directory() }

type sink struct {
	writer     *runstore.Writer
	captureRaw bool
}

func (s *sink) AppendRaw(raw []byte) error {
	if !s.captureRaw {
		return nil
	}
	return s.writer.AppendRaw(raw)
}

func (s *sink) AppendEvent(wire dt5215.StreamEvent, event dt5202.Event) error {
	return s.writer.AppendEvent(wire, event)
}
