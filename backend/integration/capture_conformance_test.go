//go:build integration && capture

package integration

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/rawcapture"
)

func TestNativeRunRawCaptureConformance(t *testing.T) {
	path := os.Getenv("NATIVE_RUN_RAW_CAPTURE")
	if path == "" {
		path = "../../../pcap/runs/run-go-native-detector-hvon-003/wire.raw"
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("external native raw capture not found at %s", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := rawcapture.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	var batches, events int
	var chains [dt5215.MaxChains]int
	qualifiers := make(map[uint8]int)
	kinds := make(map[dt5202.EventKind]int)
	for {
		batch, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatalf("raw batch %d: %v", batches, readErr)
		}
		wireEvents, decodeErr := dt5215.DecodeStreamBatch(batch)
		if decodeErr != nil {
			t.Fatalf("raw batch %d: %v", batches, decodeErr)
		}
		batches++
		for _, wire := range wireEvents {
			decoded, decodeErr := dt5202.DecodeEvent(wire.Descriptor.Qualifier, wire.Descriptor.TriggerID, wire.Descriptor.Timestamp, wire.Payload)
			if decodeErr != nil {
				t.Fatalf("batch %d chain %d qualifier 0x%02x: %v", batches-1, wire.Chain, wire.Descriptor.Qualifier, decodeErr)
			}
			events++
			chains[wire.Chain]++
			qualifiers[wire.Descriptor.Qualifier]++
			kinds[decoded.Kind]++
		}
	}
	t.Logf("batches=%d events=%d chains=%v qualifiers=%v kinds=%v", batches, events, chains, qualifiers, kinds)
	if batches != 36_255 || events != 87_989 {
		t.Fatalf("native raw totals changed: batches=%d events=%d", batches, events)
	}
}

const defaultJANUSDataTakingPCAP = "../../../pcap/janus_data_taking.pcap"

type tcpSegment struct {
	sequence uint32
	payload  []byte
}

type tcpFlow struct {
	clientIP   string
	clientPort uint16
	segments   []tcpSegment
}

// TestJANUSDataTakingCaptureConformance replays the complete DT5215 port-9000
// TCP byte stream captured during a real four-board JANUS run through the
// production framing and DT5202 event decoders. The large external evidence
// file is intentionally opt-in and remains outside normal Git fixtures.
func TestJANUSDataTakingCaptureConformance(t *testing.T) {
	path := os.Getenv("JANUS_DATA_TAKING_PCAP")
	if path == "" {
		path = defaultJANUSDataTakingPCAP
	}
	sourceIP := os.Getenv("JANUS_DATA_TAKING_SOURCE_IP")
	if sourceIP == "" {
		sourceIP = "172.16.0.11"
	}
	flows, err := extractServerTCPStreams(path, sourceIP, 9000)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("external capture not found at %s", path)
	}
	if err != nil {
		t.Fatal(err)
	}

	type counts struct {
		batches, descriptors, decoded, payloadBytes int
		byChain                                     [dt5215.MaxChains]int
		byQualifier                                 map[uint8]int
		byKind                                      map[dt5202.EventKind]int
		byServiceVersion                            map[uint8]int
	}
	got := counts{byQualifier: make(map[uint8]int), byKind: make(map[dt5202.EventKind]int), byServiceVersion: make(map[uint8]int)}
	streamBytes := 0
	streamHash := sha256.New()
	for _, flow := range flows {
		stream := reassembleTCPFlow(t, flow)
		streamBytes += len(stream)
		_, _ = streamHash.Write(stream)
		for offset := 0; offset < len(stream); {
			batch, next, err := nextCapturedBatch(stream, offset)
			if err != nil {
				t.Fatalf("batch %d at stream offset %d: %v", got.batches, offset, err)
			}
			events, err := dt5215.DecodeStreamBatch(batch)
			if err != nil {
				t.Fatalf("batch %d at stream offset %d: %v", got.batches, offset, err)
			}
			got.batches++
			for row, wire := range events {
				got.descriptors++
				got.payloadBytes += len(wire.Payload)
				got.byChain[wire.Chain]++
				got.byQualifier[wire.Descriptor.Qualifier]++
				decoded, err := dt5202.DecodeEvent(wire.Descriptor.Qualifier, wire.Descriptor.TriggerID, wire.Descriptor.Timestamp, wire.Payload)
				if err != nil {
					preview := wire.Payload
					if len(preview) > 64 {
						preview = preview[:64]
					}
					t.Fatalf("batch %d row %d chain %d qualifier 0x%02x payload_bytes=%d payload_prefix=%x: %v", got.batches-1, row, wire.Chain, wire.Descriptor.Qualifier, len(wire.Payload), preview, err)
				}
				got.decoded++
				got.byKind[decoded.Kind]++
				if decoded.Service != nil {
					got.byServiceVersion[decoded.Service.Version]++
				}
			}
			offset = next
		}
	}

	t.Logf("flows=%d stream_sha256=%x stream_bytes=%d batches=%d descriptors=%d decoded=%d payload_bytes=%d chains=%v qualifiers=%v kinds=%v service_versions=%v", len(flows), streamHash.Sum(nil), streamBytes, got.batches, got.descriptors, got.decoded, got.payloadBytes, got.byChain, got.byQualifier, got.byKind, got.byServiceVersion)
	type golden struct {
		hash                                              string
		flows, streamBytes, batches, events, payloadBytes int
		chains                                            [dt5215.MaxChains]int
		qualifiers                                        map[uint8]int
	}
	goldens := map[string]golden{
		"janus_data_taking.pcap": {
			hash: "98ab7980e6d689d86b7f260bf7e978bbde6a734b0b07823fd7ddfa36d1383b44", flows: 1,
			streamBytes: 4_605_560, batches: 18_590, events: 18_784, payloadBytes: 3_781_392,
			chains: [dt5215.MaxChains]int{4_696, 4_696, 4_696, 4_696}, qualifiers: map[uint8]int{0x23: 18_668, 0x2f: 116},
		},
		"janus_data_taking_2.pcap": {
			hash: "cf75d42e3003d1547cde5be7e814b74ec5acd0612de9c238712433dfad3dfe1e", flows: 2,
			streamBytes: 59_541_516, batches: 54_081, events: 158_417, payloadBytes: 53_823_200,
			chains: [dt5215.MaxChains]int{33_484, 45_114, 59_342, 20_477}, qualifiers: map[uint8]int{0x2f: 184, 0x33: 158_233},
		},
	}
	want, known := goldens[filepath.Base(path)]
	if !known {
		t.Fatalf("capture %q decoded but has no golden profile", filepath.Base(path))
	}
	if fmt.Sprintf("%x", streamHash.Sum(nil)) != want.hash || len(flows) != want.flows || streamBytes != want.streamBytes || got.batches != want.batches || got.descriptors != want.events || got.decoded != want.events || got.payloadBytes != want.payloadBytes {
		t.Fatalf("capture totals changed: stream=%d batches=%d descriptors=%d decoded=%d payload=%d", streamBytes, got.batches, got.descriptors, got.decoded, got.payloadBytes)
	}
	for chain := range got.byChain {
		if got.byChain[chain] != want.chains[chain] {
			t.Fatalf("chain %d events = %d, want %d", chain, got.byChain[chain], want.chains[chain])
		}
	}
	if fmt.Sprint(got.byQualifier) != fmt.Sprint(want.qualifiers) {
		t.Fatalf("qualifier counts = %v", got.byQualifier)
	}
	if len(got.byKind) != 2 || got.byKind[dt5202.EventSpectroscopy] != want.events-want.qualifiers[0x2f] || got.byKind[dt5202.EventService] != want.qualifiers[0x2f] {
		t.Fatalf("event kind counts = %v", got.byKind)
	}
}

