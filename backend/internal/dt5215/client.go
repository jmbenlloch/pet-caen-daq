package dt5215

import (
	"context"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

const defaultOperationTimeout = 3 * time.Second

// Client owns one DT5215 control connection and one stream connection.
type Client struct {
	control  net.Conn
	stream   net.Conn
	mu       sync.Mutex
	streamMu sync.Mutex
}

func (c *Client) WriteRegister(ctx context.Context, chain, node uint16, address, value uint32) error {
	request, err := EncodeWriteRegisterRequest(chain, node, address, value)
	if err != nil {
		return err
	}
	response, err := c.exchange(ctx, request, 4)
	if err != nil {
		return fmt.Errorf("chain %d node %d WREG 0x%08x: %w", chain, node, address, err)
	}
	return DecodeStatusResponse("WREG", response)
}
func (c *Client) SendCommand(ctx context.Context, chain, node uint16, command, delay uint32) error {
	return c.sendCommand(ctx, false, chain, node, command, delay)
}
func (c *Client) SetDelayedCommand(ctx context.Context, command, delay uint32) error {
	return c.sendCommand(ctx, true, 0xff, 0xff, command, delay)
}
func (c *Client) sendCommand(ctx context.Context, delayed bool, chain, node uint16, command, delay uint32) error {
	request, err := EncodeCommandRequest(delayed, chain, node, command, delay)
	if err != nil {
		return err
	}
	response, err := c.exchange(ctx, request, 4)
	op := "FCMD"
	if delayed {
		op = "DCMD"
	}
	if err != nil {
		return fmt.Errorf("%s command 0x%02x: %w", op, command, err)
	}
	return DecodeStatusResponse(op, response)
}
func (c *Client) Synchronize(ctx context.Context) error { return c.simple(ctx, "SNT0") }
func (c *Client) ResetLinks(ctx context.Context) error  { return c.simple(ctx, "RLNK") }
func (c *Client) ClearStream(ctx context.Context) error { return c.simple(ctx, "CLRS") }
func (c *Client) simple(ctx context.Context, operation string) error {
	request, err := EncodeSimpleRequest(operation)
	if err != nil {
		return err
	}
	response, err := c.exchange(ctx, request, 4)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return DecodeStatusResponse(operation, response)
}

func Dial(ctx context.Context, controlAddress, streamAddress string) (*Client, error) {
	dialer := net.Dialer{}
	control, err := dialer.DialContext(ctx, "tcp", controlAddress)
	if err != nil {
		return nil, fmt.Errorf("dial DT5215 control %s: %w", controlAddress, err)
	}
	stream, err := dialer.DialContext(ctx, "tcp", streamAddress)
	if err != nil {
		control.Close()
		return nil, fmt.Errorf("dial DT5215 stream %s: %w", streamAddress, err)
	}
	return &Client{control: control, stream: stream}, nil
}

func (c *Client) Close() error {
	controlErr := c.control.Close()
	streamErr := c.stream.Close()
	if controlErr != nil {
		return fmt.Errorf("close control connection: %w", controlErr)
	}
	if streamErr != nil {
		return fmt.Errorf("close stream connection: %w", streamErr)
	}
	return nil
}

func (c *Client) ChainInfo(ctx context.Context, chain uint16) (ChainInfo, error) {
	request, err := EncodeChainInfoRequest(chain)
	if err != nil {
		return ChainInfo{}, err
	}
	response, err := c.exchange(ctx, request, 40)
	if err != nil {
		return ChainInfo{}, fmt.Errorf("chain %d CINF: %w", chain, err)
	}
	return DecodeChainInfoResponse(response)
}

func (c *Client) Enumerate(ctx context.Context, chain uint16) (uint32, error) {
	request, err := EncodeEnumerateRequest(chain)
	if err != nil {
		return 0, err
	}
	response, err := c.exchange(ctx, request, 8)
	if err != nil {
		return 0, fmt.Errorf("chain %d ENUM: %w", chain, err)
	}
	return DecodeEnumerateResponse(response)
}

func (c *Client) ReadRegister(ctx context.Context, chain, node uint16, address uint32) (uint32, error) {
	request, err := EncodeReadRegisterRequest(chain, node, address)
	if err != nil {
		return 0, err
	}
	response, err := c.exchange(ctx, request, 8)
	if err != nil {
		return 0, fmt.Errorf("chain %d node %d RREG 0x%08x: %w", chain, node, address, err)
	}
	return DecodeReadRegisterResponse(response)
}

func (c *Client) exchange(ctx context.Context, request []byte, responseSize int) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	deadline := time.Now().Add(defaultOperationTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := c.control.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set control deadline: %w", err)
	}
	defer c.control.SetDeadline(time.Time{})

	if err := writeAll(c.control, request); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	response := make([]byte, responseSize)
	if _, err := io.ReadFull(c.control, response); err != nil {
		return nil, fmt.Errorf("read %d-byte response: %w", responseSize, err)
	}
	return response, nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

// Topology is the discovered and validated version-one system topology.
type Topology struct {
	Chains [MaxChains]ChainInfo
	Boards []BoardInfo
}

// DiscoverProductionTopology verifies web provisioning before reading board
// identity/status registers. It performs no writes.
func (c *Client) DiscoverProductionTopology(ctx context.Context, expected []janusconfig.Connection) (Topology, error) {
	if err := janusconfig.ValidateProductionTopology(expected); err != nil {
		return Topology{}, fmt.Errorf("expected topology: %w", err)
	}
	expectedByChain := make(map[int]janusconfig.Connection, len(expected))
	for _, connection := range expected {
		expectedByChain[connection.Chain] = connection
	}

	var topology Topology
	for chain := 0; chain < MaxChains; chain++ {
		info, err := c.ChainInfo(ctx, uint16(chain))
		if err != nil {
			return Topology{}, err
		}
		topology.Chains[chain] = info
		_, wanted := expectedByChain[chain]
		// The concentrator reports disabled as status zero. Non-zero states also
		// include pre-enumeration states, which are enabled links that this client
		// must not mistake for disabled web provisioning.
		enabled := info.Status != 0
		if wanted && !enabled {
			return Topology{}, fmt.Errorf("TDlink %d is disabled; enable links 0-3 in the DT5215 web interface", chain)
		}
		if !wanted && enabled {
			return Topology{}, fmt.Errorf("unexpected enabled TDlink %d; disable unused links 4-7 in the DT5215 web interface", chain)
		}
		if !wanted {
			continue
		}
		nodes, err := c.Enumerate(ctx, uint16(chain))
		if err != nil {
			return Topology{}, err
		}
		if nodes != 1 || info.BoardCount != 1 {
			return Topology{}, fmt.Errorf("TDlink %d has %d enumerated nodes and reports %d boards; expected exactly one", chain, nodes, info.BoardCount)
		}

		connection := expectedByChain[chain]
		productID, err := c.ReadRegister(ctx, uint16(chain), uint16(connection.Node), RegisterProductID)
		if err != nil {
			return Topology{}, err
		}
		firmware, err := c.ReadRegister(ctx, uint16(chain), uint16(connection.Node), RegisterFirmwareRevision)
		if err != nil {
			return Topology{}, err
		}
		status, err := c.ReadRegister(ctx, uint16(chain), uint16(connection.Node), RegisterAcquisitionStatus)
		if err != nil {
			return Topology{}, err
		}
		topology.Boards = append(topology.Boards, BoardInfo{
			Chain:            uint16(chain),
			Node:             uint16(connection.Node),
			ProductID:        productID,
			FirmwareRevision: firmware,
			AcquisitionState: status,
		})
	}
	sort.Slice(topology.Boards, func(i, j int) bool { return topology.Boards[i].Chain < topology.Boards[j].Chain })
	return topology, nil
}
