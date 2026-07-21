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
	QualifierCounting     uint8 = 0x04
	QualifierWaveform     uint8 = 0x08
	QualifierStart        uint8 = 0x0f
	QualifierBothGains    uint8 = 0x10
	QualifierLeadingEdge  uint8 = 0x20
	QualifierCommonStop   uint8 = 0x12
	QualifierStreaming    uint8 = 0x22
	QualifierService      uint8 = 0x2f
	QualifierRelativeTime uint8 = 0x80
	QualifierTest         uint8 = 0xff

	MaxEventPayloadBytes = 64*1024 - 5*4
	MaxTimingHits        = 1024
	MaxTestWords         = 4
)

type EventKind string

const (
	EventSpectroscopy EventKind = "spectroscopy"
	EventTiming       EventKind = "timing"
	EventCounting     EventKind = "counting"
	EventWaveform     EventKind = "waveform"
	EventService      EventKind = "service"
	EventTest         EventKind = "test"
)

// Event is the qualifier-dispatched result. Exactly one typed pointer is set.
type Event struct {
	Kind         EventKind          `json:"kind"`
	Qualifier    uint8              `json:"qualifier"`
	Spectroscopy *SpectroscopyEvent `json:"spectroscopy,omitempty"`
	Timing       *TimingEvent       `json:"timing,omitempty"`
	Counting     *CountingEvent     `json:"counting,omitempty"`
	Waveform     *WaveformEvent     `json:"waveform,omitempty"`
	Service      *ServiceEvent      `json:"service,omitempty"`
	Test         *TestEvent         `json:"test,omitempty"`
}

type Energy struct {
	Channel       uint8  `json:"channel"`
	LowGain       uint16 `json:"low_gain"`
	HighGain      uint16 `json:"high_gain"`
	HasLowGain    bool   `json:"has_low_gain"`
	HasHighGain   bool   `json:"has_high_gain"`
	Discriminator bool   `json:"discriminator"`
}
type Timing struct {
	Channel uint8  `json:"channel"`
	ToA     uint32 `json:"toa"`
	ToT     uint16 `json:"tot"`
}
type SpectroscopyEvent struct {
	TriggerID              uint64   `json:"trigger_id,string"`
	Timestamp              uint64   `json:"timestamp,string"`
	RelativeTimestampClock *uint32  `json:"relative_timestamp_clock,omitempty"`
	ChannelMask            uint64   `json:"channel_mask,string"`
	Energies               []Energy `json:"energies"`
	Timings                []Timing `json:"timings,omitempty"`
	TimeReference          *uint32  `json:"time_reference,omitempty"`
}

type TimingEvent struct {
	TriggerID     uint64   `json:"trigger_id,string"`
	Timestamp     uint64   `json:"timestamp,string"`
	TimeReference uint64   `json:"time_reference,string"`
	Hits          []Timing `json:"hits"`
}

type Count struct {
	Channel uint8  `json:"channel"`
	Value   uint32 `json:"value"`
}
type CountingEvent struct {
	TriggerID              uint64  `json:"trigger_id,string"`
	Timestamp              uint64  `json:"timestamp,string"`
	RelativeTimestampClock *uint32 `json:"relative_timestamp_clock,omitempty"`
	ChannelMask            uint64  `json:"channel_mask,string"`
	Counts                 []Count `json:"counts"`
	TORCount               uint32  `json:"t_or_count"`
	QORCount               uint32  `json:"q_or_count"`
}

type WaveformSample struct {
	HighGain      uint16 `json:"high_gain"`
	LowGain       uint16 `json:"low_gain"`
	DigitalProbes uint8  `json:"digital_probes"`
}
type WaveformEvent struct {
	TriggerID uint64           `json:"trigger_id,string"`
	Timestamp uint64           `json:"timestamp,string"`
	Samples   []WaveformSample `json:"samples"`
}

type ServiceCounter struct {
	Channel uint8  `json:"channel"`
	Value   uint32 `json:"value"`
}
type ServiceEvent struct {
	Timestamp           uint64           `json:"timestamp,string"`
	Version             uint8            `json:"version"`
	Format              uint8            `json:"format"`
	FPGATemperature     *float64         `json:"fpga_temperature_c,omitempty"`
	BoardTemperature    *float64         `json:"board_temperature_c,omitempty"`
	HVTemperature       *float64         `json:"hv_temperature_c,omitempty"`
	DetectorTemperature *float64         `json:"detector_temperature_c,omitempty"`
	HVVoltage           *float64         `json:"hv_voltage_v,omitempty"`
	HVCurrent           *float64         `json:"hv_current_a,omitempty"`
	HVOn                bool             `json:"hv_on"`
	HVRamping           bool             `json:"hv_ramping"`
	HVOverCurrent       bool             `json:"hv_over_current"`
	HVOverVoltage       bool             `json:"hv_over_voltage"`
	Status              *uint16          `json:"status,omitempty"`
	Counters            []ServiceCounter `json:"counters,omitempty"`
	TORCount            uint32           `json:"t_or_count"`
	QORCount            uint32           `json:"q_or_count"`
	UnknownPayload      []byte           `json:"unknown_payload,omitempty"`
}

