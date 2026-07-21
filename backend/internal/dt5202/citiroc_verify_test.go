package dt5202

import (
	"context"
	"strings"
	"testing"
)

type readbackCitirocHardware struct {
	control uint32
	streams [2]CitirocStream
	writes  []citirocCall
}

func (h *readbackCitirocHardware) WriteRegister(_ context.Context, _, _ uint16, address, value uint32) error {
	h.writes = append(h.writes, citirocCall{"write", address, value})
	if address == uint32(CitirocSlowControl) {
		h.control = value
	}
	return nil
}
func (h *readbackCitirocHardware) SendCommand(_ context.Context, _, _ uint16, command, delay uint32) error {
	h.writes = append(h.writes, citirocCall{"command", command, delay})
	return nil
}
func (h *readbackCitirocHardware) ReadRegister(_ context.Context, _, _ uint16, address uint32) (uint32, error) {
	if address == uint32(CitirocSlowControl) {
		return h.control, nil
	}
	chip := (h.control >> 9) & 1
	word := h.control & 0x3f
	return h.streams[chip][word], nil
}

func TestVerifyCitirocReadbackMatchesBothCompleteStreams(t *testing.T) {
	var chips [2]CitirocChip
	chips[0].Channels[0] = CitirocChannel{TimeFineThreshold: 3, HighGain: 12, HVAdjustment: 0x155}
	chips[0].Common = CitirocCommon{PowerEnableChargeDiscriminator: true, OTAForceOn: true, NegativeTriggerPolarity: true}
	chips[1].Channels[31] = CitirocChannel{ChargeFineThreshold: 9, LowGain: 27, HVAdjustment: 0x100}
	chips[1].Common = CitirocCommon{PowerEnableTimeDiscriminator: true, EnableChannelTriggers: true}
	hw := &readbackCitirocHardware{control: 0x55}
	for chip := range chips {
		stream, err := chips[chip].Stream()
		if err != nil {
			t.Fatal(err)
		}
		hw.streams[chip] = stream
	}
	captured, err := VerifyCitirocReadback(context.Background(), hw, 2, 0, chips)
	if err != nil {
		t.Fatal(err)
	}
	if captured != hw.streams {
		t.Fatal("captured streams differ")
	}
	if hw.control != 0x55 {
		t.Fatalf("control = %#x, want restored %#x", hw.control, 0x55)
	}
	for _, call := range hw.writes {
		if call.kind == "write" && call.address == uint32(CitirocSlowControl) && call.value&(1<<8) != 0 {
			t.Fatalf("manual loading was enabled by write %#x", call.value)
		}
	}
}

func TestCompareCitirocStreamReportsExactBit(t *testing.T) {
	var expected, actual CitirocStream
	expected[17] = 1 << 11
	err := CompareCitirocStream(expected, actual)
	if err == nil || !strings.Contains(err.Error(), "bit 555") || !strings.Contains(err.Error(), "word 17") {
		t.Fatalf("error = %v", err)
	}
}

func TestReadCitirocStreamRejectsBitsBeyondRegister(t *testing.T) {
	hw := &readbackCitirocHardware{}
	hw.streams[0][35] = 1 << 24
	if _, err := ReadCitirocStream(context.Background(), hw, 0, 0, 0); err == nil || !strings.Contains(err.Error(), "beyond bit 1143") {
		t.Fatalf("error = %v", err)
	}
}

func TestCitirocPowerProvenanceIsCompleteAndManualLoadingDisabled(t *testing.T) {
	fields := CitirocPowerProvenance()
	seenName := map[string]bool{}
	seenBit := map[int]bool{}
	for _, field := range fields {
		if field.Name == "" || field.Semantics == "" || field.ValueOwner == "" {
			t.Fatalf("incomplete provenance: %#v", field)
		}
		if seenName[field.Name] || seenBit[field.Bit] {
			t.Fatalf("duplicate provenance: %#v", field)
		}
		seenName[field.Name], seenBit[field.Bit] = true, true
	}
	for _, bit := range []int{256, 257, 259, 260, 261, 262, 263, 264, 297, 298, 299, 300, 302, 303, 304, 305, 310, 311, 312, 313, 314, 318, 319, 324, 325, 326, 327, 329, 1099, 1100, 1101, 1102, 1103, 1104, 1105, 1106, 1127, 1128, 1129, 1130, 1131, 1132, 1133, 1134, 1135, 1136, 1137, 1138, 1139, 1140, 1142, 1143} {
		if !seenBit[bit] {
			t.Errorf("power-control bit %d has no provenance", bit)
		}
	}
	if len(fields) != 52 {
		t.Fatalf("power fields = %d, want 52", len(fields))
	}
	if CitirocManualLoadingVerified() {
		t.Fatal("manual loading must remain disabled without hardware capture")
	}
}