func nextCapturedBatch(stream []byte, offset int) ([]byte, int, error) {
	if len(stream)-offset < 12 {
		return nil, offset, fmt.Errorf("trailing stream bytes: %d", len(stream)-offset)
	}
	header := stream[offset : offset+12]
	if binary.LittleEndian.Uint32(header) != 0xffffffff || binary.LittleEndian.Uint32(header[4:]) != 0xffffffff {
		return nil, offset, fmt.Errorf("invalid sentinel %x", header[:8])
	}
	rows := int(binary.LittleEndian.Uint32(header[8:]) >> 8)
	if rows == 0 || rows > dt5215.MaxDescriptorRows {
		return nil, offset, fmt.Errorf("invalid descriptor row count %d", rows)
	}
	tableEnd := offset + 12 + rows*32
	if tableEnd > len(stream) {
		return nil, offset, io.ErrUnexpectedEOF
	}
	var extent uint64
	for row := 0; row < rows; row++ {
		descriptor := stream[offset+12+row*32:]
		word0 := binary.LittleEndian.Uint32(descriptor)
		word1 := binary.LittleEndian.Uint32(descriptor[4:])
		payloadOffset := uint64((word0 >> 24) | ((word1 & 0x00ffffff) << 8))
		payloadWords := uint64(word0 & 0x00ffffff)
		if end := (payloadOffset + payloadWords) * 4; end > extent {
			extent = end
		}
	}
	end := uint64(tableEnd) + extent
	if end > uint64(len(stream)) {
		return nil, offset, io.ErrUnexpectedEOF
	}
	return stream[offset:end], int(end), nil
}