type TestEvent struct {
	TriggerID uint64   `json:"trigger_id,string"`
	Timestamp uint64   `json:"timestamp,string"`
	Words     []uint32 `json:"words"`
}

func validatePayload(name string, payload []byte, minimum int) error {
	if len(payload) < minimum || len(payload)%4 != 0 {
		return fmt.Errorf("%s payload length %d is not valid", name, len(payload))
	}
	if len(payload) > MaxEventPayloadBytes {
		return fmt.Errorf("%s payload length %d exceeds %d", name, len(payload), MaxEventPayloadBytes)
	}
	return nil
}

// DecodeEvent validates the qualifier and dispatches an immutable payload.
func DecodeEvent(qualifier uint8, triggerID, timestamp uint64, payload []byte) (Event, error) {
	var out Event
	out.Qualifier = qualifier
	switch {
	case qualifier == QualifierService:
		v, err := DecodeService(timestamp, payload)
		out.Kind, out.Service = EventService, &v
		if err != nil {
			return Event{}, err
		}
	case qualifier == QualifierTest:
		v, err := DecodeTest(triggerID, timestamp, payload)
		out.Kind, out.Test = EventTest, &v
		if err != nil {
			return Event{}, err
		}
	case qualifier == QualifierWaveform:
		v, err := DecodeWaveform(triggerID, timestamp, payload)
		out.Kind, out.Waveform = EventWaveform, &v
		if err != nil {
			return Event{}, err
		}
	case qualifier&0x0f == QualifierSpectroscopy || qualifier&0x0f == QualifierSpectroscopy|QualifierTiming:
		if qualifier & ^uint8(0xb3) != 0 {
			return Event{}, fmt.Errorf("unsupported spectroscopy qualifier 0x%02x", qualifier)
		}
		v, err := DecodeSpectroscopy(qualifier, triggerID, timestamp, payload)
		out.Kind, out.Spectroscopy = EventSpectroscopy, &v
		if err != nil {
			return Event{}, err
		}
	case qualifier&0x0f == QualifierCounting:
		if qualifier & ^uint8(0x84) != 0 {
			return Event{}, fmt.Errorf("unsupported counting qualifier 0x%02x", qualifier)
		}
		v, err := DecodeCounting(qualifier, triggerID, timestamp, payload)
		out.Kind, out.Counting = EventCounting, &v
		if err != nil {
			return Event{}, err
		}
	case qualifier == QualifierTiming || qualifier == QualifierCommonStop || qualifier == QualifierStreaming:
		v, err := DecodeTiming(qualifier, triggerID, timestamp, payload)
		out.Kind, out.Timing = EventTiming, &v
		if err != nil {
			return Event{}, err
		}
	default:
		return Event{}, fmt.Errorf("unsupported DT5202 qualifier 0x%02x", qualifier)
	}
	return out, nil
}

