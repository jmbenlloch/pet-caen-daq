package simulator

import (
	"encoding/binary"
	"testing"
)

func TestFaultValidation(t *testing.T) {
	if err := (Fault{Kind: "unknown"}).validate(); err == nil {
		t.Fatal("accepted unknown fault")
	}
	if err := (Fault{Kind: FaultStreamDelay, Delay: -1}).validate(); err == nil {
		t.Fatal("accepted negative delay")
	}
	for _, kind := range []FaultKind{FaultControlDelay, FaultControlTimeout, FaultControlDisconnect, FaultPartialReply, FaultStreamDelay, FaultStreamDisconnect, FaultStreamTruncation, FaultMalformedDescriptor, FaultInvalidSize, FaultInvalidOffset, FaultCRC, FaultMissingService, FaultMissingCompletion, FaultStalledDrain} {
		if err := (Fault{Kind: kind}).validate(); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}
}

func TestMutateBatchFaultsAreByteExact(t *testing.T) {
	base := eventBatch(0, 2, 3, 1, make([]byte, 4))
	tests := []struct {
		kind  FaultKind
		check func([]byte) bool
	}{
		{FaultMalformedDescriptor, func(b []byte) bool { return uint8(binary.LittleEndian.Uint32(b[40:])) == 0xff }},
		{FaultInvalidSize, func(b []byte) bool { return binary.LittleEndian.Uint32(b[12:])&0xffffff == 0xffffff }},
		{FaultInvalidOffset, func(b []byte) bool {
			return binary.LittleEndian.Uint32(b[12:])>>24 == 0xff && binary.LittleEndian.Uint32(b[16:])&0xffffff == 0xffffff
		}},
		{FaultCRC, func(b []byte) bool { return binary.LittleEndian.Uint32(b[40:])&(1<<16) != 0 }},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			got := mutateBatch(base, Fault{Kind: tt.kind})
			if !tt.check(got) {
				t.Fatalf("mutation %s failed", tt.kind)
			}
			if string(base) == string(got) {
				t.Fatal("base mutated or output unchanged")
			}
		})
	}
}

func TestFaultQueueIsFIFOWithinMatchingClass(t *testing.T) {
	s := &Server{}
	if err := s.QueueFault(Fault{Kind: FaultControlDelay, Operation: "RREG"}); err != nil {
		t.Fatal(err)
	}
	if err := s.QueueFault(Fault{Kind: FaultCRC}); err != nil {
		t.Fatal(err)
	}
	fault, ok := s.takeFault(func(f Fault) bool { return isMutationFault(f.Kind) })
	if !ok || fault.Kind != FaultCRC {
		t.Fatalf("fault=%#v ok=%v", fault, ok)
	}
	fault, ok = s.takeFault(func(f Fault) bool { return isControlFault(f.Kind) })
	if !ok || fault.Kind != FaultControlDelay {
		t.Fatalf("fault=%#v ok=%v", fault, ok)
	}
}
