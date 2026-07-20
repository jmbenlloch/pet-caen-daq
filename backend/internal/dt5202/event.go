// Package dt5202 decodes project-owned DT5202 event values.
package dt5202

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

const (
	QualifierSpectroscopy uint8 = 0x01
	QualifierTiming       uint8 = 0x02
	QualifierBothGains    uint8 = 0x10
)

type Energy struct {
	Channel                                uint8
	LowGain, HighGain                      uint16
	HasLowGain, HasHighGain, Discriminator bool
}
type Timing struct {
	Channel uint8
	ToA     uint16
	ToT     uint16
}
type SpectroscopyEvent struct {
	TriggerID, Timestamp uint64
	ChannelMask          uint64
	Energies             []Energy
	Timings              []Timing
	TimeReference        *uint32
}

// DecodeSpectroscopy decodes a descriptor payload for spectroscopy, optionally
// with timing. Pedestal correction is intentionally a later configuration step.
func DecodeSpectroscopy(qualifier uint8, triggerID, timestamp uint64, payload []byte) (SpectroscopyEvent, error) {
	if qualifier&QualifierSpectroscopy == 0 {
		return SpectroscopyEvent{}, fmt.Errorf("qualifier 0x%02x is not spectroscopy", qualifier)
	}
	if len(payload) < 8 || len(payload)%4 != 0 {
		return SpectroscopyEvent{}, fmt.Errorf("spectroscopy payload length %d is not valid", len(payload))
	}
	mask := binary.LittleEndian.Uint64(payload)
	p := payload[8:]
	n := bits.OnesCount64(mask)
	energyBytes := n * 2
	if qualifier&QualifierBothGains != 0 {
		energyBytes = n * 4
	} else if energyBytes%4 != 0 {
		energyBytes += 2
	}
	if len(p) < energyBytes {
		return SpectroscopyEvent{}, fmt.Errorf("energy data needs %d bytes, got %d", energyBytes, len(p))
	}
	e := SpectroscopyEvent{TriggerID: triggerID, Timestamp: timestamp, ChannelMask: mask, Energies: make([]Energy, 0, n)}
	energyData := p[:energyBytes]
	p = p[energyBytes:]
	entry := 0
	for ch := 0; ch < 64; ch++ {
		if mask&(uint64(1)<<ch) == 0 {
			continue
		}
		out := Energy{Channel: uint8(ch)}
		if qualifier&QualifierBothGains != 0 {
			word := binary.LittleEndian.Uint32(energyData[entry*4:])
			out.HighGain = uint16(word) & 0x3fff
			out.LowGain = uint16(word>>16) & 0x3fff
			out.HasHighGain = true
			out.HasLowGain = true
			out.Discriminator = word&(1<<15) != 0
		} else {
			v := binary.LittleEndian.Uint16(energyData[entry*2:])
			out.Discriminator = v&(1<<15) != 0
			if v&(1<<14) != 0 {
				out.LowGain = v & 0x3fff
				out.HasLowGain = true
			} else {
				out.HighGain = v & 0x3fff
				out.HasHighGain = true
			}
		}
		e.Energies = append(e.Energies, out)
		entry++
	}
	if qualifier&QualifierTiming == 0 {
		if len(p) != 0 {
			return SpectroscopyEvent{}, fmt.Errorf("%d trailing bytes without timing qualifier", len(p))
		}
		return e, nil
	}
	seen := [64]bool{}
	for len(p) > 0 {
		word := binary.LittleEndian.Uint32(p)
		p = p[4:]
		if word&(1<<31) != 0 {
			v := word & 0x7fffffff
			e.TimeReference = &v
			continue
		}
		ch := uint8(word >> 25)
		if ch >= 64 {
			return SpectroscopyEvent{}, fmt.Errorf("timing channel %d out of range", ch)
		}
		if seen[ch] {
			continue
		}
		seen[ch] = true
		e.Timings = append(e.Timings, Timing{Channel: ch, ToA: uint16(word), ToT: uint16((word >> 16) & 0x1ff)})
	}
	return e, nil
}