// DecodeSpectroscopy decodes raw spectroscopy values, optionally with timing.
func DecodeSpectroscopy(qualifier uint8, triggerID, timestamp uint64, payload []byte) (SpectroscopyEvent, error) {
	if qualifier&0x0f != QualifierSpectroscopy && qualifier&0x0f != QualifierSpectroscopy|QualifierTiming {
		return SpectroscopyEvent{}, fmt.Errorf("qualifier 0x%02x is not spectroscopy", qualifier)
	}
	if qualifier & ^uint8(0xb3) != 0 {
		return SpectroscopyEvent{}, fmt.Errorf("unsupported spectroscopy qualifier 0x%02x", qualifier)
	}
	minimum := 8
	if qualifier&QualifierRelativeTime != 0 {
		minimum += 4
	}
	if err := validatePayload("spectroscopy", payload, minimum); err != nil {
		return SpectroscopyEvent{}, err
	}
	e := SpectroscopyEvent{TriggerID: triggerID, Timestamp: timestamp}
	p := payload
	if qualifier&QualifierRelativeTime != 0 {
		v := binary.LittleEndian.Uint32(p)
		e.RelativeTimestampClock = &v
		p = p[4:]
	}
	mask := binary.LittleEndian.Uint64(p)
	e.ChannelMask = mask
	p = p[8:]
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
	energyData := p[:energyBytes]
	p = p[energyBytes:]
	e.Energies = make([]Energy, 0, n)
	entry := 0
	for ch := 0; ch < 64; ch++ {
		if mask&(uint64(1)<<ch) == 0 {
			continue
		}
		v := Energy{Channel: uint8(ch)}
		if qualifier&QualifierBothGains != 0 {
			word := binary.LittleEndian.Uint32(energyData[entry*4:])
			v.HighGain, v.LowGain = uint16(word)&0x3fff, uint16(word>>16)&0x3fff
			v.HasHighGain, v.HasLowGain, v.Discriminator = true, true, word&(1<<15) != 0
		} else {
			word := binary.LittleEndian.Uint16(energyData[entry*2:])
			v.Discriminator = word&(1<<15) != 0
			if word&(1<<14) != 0 {
				v.LowGain, v.HasLowGain = word&0x3fff, true
			} else {
				v.HighGain, v.HasHighGain = word&0x3fff, true
			}
		}
		e.Energies = append(e.Energies, v)
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
		e.Timings = append(e.Timings, Timing{Channel: ch, ToA: word & 0xffff, ToT: uint16((word >> 16) & 0x1ff)})
	}
	return e, nil
}

func DecodeTiming(qualifier uint8, triggerID, timestamp uint64, payload []byte) (TimingEvent, error) {
	if qualifier != QualifierTiming && qualifier != QualifierCommonStop && qualifier != QualifierStreaming {
		return TimingEvent{}, fmt.Errorf("unsupported timing qualifier 0x%02x", qualifier)
	}
	if err := validatePayload("timing", payload, 4); err != nil {
		return TimingEvent{}, err
	}
	if len(payload)/4-1 > MaxTimingHits {
		return TimingEvent{}, fmt.Errorf("timing hit count %d exceeds %d", len(payload)/4-1, MaxTimingHits)
	}
	fine := binary.LittleEndian.Uint32(payload) & 0xf
	e := TimingEvent{TriggerID: triggerID, Timestamp: timestamp, TimeReference: (timestamp << 4) | uint64(fine), Hits: make([]Timing, 0, len(payload)/4-1)}
	for p := payload[4:]; len(p) > 0; p = p[4:] {
		word := binary.LittleEndian.Uint32(p)
		ch := uint8(word >> 25)
		if ch >= 64 {
			return TimingEvent{}, fmt.Errorf("timing channel %d out of range", ch)
		}
		h := Timing{Channel: ch}
		if qualifier == QualifierStreaming {
			h.ToA = word & 0x1ffffff
		} else {
			h.ToA = word & 0xffff
			h.ToT = uint16((word >> 16) & 0x1ff)
		}
		e.Hits = append(e.Hits, h)
	}
	return e, nil
}

func DecodeCounting(qualifier uint8, triggerID, timestamp uint64, payload []byte) (CountingEvent, error) {
	if qualifier&0x0f != QualifierCounting || qualifier & ^uint8(0x84) != 0 {
		return CountingEvent{}, fmt.Errorf("unsupported counting qualifier 0x%02x", qualifier)
	}
	minimum := 0
	if qualifier&QualifierRelativeTime != 0 {
		minimum = 4
	}
	if err := validatePayload("counting", payload, minimum); err != nil {
		return CountingEvent{}, err
	}
	e := CountingEvent{TriggerID: triggerID, Timestamp: timestamp}
	p := payload
	if qualifier&QualifierRelativeTime != 0 {
		v := binary.LittleEndian.Uint32(p)
		e.RelativeTimestampClock = &v
		p = p[4:]
	}
	seen := [66]bool{}
	for len(p) > 0 {
		word := binary.LittleEndian.Uint32(p)
		p = p[4:]
		ch := uint8(word >> 24)
		value := word & 0xffffff
		if ch > 65 {
			return CountingEvent{}, fmt.Errorf("counting channel %d out of range", ch)
		}
		if seen[ch] {
			return CountingEvent{}, fmt.Errorf("duplicate counting channel %d", ch)
		}
		seen[ch] = true
		switch ch {
		case 64:
			e.TORCount = value
		case 65:
			e.QORCount = value
		default:
			e.ChannelMask |= uint64(1) << ch
			e.Counts = append(e.Counts, Count{Channel: ch, Value: value})
		}
	}
	return e, nil
}

