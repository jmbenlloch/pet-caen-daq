package dt5215

import (
	"encoding/hex"
	"math"
	"testing"
)

func TestEncodeRequestsGolden(t *testing.T) {
	tests := []struct {
		name string
		got  func() ([]byte, error)
		want string
	}{
		{"chain info", func() ([]byte, error) { return EncodeChainInfoRequest(3) }, "43494e460300"},
		{"enumerate", func() ([]byte, error) { return EncodeEnumerateRequest(2) }, "454e554d0200"},
		{"disable chain", func() ([]byte, error) { return EncodeChainControlRequest(2, false, 0) }, "43434e540200000000000000"},
		{"enable chain", func() ([]byte, error) { return EncodeChainControlRequest(3, true, 0x100) }, "43434e540300010000010000"},
		{"read register", func() ([]byte, error) {
			return EncodeReadRegisterRequest(1, 0, RegisterProductID)
		}, "525245470100000000040001"},
		{"write register", func() ([]byte, error) { return EncodeWriteRegisterRequest(2, 3, 0x01000050, 42) }, "5752454702000300500000012a000000"},
		{"command", func() ([]byte, error) { return EncodeCommandRequest(false, 1, 0, CommandTestPulse, 1000000) }, "46434d4401000000160000000000000040420f00"},
		{"delayed broadcast", func() ([]byte, error) { return EncodeCommandRequest(true, 0xff, 0xff, CommandAcquisitionStart, 0) }, "44434d44ff00ff00120000000000000000000000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.got()
			if err != nil {
				t.Fatal(err)
			}
			if encoded := hex.EncodeToString(got); encoded != test.want {
				t.Fatalf("request = %s, want %s", encoded, test.want)
			}
		})
	}
}

func TestDecodeChainInfo(t *testing.T) {
	response := make([]byte, 40)
	littleEndian.PutUint16(response[0:2], 3)
	littleEndian.PutUint16(response[2:4], 1)
	littleEndian.PutUint32(response[4:8], math.Float32bits(12.5))
	littleEndian.PutUint64(response[8:16], 42)
	littleEndian.PutUint64(response[16:24], 4096)
	littleEndian.PutUint32(response[24:28], math.Float32bits(3.5))
	littleEndian.PutUint32(response[28:32], math.Float32bits(1.25))

	info, err := DecodeChainInfoResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != 3 || info.BoardCount != 1 || info.RoundTrip != 12.5 || info.EventCount != 42 {
		t.Fatalf("decoded info = %#v", info)
	}
}

func TestDecodeEnumerateStatus(t *testing.T) {
	response := make([]byte, 12)
	littleEndian.PutUint32(response[0:4], StatusChainDisabled)
	_, err := DecodeEnumerateResponse(response)
	if !IsStatus(err, StatusChainDisabled) {
		t.Fatalf("error = %v, want chain-disabled status", err)
	}
}

func TestDecodeEnumerateCaptureVerifiedReply(t *testing.T) {
	response, err := hex.DecodeString("00000000010000003c000000")
	if err != nil {
		t.Fatal(err)
	}
	info, err := DecodeEnumerateResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if info.NodeCount != 1 || info.Word2 != 60 {
		t.Fatalf("decoded ENUM = %#v", info)
	}
}
