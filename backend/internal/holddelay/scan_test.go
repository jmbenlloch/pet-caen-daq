package holddelay

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

type fakeHardware struct {
	registers    map[uint32]uint32
	events       []dt5215.StreamEvent
	commands     []uint32
	synchronized bool
}

func (f *fakeHardware) WriteRegister(_ context.Context, _, _ uint16, address, value uint32) error {
	f.registers[address] = value
	return nil
}
func (f *fakeHardware) ReadRegister(_ context.Context, _, _ uint16, address uint32) (uint32, error) {
	return f.registers[address], nil
}
func (f *fakeHardware) SendCommand(_ context.Context, _, _ uint16, command, _ uint32) error {
	f.commands = append(f.commands, command)
	return nil
}
func (f *fakeHardware) Synchronize(context.Context) error {
	f.synchronized = true
	return nil
}
func (*fakeHardware) ClearStream(context.Context) error { return nil }
func (f *fakeHardware) ReadRawStreamBatch(context.Context) ([]byte, []dt5215.StreamEvent, error) {
	return nil, f.events, nil
}

func spectroscopyEvent(high uint16) dt5215.StreamEvent {
	payload := make([]byte, 12)
	binary.LittleEndian.PutUint64(payload, 1)
	binary.LittleEndian.PutUint32(payload[8:], uint32(high)|uint32(7)<<16)
	return dt5215.StreamEvent{
		Chain:      1,
		Descriptor: dt5215.Descriptor{Node: 2, Qualifier: dt5202.QualifierSpectroscopy | dt5202.QualifierBothGains},
		Payload:    payload,
	}
}

func TestRequestValidationAndEnergyBinning(t *testing.T) {
	valid := Request{MinimumDelayNS: 0, MaximumDelayNS: 504, StepNS: 8, EventsPerDelay: 10, PointTimeout: time.Second}
	if err := valid.Validate(); err != nil || valid.PointCount() != 64 {
		t.Fatalf("valid request: count=%d err=%v", valid.PointCount(), err)
	}
	for _, invalid := range []Request{
		{MinimumDelayNS: 1, MaximumDelayNS: 8, StepNS: 8, EventsPerDelay: 10, PointTimeout: time.Second},
		{MinimumDelayNS: 0, MaximumDelayNS: 8, StepNS: 4, EventsPerDelay: 10, PointTimeout: time.Second},
		{MinimumDelayNS: 0, MaximumDelayNS: 8, StepNS: 8, EventsPerDelay: 9, PointTimeout: time.Second},
	} {
		if invalid.Validate() == nil {
			t.Fatalf("accepted invalid request %+v", invalid)
		}
	}
	if EnergyBin(0x1fff, false) != 511 || EnergyBin(0x3fff, true) != 511 || EnergyBin(160, false) != 10 {
		t.Fatal("unexpected JANUS-compatible energy bin")
	}
}

func TestRunCollectsHighGainDistributionsAtEveryDelay(t *testing.T) {
	hardware := &fakeHardware{registers: map[uint32]uint32{}, events: []dt5215.StreamEvent{spectroscopyEvent(160)}}
	request := Request{MinimumDelayNS: 0, MaximumDelayNS: 8, StepNS: 8, EventsPerDelay: 10, PointTimeout: time.Second}
	var points []Point
	if err := Run(context.Background(), hardware, 1, 2, request, false, func(point Point) error {
		points = append(points, point)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 || points[1].EffectiveDelay != 8 || points[0].HighGainBins[0][10] != 10 {
		t.Fatalf("points = %+v", points)
	}
	if len(hardware.commands) != 4 || hardware.commands[0] != dt5215.CommandAcquisitionStart || hardware.commands[1] != dt5215.CommandAcquisitionStop {
		t.Fatalf("commands = %v", hardware.commands)
	}
	if !hardware.synchronized {
		t.Fatal("hold-delay scan did not synchronize before acquisition")
	}
}