func DecodeWaveform(triggerID, timestamp uint64, payload []byte) (WaveformEvent, error) {
	if err := validatePayload("waveform", payload, 0); err != nil {
		return WaveformEvent{}, err
	}
	e := WaveformEvent{TriggerID: triggerID, Timestamp: timestamp, Samples: make([]WaveformSample, 0, len(payload)/4)}
	for len(payload) > 0 {
		word := binary.LittleEndian.Uint32(payload)
		payload = payload[4:]
		e.Samples = append(e.Samples, WaveformSample{HighGain: uint16(word & 0x3fff), LowGain: uint16((word >> 14) & 0x3fff), DigitalProbes: uint8(word >> 28)})
	}
	return e, nil
}

func DecodeService(timestamp uint64, payload []byte) (ServiceEvent, error) {
	if err := validatePayload("service", payload, 4); err != nil {
		return ServiceEvent{}, err
	}
	header := binary.LittleEndian.Uint32(payload)
	e := ServiceEvent{Timestamp: timestamp, Version: uint8(header >> 24), Format: uint8((header >> 12) & 0xf)}
	if e.Version > 1 {
		e.UnknownPayload = append([]byte(nil), payload[4:]...)
		return e, nil
	}
	fpga := float64(header&0xfff)*503.975/4096 - 273.15
	e.FPGATemperature = &fpga
	p := payload[4:]
	if e.Format&^uint8(3) != 0 {
		return ServiceEvent{}, fmt.Errorf("unsupported service format 0x%x", e.Format)
	}
	if e.Format&1 != 0 {
		need := 12
		if e.Version > 0 {
			need = 16
		}
		if len(p) < need {
			return ServiceEvent{}, fmt.Errorf("service HV section needs %d bytes, got %d", need, len(p))
		}
		v := float64(binary.LittleEndian.Uint32(p)) / 10000
		// The wire value is scaled by 10,000 in mA. The project API exposes
		// hv_current_a, so convert the decoded monitor value to amperes.
		i := float64(binary.LittleEndian.Uint32(p[4:])) / 10000000
		statusWord := binary.LittleEndian.Uint32(p[8:])
		det := float64(statusWord&0x1fff) * 256 / 10000
		hv := float64((statusWord>>13)&0x1fff) * 256 / 10000
		e.HVVoltage, e.HVCurrent, e.DetectorTemperature, e.HVTemperature = &v, &i, &det, &hv
		e.HVOn = statusWord&(1<<26) != 0
		e.HVRamping = statusWord&(1<<27) != 0
		e.HVOverCurrent = statusWord&(1<<28) != 0
		e.HVOverVoltage = statusWord&(1<<29) != 0
		p = p[12:]
		if e.Version > 0 {
			w := binary.LittleEndian.Uint32(p)
			s := uint16(w)
			e.Status = &s
			raw := (w >> 16) & 0x3ff
			if raw != 0x3ff {
				b := float64(raw) / 4
				e.BoardTemperature = &b
			}
			p = p[4:]
		}
	}
	if e.Format&2 == 0 {
		if len(p) != 0 {
			return ServiceEvent{}, fmt.Errorf("service event has %d unexpected trailing bytes", len(p))
		}
		return e, nil
	}
	seen := [66]bool{}
	for len(p) > 0 {
		word := binary.LittleEndian.Uint32(p)
		p = p[4:]
		ch := uint8((word >> 24) & 0x7f)
		value := word & 0xffffff
		if ch > 65 {
			return ServiceEvent{}, fmt.Errorf("service counter channel %d out of range", ch)
		}
		if seen[ch] {
			return ServiceEvent{}, fmt.Errorf("duplicate service counter channel %d", ch)
		}
		seen[ch] = true
		switch ch {
		case 64:
			e.TORCount = value
		case 65:
			e.QORCount = value
		default:
			e.Counters = append(e.Counters, ServiceCounter{Channel: ch, Value: value})
		}
	}
	return e, nil
}

func DecodeTest(triggerID, timestamp uint64, payload []byte) (TestEvent, error) {
	if err := validatePayload("test", payload, 0); err != nil {
		return TestEvent{}, err
	}
	if len(payload)/4 > MaxTestWords {
		return TestEvent{}, fmt.Errorf("test word count %d exceeds %d", len(payload)/4, MaxTestWords)
	}
	e := TestEvent{TriggerID: triggerID, Timestamp: timestamp, Words: make([]uint32, len(payload)/4)}
	for i := range e.Words {
		e.Words[i] = binary.LittleEndian.Uint32(payload[i*4:])
	}
	return e, nil
}
