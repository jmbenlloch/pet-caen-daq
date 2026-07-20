// Package rawcapture stores byte-exact DT5215 stream batches for offline replay.
package rawcapture

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

var magic = []byte{'P', 'E', 'T', 'R', 'A', 'W', 1, '\n'}

const MaxRecordSize = 64 * 1024 * 1024

type Writer struct {
	w      io.Writer
	closed bool
}

func NewWriter(w io.Writer) (*Writer, error) {
	if err := writeAll(w, magic); err != nil {
		return nil, fmt.Errorf("write raw capture header: %w", err)
	}
	return &Writer{w: w}, nil
}
func (w *Writer) Append(batch []byte) error {
	if w.closed {
		return errors.New("raw capture writer is closed")
	}
	if len(batch) == 0 || len(batch) > MaxRecordSize {
		return fmt.Errorf("raw batch size %d out of range", len(batch))
	}
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header, uint32(len(batch)))
	binary.LittleEndian.PutUint32(header[4:], crc32.ChecksumIEEE(batch))
	if err := writeAll(w.w, header); err != nil {
		return fmt.Errorf("write raw record header: %w", err)
	}
	if err := writeAll(w.w, batch); err != nil {
		return fmt.Errorf("write raw record payload: %w", err)
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
			return fmt.Errorf("sync raw capture: %w", err)
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
		return nil, fmt.Errorf("read raw capture header: %w", err)
	}
	if string(header) != string(magic) {
		return nil, errors.New("invalid raw capture magic or version")
	}
	return &Reader{r: b, offset: int64(len(magic))}, nil
}
func (r *Reader) Next() ([]byte, error) {
	start := r.offset
	header := make([]byte, 8)
	if _, err := io.ReadFull(r.r, header); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("raw record %d at offset %d header: %w", r.index+1, start, err)
	}
	size := binary.LittleEndian.Uint32(header)
	if size == 0 || size > MaxRecordSize {
		return nil, fmt.Errorf("raw record %d at offset %d size %d out of range", r.index+1, start, size)
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(r.r, payload); err != nil {
		return nil, fmt.Errorf("raw record %d at offset %d truncated payload: %w", r.index+1, start, err)
	}
	want := binary.LittleEndian.Uint32(header[4:])
	got := crc32.ChecksumIEEE(payload)
	if got != want {
		return nil, fmt.Errorf("raw record %d at offset %d checksum %08x, want %08x", r.index+1, start, got, want)
	}
	r.offset += int64(8 + size)
	r.index++
	return payload, nil
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
