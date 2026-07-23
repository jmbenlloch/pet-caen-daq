//go:build hdf5

package hdf5store

import (
	"errors"
	"fmt"
	"unsafe"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

type configurationBoardRow struct {
	Board            uint32
	Chain            uint16
	Node             uint16
	ProductID        uint32
	FirmwareRevision uint32
	AcquisitionState uint32
}

type configurationChannelRow struct {
	Board                uint32
	Channel              uint8
	Chip                 uint8
	ChipChannel          uint8
	ReadoutEnabled       uint8
	QDEnabled            uint8
	TDEnabled            uint8
	QDFine               uint8
	TDFine               uint8
	HighGain             uint8
	LowGain              uint8
	HVAdjustment         uint16
	CalibrateHighGain    uint8
	CalibrateLowGain     uint8
	PreamplifierDisabled uint8
}

type configurationChipRow struct {
	Board                     uint32
	Chip                      uint8
	DiscriminatorMask         uint32
	ChargeCoarseThreshold     uint16
	TimeCoarseThreshold       uint16
	LowShapingTimeCode        uint8
	HighShapingTimeCode       uint8
	FastShaperOnLowGain       uint8
	EnableInputDAC            uint8
	InputDACReference45V      uint8
	EnableDigitalOutput       uint8
	EnableOR32                uint8
	EnableOpenCollectorOR32   uint8
	NegativeTriggerPolarity   uint8
	EnableOpenCollectorTimeOR uint8
	EnableChannelTriggers     uint8
}

type configurationStreamWordRow struct {
	Board     uint32
	Chip      uint8
	WordIndex uint8
	BitCount  uint16
	Word      uint32
}

type configurationWriteRow struct {
	Board   uint32
	Ordinal uint32
	Address uint32
	Value   uint32
}

type configurationHVRow struct {
	Board               uint32
	VoltageV            float64
	CurrentLimitMA      float64
	TemperatureFeedback uint8
	FeedbackMVPerC      float64
	Coefficient0        float64
	Coefficient1        float64
	Coefficient2        float64
}

type configurationHVTransactionRow struct {
	Board    uint32
	Ordinal  uint32
	Register uint8
	DataType uint8
	Data     uint32
}

type configurationPedestalRow struct {
	Board                uint32
	Common               uint16
	AcquisitionMode      uint32
	ZeroSuppressLowGain  uint16
	ZeroSuppressHighGain uint16
	PerChannel           uint8
	CalibrationPresent   uint8
}

type configurationPedestalChannelRow struct {
	Board                uint32
	Channel              uint8
	ZeroSuppressLowGain  uint16
	ZeroSuppressHighGain uint16
	CalibrationPresent   uint8
	LowGainPedestal      uint16
	HighGainPedestal     uint16
}

func writeEffectiveConfiguration(configuration *hdf5.Group, metadata Metadata) error {
	effective, err := configuration.CreateGroup("effective")
	if err != nil {
		return fmt.Errorf("create effective configuration group: %w", err)
	}
	defer effective.Close()

	boardRows := make([]configurationBoardRow, len(metadata.Boards))
	for i, board := range metadata.Boards {
		boardRows[i] = configurationBoardRow{
			Board: uint32(board.Board), Chain: board.Chain, Node: board.Node, ProductID: board.ProductID,
			FirmwareRevision: board.FirmwareRevision, AcquisitionState: board.AcquisitionState,
		}
	}
	var channels []configurationChannelRow
	var chips []configurationChipRow
	var streams []configurationStreamWordRow
	var writes []configurationWriteRow
	var hvPlans []configurationHVRow
	var hvTransactions []configurationHVTransactionRow
	var pedestalPlans []configurationPedestalRow
	var pedestalChannels []configurationPedestalChannelRow
	for _, plan := range metadata.EffectiveConfiguration {
		masks := effectiveMasks(plan)
		for chipIndex, chip := range plan.Citiroc {
			common := chip.Common
			chips = append(chips, configurationChipRow{
				Board: uint32(plan.Board), Chip: uint8(chipIndex), DiscriminatorMask: common.DiscriminatorMask,
				ChargeCoarseThreshold: common.ChargeCoarseThreshold, TimeCoarseThreshold: common.TimeCoarseThreshold,
				LowShapingTimeCode: common.LowShapingTime, HighShapingTimeCode: common.HighShapingTime,
				FastShaperOnLowGain: boolean(common.FastShaperOnLowGain), EnableInputDAC: boolean(common.EnableInputDAC),
				InputDACReference45V: boolean(common.InputDACReference45V), EnableDigitalOutput: boolean(common.EnableDigitalOutput),
				EnableOR32: boolean(common.EnableOR32), EnableOpenCollectorOR32: boolean(common.EnableOpenCollectorOR32),
				NegativeTriggerPolarity:   boolean(common.NegativeTriggerPolarity),
				EnableOpenCollectorTimeOR: boolean(common.EnableOpenCollectorTimeOR),
				EnableChannelTriggers:     boolean(common.EnableChannelTriggers),
			})
			stream, streamErr := chip.Stream()
			if streamErr != nil {
				return fmt.Errorf("encode board %d Citiroc %d stream: %w", plan.Board, chipIndex, streamErr)
			}
			for wordIndex, word := range stream {
				streams = append(streams, configurationStreamWordRow{
					Board: uint32(plan.Board), Chip: uint8(chipIndex), WordIndex: uint8(wordIndex),
					BitCount: dt5202.CitirocBitCount, Word: word,
				})
			}
			for chipChannel, channel := range chip.Channels {
				boardChannel := chipIndex*32 + chipChannel
				channels = append(channels, configurationChannelRow{
					Board: uint32(plan.Board), Channel: uint8(boardChannel), Chip: uint8(chipIndex), ChipChannel: uint8(chipChannel),
					ReadoutEnabled: boolean(masks.readout&(uint64(1)<<boardChannel) != 0),
					QDEnabled:      boolean(masks.qd&(uint64(1)<<boardChannel) != 0),
					TDEnabled:      boolean(masks.td&(uint64(1)<<boardChannel) != 0),
					QDFine:         channel.ChargeFineThreshold, TDFine: channel.TimeFineThreshold,
					HighGain: channel.HighGain, LowGain: channel.LowGain, HVAdjustment: channel.HVAdjustment,
					CalibrateHighGain: boolean(channel.CalibrateHighGain), CalibrateLowGain: boolean(channel.CalibrateLowGain),
					PreamplifierDisabled: boolean(channel.DisablePreamplifier),
				})
			}
		}
		for ordinal, write := range plan.Writes {
			writes = append(writes, configurationWriteRow{uint32(plan.Board), uint32(ordinal), uint32(write.Address), write.Value})
		}
		hv := plan.HV
		hvPlans = append(hvPlans, configurationHVRow{
			uint32(plan.Board), hv.VoltageV, hv.CurrentLimitMA, boolean(hv.TemperatureFeedback), hv.FeedbackMVPerC,
			hv.TemperatureCoefficients[0], hv.TemperatureCoefficients[1], hv.TemperatureCoefficients[2],
		})
		for ordinal, transaction := range hv.Transactions {
			hvTransactions = append(hvTransactions, configurationHVTransactionRow{
				uint32(plan.Board), uint32(ordinal), transaction.Register, transaction.DataType, transaction.Data,
			})
		}
		pedestal := plan.Pedestal
		present := pedestal.Calibration != nil
		pedestalPlans = append(pedestalPlans, configurationPedestalRow{
			uint32(plan.Board), pedestal.Common, pedestal.AcquisitionMode, pedestal.ZeroSuppressLowGain,
			pedestal.ZeroSuppressHighGain, boolean(pedestal.PerChannel), boolean(present),
		})
		for channel := range dt5202.ChannelCount {
			row := configurationPedestalChannelRow{
				Board: uint32(plan.Board), Channel: uint8(channel), CalibrationPresent: boolean(present),
				ZeroSuppressLowGain: pedestal.ZeroSuppressLowGain, ZeroSuppressHighGain: pedestal.ZeroSuppressHighGain,
			}
			if pedestal.PerChannel {
				row.ZeroSuppressLowGain = pedestal.ZeroSuppressLowGainChannels[channel]
				row.ZeroSuppressHighGain = pedestal.ZeroSuppressHighGainChannels[channel]
			}
			if present {
				row.LowGainPedestal = pedestal.Calibration.LowGain[channel]
				row.HighGainPedestal = pedestal.Calibration.HighGain[channel]
			}
			pedestalChannels = append(pedestalChannels, row)
		}
	}

	for _, item := range []struct {
		name string
		rows any
		typ  *hdf5.CompoundType
	}{
		{"boards", boardRows, compoundConfigurationBoard()},
		{"channels", channels, compoundConfigurationChannel()},
		{"citiroc_chips", chips, compoundConfigurationChip()},
		{"citiroc_stream_words", streams, compoundConfigurationStreamWord()},
		{"fpga_writes", writes, compoundConfigurationWrite()},
		{"hv_plans", hvPlans, compoundConfigurationHV()},
		{"hv_transactions", hvTransactions, compoundConfigurationHVTransaction()},
		{"pedestal_plans", pedestalPlans, compoundConfigurationPedestal()},
		{"pedestal_channels", pedestalChannels, compoundConfigurationPedestalChannel()},
	} {
		if err := writeConfigurationTable(effective, item.name, item.rows, item.typ); err != nil {
			return err
		}
	}
	return nil
}

type maskSet struct{ readout, qd, td uint64 }

func effectiveMasks(plan dt5202.ConfigurationPlan) maskSet {
	var result maskSet
	for _, write := range plan.Writes {
		switch write.Address {
		case dt5202.ChannelMaskLow:
			result.readout = result.readout&0xffffffff00000000 | uint64(write.Value)
		case dt5202.ChannelMaskHigh:
			result.readout = result.readout&0xffffffff | uint64(write.Value)<<32
		case dt5202.ChargeDiscriminatorMaskLow:
			result.qd = result.qd&0xffffffff00000000 | uint64(write.Value)
		case dt5202.ChargeDiscriminatorMaskHigh:
			result.qd = result.qd&0xffffffff | uint64(write.Value)<<32
		case dt5202.TimeDiscriminatorMaskLow:
			result.td = result.td&0xffffffff00000000 | uint64(write.Value)
		case dt5202.TimeDiscriminatorMaskHigh:
			result.td = result.td&0xffffffff | uint64(write.Value)<<32
		}
	}
	return result
}

func writeConfigurationTable(group *hdf5.Group, name string, rows any, datatype *hdf5.CompoundType) error {
	dataset, err := createTable(group, name, datatype)
	if err != nil {
		return err
	}
	target := table{dataset: dataset}
	var appendErr error
	switch values := rows.(type) {
	case []configurationBoardRow:
		appendErr = appendRows(&target, values)
	case []configurationChannelRow:
		appendErr = appendRows(&target, values)
	case []configurationChipRow:
		appendErr = appendRows(&target, values)
	case []configurationStreamWordRow:
		appendErr = appendRows(&target, values)
	case []configurationWriteRow:
		appendErr = appendRows(&target, values)
	case []configurationHVRow:
		appendErr = appendRows(&target, values)
	case []configurationHVTransactionRow:
		appendErr = appendRows(&target, values)
	case []configurationPedestalRow:
		appendErr = appendRows(&target, values)
	case []configurationPedestalChannelRow:
		appendErr = appendRows(&target, values)
	default:
		appendErr = fmt.Errorf("unsupported configuration table %s", name)
	}
	return errors.Join(appendErr, dataset.Close())
}

func compoundConfigurationBoard() *hdf5.CompoundType {
	var v configurationBoardRow
	return mustCompound(unsafe.Sizeof(v), []field{{"board", unsafe.Offsetof(v.Board), hdf5.T_STD_U32LE}, {"chain", unsafe.Offsetof(v.Chain), hdf5.T_STD_U16LE}, {"node", unsafe.Offsetof(v.Node), hdf5.T_STD_U16LE}, {"product_id", unsafe.Offsetof(v.ProductID), hdf5.T_STD_U32LE}, {"firmware_revision", unsafe.Offsetof(v.FirmwareRevision), hdf5.T_STD_U32LE}, {"acquisition_state", unsafe.Offsetof(v.AcquisitionState), hdf5.T_STD_U32LE}})
}

func compoundConfigurationChannel() *hdf5.CompoundType {
	var v configurationChannelRow
	return mustCompound(unsafe.Sizeof(v), []field{{"board", unsafe.Offsetof(v.Board), hdf5.T_STD_U32LE}, {"channel", unsafe.Offsetof(v.Channel), hdf5.T_STD_U8LE}, {"chip", unsafe.Offsetof(v.Chip), hdf5.T_STD_U8LE}, {"chip_channel", unsafe.Offsetof(v.ChipChannel), hdf5.T_STD_U8LE}, {"readout_enabled", unsafe.Offsetof(v.ReadoutEnabled), hdf5.T_STD_U8LE}, {"qd_enabled", unsafe.Offsetof(v.QDEnabled), hdf5.T_STD_U8LE}, {"td_enabled", unsafe.Offsetof(v.TDEnabled), hdf5.T_STD_U8LE}, {"qd_fine", unsafe.Offsetof(v.QDFine), hdf5.T_STD_U8LE}, {"td_fine", unsafe.Offsetof(v.TDFine), hdf5.T_STD_U8LE}, {"high_gain", unsafe.Offsetof(v.HighGain), hdf5.T_STD_U8LE}, {"low_gain", unsafe.Offsetof(v.LowGain), hdf5.T_STD_U8LE}, {"hv_adjustment", unsafe.Offsetof(v.HVAdjustment), hdf5.T_STD_U16LE}, {"calibrate_high_gain", unsafe.Offsetof(v.CalibrateHighGain), hdf5.T_STD_U8LE}, {"calibrate_low_gain", unsafe.Offsetof(v.CalibrateLowGain), hdf5.T_STD_U8LE}, {"preamplifier_disabled", unsafe.Offsetof(v.PreamplifierDisabled), hdf5.T_STD_U8LE}})
}

func compoundConfigurationChip() *hdf5.CompoundType {
	var v configurationChipRow
	return mustCompound(unsafe.Sizeof(v), []field{{"board", unsafe.Offsetof(v.Board), hdf5.T_STD_U32LE}, {"chip", unsafe.Offsetof(v.Chip), hdf5.T_STD_U8LE}, {"discriminator_mask", unsafe.Offsetof(v.DiscriminatorMask), hdf5.T_STD_U32LE}, {"charge_coarse_threshold", unsafe.Offsetof(v.ChargeCoarseThreshold), hdf5.T_STD_U16LE}, {"time_coarse_threshold", unsafe.Offsetof(v.TimeCoarseThreshold), hdf5.T_STD_U16LE}, {"low_shaping_time_code", unsafe.Offsetof(v.LowShapingTimeCode), hdf5.T_STD_U8LE}, {"high_shaping_time_code", unsafe.Offsetof(v.HighShapingTimeCode), hdf5.T_STD_U8LE}, {"fast_shaper_on_low_gain", unsafe.Offsetof(v.FastShaperOnLowGain), hdf5.T_STD_U8LE}, {"enable_input_dac", unsafe.Offsetof(v.EnableInputDAC), hdf5.T_STD_U8LE}, {"input_dac_reference_45v", unsafe.Offsetof(v.InputDACReference45V), hdf5.T_STD_U8LE}, {"enable_digital_output", unsafe.Offsetof(v.EnableDigitalOutput), hdf5.T_STD_U8LE}, {"enable_or32", unsafe.Offsetof(v.EnableOR32), hdf5.T_STD_U8LE}, {"enable_open_collector_or32", unsafe.Offsetof(v.EnableOpenCollectorOR32), hdf5.T_STD_U8LE}, {"negative_trigger_polarity", unsafe.Offsetof(v.NegativeTriggerPolarity), hdf5.T_STD_U8LE}, {"enable_open_collector_time_or", unsafe.Offsetof(v.EnableOpenCollectorTimeOR), hdf5.T_STD_U8LE}, {"enable_channel_triggers", unsafe.Offsetof(v.EnableChannelTriggers), hdf5.T_STD_U8LE}})
}

func compoundConfigurationStreamWord() *hdf5.CompoundType {
	var v configurationStreamWordRow
	return mustCompound(unsafe.Sizeof(v), []field{{"board", unsafe.Offsetof(v.Board), hdf5.T_STD_U32LE}, {"chip", unsafe.Offsetof(v.Chip), hdf5.T_STD_U8LE}, {"word_index", unsafe.Offsetof(v.WordIndex), hdf5.T_STD_U8LE}, {"bit_count", unsafe.Offsetof(v.BitCount), hdf5.T_STD_U16LE}, {"word", unsafe.Offsetof(v.Word), hdf5.T_STD_U32LE}})
}

func compoundConfigurationWrite() *hdf5.CompoundType {
	var v configurationWriteRow
	return mustCompound(unsafe.Sizeof(v), []field{{"board", unsafe.Offsetof(v.Board), hdf5.T_STD_U32LE}, {"ordinal", unsafe.Offsetof(v.Ordinal), hdf5.T_STD_U32LE}, {"address", unsafe.Offsetof(v.Address), hdf5.T_STD_U32LE}, {"value", unsafe.Offsetof(v.Value), hdf5.T_STD_U32LE}})
}

func compoundConfigurationHV() *hdf5.CompoundType {
	var v configurationHVRow
	return mustCompound(unsafe.Sizeof(v), []field{{"board", unsafe.Offsetof(v.Board), hdf5.T_STD_U32LE}, {"voltage_v", unsafe.Offsetof(v.VoltageV), hdf5.T_IEEE_F64LE}, {"current_limit_ma", unsafe.Offsetof(v.CurrentLimitMA), hdf5.T_IEEE_F64LE}, {"temperature_feedback", unsafe.Offsetof(v.TemperatureFeedback), hdf5.T_STD_U8LE}, {"feedback_mv_per_c", unsafe.Offsetof(v.FeedbackMVPerC), hdf5.T_IEEE_F64LE}, {"coefficient_0", unsafe.Offsetof(v.Coefficient0), hdf5.T_IEEE_F64LE}, {"coefficient_1", unsafe.Offsetof(v.Coefficient1), hdf5.T_IEEE_F64LE}, {"coefficient_2", unsafe.Offsetof(v.Coefficient2), hdf5.T_IEEE_F64LE}})
}

func compoundConfigurationHVTransaction() *hdf5.CompoundType {
	var v configurationHVTransactionRow
	return mustCompound(unsafe.Sizeof(v), []field{{"board", unsafe.Offsetof(v.Board), hdf5.T_STD_U32LE}, {"ordinal", unsafe.Offsetof(v.Ordinal), hdf5.T_STD_U32LE}, {"register", unsafe.Offsetof(v.Register), hdf5.T_STD_U8LE}, {"data_type", unsafe.Offsetof(v.DataType), hdf5.T_STD_U8LE}, {"data", unsafe.Offsetof(v.Data), hdf5.T_STD_U32LE}})
}

func compoundConfigurationPedestal() *hdf5.CompoundType {
	var v configurationPedestalRow
	return mustCompound(unsafe.Sizeof(v), []field{{"board", unsafe.Offsetof(v.Board), hdf5.T_STD_U32LE}, {"common", unsafe.Offsetof(v.Common), hdf5.T_STD_U16LE}, {"acquisition_mode", unsafe.Offsetof(v.AcquisitionMode), hdf5.T_STD_U32LE}, {"zero_suppress_low_gain", unsafe.Offsetof(v.ZeroSuppressLowGain), hdf5.T_STD_U16LE}, {"zero_suppress_high_gain", unsafe.Offsetof(v.ZeroSuppressHighGain), hdf5.T_STD_U16LE}, {"per_channel", unsafe.Offsetof(v.PerChannel), hdf5.T_STD_U8LE}, {"calibration_present", unsafe.Offsetof(v.CalibrationPresent), hdf5.T_STD_U8LE}})
}

func compoundConfigurationPedestalChannel() *hdf5.CompoundType {
	var v configurationPedestalChannelRow
	return mustCompound(unsafe.Sizeof(v), []field{{"board", unsafe.Offsetof(v.Board), hdf5.T_STD_U32LE}, {"channel", unsafe.Offsetof(v.Channel), hdf5.T_STD_U8LE}, {"zero_suppress_low_gain", unsafe.Offsetof(v.ZeroSuppressLowGain), hdf5.T_STD_U16LE}, {"zero_suppress_high_gain", unsafe.Offsetof(v.ZeroSuppressHighGain), hdf5.T_STD_U16LE}, {"calibration_present", unsafe.Offsetof(v.CalibrationPresent), hdf5.T_STD_U8LE}, {"low_gain_pedestal", unsafe.Offsetof(v.LowGainPedestal), hdf5.T_STD_U16LE}, {"high_gain_pedestal", unsafe.Offsetof(v.HighGainPedestal), hdf5.T_STD_U16LE}})
}
