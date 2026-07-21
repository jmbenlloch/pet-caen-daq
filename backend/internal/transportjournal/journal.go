// Package transportjournal preserves stream bytes below protocol framing.
package transportjournal

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

var magic = []byte{'P', 'E', 'T', 'J', 'R', 'N', 1, '\n'}

const MaxRecordSize = 64 * 1024 * 1024

type Kind uint8

const (
	Data           Kind = 1
	Termination    Kind = 2
	FramingFailure Kind = 3
)

type Record struct {
	Kind              Kind
	Offset            uint64
	TimestampUnixNano int64
	ConnectionID      string
	Stage             string
	Reason            string
	Data              []byte
}
type Sink interface{ AppendRecord(Record) error }

type Writer struct {
	w      io.Writer
	closed bool
}

func NewWriter(w io.Writer) (*Writer, error) {
	if err := writeAll(w, magic); err != nil {
		return nil, fmt.Errorf("write transport journal header: %w", err)
	}
	return &Writer{w: w}, nil
}
func (w *Writer) AppendRecord(record Record) error {
	if w.closed {
		return errors.New("transport journal writer is closed")
	}
	if record.Kind < Data || record.Kind > FramingFailure {
		return fmt.Errorf("invalid transport record kind %d", record.Kind)
	}
	if record.ConnectionID == "" {
		return errors.New("transport connection identity is required")
	}
	if len(record.ConnectionID) > 65535 || len(record.Stage) > 65535 || len(record.Reason) > 65535 {
		return errors.New("transport record metadata is too long")
	}
	if len(record.Data) > MaxRecordSize {
		return fmt.Errorf("transport data length %d exceeds %d", len(record.Data), MaxRecordSize)
	}
	if record.Kind == Data && len(record.Data) == 0 {
		return errors.New("transport data record is empty")
	}
	if record.Kind != Data && record.Reason == "" {
		return errors.New("transport failure reason is required")
	}
	bodySize := 30 + len(record.ConnectionID) + len(record.Stage) + len(record.Reason) + len(record.Data)
	if bodySize > MaxRecordSize {
		return fmt.Errorf("transport record size %d exceeds %d", bodySize, MaxRecordSize)
	}
	body := make([]byte, bodySize)
	body[0] = byte(record.Kind)
	binary.LittleEndian.PutUint64(body[4:], record.Offset)
	binary.LittleEndian.PutUint64(body[12:], uint64(record.TimestampUnixNano))
	binary.LittleEndian.PutUint16(body[20:], uint16(len(record.ConnectionID)))
	binary.LittleEndian.PutUint16(body[22:], uint16(len(record.Stage)))
	binary.LittleEndian.PutUint16(body[24:], uint16(len(record.Reason)))
	binary.LittleEndian.PutUint32(body[26:], uint32(len(record.Data)))
	p := 30
	copy(body[p:], record.ConnectionID)
	p += len(record.ConnectionID)
	copy(body[p:], record.Stage)
	p += len(record.Stage)
	copy(body[p:], record.Reason)
	p += len(record.Reason)
	copy(body[p:], record.Data)
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header, uint32(len(body)))
	binary.LittleEndian.PutUint32(header[4:], crc32.ChecksumIEEE(body))
	if err := writeAll(w.w, header); err != nil {
		return fmt.Errorf("write transport record header: %w", err)
	}
	if err := writeAll(w.w, body); err != nil {
		return fmt.Errorf("write transport record: %w", err)
	}
	return nil
}
func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if syncer, ok := w.w.(interface{ Sync() error }); ok {
		if err := syncer.Sync(); err != nil {
			return fmt.Errorf("sync transport journal: %w", err)
		}
	}
	if closer, ok := w.w.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type Reader struct {
	r      *bufio.Reader
	index  uint64
	offset int64
}

func NewReader(r io.Reader) (*Reader, error) {
	b := bufio.NewReader(r)
	header := make([]byte, len(magic))
	if _, err := io.ReadFull(b, header); err != nil {
		return nil, fmt.Errorf("read transport journal header: %w", err)
	}
	if string(header) != string(magic) {
		return nil, errors.New("invalid transport journal magic or version")
	}
	return &Reader{r: b, offset: int64(len(magic))}, nil
}
func (r *Reader) Next() (Record, error) {
	start := r.offset
	header := make([]byte, 8)
	if _, err := io.ReadFull(r.r, header); err != nil {
		if errors.Is(err, io.EOF) {
			return Record{}, io.EOF
		}
		return Record{}, fmt.Errorf("transport record %d at offset %d header: %w", r.index+1, start, err)
	}
	size := binary.LittleEndian.Uint32(header)
	if size < 30 || size > MaxRecordSize {
		return Record{}, fmt.Errorf("transport record %d at offset %d size %d out of range", r.index+1, start, size)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r.r, body); err != nil {
		return Record{}, fmt.Errorf("transport record %d at offset %d truncated: %w", r.index+1, start, err)
	}
	if got, want := crc32.ChecksumIEEE(body), binary.LittleEndian.Uint32(header[4:]); got != want {
		return Record{}, fmt.Errorf("transport record %d at offset %d checksum %08x, want %08x", r.index+1, start, got, want)
	}
	cl, sl, rl, dl := int(binary.LittleEndian.Uint16(body[20:])), int(binary.LittleEndian.Uint16(body[22:])), int(binary.LittleEndian.Uint16(body[24:])), int(binary.LittleEndian.Uint32(body[26:]))
	if 30+cl+sl+rl+dl != len(body) {
		return Record{}, fmt.Errorf("transport record %d at offset %d malformed lengths", r.index+1, start)
	}
	p := 30
	record := Record{Kind: Kind(body[0]), Offset: binary.LittleEndian.Uint64(body[4:]), TimestampUnixNano: int64(binary.LittleEndian.Uint64(body[12:])), ConnectionID: string(body[p : p+cl])}
	p += cl
	record.Stage = string(body[p : p+sl])
	p += sl
	record.Reason = string(body[p : p+rl])
	p += rl
	record.Data = append([]byte(nil), body[p:p+dl]...)
	if record.Kind < Data || record.Kind > FramingFailure {
		return Record{}, fmt.Errorf("transport record %d invalid kind %d", r.index+1, record.Kind)
	}
	r.offset += int64(8 + size)
	r.index++
	return record, nil
}

// Replay returns the exact concatenated data records for one connection plus
// its ordered framing/termination evidence.
func Replay(r *Reader, connectionID string) ([]byte, []Record, error) {
	var data []byte
	var failures []Record
	var expected uint64
	for {
		record, err := r.Next()
		if errors.Is(err, io.EOF) {
			return data, failures, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if record.ConnectionID != connectionID {
			continue
		}
		if record.Offset != expected {
			return nil, nil, fmt.Errorf("transport replay connection %q offset %d, want %d", connectionID, record.Offset, expected)
		}
		if record.Kind == Data {
			data = append(data, record.Data...)
			expected += uint64(len(record.Data))
		} else {
			failures = append(failures, record)
		}
	}
}
func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
