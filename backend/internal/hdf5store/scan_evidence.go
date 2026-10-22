//go:build hdf5

package hdf5store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"unsafe"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/scanrecord"
	hdf5 "github.com/next-exp/hdf5-go"
)

// RecordIndex joins a sample back to the full evidence JSONL. Validity uses the
// same bits as ordinary service rows; monitor reads provide all six floats,
// but do not expose the separate board-status word (bit 6).
type scanTelemetryRow struct {
	RecordIndex                                                           uint64
	ObservedAtUnixNS                                                      int64
	HardwareTimestamp                                                     uint64
	Board                                                                 uint32
	PointIndex                                                            uint32
	Chain, Node                                                           uint16
	Source, HasPoint, HasHardwareTimestamp, CRCError                      uint8
	Validity                                                              uint8
	FPGATemperature, BoardTemperature, DetectorTemperature, HVTemperature float64
	HVVoltage, HVCurrent                                                  float64
	HVOn, HVRamping, HVOverCurrent, HVOverVoltage                         uint8
	Status                                                                uint16
}

func writeScanEvidence(file *hdf5.File, chunks [][]byte) error {
	if len(chunks) == 0 {
		return nil
	}
	data := bytes.Join(chunks, nil)
	group, err := file.CreateGroup("evidence")
	if err != nil {
		return err
	}
	defer group.Close()
	if err = createBytes(group, "records_jsonl", data); err != nil {
		return err
	}
	telemetry, err := file.CreateGroup("telemetry")
	if err != nil {
		return err
	}
	defer telemetry.Close()
	rows := []scanTelemetryRow{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// Configuration plans can exceed bufio's default 64 KiB token limit.
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var index uint64
	for scanner.Scan() {
		var record scanrecord.Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("scan evidence row %d: %w", index, err)
		}
		if record.Service != nil || record.Monitor != nil {
			row := scanTelemetryRow{RecordIndex: index, ObservedAtUnixNS: record.ObservedAt.UnixNano(), Board: record.Board, Chain: record.Chain, Node: record.Node, CRCError: boolean(record.CRCError)}
			if record.PointIndex != nil {
				row.HasPoint = 1
				row.PointIndex = *record.PointIndex
			}
			if record.HardwareTimestamp != nil {
				row.HasHardwareTimestamp = 1
				row.HardwareTimestamp = *record.HardwareTimestamp
			}
			if service := record.Service; service != nil {
				row.Source = 1
				setOptionalFloat(&row.Validity, 0, service.FPGATemperature, &row.FPGATemperature)
				setOptionalFloat(&row.Validity, 1, service.BoardTemperature, &row.BoardTemperature)
				setOptionalFloat(&row.Validity, 2, service.DetectorTemperature, &row.DetectorTemperature)
				setOptionalFloat(&row.Validity, 3, service.HVTemperature, &row.HVTemperature)
				setOptionalFloat(&row.Validity, 4, service.HVVoltage, &row.HVVoltage)
				setOptionalFloat(&row.Validity, 5, service.HVCurrent, &row.HVCurrent)
				if service.Status != nil {
					row.Validity |= 1 << 6
					row.Status = *service.Status
				}
				row.HVOn = boolean(service.HVOn)
				row.HVRamping = boolean(service.HVRamping)
				row.HVOverCurrent = boolean(service.HVOverCurrent)
				row.HVOverVoltage = boolean(service.HVOverVoltage)
			}
			if monitor := record.Monitor; monitor != nil {
				row.Source = 2
				row.Validity = 0x3f
				row.FPGATemperature = monitor.FPGATemperatureC
				row.BoardTemperature = monitor.BoardTemperatureC
				row.DetectorTemperature = monitor.DetectorTemperatureC
				row.HVTemperature = monitor.HVTemperatureC
				row.HVVoltage = monitor.VoltageV
				row.HVCurrent = monitor.CurrentA
				row.HVOn = boolean(monitor.On)
				row.HVRamping = boolean(monitor.Ramping)
				row.HVOverCurrent = boolean(monitor.OverCurrent)
				row.HVOverVoltage = boolean(monitor.OverVoltage)
			}
			rows = append(rows, row)
		}
		index++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	dataset, err := createTable(telemetry, "samples", compoundScanTelemetry(), "none")
	if err != nil {
		return err
	}
	defer dataset.Close()
	target := table{dataset: dataset}
	return appendRows(&target, rows)
}

func compoundScanTelemetry() *hdf5.CompoundType {
	var v scanTelemetryRow
	return mustCompound(unsafe.Sizeof(v), []field{
		{"record_index", unsafe.Offsetof(v.RecordIndex), hdf5.T_STD_U64LE},
		{"observed_at_unix_ns", unsafe.Offsetof(v.ObservedAtUnixNS), hdf5.T_STD_I64LE},
		{"hardware_timestamp", unsafe.Offsetof(v.HardwareTimestamp), hdf5.T_STD_U64LE},
		{"board", unsafe.Offsetof(v.Board), hdf5.T_STD_U32LE},
		{"point_index", unsafe.Offsetof(v.PointIndex), hdf5.T_STD_U32LE},
		{"chain", unsafe.Offsetof(v.Chain), hdf5.T_STD_U16LE},
		{"node", unsafe.Offsetof(v.Node), hdf5.T_STD_U16LE},
		{"source", unsafe.Offsetof(v.Source), hdf5.T_STD_U8LE},
		{"has_point", unsafe.Offsetof(v.HasPoint), hdf5.T_STD_U8LE},
		{"has_hardware_timestamp", unsafe.Offsetof(v.HasHardwareTimestamp), hdf5.T_STD_U8LE},
		{"crc_error", unsafe.Offsetof(v.CRCError), hdf5.T_STD_U8LE},
		{"validity", unsafe.Offsetof(v.Validity), hdf5.T_STD_U8LE},
		{"fpga_temperature_c", unsafe.Offsetof(v.FPGATemperature), hdf5.T_IEEE_F64LE},
		{"board_temperature_c", unsafe.Offsetof(v.BoardTemperature), hdf5.T_IEEE_F64LE},
		{"detector_temperature_c", unsafe.Offsetof(v.DetectorTemperature), hdf5.T_IEEE_F64LE},
		{"hv_temperature_c", unsafe.Offsetof(v.HVTemperature), hdf5.T_IEEE_F64LE},
		{"hv_voltage_v", unsafe.Offsetof(v.HVVoltage), hdf5.T_IEEE_F64LE},
		{"hv_current_a", unsafe.Offsetof(v.HVCurrent), hdf5.T_IEEE_F64LE},
		{"hv_on", unsafe.Offsetof(v.HVOn), hdf5.T_STD_U8LE},
		{"hv_ramping", unsafe.Offsetof(v.HVRamping), hdf5.T_STD_U8LE},
		{"hv_over_current", unsafe.Offsetof(v.HVOverCurrent), hdf5.T_STD_U8LE},
		{"hv_over_voltage", unsafe.Offsetof(v.HVOverVoltage), hdf5.T_STD_U8LE},
		{"status", unsafe.Offsetof(v.Status), hdf5.T_STD_U16LE},
	})
}
