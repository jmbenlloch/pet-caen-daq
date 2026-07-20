// Package dt5215 implements the source-confirmed DT5215 TCP protocol.
package dt5215

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const (
	ControlPort = 9760
	StreamPort  = 9000

	MaxChains = 8
	MaxNodes  = 16

	StatusOK            uint32 = 0
	StatusChainDisabled uint32 = 25

	RegisterFirmwareRevision  uint32 = 0x01000300
	RegisterAcquisitionStatus uint32 = 0x01000304
	RegisterProductID         uint32 = 0x01000400
)

var littleEndian = binary.LittleEndian

// ChainInfo is the source-confirmed portion of a CINF response.
type ChainInfo struct {
	Status      uint16
	BoardCount  uint16
	RoundTrip   float32
	EventCount  uint64
	ByteCount   uint64
	EventRateHz float32
	Megabits    float32
}

type BoardInfo struct {
	Chain            uint16
	Node             uint16
	ProductID        uint32
	FirmwareRevision uint32
	AcquisitionState uint32
}

func EncodeChainInfoRequest(chain uint16) ([]byte, error) {
	if chain >= MaxChains {
		return nil, fmt.Errorf("chain %d out of range", chain)
	}
	request := make([]byte, 6)
	copy(request, "CINF")
	littleEndian.PutUint16(request[4:], chain)
	return request, nil
}

func DecodeChainInfoResponse(response []byte) (ChainInfo, error) {
	if len(response) != 40 {
		return ChainInfo{}, fmt.Errorf("CINF response length = %d, want 40", len(response))
	}
	return ChainInfo{
		Status:      littleEndian.Uint16(response[0:2]),
		BoardCount:  littleEndian.Uint16(response[2:4]),
		RoundTrip:   math.Float32frombits(littleEndian.Uint32(response[4:8])),
		EventCount:  littleEndian.Uint64(response[8:16]),
		ByteCount:   littleEndian.Uint64(response[16:24]),
		EventRateHz: math.Float32frombits(littleEndian.Uint32(response[24:28])),
		Megabits:    math.Float32frombits(littleEndian.Uint32(response[28:32])),
	}, nil
}

func EncodeEnumerateRequest(chain uint16) ([]byte, error) {
	if chain >= MaxChains {
		return nil, fmt.Errorf("chain %d out of range", chain)
	}
	request := make([]byte, 6)
	copy(request, "ENUM")
	littleEndian.PutUint16(request[4:], chain)
	return request, nil
}

func DecodeEnumerateResponse(response []byte) (nodeCount uint32, err error) {
	if len(response) != 8 {
		return 0, fmt.Errorf("ENUM response length = %d, want 8", len(response))
	}
	if status := littleEndian.Uint32(response[0:4]); status != StatusOK {
		return 0, &StatusError{Operation: "ENUM", Status: status}
	}
	nodeCount = littleEndian.Uint32(response[4:8])
	if nodeCount > MaxNodes {
		return 0, fmt.Errorf("ENUM node count = %d, maximum %d", nodeCount, MaxNodes)
	}
	return nodeCount, nil
}

func EncodeReadRegisterRequest(chain, node uint16, address uint32) ([]byte, error) {
	if chain >= MaxChains {
		return nil, fmt.Errorf("chain %d out of range", chain)
	}
	if node >= MaxNodes {
		return nil, fmt.Errorf("node %d out of range", node)
	}
	request := make([]byte, 12)
	copy(request, "RREG")
	littleEndian.PutUint16(request[4:6], chain)
	littleEndian.PutUint16(request[6:8], node)
	littleEndian.PutUint32(request[8:12], address)
	return request, nil
}

func DecodeReadRegisterResponse(response []byte) (uint32, error) {
	if len(response) != 8 {
		return 0, fmt.Errorf("RREG response length = %d, want 8", len(response))
	}
	if status := littleEndian.Uint32(response[0:4]); status != StatusOK {
		return 0, &StatusError{Operation: "RREG", Status: status}
	}
	return littleEndian.Uint32(response[4:8]), nil
}

// StatusError reports a non-zero status returned by the concentrator.
type StatusError struct {
	Operation string
	Status    uint32
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("DT5215 %s status %d", e.Operation, e.Status)
}

func IsStatus(err error, status uint32) bool {
	var statusError *StatusError
	return errors.As(err, &statusError) && statusError.Status == status
}
