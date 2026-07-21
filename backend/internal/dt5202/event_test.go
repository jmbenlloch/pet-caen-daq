package dt5202

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestDecodeSpectroscopyTimingBothGains(t *testing.T) {
	p := make([]byte, 8+8+12)
	binary.LittleEndian.PutUint64(p, 1|(1<<5))
	binary.LittleEndian.PutUint32(p[8:], 100|(200<<16)|(1<<15))
	binary.LittleEndian.PutUint32(p[12:], 300|(400<<16))
	binary.LittleEndian.PutUint32(p[16:], 1<<31|1234)
	binary.LittleEndian.PutUint32(p[20:], 5<<25|17<<16|222)
	binary.LittleEndian.PutUint32(p[24:], 5<<25|18<<16|333)
	e, err := DecodeSpectroscopy(QualifierSpectroscopy|QualifierTiming|QualifierBothGains, 9, 10, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Energies) != 2 || e.Energies[0].HighGain != 100 || e.Energies[0].LowGain != 200 || !e.Energies[0].Discriminator || len(e.Timings) != 1 || e.Timings[0].Channel != 5 || e.Timings[0].ToA != 222 || e.Timings[0].ToT != 17 || e.TimeReference == nil || *e.TimeReference != 1234 {
		t.Fatalf("event = %#v", e)
	}
}

func TestDecodeSpectroscopyAcceptsCapturedLeadingEdgeQualifier(t *testing.T) {
	p := words(1, 0, 123, 3<<25|0x1234)
	e, err := DecodeEvent(QualifierLeadingEdge|QualifierSpectroscopy|QualifierTiming, 9, 10, p)
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != EventSpectroscopy || len(e.Spectroscopy.Energies) != 1 || len(e.Spectroscopy.Timings) != 1 {
		t.Fatalf("decoded event = %#v", e)
	}
}
func TestDecodeSpectroscopySingleGainPacked(t *testing.T) {
	p := make([]byte, 12)
	binary.LittleEndian.PutUint64(p, 3)
	binary.LittleEndian.PutUint16(p[8:], 42)
	binary.LittleEndian.PutUint16(p[10:], 1<<14|84)
	e, err := DecodeSpectroscopy(QualifierSpectroscopy, 1, 2, p)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Energies[0].HasHighGain || e.Energies[0].HighGain != 42 || !e.Energies[1].HasLowGain || e.Energies[1].LowGain != 84 {
		t.Fatalf("energies = %#v", e.Energies)
	}
}
func TestDecodeSpectroscopyRejectsMalformed(t *testing.T) {
	if _, err := DecodeSpectroscopy(QualifierTiming, 0, 0, make([]byte, 8)); err == nil {
		t.Fatal("accepted non-spectroscopy qualifier")
	}
	p := make([]byte, 8)
	binary.LittleEndian.PutUint64(p, 1)
	if _, err := DecodeSpectroscopy(QualifierSpectroscopy, 0, 0, p); err == nil {
		t.Fatal("accepted missing energy")
	}
}
func FuzzDecodeSpectroscopy(f *testing.F) {
	f.Add(uint8(3), []byte{})
	f.Fuzz(func(t *testing.T, q uint8, p []byte) { DecodeSpectroscopy(q, 0, 0, p) })
}

func words(values ...uint32) []byte {
	b := make([]byte, 4*len(values))
	for i, value := range values {
		binary.LittleEndian.PutUint32(b[4*i:], value)
	}
	return b
}

func TestDecodeTimingGoldenCommonStartAndStop(t *testing.T) {
	tests := []struct {
		name      string
		qualifier uint8
		word      uint32
		wantToA   uint32
		wantToT   uint16
	}{
		{"common_start", QualifierTiming, 3<<25 | 0x12<<16 | 0x3456, 0x3456, 0x12},
		{"common_stop", QualifierCommonStop, 4<<25 | 0x1ab<<16 | 0x9876, 0x9876, 0x1ab},
		{"streaming", QualifierStreaming, 5<<25 | 0x123456, 0x123456, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := DecodeEvent(tt.qualifier, 7, 0x123, words(0xa, tt.word))
			if err != nil {
				t.Fatal(err)
			}
			if e.Kind != EventTiming || e.Timing.TriggerID != 7 || e.Timing.TimeReference != 0x123a || len(e.Timing.Hits) != 1 || e.Timing.Hits[0].Channel != uint8(tt.word>>25) || e.Timing.Hits[0].ToA != tt.wantToA || e.Timing.Hits[0].ToT != tt.wantToT {
				t.Fatalf("event = %#v", e)
			}
		})
	}
}

