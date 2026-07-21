package dt5215

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/transportjournal"
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
func TestStreamJournalPreservesConnectionTruncation(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	var capture bytes.Buffer
	journal, _ := transportjournal.NewWriter(&capture)
	client := &Client{stream: clientConn}
	client.SetStreamJournal(journal, "connection-7", func() time.Time { return time.Unix(0, 123) })
	go func() { _, _ = serverConn.Write([]byte{0xff, 0xff, 0xff, 0xff, 0xff}); _ = serverConn.Close() }()
	_, _, err := client.ReadRawStreamBatch(context.Background())
	if err == nil {
		t.Fatal("accepted truncated header")
	}
	_ = journal.Close()
	reader, err := transportjournal.NewReader(bytes.NewReader(capture.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	data, failures, err := transportjournal.Replay(reader, "connection-7")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte{0xff, 0xff, 0xff, 0xff, 0xff}) || len(failures) != 1 || failures[0].Kind != transportjournal.Termination || failures[0].Stage != "header" || failures[0].TimestampUnixNano != 123 {
		t.Fatalf("data=%x failures=%#v", data, failures)
	}
}
func TestStreamJournalPreservesMalformedHeader(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	var capture bytes.Buffer
	journal, _ := transportjournal.NewWriter(&capture)
	client := &Client{stream: clientConn}
	client.SetStreamJournal(journal, "connection-8", nil)
	go func() { _, _ = serverConn.Write(make([]byte, 12)) }()
	_, _, err := client.ReadRawStreamBatch(context.Background())
	if err == nil {
		t.Fatal("accepted malformed header")
	}
	reader, _ := transportjournal.NewReader(bytes.NewReader(capture.Bytes()))
	data, failures, err := transportjournal.Replay(reader, "connection-8")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 12 || len(failures) != 1 || failures[0].Kind != transportjournal.FramingFailure || failures[0].Reason != "invalid stream batch sentinel" {
		t.Fatalf("data=%x failures=%#v", data, failures)
	}
}
func TestReadRawStreamBatchCancellationInterruptsStalledRead(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	client := &Client{stream: clientConn}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(10*time.Millisecond, cancel)
	started := time.Now()
	_, _, err := client.ReadRawStreamBatch(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("cancellation took %s", time.Since(started))
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
