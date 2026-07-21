package dt5202

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
)

func goldenPedestalPage(t *testing.T) []byte {
	t.Helper()
	text, err := os.ReadFile("testdata/pedestal_page_v0.hex")
	if err != nil {
		t.Fatal(err)
	}
	page, err := hex.DecodeString(strings.TrimSpace(string(text)))
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestParsePedestalFlashPageGolden(t *testing.T) {
	page := goldenPedestalPage(t)
	if len(page) != PedestalFlashPageBytes {
		t.Fatalf("fixture length=%d", len(page))
	}
	got, err := ParsePedestalFlashPage(page, "source-confirmed synthetic fixture")
	if err != nil {
		t.Fatal(err)
	}
	if got.Page != 4 || got.Format != 0 || got.CalibrationDate.Format("2006-01-02") != "2026-07-21" || got.DCOffsets != [4]uint16{2750, 2750, 2750, 2750} || got.Calibration.LowGain[0] != 100 || got.Calibration.LowGain[63] != 163 || got.Calibration.HighGain[0] != 200 || got.Calibration.HighGain[63] != 263 {
		t.Fatalf("calibration=%#v", got)
	}
}

func TestParsePedestalFlashPageRejectsMalformed(t *testing.T) {
	base := goldenPedestalPage(t)
	tests := []struct {
		name   string
		mutate func([]byte)
		slice  int
		source string
	}{
		{"short", func([]byte) {}, len(base) - 1, "fixture"}, {"long", func([]byte) {}, len(base) + 1, "fixture"},
		{"tag", func(b []byte) { b[0] = 'X' }, len(base), "fixture"}, {"format", func(b []byte) { b[1] = 1 }, len(base), "fixture"},
		{"month", func(b []byte) { b[4] = 13 }, len(base), "fixture"}, {"day", func(b []byte) { b[4] = 2; b[5] = 30 }, len(base), "fixture"},
		{"dc zero", func(b []byte) { b[6] = 0; b[7] = 0 }, len(base), "fixture"}, {"dc maximum", func(b []byte) { b[6] = 0xff; b[7] = 0x0f }, len(base), "fixture"},
		{"LG range", func(b []byte) { b[16] = 0; b[17] = 0x40 }, len(base), "fixture"}, {"HG range", func(b []byte) { b[144] = 0; b[145] = 0x40 }, len(base), "fixture"},
		{"source", func([]byte) {}, len(base), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := tt.slice
			page := make([]byte, size)
			copy(page, base)
			tt.mutate(page)
			if _, err := ParsePedestalFlashPage(page, tt.source); err == nil {
				t.Fatal("accepted malformed page")
			}
		})
	}
}

type flashReaderFake struct {
	page     []byte
	writes   []uint32
	reads    int
	writeErr error
	readErr  error
}

func (f *flashReaderFake) WriteRegister(_ context.Context, chain, node uint16, address, value uint32) error {
	if chain != 2 || node != 3 || address != uint32(SPIData) {
		panic("unexpected write target")
	}
	f.writes = append(f.writes, value)
	return f.writeErr
}
func (f *flashReaderFake) ReadRegister(_ context.Context, chain, node uint16, address uint32) (uint32, error) {
	if chain != 2 || node != 3 || address != uint32(SPIData) {
		panic("unexpected read target")
	}
	if f.readErr != nil {
		return 0, f.readErr
	}
	value := f.page[f.reads]
	f.reads++
	return uint32(value), nil
}

func TestLoadPedestalCalibrationUsesReadOnlySPISequence(t *testing.T) {
	fake := &flashReaderFake{page: goldenPedestalPage(t)}
	got, err := LoadPedestalCalibration(context.Background(), fake, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []uint32{0, 0x1d2, 0x100, 0x110, 0x100, 0x100, 0x100, 0x100, 0x100}
	if len(fake.writes) != len(wantPrefix)+PedestalFlashPageBytes+1 {
		t.Fatalf("write clocks=%d", len(fake.writes))
	}
	for index, want := range wantPrefix {
		if fake.writes[index] != want {
			t.Fatalf("write %d=%#x want %#x", index, fake.writes[index], want)
		}
	}
	for index, value := range fake.writes[len(wantPrefix) : len(fake.writes)-1] {
		want := uint32(0x100)
		if index == PedestalFlashPageBytes-1 {
			want = 0
		}
		if value != want {
			t.Fatalf("read clock %d=%#x want %#x", index, value, want)
		}
	}
	if fake.writes[len(fake.writes)-1] != 0 {
		t.Fatalf("final chip-select write=%#x, want 0", fake.writes[len(fake.writes)-1])
	}
	if fake.reads != PedestalFlashPageBytes || got.Calibration.Source != "DT5202 protected flash page 4 at chain 2 node 3" {
		t.Fatalf("reads=%d calibration=%#v", fake.reads, got)
	}
}

func TestLoadPedestalCalibrationPropagatesIOErrors(t *testing.T) {
	sentinel := errors.New("SPI failed")
	if _, err := LoadPedestalCalibration(context.Background(), &flashReaderFake{page: goldenPedestalPage(t), writeErr: sentinel}, 2, 3); !errors.Is(err, sentinel) {
		t.Fatalf("write error=%v", err)
	}
	if _, err := LoadPedestalCalibration(context.Background(), &flashReaderFake{page: goldenPedestalPage(t), readErr: sentinel}, 2, 3); !errors.Is(err, sentinel) {
		t.Fatalf("read error=%v", err)
	}
}

func FuzzParsePedestalFlashPage(f *testing.F) {
	page, err := os.ReadFile("testdata/pedestal_page_v0.hex")
	if err != nil {
		f.Fatal(err)
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(page)))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Fuzz(func(t *testing.T, page []byte) { _, _ = ParsePedestalFlashPage(page, "fuzz fixture") })
}
