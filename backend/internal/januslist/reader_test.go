package januslist

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun54Prefix(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "..", "test", "fixtures", "runs", "run54", "Run54.first256_list.dat"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	h := r.Header
	if h.FormatMajor != 3 || h.FormatMinor != 4 || h.SoftwareMajor != 4 || h.SoftwareMinor != 3 || h.SoftwarePatch != 0 || h.BoardFamily != 5202 || h.RunNumber != 54 || h.AcquisitionMode != 3 || h.EnergyBins != 4096 || h.TimeUnit != 1 || h.ToALSBNS != 0.5 || h.StartUnixMilli != 1784279197454 {
		t.Fatalf("header = %#v", h)
	}
	counts := [4]int{}
	hits := 0
	for i := 0; i < 256; i++ {
		e, err := r.Next()
		if err != nil {
			t.Fatalf("event %d: %v", i+1, err)
		}
		if e.Board > 3 {
			t.Fatalf("event %d board %d", i+1, e.Board)
		}
		counts[e.Board]++
		hits += len(e.Hits)
		if uint64(len(e.Hits)) != uint64(popcount(e.ChannelMask)) {
			t.Fatalf("event %d hit/mask mismatch", i+1)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("after prefix: %v", err)
	}
	if counts != [4]int{84, 57, 68, 47} {
		t.Fatalf("board counts = %v", counts)
	}
	if hits != 12988 {
		t.Fatalf("hits = %d", hits)
	}
}

func TestReaderRejectsMalformedInput(t *testing.T) {
	f, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "runs", "run54", "Run54.first256_list.dat"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{{"short header", f[:12], "header"}, {"bad version", append([]byte{9}, f[1:]...), "unsupported"}, {"truncated record", f[:len(f)-1], "truncated"}} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewReader(bytes.NewReader(tc.data))
			if err != nil {
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatal(err)
				}
				return
			}
			for {
				_, err = r.Next()
				if err != nil {
					if !strings.Contains(err.Error(), tc.want) {
						t.Fatal(err)
					}
					return
				}
			}
		})
	}
}
func popcount(v uint64) int {
	n := 0
	for v != 0 {
		n += int(v & 1)
		v >>= 1
	}
	return n
}
