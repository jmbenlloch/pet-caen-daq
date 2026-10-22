package scanrecord

import (
	"context"
	"errors"
	"testing"
)

type brokenMonitor struct{ reads int }

func (m *brokenMonitor) ReadRegister(context.Context, uint16, uint16, uint32) (uint32, error) {
	m.reads++
	return 0, errors.New("monitor unavailable")
}
func (*brokenMonitor) WriteRegister(context.Context, uint16, uint16, uint32, uint32) error {
	return errors.New("unexpected write")
}

func TestMonitorFailureIsRecordedWithoutFabricatedValues(t *testing.T) {
	hardware := &brokenMonitor{}
	var got Record
	err := SampleMonitor(context.Background(), hardware, func(r Record) error { got = r; return nil }, 7, 1, 2, "before_scan", nil)
	if err != nil || got.Monitor != nil || got.Error == "" || got.Board != 7 || got.Chain != 1 || got.Node != 2 || got.ObservedAt.IsZero() {
		t.Fatalf("record=%+v err=%v", got, err)
	}
	if hardware.reads != 1 {
		t.Fatalf("read attempts=%d", hardware.reads)
	}
}
func TestMonitorSinkFailurePropagates(t *testing.T) {
	full := errors.New("disk full")
	if err := SampleMonitor(context.Background(), &brokenMonitor{}, func(Record) error { return full }, 0, 0, 0, "after_point", nil); !errors.Is(err, full) {
		t.Fatalf("lost storage error: %v", err)
	}
}
