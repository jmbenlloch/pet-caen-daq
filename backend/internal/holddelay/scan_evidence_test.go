package holddelay

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/scanrecord"
)

func TestEvidenceRetainsServiceAfterQuotaAndMalformedPayload(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "malformed"}[malformed], func(t *testing.T) {
			payload := make([]byte, 4)
			binary.LittleEndian.PutUint32(payload, 1<<24|2500)
			if malformed {
				payload = []byte{1}
			}
			service := dt5215.StreamEvent{Chain: 1, Descriptor: dt5215.Descriptor{Node: 2, Qualifier: dt5202.QualifierService, Timestamp: 99}, Payload: payload}
			events := make([]dt5215.StreamEvent, 10)
			for i := range events {
				events[i] = spectroscopyEvent(160)
			}
			// Service event follows the final requested spectroscopy event in the batch.
			events = append(events, service)
			hardware := &fakeHardware{registers: map[uint32]uint32{}, events: events}
			var records []scanrecord.Record
			var collected uint32
			request := Request{Board: 7, StepNS: 8, EventsPerDelay: 10, PointTimeout: time.Second}
			err := Run(context.Background(), hardware, 1, 2, request, false, func(p Point) error { collected = p.EventsCollected; return nil }, func(r scanrecord.Record) error { records = append(records, r); return nil })
			if err != nil || collected != 10 {
				t.Fatalf("collected=%d err=%v", collected, err)
			}
			if len(records) != 2 || records[0].Type != "overrides" {
				t.Fatalf("records=%+v", records)
			}
			got := records[1]
			if got.Type != "service" || got.Board != 7 || got.PointIndex == nil || *got.PointIndex != 0 || got.HardwareTimestamp == nil || *got.HardwareTimestamp != 99 {
				t.Fatalf("service=%+v", got)
			}
			if malformed {
				if got.Error == "" || len(got.RawPayload) != 1 || got.Service != nil {
					t.Fatalf("failed decode not retained: %+v", got)
				}
			} else if got.Service == nil || got.Service.FPGATemperature == nil {
				t.Fatalf("telemetry missing: %+v", got)
			}
		})
	}
}

func TestServiceStorageFailureStopsAcquisition(t *testing.T) {
	service := dt5215.StreamEvent{Chain: 1, Descriptor: dt5215.Descriptor{Node: 2, Qualifier: dt5202.QualifierService}, Payload: make([]byte, 4)}
	hardware := &fakeHardware{registers: map[uint32]uint32{}, events: []dt5215.StreamEvent{service}}
	full := errors.New("disk full")
	err := Run(context.Background(), hardware, 1, 2, Request{StepNS: 8, EventsPerDelay: 10, PointTimeout: time.Second}, false, func(Point) error { return nil }, func(r scanrecord.Record) error {
		if r.Type == "service" {
			return full
		}
		return nil
	})
	if !errors.Is(err, full) {
		t.Fatalf("error=%v", err)
	}
	if len(hardware.commands) != 2 || hardware.commands[1] != dt5215.CommandAcquisitionStop {
		t.Fatalf("acquisition was not stopped: %v", hardware.commands)
	}
}
