package runpipeline

import (
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/transportjournal"
)

type runWriter interface {
	Directory() string
	Artifacts() []runstore.Artifact
	EnableTransportJournal() error
	TransportJournal() transportjournal.Sink
	EnableRawCapture() error
	AppendRaw([]byte) error
	AppendEvent(dt5215.StreamEvent, dt5202.Event) error
	Finalize(completedAt, reason string) error
	Abort() error
}
