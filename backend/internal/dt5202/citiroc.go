package dt5202

import (
	"context"
	"fmt"
)

const (
	CitirocBitCount  = 1144
	CitirocWordCount = 36
)

// CitirocStream stores bit zero in word 0 bit 0, matching ReadSCbsFromFile and
// WriteCStoFile in the bundled FERS_configure_5202.c. Only the low 24 bits of
// word 35 belong to the 1,144-bit stream.
type CitirocStream [CitirocWordCount]uint32

func (s *CitirocStream) Set(start, width int, value uint32) error {
	if start < 0 || width < 1 || start+width > CitirocBitCount || width > 32 {
		return fmt.Errorf("invalid Citiroc field start=%d width=%d", start, width)
	}
	if width < 32 && value >= uint32(1)<<width {
		return fmt.Errorf("Citiroc field start=%d width=%d cannot hold %#x", start, width, value)
	}
	for bit := 0; bit < width; bit++ {
		mask := uint32(1) << ((start + bit) % 32)
		word := (start + bit) / 32
		if value&(uint32(1)<<bit) != 0 {
			s[word] |= mask
		} else {
			s[word] &^= mask
		}
	}
	return nil
}

func (s CitirocStream) Field(start, width int) (uint32, error) {
	if start < 0 || width < 1 || start+width > CitirocBitCount || width > 32 {
		return 0, fmt.Errorf("invalid Citiroc field start=%d width=%d", start, width)
	}
	var value uint32
	for bit := 0; bit < width; bit++ {
		if s[(start+bit)/32]&(uint32(1)<<((start+bit)%32)) != 0 {
			value |= uint32(1) << bit
		}
	}
	return value, nil
}

type CitirocChannel struct {
	TimeFineThreshold   uint8
	ChargeFineThreshold uint8
	HVAdjustment        uint16 // DAC[7:0] plus DAC-on in bit 8
	HighGain            uint8
	LowGain             uint8
	CalibrateHighGain   bool
	CalibrateLowGain    bool
	DisablePreamplifier bool
}

// CitirocCommon represents every non-channel field exposed by the source
// layout. Power-control values are explicit so callers cannot accidentally
// confuse a configurable overlay with a safe complete manual-load stream.
type CitirocCommon struct {
	PowerEnableChargeDiscriminator, PowerModeChargeDiscriminator              bool
	ChargeDiscriminatorLatch                                                  bool
	PowerEnableTimeDiscriminator, PowerModeTimeDiscriminator                  bool
	PowerEnableChargeFineDAC, PowerModeChargeFineDAC                          bool
	PowerEnableTimeFineDAC, PowerModeTimeFineDAC                              bool
	DiscriminatorMask                                                         uint32
	PowerModeTrackHoldHigh, PowerEnableTrackHoldHigh                          bool
	PowerModeTrackHoldLow, PowerEnableTrackHoldLow                            bool
	SCABias, PowerModePeakHigh, PowerEnablePeakHigh                           bool
	PowerModePeakLow, PowerEnablePeakLow                                      bool
	PeakDetectorHigh, PeakDetectorLow                                         bool
	PeakSensingExternalControl, PeakSensingExternalTrigger                    bool
	PowerModeFastShaperFollower, PowerEnableFastShaper, PowerModeFastShaper   bool
	PowerModeLowShaper, PowerEnableLowShaper                                  bool
	LowShapingTime                                                            uint8
	PowerModeHighShaper, PowerEnableHighShaper                                bool
	HighShapingTime                                                           uint8
	LowGainPreamplifierWeakBias                                               bool
	PowerModeHighPreamplifier, PowerEnableHighPreamplifier                    bool
	PowerModeLowPreamplifier, PowerEnableLowPreamplifier                      bool
	FastShaperOnLowGain, EnableInputDAC, InputDACReference45V                 bool
	PowerModeTemperature, PowerEnableTemperature                              bool
	PowerModeBandgap, PowerEnableBandgap                                      bool
	PowerEnableChargeDAC, PowerModeChargeDAC                                  bool
	PowerEnableTimeDAC, PowerModeTimeDAC                                      bool
	ChargeCoarseThreshold, TimeCoarseThreshold                                uint16
	PowerEnableHighOTA, PowerModeHighOTA                                      bool
	PowerEnableLowOTA, PowerModeLowOTA                                        bool
	PowerEnableProbeOTA, PowerModeProbeOTA                                    bool
	OTAForceOn, PowerEnableValidation, PowerModeValidation                    bool
	PowerEnableReset, PowerModeReset                                          bool
	EnableDigitalOutput, EnableOR32, EnableOpenCollectorOR32                  bool
	NegativeTriggerPolarity, EnableOpenCollectorTimeOR, EnableChannelTriggers bool
}

