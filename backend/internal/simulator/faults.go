package simulator

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

type FaultKind string

const (
	FaultControlDelay        FaultKind = "control_delay"
	FaultControlTimeout      FaultKind = "control_timeout"
	FaultControlDisconnect   FaultKind = "control_disconnect"
	FaultPartialReply        FaultKind = "partial_reply"
	FaultStreamDelay         FaultKind = "stream_delay"
	FaultStreamDisconnect    FaultKind = "stream_disconnect"
	FaultStreamTruncation    FaultKind = "stream_truncation"
	FaultMalformedDescriptor FaultKind = "malformed_descriptor"
	FaultInvalidSize         FaultKind = "invalid_size"
	FaultInvalidOffset       FaultKind = "invalid_offset"
	FaultCRC                 FaultKind = "crc"
	FaultMissingService      FaultKind = "missing_service"
	FaultMissingCompletion   FaultKind = "missing_completion"
	FaultStalledDrain        FaultKind = "stalled_drain"
)

// Fault is a deterministic one-shot simulator action. Operation optionally
// restricts control faults to an opcode such as "RREG" or "FCMD". AfterBytes
// controls partial replies and stream truncation.
type Fault struct {
	Kind       FaultKind
	Operation  string
	Delay      time.Duration
	AfterBytes int
}

func (f Fault) validate() error {
	switch f.Kind {
	case FaultControlDelay, FaultControlTimeout, FaultControlDisconnect, FaultPartialReply,
		FaultStreamDelay, FaultStreamDisconnect, FaultStreamTruncation, FaultMalformedDescriptor,
		FaultInvalidSize, FaultInvalidOffset, FaultCRC, FaultMissingService, FaultMissingCompletion, FaultStalledDrain:
	default:
		return fmt.Errorf("unknown simulator fault %q", f.Kind)
	}
	if f.Delay < 0 || f.AfterBytes < 0 {
		return fmt.Errorf("invalid simulator fault parameters")
	}
	return nil
}

type streamItem struct {
	data  []byte
	fault *Fault
}

func (s *Server) QueueFault(fault Fault) error {
	if err := fault.validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = append(s.faults, fault)
	return nil
}

func (s *Server) takeFault(match func(Fault) bool) (Fault, bool) {
	for index, fault := range s.faults {
		if match(fault) {
			s.faults = append(s.faults[:index], s.faults[index+1:]...)
			return fault, true
		}
	}
	return Fault{}, false
}

func isControlFault(kind FaultKind) bool {
	return kind == FaultControlDelay || kind == FaultControlTimeout || kind == FaultControlDisconnect || kind == FaultPartialReply
}
func isStreamFault(kind FaultKind) bool {
	return kind == FaultStreamDelay || kind == FaultStreamDisconnect || kind == FaultStreamTruncation
}
func isMutationFault(kind FaultKind) bool {
	return kind == FaultMalformedDescriptor || kind == FaultInvalidSize || kind == FaultInvalidOffset || kind == FaultCRC
}

type faultWriter struct {
	net.Conn
	fault Fault
	used  bool
}

func (w *faultWriter) Write(data []byte) (int, error) {
	if w.fault.Kind != FaultPartialReply || w.used {
		return w.Conn.Write(data)
	}
	w.used = true
	n := w.fault.AfterBytes
	if n == 0 {
		n = 1
	}
	if n > len(data) {
		n = len(data)
	}
	written, err := w.Conn.Write(data[:n])
	if err != nil {
		return written, err
	}
	return written, io.ErrUnexpectedEOF
}

func mutateBatch(batch []byte, fault Fault) []byte {
	out := append([]byte(nil), batch...)
	if len(out) < 44 {
		return out
	}
	switch fault.Kind {
	case FaultMalformedDescriptor:
		word := binary.LittleEndian.Uint32(out[40:])
		binary.LittleEndian.PutUint32(out[40:], word&^0xff|0xff)
	case FaultInvalidSize:
		word := binary.LittleEndian.Uint32(out[12:])
		binary.LittleEndian.PutUint32(out[12:], word&0xff000000|0x00ffffff)
	case FaultInvalidOffset:
		word0 := binary.LittleEndian.Uint32(out[12:])
		binary.LittleEndian.PutUint32(out[12:], word0&0x00ffffff|0xff000000)
		word1 := binary.LittleEndian.Uint32(out[16:])
		binary.LittleEndian.PutUint32(out[16:], word1|0x00ffffff)
	case FaultCRC:
		word := binary.LittleEndian.Uint32(out[40:])
		binary.LittleEndian.PutUint32(out[40:], word|1<<16)
	}
	return out
}

// prepareStreamItem is called with s.mu held so fault consumption and event
// sequencing stay deterministic across concurrent control connections.
func (s *Server) prepareStreamItem(batch []byte, service, completion bool) (streamItem, bool) {
	if _, ok := s.takeFault(func(f Fault) bool {
		return (service && f.Kind == FaultMissingService) || (completion && (f.Kind == FaultMissingCompletion || f.Kind == FaultStalledDrain))
	}); ok {
		return streamItem{}, true
	}
	if fault, ok := s.takeFault(func(f Fault) bool { return isMutationFault(f.Kind) }); ok {
		batch = mutateBatch(batch, fault)
	}
	item := streamItem{data: append([]byte(nil), batch...)}
	if fault, ok := s.takeFault(func(f Fault) bool { return isStreamFault(f.Kind) }); ok {
		item.fault = &fault
	}
	return item, false
}
