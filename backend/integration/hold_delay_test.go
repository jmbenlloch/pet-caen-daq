//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/simulator"
)

func TestHoldDelayScanCollectsEventsFromSimulator(t *testing.T) {
	server, err := simulator.Start("127.0.0.1:0", "127.0.0.1:0", simulator.ProductionTopology())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := server.EnablePeriodicEvents(time.Millisecond); err != nil {
		t.Fatal(err)
	}
	client, err := dt5215.Dial(context.Background(), server.ControlAddress(), server.StreamAddress())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})

	request := holddelay.Request{
		MinimumDelayNS: 0,
		MaximumDelayNS: 8,
		StepNS:         8,
		EventsPerDelay: 10,
		PointTimeout:   time.Second,
	}
	var points []holddelay.Point
	if err := holddelay.Run(context.Background(), client, 0, 0, request, false, func(point holddelay.Point) error {
		points = append(points, point)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("collected %d points, want 2", len(points))
	}
	for index, point := range points {
		if point.EventsCollected != request.EventsPerDelay {
			t.Fatalf("point %d collected %d events, want %d", index, point.EventsCollected, request.EventsPerDelay)
		}
		var highGainEvents uint32
		for _, count := range point.HighGainBins[0] {
			highGainEvents += count
		}
		if highGainEvents != request.EventsPerDelay {
			t.Fatalf("point %d high-gain spectrum contains %d events, want %d", index, highGainEvents, request.EventsPerDelay)
		}
	}
}

func TestHoldDelayScanCollectsDefaultPointAtLocalSimulatorRate(t *testing.T) {
	server, err := simulator.Start("127.0.0.1:0", "127.0.0.1:0", simulator.ProductionTopology())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := server.EnablePeriodicEvents(10 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	client, err := dt5215.Dial(context.Background(), server.ControlAddress(), server.StreamAddress())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})

	request := holddelay.Request{
		MinimumDelayNS: 0,
		MaximumDelayNS: 0,
		StepNS:         8,
		EventsPerDelay: 100,
		PointTimeout:   3 * time.Second,
	}
	var points []holddelay.Point
	if err := holddelay.Run(context.Background(), client, 0, 0, request, false, func(point holddelay.Point) error {
		points = append(points, point)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].EventsCollected != request.EventsPerDelay {
		t.Fatalf("points=%+v, want one complete %d-event point", points, request.EventsPerDelay)
	}
}
