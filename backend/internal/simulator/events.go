package simulator

import (
	"encoding/binary"
	"fmt"
	"math/bits"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

// generatedBatch deterministically derives event data from the board's
// effective register state. A zero requestedQualifier selects AcquisitionControl.
func generatedBatch(chain, node uint8, sequence uint64, requestedQualifier uint8, board *Board) ([]byte, error) {
	qualifier := requestedQualifier
	if qualifier == 0 {
		qualifier = uint8(board.Registers[uint32(dt5202.AcquisitionControl)] & 0xff)
		if qualifier == 0 { // Backwards-compatible power-on behavior for the minimal slice.
			qualifier = dt5202.QualifierSpectroscopy | dt5202.QualifierTiming
		}
	}
	var payload []byte
	var err error
	switch qualifier {
	case dt5202.QualifierSpectroscopy, dt5202.QualifierSpectroscopy | dt5202.QualifierTiming:
		qualifier, payload = spectroscopyPayload(chain, sequence, qualifier, board)
	case dt5202.QualifierTiming, dt5202.QualifierCommonStop:
		payload = timingPayload(chain, sequence, qualifier, board)
	case dt5202.QualifierCounting:
		payload = countingPayload(sequence, board)
	case dt5202.QualifierWaveform:
		payload, err = waveformPayload(sequence, board)
	case dt5202.QualifierService:
		payload = servicePayload(chain, sequence, board)
	default:
		err = fmt.Errorf("unsupported simulated acquisition qualifier 0x%02x", qualifier)
	}
	if err != nil {
		return nil, err
	}
	return eventBatch(chain, node, qualifier, sequence, payload), nil
}

func register(board *Board, base dt5202.Register, channel uint8) (uint32, bool) {
	value, ok := board.Registers[uint32(dt5202.IndividualRegister(base, channel))]
	return value, ok
}

func enabledMask(board *Board, fallback uint8) uint64 {
	low, lowSet := board.Registers[uint32(dt5202.ChannelMaskLow)]
	high, highSet := board.Registers[uint32(dt5202.ChannelMaskHigh)]
	if !lowSet && !highSet {
		return uint64(1) << fallback
	}
	return uint64(low) | uint64(high)<<32
}

func discriminatorMask(board *Board, lowReg, highReg dt5202.Register) (uint64, bool) {
	low, lowSet := board.Registers[uint32(lowReg)]
	high, highSet := board.Registers[uint32(highReg)]
	return uint64(low) | uint64(high)<<32, lowSet || highSet
}

func pulseEnergy(board *Board, channel uint8, sequence uint64, low bool) uint16 {
	base := uint32(100 + sequence)
	gainBase := dt5202.HighGain
	calibration := board.Pedestal.HighGain[channel]
	if low {
		base += 100
		gainBase = dt5202.LowGain
		calibration = board.Pedestal.LowGain[channel]
	}
	if gain, configured := register(board, gainBase, channel); configured {
		base += gain * 4
	}
	// Generate the raw FPGA value whose host-side pedestal correction recovers base.
	raw := int64(base) - int64(board.CommonPedestal) + int64(calibration)
	if raw < 0 {
		return 0
	}
	if raw > int64(dt5202.MaxEnergy) {
		return dt5202.MaxEnergy
	}
	return uint16(raw)
}

func passesThreshold(board *Board, channel uint8, low bool, value uint16) bool {
	base := dt5202.ZeroSuppressionHighGain
	if low {
		base = dt5202.ZeroSuppressionLowGain
	}
	threshold, configured := register(board, base, channel)
	return !configured || uint32(value) >= threshold
}

func timingEnabled(board *Board, channel uint8, sequence uint64) bool {
	mask, configured := discriminatorMask(board, dt5202.TimeDiscriminatorMaskLow, dt5202.TimeDiscriminatorMaskHigh)
	if configured && mask&(uint64(1)<<channel) == 0 {
		return false
	}
	fine, _ := register(board, dt5202.TimeFineThreshold, channel)
	return board.Registers[uint32(dt5202.TimeCoarseThreshold)]+fine <= uint32(256+sequence)
}

func spectroscopyPayload(chain uint8, sequence uint64, mode uint8, board *Board) (uint8, []byte) {
	control := board.Registers[uint32(dt5202.AcquisitionControl)]
	gainSelect := (control >> 12) & 3
	both := gainSelect == 3 || control == 0
	qualifier := mode
	if both {
		qualifier |= dt5202.QualifierBothGains
	}
	mask := enabledMask(board, chain)
	if chargeMask, configured := discriminatorMask(board, dt5202.ChargeDiscriminatorMaskLow, dt5202.ChargeDiscriminatorMaskHigh); configured {
		mask &= chargeMask
	}
	for channel := uint8(0); channel < 64; channel++ {
		if mask&(uint64(1)<<channel) == 0 {
			continue
		}
		low, high := pulseEnergy(board, channel, sequence, true), pulseEnergy(board, channel, sequence, false)
		if (!both && gainSelect == 2 && !passesThreshold(board, channel, true, low)) || (!both && gainSelect != 2 && !passesThreshold(board, channel, false, high)) || (both && !passesThreshold(board, channel, true, low) && !passesThreshold(board, channel, false, high)) {
			mask &^= uint64(1) << channel
		}
	}
	energyBytes := bits.OnesCount64(mask) * 2
	if both {
		energyBytes = bits.OnesCount64(mask) * 4
	} else if energyBytes%4 != 0 {
		energyBytes += 2
	}
	payload := make([]byte, 8+energyBytes)
	binary.LittleEndian.PutUint64(payload, mask)
	offset := 8
	for channel := uint8(0); channel < 64; channel++ {
		if mask&(uint64(1)<<channel) == 0 {
			continue
		}
		low, high := pulseEnergy(board, channel, sequence, true), pulseEnergy(board, channel, sequence, false)
		if both {
			binary.LittleEndian.PutUint32(payload[offset:], uint32(high)|1<<15|uint32(low)<<16)
			offset += 4
		} else {
			value := high | 1<<15
			if gainSelect == 2 {
				value = low | 1<<14 | 1<<15
			}
			binary.LittleEndian.PutUint16(payload[offset:], value)
			offset += 2
		}
	}
	if mode&dt5202.QualifierTiming != 0 {
		for channel := uint8(0); channel < 64; channel++ {
			if mask&(uint64(1)<<channel) != 0 && timingEnabled(board, channel, sequence) {
				word := uint32(channel)<<25 | uint32(10+channel)<<16 | uint32(300+sequence+uint64(channel))
				payload = appendWord(payload, word)
			}
		}
	}
	return qualifier, payload
}

func timingPayload(chain uint8, sequence uint64, qualifier uint8, board *Board) []byte {
	payload := appendWord(nil, uint32(sequence)&0xf)
	mask := enabledMask(board, chain)
	for channel := uint8(0); channel < 64; channel++ {
		if mask&(uint64(1)<<channel) == 0 || !timingEnabled(board, channel, sequence) {
			continue
		}
		word := uint32(channel)<<25 | uint32(10+channel)<<16 | uint32(300+sequence+uint64(channel))
		payload = appendWord(payload, word)
	}
	return payload
}

func countingPayload(sequence uint64, board *Board) []byte {
	mask := enabledMask(board, 0)
	zeroSuppress := board.Registers[uint32(dt5202.AcquisitionControl)]&(1<<30) != 0
	payload := []byte{}
	for channel := uint8(0); channel < 64; channel++ {
		if mask&(uint64(1)<<channel) == 0 {
			continue
		}
		count := uint32(sequence) + uint32(channel) + 1
		if zeroSuppress && count == 0 {
			continue
		}
		payload = appendWord(payload, uint32(channel)<<24|count)
	}
	payload = appendWord(payload, 64<<24|uint32(bits.OnesCount64(mask))*uint32(sequence+1))
	payload = appendWord(payload, 65<<24|uint32(bits.OnesCount64(mask))*uint32(sequence+2))
	return payload
}

func waveformPayload(sequence uint64, board *Board) ([]byte, error) {
	length := board.Registers[uint32(dt5202.WaveformLength)]
	if length == 0 {
		length = 4
	}
	if length*4 > dt5202.MaxEventPayloadBytes {
		return nil, fmt.Errorf("waveform length %d exceeds event boundary", length)
	}
	payload := make([]byte, 0, length*4)
	mask := enabledMask(board, 0)
	channel := uint8(0)
	if mask != 0 {
		channel = uint8(bits.TrailingZeros64(mask))
	}
	highBase := uint32(pulseEnergy(board, channel, sequence, false))
	lowBase := uint32(pulseEnergy(board, channel, sequence, true))
	for sample := uint32(0); sample < length; sample++ {
		hg := highBase + sample
		lg := lowBase + sample
		payload = appendWord(payload, hg&0x3fff|(lg&0x3fff)<<14|(sample&0xf)<<28)
	}
	return payload, nil
}

func servicePayload(chain uint8, sequence uint64, board *Board) []byte {
	format := uint32(3)
	header := uint32(1)<<24 | format<<12 | uint32(2200) + uint32(chain)
	payload := appendWord(nil, header)
	payload = appendWord(payload, 454000+uint32(chain)*100)
	payload = appendWord(payload, 10000+uint32(sequence))
	payload = appendWord(payload, 1000|2000<<13|1<<26)
	payload = appendWord(payload, uint32(board.Status)|uint32(100+chain)<<16)
	mask := enabledMask(board, chain)
	for channel := uint8(0); channel < 64; channel++ {
		if mask&(uint64(1)<<channel) != 0 {
			payload = appendWord(payload, uint32(channel)<<24|uint32(sequence+uint64(channel)+1))
		}
	}
	payload = appendWord(payload, 64<<24|uint32(sequence+10))
	payload = appendWord(payload, 65<<24|uint32(sequence+20))
	return payload
}

func appendWord(payload []byte, value uint32) []byte {
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	return append(payload, word[:]...)
}
