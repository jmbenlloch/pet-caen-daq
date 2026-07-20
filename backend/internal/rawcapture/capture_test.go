package rawcapture

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestRoundTripDeterministic(t *testing.T) {
	var a, b bytes.Buffer
	for _, dst := range []*bytes.Buffer{&a, &b} {
		w, err := NewWriter(dst)
		if err != nil {
			t.Fatal(err)
		}
		if err = w.Append([]byte{1, 2, 3}); err != nil {
			t.Fatal(err)
		}
		if err = w.Append([]byte("second")); err != nil {
			t.Fatal(err)
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("capture is not deterministic")
	}
	r, err := NewReader(bytes.NewReader(a.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{{1, 2, 3}, []byte("second")} {
		got, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("got %x want %x", got, want)
		}
	}
	if _, err = r.Next(); err != io.EOF {
		t.Fatalf("end = %v", err)
	}
}
func TestRejectsCorruptionAndTruncation(t *testing.T) {
	var data bytes.Buffer
	w, _ := NewWriter(&data)
	_ = w.Append([]byte("evidence"))
	raw := data.Bytes()
	corrupt := append([]byte(nil), raw...)
	corrupt[len(corrupt)-1] ^= 1
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{{"header", raw[:4], "header"}, {"truncated", raw[:len(raw)-1], "truncated"}, {"checksum", corrupt, "checksum"}} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewReader(bytes.NewReader(tc.data))
			if err == nil {
				_, err = r.Next()
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
