package acquisition

import "testing"

func TestHoldDelayAcceptsEverySpectroscopyMode(t *testing.T) {
	for _, mode := range []uint32{1, 3} {
		if !hasSpectroscopy(mode) {
			t.Fatalf("mode %#x does not retain the spectroscopy bit", mode)
		}
	}
	for _, mode := range []uint32{2, 4, 8, 0x12} {
		if hasSpectroscopy(mode) {
			t.Fatalf("non-spectroscopy mode %#x was accepted", mode)
		}
	}
}
