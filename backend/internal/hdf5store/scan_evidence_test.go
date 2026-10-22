//go:build hdf5

package hdf5store

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/scanrecord"
	hdf5 "github.com/next-exp/hdf5-go"
)

func TestScanEvidenceHDF5RoundTrip(t *testing.T) {
	for _, kind := range []string{"staircase", "hold_delay"} {
		t.Run(kind, func(t *testing.T) {
			at := time.Unix(123, 456).UTC()
			point := uint32(7)
			timestamp := uint64(999)
			temperature := 51.5
			records := []scanrecord.Record{
				{Type: "baseline", ObservedAt: at, Board: 3},
				{Type: "service", ObservedAt: at, Board: 3, Chain: 1, Node: 2, PointIndex: &point, HardwareTimestamp: &timestamp, CRCError: true, Service: &dt5202.ServiceEvent{BoardTemperature: &temperature}},
				{Type: "monitor", ObservedAt: at, Board: 3, Chain: 1, Node: 2, Monitor: &dt5202.HVTelemetry{FPGATemperatureC: 59, BoardTemperatureC: 50, DetectorTemperatureC: 25, HVTemperatureC: 49, VoltageV: 40.7, CurrentA: 3e-7, On: true}},
				{Type: "monitor", ObservedAt: at, Error: "unavailable"},
			}
			var journal bytes.Buffer
			for _, record := range records {
				if err := json.NewEncoder(&journal).Encode(record); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(t.TempDir(), "scan.h5")
			var err error
			if kind == "staircase" {
				err = WriteStaircase(path, []byte(`{}`), nil, journal.Bytes())
			} else {
				err = WriteHoldDelay(path, []byte(`{}`), nil, journal.Bytes())
			}
			if err != nil {
				t.Fatal(err)
			}
			file, err := hdf5.OpenFile(path, hdf5.F_ACC_RDONLY)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			dataset, err := file.OpenDataset("telemetry/samples")
			if err != nil {
				t.Fatal(err)
			}
			defer dataset.Close()
			var rows []scanTelemetryRow
			readDatasetRows(t, dataset, &rows, 2)
			s, m := rows[0], rows[1]
			if s.Validity != 2 || s.BoardTemperature != 51.5 || s.RecordIndex != 1 || s.HasPoint != 1 || s.PointIndex != 7 || s.HardwareTimestamp != 999 || s.CRCError != 1 || s.ObservedAtUnixNS != at.UnixNano() {
				t.Fatalf("service row=%+v", s)
			}
			if m.Validity != 0x3f || m.Source != 2 || m.RecordIndex != 2 || m.HasPoint != 0 || m.HasHardwareTimestamp != 0 || m.HVVoltage != 40.7 || m.HVCurrent != 3e-7 || m.DetectorTemperature != 25 || m.HVTemperature != 49 || m.FPGATemperature != 59 || m.HVOn != 1 {
				t.Fatalf("monitor row=%+v", m)
			}
			evidence, err := file.OpenDataset("evidence/records_jsonl")
			if err != nil {
				t.Fatal(err)
			}
			defer evidence.Close()
			got := make([]byte, journal.Len())
			if err := evidence.Read(&got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, journal.Bytes()) {
				t.Fatal("embedded evidence differs from durable journal")
			}
		})
	}
}
