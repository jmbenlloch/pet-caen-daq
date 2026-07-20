package dt5202

import (
	"encoding/binary"
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