type CitirocChip struct {
	Channels [32]CitirocChannel
	Common   CitirocCommon
}

// SplitCitirocChannels maps board channels 0..31 to chip 0 and 32..63 to chip
// 1 without reversing either ASIC's source-confirmed channel order.
func SplitCitirocChannels(channels [ChannelCount]CitirocChannel, common CitirocCommon) [2]CitirocChip {
	var chips [2]CitirocChip
	for chip := range chips {
		chips[chip].Common = common
		copy(chips[chip].Channels[:], channels[chip*32:(chip+1)*32])
	}
	return chips
}

func (c CitirocChip) Stream() (CitirocStream, error) {
	var stream CitirocStream
	set := func(start, width int, value uint32) error { return stream.Set(start, width, value) }
	flag := func(start int, value bool) error {
		if value {
			return set(start, 1, 1)
		}
		return nil
	}
	for channel, cfg := range c.Channels {
		if cfg.TimeFineThreshold > 15 || cfg.ChargeFineThreshold > 15 || cfg.HVAdjustment > 0x1ff || cfg.HighGain > 63 || cfg.LowGain > 63 {
			return stream, fmt.Errorf("channel %d Citiroc value exceeds its field width", channel)
		}
		if err := set(channel*4, 4, uint32(cfg.TimeFineThreshold)); err != nil {
			return stream, fmt.Errorf("channel %d time threshold: %w", channel, err)
		}
		if err := set(128+channel*4, 4, uint32(cfg.ChargeFineThreshold)); err != nil {
			return stream, fmt.Errorf("channel %d charge threshold: %w", channel, err)
		}
		if err := set(331+channel*9, 9, uint32(cfg.HVAdjustment)); err != nil {
			return stream, fmt.Errorf("channel %d HV adjustment: %w", channel, err)
		}
		preamp := uint32(cfg.HighGain) | uint32(cfg.LowGain)<<6
		if cfg.CalibrateHighGain {
			preamp |= 1 << 12
		}
		if cfg.CalibrateLowGain {
			preamp |= 1 << 13
		}
		if cfg.DisablePreamplifier {
			preamp |= 1 << 14
		}
		if err := set(619+channel*15, 15, preamp); err != nil {
			return stream, fmt.Errorf("channel %d preamplifier: %w", channel, err)
		}
	}
	b := []struct {
		bit   int
		value bool
	}{
		{256, c.Common.PowerEnableChargeDiscriminator}, {257, c.Common.PowerModeChargeDiscriminator}, {258, c.Common.ChargeDiscriminatorLatch},
		{259, c.Common.PowerEnableTimeDiscriminator}, {260, c.Common.PowerModeTimeDiscriminator}, {261, c.Common.PowerEnableChargeFineDAC}, {262, c.Common.PowerModeChargeFineDAC},
		{263, c.Common.PowerEnableTimeFineDAC}, {264, c.Common.PowerModeTimeFineDAC}, {297, c.Common.PowerModeTrackHoldHigh}, {298, c.Common.PowerEnableTrackHoldHigh},
		{299, c.Common.PowerModeTrackHoldLow}, {300, c.Common.PowerEnableTrackHoldLow}, {301, c.Common.SCABias}, {302, c.Common.PowerModePeakHigh}, {303, c.Common.PowerEnablePeakHigh},
		{304, c.Common.PowerModePeakLow}, {305, c.Common.PowerEnablePeakLow}, {306, c.Common.PeakDetectorHigh}, {307, c.Common.PeakDetectorLow},
		{308, c.Common.PeakSensingExternalControl}, {309, c.Common.PeakSensingExternalTrigger}, {310, c.Common.PowerModeFastShaperFollower},
		{311, c.Common.PowerEnableFastShaper}, {312, c.Common.PowerModeFastShaper}, {313, c.Common.PowerModeLowShaper}, {314, c.Common.PowerEnableLowShaper},
		{318, c.Common.PowerModeHighShaper}, {319, c.Common.PowerEnableHighShaper}, {323, c.Common.LowGainPreamplifierWeakBias},
		{324, c.Common.PowerModeHighPreamplifier}, {325, c.Common.PowerEnableHighPreamplifier}, {326, c.Common.PowerModeLowPreamplifier}, {327, c.Common.PowerEnableLowPreamplifier},
		{328, c.Common.FastShaperOnLowGain}, {329, c.Common.EnableInputDAC}, {330, c.Common.InputDACReference45V},
		{1099, c.Common.PowerModeTemperature}, {1100, c.Common.PowerEnableTemperature}, {1101, c.Common.PowerModeBandgap}, {1102, c.Common.PowerEnableBandgap},
		{1103, c.Common.PowerEnableChargeDAC}, {1104, c.Common.PowerModeChargeDAC}, {1105, c.Common.PowerEnableTimeDAC}, {1106, c.Common.PowerModeTimeDAC},
		{1127, c.Common.PowerEnableHighOTA}, {1128, c.Common.PowerModeHighOTA}, {1129, c.Common.PowerEnableLowOTA}, {1130, c.Common.PowerModeLowOTA},
		{1131, c.Common.PowerEnableProbeOTA}, {1132, c.Common.PowerModeProbeOTA}, {1133, c.Common.OTAForceOn}, {1134, c.Common.PowerEnableValidation},
		{1135, c.Common.PowerModeValidation}, {1136, c.Common.PowerEnableReset}, {1137, c.Common.PowerModeReset}, {1138, c.Common.EnableDigitalOutput},
		{1139, c.Common.EnableOR32}, {1140, c.Common.EnableOpenCollectorOR32}, {1141, c.Common.NegativeTriggerPolarity},
		{1142, c.Common.EnableOpenCollectorTimeOR}, {1143, c.Common.EnableChannelTriggers},
	}
	for _, field := range b {
		if err := flag(field.bit, field.value); err != nil {
			return stream, err
		}
	}
	for _, field := range []struct {
		start, width int
		value        uint32
	}{
		{265, 32, c.Common.DiscriminatorMask}, {315, 3, uint32(c.Common.LowShapingTime)}, {320, 3, uint32(c.Common.HighShapingTime)},
		{1107, 10, uint32(c.Common.ChargeCoarseThreshold)}, {1117, 10, uint32(c.Common.TimeCoarseThreshold)},
	} {
		if err := set(field.start, field.width, field.value); err != nil {
			return stream, err
		}
	}
	return stream, nil
}

type CitirocHardware interface {
	WriteRegister(context.Context, uint16, uint16, uint32, uint32) error
	SendCommand(context.Context, uint16, uint16, uint32, uint32) error
}

// ConfigureCitirocAutomatic reproduces the normal JANUS/FERSlib sequence in
// which FPGA firmware constructs and shifts each chip's stream from registers.
func ConfigureCitirocAutomatic(ctx context.Context, hw CitirocHardware, chain, node uint16) error {
	for chip := uint32(0); chip < 2; chip++ {
		if err := hw.WriteRegister(ctx, chain, node, uint32(CitirocSlowControl), chip<<9); err != nil {
			return fmt.Errorf("select Citiroc %d: %w", chip, err)
		}
		if err := hw.SendCommand(ctx, chain, node, uint32(CommandConfigureASIC), 0); err != nil {
			return fmt.Errorf("configure Citiroc %d: %w", chip, err)
		}
	}
	return nil
}
