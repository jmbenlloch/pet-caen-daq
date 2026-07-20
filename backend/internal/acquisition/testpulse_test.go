package acquisition

import (
	"context"
	"errors"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"testing"
)

type fakeHardware struct {
	commands []uint32
	readErr  error
}

func (f *fakeHardware) Synchronize(context.Context) error { return nil }
func (f *fakeHardware) ClearStream(context.Context) error { return nil }
func (f *fakeHardware) SendCommand(_ context.Context, _, _ uint16, c, _ uint32) error {
	f.commands = append(f.commands, c)
	return nil
}
func (f *fakeHardware) ReadRawStreamBatch(context.Context) ([]byte, []dt5215.StreamEvent, error) {
	return nil, nil, f.readErr
}

type fakeSink struct{}

func (fakeSink) AppendRaw([]byte) error                                           { return nil }
func (fakeSink) AppendDecoded(dt5215.StreamEvent, dt5202.SpectroscopyEvent) error { return nil }
func TestRunTestPulseStopsAfterReadFailure(t *testing.T) {
	sentinel := errors.New("disconnect")
	hardware := &fakeHardware{readErr: sentinel}
	err := RunTestPulse(context.Background(), hardware, fakeSink{}, 4)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
	want := []uint32{dt5215.CommandAcquisitionStart, dt5215.CommandTestPulse, dt5215.CommandAcquisitionStop}
	if len(hardware.commands) != len(want) {
		t.Fatalf("commands = %v", hardware.commands)
	}
	for i := range want {
		if hardware.commands[i] != want[i] {
			t.Fatalf("commands = %v", hardware.commands)
		}
	}
}
func TestRunTestPulseValidatesChainCount(t *testing.T) {
	if err := RunTestPulse(context.Background(), &fakeHardware{}, fakeSink{}, 0); err == nil {
		t.Fatal("accepted zero chains")
	}
}
