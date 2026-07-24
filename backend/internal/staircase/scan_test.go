package staircase

import (
	"context"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

type fakeHardware struct {
	writes   []dt5202.RegisterWrite
	commands []uint32
	delays   []uint32
}

func (f *fakeHardware) WriteRegister(_ context.Context, _, _ uint16, address, value uint32) error {
	f.writes = append(f.writes, dt5202.RegisterWrite{Address: dt5202.Register(address), Value: value})
	return nil
}
func (f *fakeHardware) ReadRegister(_ context.Context, _, _ uint16, address uint32) (uint32, error) {
	if dt5202.Register(address) == dt5202.TimeORCount {
		return 10, nil
	}
	if dt5202.Register(address) == dt5202.ChargeORCount {
		return 20, nil
	}
	return 5, nil
}
func (f *fakeHardware) SendCommand(_ context.Context, _, _ uint16, command, delay uint32) error {
	f.commands = append(f.commands, command)
	f.delays = append(f.delays, delay)
	return nil
}

func TestRequestValidation(t *testing.T) {
	for _, request := range []Request{
		{Minimum: 2, Maximum: 1, Step: 1, Dwell: time.Millisecond},
		{Minimum: 0, Maximum: 1, Step: 0, Dwell: time.Millisecond},
		{Minimum: 0, Maximum: 2048, Step: 1, Dwell: time.Millisecond},
	} {
		if request.Validate() == nil {
			t.Fatalf("accepted invalid request %#v", request)
		}
	}
}

func TestRunScansDescendingAndReportsRates(t *testing.T) {
	hardware := &fakeHardware{}
	request := Request{Minimum: 2, Maximum: 4, Step: 1, Dwell: time.Millisecond}
	var points []Point
	if err := Run(context.Background(), hardware, 1, 2, request, 3, func(point Point) error {
		points = append(points, point)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(points) != 3 || points[0].Threshold != 4 || points[2].Threshold != 2 {
		t.Fatalf("points = %#v", points)
	}
	if points[0].ChannelCounts[63] != 5 || points[0].ChannelRatesCPS[63] != 5000 || points[0].TORRateCPS != 10000 {
		t.Fatalf("point = %#v", points[0])
	}
	if hardware.commands[0] != uint32(dt5202.CommandConfigureASIC) ||
		hardware.commands[1] != uint32(dt5202.CommandConfigureASIC) ||
		hardware.commands[2] != uint32(dt5202.CommandResetPeriodicTrigger) {
		t.Fatalf("commands = %#v", hardware.commands)
	}
	for index, delay := range hardware.delays {
		if delay != dt5215.TDLCommandDelay {
			t.Fatalf("command %d delay = %d, want %d", index, delay, dt5215.TDLCommandDelay)
		}
	}
}