func TestDecodeCountingGolden(t *testing.T) {
	e, err := DecodeEvent(QualifierCounting|QualifierRelativeTime, 11, 12, words(99, 2<<24|123, 64<<24|456, 65<<24|789))
	if err != nil {
		t.Fatal(err)
	}
	if e.Counting.RelativeTimestampClock == nil || *e.Counting.RelativeTimestampClock != 99 || e.Counting.ChannelMask != 4 || len(e.Counting.Counts) != 1 || e.Counting.Counts[0] != (Count{2, 123}) || e.Counting.TORCount != 456 || e.Counting.QORCount != 789 {
		t.Fatalf("event = %#v", e.Counting)
	}
}

func TestDecodeWaveformGolden(t *testing.T) {
	e, err := DecodeEvent(QualifierWaveform, 1, 2, words(0xa<<28|222<<14|111))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Waveform.Samples) != 1 || e.Waveform.Samples[0] != (WaveformSample{111, 222, 0xa}) {
		t.Fatalf("event = %#v", e.Waveform)
	}
}

func TestDecodeServiceGoldenVersions(t *testing.T) {
	header := uint32(1)<<24 | 3<<12 | 2048
	status := uint32(0x4321) | 100<<16
	e, err := DecodeEvent(QualifierService, 0, 55, words(header, 454000, 10000, 1000|2000<<13|1<<26|1<<28, status, 7<<24|88, 64<<24|99, 65<<24|111))
	if err != nil {
		t.Fatal(err)
	}
	s := e.Service
	if s.Version != 1 || s.Format != 3 || s.Status == nil || *s.Status != 0x4321 || s.BoardTemperature == nil || *s.BoardTemperature != 25 || s.HVVoltage == nil || *s.HVVoltage != 45.4 || s.HVCurrent == nil || *s.HVCurrent != 0.001 || !s.HVOn || !s.HVOverCurrent || s.HVRamping || s.HVOverVoltage || len(s.Counters) != 1 || s.Counters[0] != (ServiceCounter{7, 88}) || s.TORCount != 99 || s.QORCount != 111 {
		t.Fatalf("service = %#v", s)
	}
	unknown, err := DecodeService(1, words(2<<24, 0x12345678))
	if err != nil || unknown.Version != 2 || unknown.FPGATemperature != nil || !bytes.Equal(unknown.UnknownPayload, words(0x12345678)) {
		t.Fatalf("unknown service = %#v, %v", unknown, err)
	}
}

func TestDecodeSpectroscopyRelativeTimestamp(t *testing.T) {
	p := append(words(77), make([]byte, 12)...)
	binary.LittleEndian.PutUint64(p[4:], 1)
	binary.LittleEndian.PutUint16(p[12:], 42)
	e, err := DecodeSpectroscopy(QualifierSpectroscopy|QualifierRelativeTime, 1, 2, p)
	if err != nil {
		t.Fatal(err)
	}
	if e.RelativeTimestampClock == nil || *e.RelativeTimestampClock != 77 || e.Energies[0].HighGain != 42 {
		t.Fatalf("event = %#v", e)
	}
}

func TestDecodeTestGolden(t *testing.T) {
	e, err := DecodeEvent(QualifierTest, 8, 9, words(0x11223344, 0xaabbccdd))
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != EventTest || len(e.Test.Words) != 2 || e.Test.Words[1] != 0xaabbccdd {
		t.Fatalf("event = %#v", e)
	}
}

func TestDecodeEventRejectsUnsupportedAndMalformed(t *testing.T) {
	tests := []struct {
		name      string
		qualifier uint8
		payload   []byte
		contains  string
	}{
		{"unsupported", 0x55, nil, "unsupported DT5202 qualifier"},
		{"spectroscopy qualifier bits", 0x41, make([]byte, 8), "unsupported spectroscopy qualifier"},
		{"truncated timing", QualifierTiming, nil, "payload length"},
		{"timing channel", QualifierTiming, words(0, 64<<25), "channel 64"},
		{"too many timing hits", QualifierTiming, make([]byte, (MaxTimingHits+2)*4), "hit count"},
		{"duplicate count", QualifierCounting, words(1<<24|1, 1<<24|2), "duplicate"},
		{"bad count channel", QualifierCounting, words(66 << 24), "channel 66"},
		{"truncated service HV", QualifierService, words(1 << 12), "HV section"},
		{"service trailing", QualifierService, words(0, 1), "unexpected trailing"},
		{"oversized test", QualifierTest, make([]byte, (MaxTestWords+1)*4), "test word count"},
		{"oversized", QualifierWaveform, make([]byte, MaxEventPayloadBytes+4), "exceeds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeEvent(tt.qualifier, 0, 0, tt.payload)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("error = %v, want %q", err, tt.contains)
			}
		})
	}
}

func FuzzDecodeEvent(f *testing.F) {
	f.Add(uint8(QualifierTiming), []byte{})
	f.Add(uint8(QualifierService), words(0))
	f.Add(uint8(QualifierCounting), words(1))
	f.Fuzz(func(t *testing.T, qualifier uint8, payload []byte) { _, _ = DecodeEvent(qualifier, 1, 2, payload) })
}
