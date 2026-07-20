package dt5202

import "testing"

func TestIndividualRegisterMatchesVendorMacro(t *testing.T) {
	tests := []struct {
		base    Register
		channel uint8
		want    Register
	}{
		{LowGain, 0, 0x02000010},
		{HighGain, 1, 0x02010014},
		{TimeFineThreshold, 31, 0x021f000c},
		{HVIndividualAdjustment, 63, 0x023f0018},
	}
	for _, test := range tests {
		if got := IndividualRegister(test.base, test.channel); got != test.want {
			t.Errorf("IndividualRegister(%#x, %d) = %#x, want %#x", test.base, test.channel, got, test.want)
		}
	}
}

func TestBroadcastRegisterMatchesVendorMacro(t *testing.T) {
	if got, want := BroadcastRegister(HighGain), Register(0x03000014); got != want {
		t.Fatalf("BroadcastRegister(HighGain) = %#x, want %#x", got, want)
	}
}

func TestStatusHas(t *testing.T) {
	status := StatusReady | StatusRunning | StatusTDLinkSynchronized
	if !status.Has(StatusRunning) || !status.Has(StatusTDLinkSynchronized) {
		t.Fatalf("expected set flags in %#x", status)
	}
	if status.Has(StatusFailure) {
		t.Fatalf("unexpected failure flag in %#x", status)
	}
}
