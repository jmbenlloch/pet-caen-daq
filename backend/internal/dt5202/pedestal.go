package dt5202

import "fmt"

const MaxEnergy = 0x3fff

type PedestalCalibration struct {
	LowGain  [ChannelCount]uint16
	HighGain [ChannelCount]uint16
	Source   string
}

type PedestalPlan struct {
	Common                       uint16
	AcquisitionMode              uint32
	ZeroSuppressLowGain          uint16
	ZeroSuppressHighGain         uint16
	ZeroSuppressLowGainChannels  [ChannelCount]uint16
	ZeroSuppressHighGainChannels [ChannelCount]uint16
	PerChannel                   bool
	Calibration                  *PedestalCalibration
}

// WithPedestalCalibration supplies the protected-flash calibration evidence
// needed for host-side correction and spectroscopy-only zero suppression.
func (p ConfigurationPlan) WithPedestalCalibration(calibration PedestalCalibration) (ConfigurationPlan, error) {
	if calibration.Source == "" {
		return ConfigurationPlan{}, fmt.Errorf("pedestal calibration source is required")
	}
	p.Pedestal.Calibration = &calibration
	remaining := p.Deferred[:0]
	for _, name := range p.Deferred {
		if name != "Pedestal" && name != "ZS_Threshold_LG" && name != "ZS_Threshold_HG" {
			remaining = append(remaining, name)
		}
	}
	p.Deferred = remaining
	if p.Pedestal.AcquisitionMode == 1 {
		for channel := uint8(0); channel < ChannelCount; channel++ {
			requestedLG, requestedHG := p.Pedestal.ZeroSuppressLowGain, p.Pedestal.ZeroSuppressHighGain
			if p.Pedestal.PerChannel {
				requestedLG = p.Pedestal.ZeroSuppressLowGainChannels[channel]
				requestedHG = p.Pedestal.ZeroSuppressHighGainChannels[channel]
			}
			lg := zeroSuppressionValue(requestedLG, p.Pedestal.Common, calibration.LowGain[channel])
			hg := zeroSuppressionValue(requestedHG, p.Pedestal.Common, calibration.HighGain[channel])
			p.Writes = append(p.Writes, RegisterWrite{IndividualRegister(ZeroSuppressionLowGain, channel), uint32(lg)}, RegisterWrite{IndividualRegister(ZeroSuppressionHighGain, channel), uint32(hg)})
		}
	}
	return p, nil
}

func zeroSuppressionValue(requested, common, calibrated uint16) uint16 {
	if requested == 0 {
		return 0
	}
	// Match the source's assignment to uint16_t, including two's-complement
	// wrapping when requested-common+calibrated is negative.
	return uint16(int(requested) - int(common) + int(calibrated))
}

// ApplyPedestalCalibration performs the same host-side energy correction as
// FERS_readout.c and clamps the result to the 14-bit energy range.
func ApplyPedestalCalibration(event SpectroscopyEvent, common uint16, calibration PedestalCalibration) SpectroscopyEvent {
	event.Energies = append([]Energy(nil), event.Energies...)
	for index := range event.Energies {
		energy := &event.Energies[index]
		channel := energy.Channel
		if energy.HasLowGain {
			energy.LowGain = correctedEnergy(energy.LowGain, common, calibration.LowGain[channel])
		}
		if energy.HasHighGain {
			energy.HighGain = correctedEnergy(energy.HighGain, common, calibration.HighGain[channel])
		}
	}
	return event
}

func correctedEnergy(raw, common, calibrated uint16) uint16 {
	value := int(raw) + int(common) - int(calibrated)
	if value < 0 {
		return 0
	}
	if value > MaxEnergy {
		return MaxEnergy
	}
	return uint16(value)
}
