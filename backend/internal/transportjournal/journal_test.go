package transportjournal

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestJournalRoundTripAndReplay(t *testing.T) {
	var first, second bytes.Buffer
	for _, dst := range []*bytes.Buffer{&first, &second} {
		writer, err := NewWriter(dst)
		if err != nil {
			t.Fatal(err)
		}
		records := []Record{{Kind: Data, Offset: 0, TimestampUnixNano: 10, ConnectionID: "stream-1", Stage: "header", Data: []byte{1, 2}}, {Kind: Data, Offset: 2, TimestampUnixNano: 11, ConnectionID: "stream-1", Stage: "descriptor", Data: []byte{3}}, {Kind: FramingFailure, Offset: 3, TimestampUnixNano: 12, ConnectionID: "stream-1", Stage: "descriptor", Reason: "invalid node 255"}}
		for _, record := range records {
			if err = writer.AppendRecord(record); err != nil {
				t.Fatal(err)
			}
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("journal is not deterministic")
	}
	reader, err := NewReader(bytes.NewReader(first.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	data, failures, err := Replay(reader, "stream-1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte{1, 2, 3}) || len(failures) != 1 || failures[0].Reason != "invalid node 255" {
		t.Fatalf("data=%x failures=%#v", data, failures)
	}
}
func TestJournalRejectsMalformed(t *testing.T) {
	var buffer bytes.Buffer
	writer, _ := NewWriter(&buffer)
	if err := writer.AppendRecord(Record{Kind: Data, ConnectionID: "c"}); err == nil {
		t.Fatal("accepted empty data")
	}
	_ = writer.AppendRecord(Record{Kind: Data, ConnectionID: "c", Data: []byte("evidence")})
	raw := buffer.Bytes()
	corrupt := append([]byte(nil), raw...)
	corrupt[len(corrupt)-1] ^= 1
	for _, tt := range []struct {
		name string
		data []byte
		want string
	}{{"header", raw[:4], "header"}, {"truncated", raw[:len(raw)-1], "truncated"}, {"checksum", corrupt, "checksum"}} {
		t.Run(tt.name, func(t *testing.T) {
			reader, err := NewReader(bytes.NewReader(tt.data))
			if err == nil {
				_, err = reader.Next()
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
func TestReplayRejectsOffsetGap(t *testing.T) {
	var buffer bytes.Buffer
	writer, _ := NewWriter(&buffer)
	_ = writer.AppendRecord(Record{Kind: Data, Offset: 1, ConnectionID: "c", Data: []byte{1}})
	reader, _ := NewReader(bytes.NewReader(buffer.Bytes()))
	if _, _, err := Replay(reader, "c"); err == nil {
		t.Fatal("accepted offset gap")
	}
	reader, _ = NewReader(bytes.NewReader(buffer.Bytes()))
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatalf("end=%v", err)
	}
}
