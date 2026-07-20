package dt5202

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestCitirocStreamMatchesSourceFieldLayout(t *testing.T) {
	chip := CitirocChip{}
	chip.Channels[0] = CitirocChannel{TimeFineThreshold: 0xa, ChargeFineThreshold: 5, HVAdjustment: 0x1ab, HighGain: 55, LowGain: 17, CalibrateHighGain: true}
	chip.Channels[31] = CitirocChannel{TimeFineThreshold: 3, ChargeFineThreshold: 0xc, HVAdjustment: 0x100, HighGain: 1, LowGain: 63, CalibrateLowGain: true, DisablePreamplifier: true}
	chip.Common = CitirocCommon{DiscriminatorMask: 0x89abcdef, LowShapingTime: 6, HighShapingTime: 5, ChargeCoarseThreshold: 250, TimeCoarseThreshold: 181, NegativeTriggerPolarity: true, EnableChannelTriggers: true}
	stream, err := chip.Stream()
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		start, width int
		want         uint32
	}{
		{0, 4, 0xa}, {124, 4, 3}, {128, 4, 5}, {252, 4, 0xc}, {265, 32, 0x89abcdef},
		{315, 3, 6}, {320, 3, 5}, {331, 9, 0x1ab}, {610, 9, 0x100},
		{619, 15, 55 | 17<<6 | 1<<12}, {1084, 15, 1 | 63<<6 | 1<<13 | 1<<14},
		{1107, 10, 250}, {1117, 10, 181}, {1141, 1, 1}, {1143, 1, 1},
	}
	for _, check := range checks {
		got, err := stream.Field(check.start, check.width)
		if err != nil || got != check.want {
			t.Errorf("Field(%d,%d) = %#x, %v; want %#x", check.start, check.width, got, err, check.want)
		}
	}
	if stream[35]&0xff000000 != 0 {
		t.Fatalf("unused top bits set: %#08x", stream[35])
	}
}

func TestCitirocStreamRejectsOverflow(t *testing.T) {
	chip := CitirocChip{}
	chip.Channels[4].HighGain = 64
	if _, err := chip.Stream(); err == nil {
		t.Fatal("expected six-bit gain overflow")
	}
}

func TestSplitCitirocChannelsBuildsBothStreamsInBoardOrder(t *testing.T) {
	var channels [ChannelCount]CitirocChannel
	channels[0].HighGain = 1
	channels[31].HighGain = 31
	channels[32].HighGain = 32
	channels[63].HighGain = 63
	chips := SplitCitirocChannels(channels, CitirocCommon{ChargeCoarseThreshold: 250})
	for chip, want := range [][]uint32{{1, 31}, {32, 63}} {
		stream, err := chips[chip].Stream()
		if err != nil {
			t.Fatal(err)
		}
		for endpoint, channel := range []int{0, 31} {
			got, err := stream.Field(619+channel*15, 6)
			if err != nil || got != want[endpoint] {
				t.Errorf("chip %d channel %d gain = %d, %v; want %d", chip, channel, got, err, want[endpoint])
			}
		}
	}
}

type citirocCall struct {
	kind           string
	address, value uint32
}
type fakeCitirocHardware struct {
	calls  []citirocCall
	failAt int
}

func (f *fakeCitirocHardware) record(call citirocCall) error {
	f.calls = append(f.calls, call)
	if len(f.calls) == f.failAt {
		return errors.New("injected")
	}
	return nil
}
func (f *fakeCitirocHardware) WriteRegister(_ context.Context, _, _ uint16, address, value uint32) error {
	return f.record(citirocCall{"write", address, value})
}
func (f *fakeCitirocHardware) SendCommand(_ context.Context, _, _ uint16, command, delay uint32) error {
	return f.record(citirocCall{"command", command, delay})
}

func TestConfigureCitirocAutomaticMatchesSourceSequence(t *testing.T) {
	hw := &fakeCitirocHardware{}
	if err := ConfigureCitirocAutomatic(context.Background(), hw, 2, 0); err != nil {
		t.Fatal(err)
	}
	want := []citirocCall{{"write", uint32(CitirocSlowControl), 0}, {"command", uint32(CommandConfigureASIC), 0}, {"write", uint32(CitirocSlowControl), 0x200}, {"command", uint32(CommandConfigureASIC), 0}}
	if !reflect.DeepEqual(hw.calls, want) {
		t.Fatalf("calls = %#v, want %#v", hw.calls, want)
	}
}

func TestConfigureCitirocAutomaticStopsOnFailure(t *testing.T) {
	hw := &fakeCitirocHardware{failAt: 2}
	if err := ConfigureCitirocAutomatic(context.Background(), hw, 0, 0); err == nil {
		t.Fatal("expected command error")
	}
	if len(hw.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(hw.calls))
	}
}