func extractServerTCPStreams(path, sourceIP string, sourcePort uint16) ([]tcpFlow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	header := make([]byte, 24)
	if _, err := io.ReadFull(file, header); err != nil {
		return nil, fmt.Errorf("read PCAP header: %w", err)
	}
	if string(header[:4]) != "\xd4\xc3\xb2\xa1" {
		return nil, fmt.Errorf("unsupported PCAP magic %x", header[:4])
	}
	flows := make(map[string]*tcpFlow)
	for packet := 0; ; packet++ {
		record := make([]byte, 16)
		if _, err := io.ReadFull(file, record); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("read PCAP record %d: %w", packet, err)
		}
		captured := binary.LittleEndian.Uint32(record[8:12])
		frame := make([]byte, captured)
		if _, err := io.ReadFull(file, frame); err != nil {
			return nil, fmt.Errorf("read PCAP frame %d: %w", packet, err)
		}
		if len(frame) < 14+20 || binary.BigEndian.Uint16(frame[12:14]) != 0x0800 {
			continue
		}
		ip := frame[14:]
		ipHeader := int(ip[0]&0x0f) * 4
		if len(ip) < ipHeader+20 || ip[9] != 6 || net.IP(ip[12:16]).String() != sourceIP {
			continue
		}
		tcp := ip[ipHeader:]
		if binary.BigEndian.Uint16(tcp[0:2]) != sourcePort {
			continue
		}
		clientIP := net.IP(ip[16:20]).String()
		clientPort := binary.BigEndian.Uint16(tcp[2:4])
		key := fmt.Sprintf("%s:%d", clientIP, clientPort)
		flow := flows[key]
		if flow == nil {
			flow = &tcpFlow{clientIP: clientIP, clientPort: clientPort}
			flows[key] = flow
		}
		tcpHeader := int(tcp[12]>>4) * 4
		if tcpHeader < 20 || len(tcp) < tcpHeader {
			return nil, fmt.Errorf("packet %d has invalid TCP header length %d", packet, tcpHeader)
		}
		if payload := tcp[tcpHeader:]; len(payload) > 0 {
			flow.segments = append(flow.segments, tcpSegment{sequence: binary.BigEndian.Uint32(tcp[4:8]), payload: append([]byte(nil), payload...)})
		}
	}
	if len(flows) == 0 {
		return nil, fmt.Errorf("no TCP payload from %s:%d", sourceIP, sourcePort)
	}
	result := make([]tcpFlow, 0, len(flows))
	for _, flow := range flows {
		if len(flow.segments) > 0 {
			result = append(result, *flow)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].clientPort < result[j].clientPort })
	return result, nil
}

func reassembleTCPFlow(t *testing.T, flow tcpFlow) []byte {
	t.Helper()
	sort.Slice(flow.segments, func(i, j int) bool { return flow.segments[i].sequence < flow.segments[j].sequence })
	stream := append([]byte(nil), flow.segments[0].payload...)
	next := uint64(flow.segments[0].sequence) + uint64(len(flow.segments[0].payload))
	for _, segment := range flow.segments[1:] {
		start := uint64(segment.sequence)
		end := start + uint64(len(segment.payload))
		if end <= next {
			continue // complete retransmission
		}
		if start > next {
			t.Fatalf("TCP capture gap in %s:%d: sequence %d follows %d", flow.clientIP, flow.clientPort, start, next)
		}
		overlap := next - start
		stream = append(stream, segment.payload[overlap:]...)
		next = end
	}
	return stream
}
