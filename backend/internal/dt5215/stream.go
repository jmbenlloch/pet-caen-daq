package dt5215

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// ReadStreamBatch reads exactly one batch from the stream TCP connection. Its
// size is derived from the descriptor table, so partial TCP delivery is safe.
func (c *Client) ReadStreamBatch(ctx context.Context) ([]StreamEvent, error) {
	_, events, err := c.ReadRawStreamBatch(ctx)
	return events, err
}

// ReadRawStreamBatch also returns the byte-exact batch for evidence capture.
func (c *Client) ReadRawStreamBatch(ctx context.Context) ([]byte, []StreamEvent, error) {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	deadline := time.Now().Add(defaultOperationTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.stream.SetReadDeadline(deadline); err != nil {
		return nil, nil, fmt.Errorf("set stream deadline: %w", err)
	}
	cancelDone := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() { _ = c.stream.SetReadDeadline(time.Now()); close(cancelDone) })
	defer func() {
		if !stopCancel() {
			<-cancelDone
		}
		_ = c.stream.SetReadDeadline(time.Time{})
	}()
	header := make([]byte, 12)
	if _, err := io.ReadFull(c.stream, header); err != nil {
		if ctxErr := operationContextError(ctx); ctxErr != nil {
			return nil, nil, ctxErr
		}
		return nil, nil, fmt.Errorf("read stream batch header: %w", err)
	}
	w2 := binary.LittleEndian.Uint32(header[8:])
	rows := int(w2 >> 8)
	if binary.LittleEndian.Uint32(header) != 0xffffffff || binary.LittleEndian.Uint32(header[4:]) != 0xffffffff {
		return nil, nil, fmt.Errorf("invalid stream batch sentinel")
	}
	if rows == 0 || rows > MaxDescriptorRows {
		return nil, nil, fmt.Errorf("invalid descriptor row count %d", rows)
	}
	table := make([]byte, rows*32)
	if _, err := io.ReadFull(c.stream, table); err != nil {
		if ctxErr := operationContextError(ctx); ctxErr != nil {
			return nil, nil, ctxErr
		}
		return nil, nil, fmt.Errorf("read stream descriptor table: %w", err)
	}
	var extent uint64
	for i := 0; i < rows; i++ {
		p := table[i*32:]
		w0 := binary.LittleEndian.Uint32(p)
		w1 := binary.LittleEndian.Uint32(p[4:])
		offset := uint64((w0 >> 24) | ((w1 & 0x00ffffff) << 8))
		size := uint64(w0 & 0x00ffffff)
		if size*4 > MaxStreamEventBytes {
			return nil, nil, fmt.Errorf("descriptor %d payload too large", i)
		}
		if end := (offset + size) * 4; end > extent {
			extent = end
		}
	}
	if extent > MaxStreamBatchBytes {
		return nil, nil, fmt.Errorf("stream batch payload extent %d is too large", extent)
	}
	payload := make([]byte, int(extent))
	if _, err := io.ReadFull(c.stream, payload); err != nil {
		if ctxErr := operationContextError(ctx); ctxErr != nil {
			return nil, nil, ctxErr
		}
		return nil, nil, fmt.Errorf("read stream payload: %w", err)
	}
	batch := append(header, table...)
	batch = append(batch, payload...)
	events, err := DecodeStreamBatch(batch)
	if err != nil {
		return batch, nil, err
	}
	return batch, events, nil
}

const (
	MaxStreamEventBytes = 64 * 1024
	// MAX_NROW_EDTAB in the source reference is 64K. The independent batch
	// bound prevents sparse descriptors from forcing unbounded payload reads.
	MaxDescriptorRows   = 64 * 1024
	MaxStreamBatchBytes = 64 * 1024 * 1024
)

// Descriptor is one source-confirmed DT5215 stream descriptor row.
type Descriptor struct {
	PayloadOffsetWords uint32
	PayloadSizeWords   uint32
	Timestamp          uint64
	TriggerID          uint64
	Node, Qualifier    uint8
	CRCError           bool
}

type StreamEvent struct {
	Chain      uint8
	Descriptor Descriptor
	Payload    []byte
}

// DecodeStreamBatch decodes one complete DT5215 descriptor-table batch. The
// returned payload slices are immutable copies and do not alias batch.
func DecodeStreamBatch(batch []byte) ([]StreamEvent, error) {
	if len(batch) < 12 {
		return nil, fmt.Errorf("stream batch header: need 12 bytes, got %d", len(batch))
	}
	if binary.LittleEndian.Uint32(batch[0:4]) != 0xffffffff || binary.LittleEndian.Uint32(batch[4:8]) != 0xffffffff {
		return nil, fmt.Errorf("invalid stream batch sentinel")
	}
	w2 := binary.LittleEndian.Uint32(batch[8:12])
	chain := uint8(w2)
	if chain >= MaxChains {
		return nil, fmt.Errorf("stream chain %d out of range", chain)
	}
	rows := int(w2 >> 8)
	if rows == 0 || rows > MaxDescriptorRows {
		return nil, fmt.Errorf("invalid descriptor row count %d", rows)
	}
	tableEnd := 12 + rows*32
	if len(batch) < tableEnd {
		return nil, fmt.Errorf("truncated descriptor table: need %d bytes, got %d", tableEnd, len(batch))
	}
	payload := batch[tableEnd:]
	if len(payload) > MaxStreamBatchBytes {
		return nil, fmt.Errorf("stream batch payload %d exceeds %d bytes", len(payload), MaxStreamBatchBytes)
	}
	events := make([]StreamEvent, 0, rows)
	for i := 0; i < rows; i++ {
		p := batch[12+i*32 : 12+(i+1)*32]
		var w [8]uint32
		for j := range w {
			w[j] = binary.LittleEndian.Uint32(p[j*4:])
		}
		d := Descriptor{PayloadOffsetWords: (w[0] >> 24) | ((w[1] & 0x00ffffff) << 8), PayloadSizeWords: w[0] & 0x00ffffff, Timestamp: uint64(w[1]>>24) | (uint64(w[2]) << 8) | (uint64(w[3]&0xffff) << 40), TriggerID: uint64(w[3]>>16) | (uint64(w[4]) << 16) | (uint64(w[5]&0xff) << 48), Node: uint8(w[7]), Qualifier: uint8(w[7] >> 8), CRCError: (w[7]>>16)&1 != 0}
		if d.Node >= MaxNodes {
			return nil, fmt.Errorf("descriptor %d node %d out of range", i, d.Node)
		}
		size := uint64(d.PayloadSizeWords) * 4
		offset := uint64(d.PayloadOffsetWords) * 4
		if size > MaxStreamEventBytes {
			return nil, fmt.Errorf("descriptor %d payload size %d exceeds %d", i, size, MaxStreamEventBytes)
		}
		if offset+size > uint64(len(payload)) {
			return nil, fmt.Errorf("descriptor %d payload [%d,%d) exceeds %d bytes", i, offset, offset+size, len(payload))
		}
		copyPayload := append([]byte(nil), payload[offset:offset+size]...)
		events = append(events, StreamEvent{Chain: chain, Descriptor: d, Payload: copyPayload})
	}
	return events, nil
}
