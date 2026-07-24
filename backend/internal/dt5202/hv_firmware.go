package dt5202

import (
	"context"
	"fmt"
	"math"
)

const (
	hvFirmwareRegister = 252
	hvFloatDataType    = 3
	hvReadFlag         = 1 << 16
)

// ReadHVModuleFirmware reads the A7585 firmware version using the same
// source-confirmed indirect transaction as FERS_HV_Get_FWVer. The returned raw
// value is preserved because the peripheral encodes the version as IEEE-754
// float bits.
func ReadHVModuleFirmware(ctx context.Context, hardware HVHardware, chain, node uint16) (uint32, float32, error) {
	if err := writeHVBusPair(ctx, hardware, chain, node, 0x2001, 0); err != nil {
		return 0, 0, fmt.Errorf("initialize HV bus: %w", err)
	}
	if err := writeHVBusPair(ctx, hardware, chain, node, 2<<8|30, 1); err != nil {
		return 0, 0, fmt.Errorf("set HV PID precision: %w", err)
	}
	selector := uint32(hvReadFlag | hvFloatDataType<<8 | hvFirmwareRegister)
	if err := hardware.WriteRegister(ctx, chain, node, uint32(HVRegisterAddress), selector); err != nil {
		return 0, 0, fmt.Errorf("select A7585 firmware register: %w", err)
	}
	if err := waitHVBus(ctx, hardware, chain, node); err != nil {
		return 0, 0, err
	}
	raw, err := hardware.ReadRegister(ctx, chain, node, uint32(HVRegisterData))
	if err != nil {
		return 0, 0, fmt.Errorf("read A7585 firmware register: %w", err)
	}
	if err := waitHVBus(ctx, hardware, chain, node); err != nil {
		return raw, math.Float32frombits(raw), err
	}
	return raw, math.Float32frombits(raw), nil
}
