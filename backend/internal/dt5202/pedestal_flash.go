package dt5202

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"
)

const (
	PedestalFlashPage      uint16 = 4
	PedestalFlashPageBytes        = 16 + 2*ChannelCount*2
	flashPageReadCommand   uint32 = 0xd2
	flashKeepEnabled       uint32 = 0x100
)

// PedestalFlashCalibration is immutable validated evidence from protected
// flash page 4. Loading it never applies the returned DC offsets.
type PedestalFlashCalibration struct {
	Page            uint16
	Format          uint8
	CalibrationDate time.Time
	DCOffsets       [4]uint16
	Calibration     PedestalCalibration
}

// ParsePedestalFlashPage validates the complete source-confirmed format-0
// payload. The caller owns page; no returned value aliases it.
func ParsePedestalFlashPage(page []byte, source string) (PedestalFlashCalibration, error) {
	if len(page) != PedestalFlashPageBytes {
		return PedestalFlashCalibration{}, fmt.Errorf("pedestal page length %d, want %d", len(page), PedestalFlashPageBytes)
	}
	if page[0] != 'P' {
		return PedestalFlashCalibration{}, fmt.Errorf("pedestal page tag 0x%02x, want 'P'", page[0])
	}
	if page[1] != 0 {
		return PedestalFlashCalibration{}, fmt.Errorf("unsupported pedestal page format %d", page[1])
	}
	if source == "" {
		return PedestalFlashCalibration{}, fmt.Errorf("pedestal calibration source is required")
	}
	year, month, day := int(binary.LittleEndian.Uint16(page[2:4])), time.Month(page[4]), int(page[5])
	if year < 1 || year > 9999 || month < time.January || month > time.December || day < 1 || day > 31 {
		return PedestalFlashCalibration{}, fmt.Errorf("invalid pedestal calibration date %04d-%02d-%02d", year, month, day)
	}
	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if date.Year() != year || date.Month() != month || date.Day() != day {
		return PedestalFlashCalibration{}, fmt.Errorf("invalid pedestal calibration date %04d-%02d-%02d", year, month, day)
	}
	out := PedestalFlashCalibration{Page: PedestalFlashPage, Format: page[1], CalibrationDate: date}
	out.Calibration.Source = source
	for index := range out.DCOffsets {
		value := binary.LittleEndian.Uint16(page[6+index*2:])
		if value == 0 || value >= 4095 {
			return PedestalFlashCalibration{}, fmt.Errorf("DC offset %d value %d outside 1..4094", index, value)
		}
		out.DCOffsets[index] = value
	}
	for channel := 0; channel < ChannelCount; channel++ {
		low := binary.LittleEndian.Uint16(page[16+channel*2:])
		high := binary.LittleEndian.Uint16(page[16+ChannelCount*2+channel*2:])
		if low > MaxEnergy {
			return PedestalFlashCalibration{}, fmt.Errorf("LG pedestal channel %d value %d exceeds %d", channel, low, MaxEnergy)
		}
		if high > MaxEnergy {
			return PedestalFlashCalibration{}, fmt.Errorf("HG pedestal channel %d value %d exceeds %d", channel, high, MaxEnergy)
		}
		out.Calibration.LowGain[channel], out.Calibration.HighGain[channel] = low, high
	}
	return out, nil
}

type PedestalFlashReader interface {
	WriteRegister(context.Context, uint16, uint16, uint32, uint32) error
	ReadRegister(context.Context, uint16, uint16, uint32) (uint32, error)
}

// LoadPedestalCalibration performs only the AT45DB321 main-memory page-read
// sequence. It never sends a program, erase, or status-write opcode.
func LoadPedestalCalibration(ctx context.Context, reader PedestalFlashReader, chain, node uint16) (PedestalFlashCalibration, error) {
	write := func(value uint32) error {
		if err := reader.WriteRegister(ctx, chain, node, uint32(SPIData), value); err != nil {
			return fmt.Errorf("pedestal flash SPI write 0x%03x: %w", value, err)
		}
		return nil
	}
	address := uint32(PedestalFlashPage) << 10
	sequence := []uint32{flashKeepEnabled | flashPageReadCommand, flashKeepEnabled | (address>>16)&0xff, flashKeepEnabled | (address>>8)&0xff, flashKeepEnabled | address&0xff, flashKeepEnabled, flashKeepEnabled, flashKeepEnabled, flashKeepEnabled}
	for _, value := range sequence {
		if err := write(value); err != nil {
			return PedestalFlashCalibration{}, err
		}
	}
	page := make([]byte, PedestalFlashPageBytes)
	for index := range page {
		value := flashKeepEnabled
		if index == len(page)-1 {
			value = 0
		}
		if err := write(value); err != nil {
			return PedestalFlashCalibration{}, err
		}
		read, err := reader.ReadRegister(ctx, chain, node, uint32(SPIData))
		if err != nil {
			return PedestalFlashCalibration{}, fmt.Errorf("read pedestal flash byte %d: %w", index, err)
		}
		page[index] = byte(read)
	}
	source := fmt.Sprintf("DT5202 protected flash page %d at chain %d node %d", PedestalFlashPage, chain, node)
	calibration, err := ParsePedestalFlashPage(page, source)
	if err != nil {
		return PedestalFlashCalibration{}, fmt.Errorf("validate pedestal flash chain %d node %d: %w", chain, node, err)
	}
	return calibration, nil
}
