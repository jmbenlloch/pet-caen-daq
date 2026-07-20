// Package januslist reads JANUS DT5202 processed binary list files.
package januslist

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	HeaderSize    = 25
	MaxRecordSize = 64 * 1024
)

type Header struct {
	FormatMajor, FormatMinor                    uint8
	SoftwareMajor, SoftwareMinor, SoftwarePatch uint8
	BoardFamily                                 uint16
	RunNumber                                   uint16
	AcquisitionMode                             uint8
	EnergyBins                                  uint16
	TimeUnit                                    uint8
	ToALSBNS                                    float32
	StartUnixMilli                              uint64
}

type Hit struct {
	Channel, DataType  uint8
	EnergyLG, EnergyHG uint16
	ToANS, ToTNS       float32
}

type Event struct {
	Board                        uint8
	TimestampUS, TimeReferenceUS float64
	TriggerID, ChannelMask       uint64
	Hits                         []Hit
}

type Reader struct {
	r      *bufio.Reader
	Header Header
	offset int64
	index  uint64
}

func NewReader(r io.Reader) (*Reader, error) {
	b := bufio.NewReader(r)
	raw := make([]byte, HeaderSize)
	if _, err := io.ReadFull(b, raw); err != nil {
		return nil, fmt.Errorf("read JANUS header: %w", err)
	}
	h := Header{
		FormatMajor: raw[0], FormatMinor: raw[1], SoftwareMajor: raw[2], SoftwareMinor: raw[3], SoftwarePatch: raw[4],
		BoardFamily: binary.LittleEndian.Uint16(raw[5:7]), RunNumber: binary.LittleEndian.Uint16(raw[7:9]),
		AcquisitionMode: raw[9], EnergyBins: binary.LittleEndian.Uint16(raw[10:12]), TimeUnit: raw[12],
		ToALSBNS: math.Float32frombits(binary.LittleEndian.Uint32(raw[13:17])), StartUnixMilli: binary.LittleEndian.Uint64(raw[17:25]),
	}
	if h.FormatMajor != 3 || h.FormatMinor != 4 {
		return nil, fmt.Errorf("unsupported JANUS list format %d.%d (want 3.4)", h.FormatMajor, h.FormatMinor)
	}
	if h.BoardFamily != 5202 {
		return nil, fmt.Errorf("unsupported JANUS board family %d", h.BoardFamily)
	}
	if h.AcquisitionMode&0x0f != 3 {
		return nil, fmt.Errorf("unsupported JANUS acquisition mode 0x%02x", h.AcquisitionMode)
	}
	if h.TimeUnit > 1 {
		return nil, fmt.Errorf("invalid JANUS time unit %d", h.TimeUnit)
	}
	return &Reader{r: b, Header: h, offset: HeaderSize}, nil
}

func (r *Reader) Next() (Event, error) {
	start := r.offset
	szraw := make([]byte, 2)
	if _, err := io.ReadFull(r.r, szraw); err != nil {
		if errors.Is(err, io.EOF) {
			return Event{}, io.EOF
		}
		return Event{}, fmt.Errorf("JANUS event %d at offset %d: read size: %w", r.index+1, start, err)
	}
	size := int(binary.LittleEndian.Uint16(szraw))
	if size < 37 || size > MaxRecordSize {
		return Event{}, fmt.Errorf("JANUS event %d at offset %d: invalid record size %d", r.index+1, start, size)
	}
	payload := make([]byte, size-2)
	if _, err := io.ReadFull(r.r, payload); err != nil {
		return Event{}, fmt.Errorf("JANUS event %d at offset %d: truncated %d-byte record: %w", r.index+1, start, size, err)
	}
	r.offset += int64(size)
	r.index++
	e, err := r.decode(payload)
	if err != nil {
		return Event{}, fmt.Errorf("JANUS event %d at offset %d: %w", r.index, start, err)
	}
	return e, nil
}

func (r *Reader) decode(p []byte) (Event, error) {
	if len(p) < 35 {
		return Event{}, io.ErrUnexpectedEOF
	}
	e := Event{Board: p[0], TimestampUS: f64(p[1:9]), TimeReferenceUS: f64(p[9:17]), TriggerID: u64(p[17:25]), ChannelMask: u64(p[25:33])}
	n := int(binary.LittleEndian.Uint16(p[33:35]))
	p = p[35:]
	if n > 64 {
		return Event{}, fmt.Errorf("hit count %d exceeds 64", n)
	}
	e.Hits = make([]Hit, 0, n)
	for i := 0; i < n; i++ {
		if len(p) < 2 {
			return Event{}, fmt.Errorf("hit %d header: %w", i, io.ErrUnexpectedEOF)
		}
		h := Hit{Channel: p[0], DataType: p[1]}
		p = p[2:]
		if h.Channel >= 64 {
			return Event{}, fmt.Errorf("hit %d channel %d out of range", i, h.Channel)
		}
		if h.DataType & ^uint8(0x33) != 0 {
			return Event{}, fmt.Errorf("hit %d invalid data type 0x%02x", i, h.DataType)
		}
		var ok bool
		if h.DataType&1 != 0 {
			h.EnergyLG, p, ok = take16(p)
			if !ok {
				return Event{}, io.ErrUnexpectedEOF
			}
		}
		if h.DataType&2 != 0 {
			h.EnergyHG, p, ok = take16(p)
			if !ok {
				return Event{}, io.ErrUnexpectedEOF
			}
		}
		if h.DataType&0x10 != 0 {
			h.ToANS, p, ok = r.takeTime(p)
			if !ok {
				return Event{}, io.ErrUnexpectedEOF
			}
		}
		if h.DataType&0x20 != 0 {
			h.ToTNS, p, ok = r.takeToT(p)
			if !ok {
				return Event{}, io.ErrUnexpectedEOF
			}
		}
		e.Hits = append(e.Hits, h)
	}
	if len(p) != 0 {
		return Event{}, fmt.Errorf("%d unconsumed record bytes", len(p))
	}
	return e, nil
}

func (r *Reader) takeTime(p []byte) (float32, []byte, bool) {
	if len(p) < 4 {
		return 0, p, false
	}
	v := binary.LittleEndian.Uint32(p)
	if r.Header.TimeUnit == 1 {
		return math.Float32frombits(v), p[4:], true
	}
	return float32(v) * r.Header.ToALSBNS, p[4:], true
}
func (r *Reader) takeToT(p []byte) (float32, []byte, bool) {
	if r.Header.TimeUnit == 1 {
		return r.takeTime(p)
	}
	if len(p) < 2 {
		return 0, p, false
	}
	return float32(binary.LittleEndian.Uint16(p)) * r.Header.ToALSBNS, p[2:], true
}
func take16(p []byte) (uint16, []byte, bool) {
	if len(p) < 2 {
		return 0, p, false
	}
	return binary.LittleEndian.Uint16(p), p[2:], true
}
func u64(p []byte) uint64  { return binary.LittleEndian.Uint64(p) }
func f64(p []byte) float64 { return math.Float64frombits(u64(p)) }
