package dt5215

import (
	"encoding/binary"
	"testing"
)

func TestDecodeStreamBatch(t *testing.T) {
	b := make([]byte, 12+32+12)
	binary.LittleEndian.PutUint32(b, 0xffffffff)
	binary.LittleEndian.PutUint32(b[4:], 0xffffffff)
	binary.LittleEndian.PutUint32(b[8:], 1|(1<<8))
	w := b[12:44]
	binary.LittleEndian.PutUint32(w[0:], 3)
	binary.LittleEndian.PutUint32(w[4:], 0x12000000)
	binary.LittleEndian.PutUint32(w[8:], 0x34567890)
	binary.LittleEndian.PutUint32(w[12:], 0xcdef5678)
	binary.LittleEndian.PutUint32(w[16:], 0x123489ab)
	binary.LittleEndian.PutUint32(w[20:], 0x56)
	binary.LittleEndian.PutUint32(w[28:], 2|(3<<8)|(1<<16))
	copy(b[44:], []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12})
	events, err := DecodeStreamBatch(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatal(len(events))
	}
	e := events[0]
	if e.Chain != 1 || e.Descriptor.Node != 2 || e.Descriptor.Qualifier != 3 || !e.Descriptor.CRCError || e.Descriptor.PayloadSizeWords != 3 || len(e.Payload) != 12 {
		t.Fatalf("event = %#v", e)
	}
}
func TestDecodeStreamBatchRejectsMalformed(t *testing.T) {
	for _, b := range [][]byte{{}, make([]byte, 12)} {
		if _, err := DecodeStreamBatch(b); err == nil {
			t.Fatal("accepted malformed batch")
		}
	}
}
func FuzzDecodeStreamBatch(f *testing.F) {
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) { DecodeStreamBatch(b) })
}
