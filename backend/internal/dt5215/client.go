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
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/transportjournal"
)

const (
	defaultOperationTimeout = 3 * time.Second
	versionOperationTimeout = 10 * time.Second
	resetOperationTimeout   = 5 * time.Second
	enumOperationTimeout    = 30 * time.Second
	syncOperationTimeout    = 10 * time.Second
	tdlCommandDelayUnit     = 10 * time.Nanosecond
	tdlCommandMaxAttempts   = 10
	tdlSyncDelaySetting     = 0x16
	tdlSyncAdjustment       = 0x00010000
)

// Client owns one DT5215 control connection and one stream connection.
type Client struct {
	control            net.Conn
	stream             net.Conn
	mu                 sync.Mutex
	streamMu           sync.Mutex
	journal            transportjournal.Sink
	streamConnectionID string
	streamOffset       uint64
	streamBuffer       []byte
	journalNow         func() time.Time
}

// SetStreamJournal installs evidence capture below DT5215 framing. Call it
// before starting stream reads. Passing nil disables journaling.
func (c *Client) SetStreamJournal(journal transportjournal.Sink, connectionID string, now func() time.Time) {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()
	c.journal = journal
	c.streamOffset = 0
	c.streamConnectionID = connectionID
	if c.streamConnectionID == "" && c.stream != nil {
		c.streamConnectionID = c.stream.LocalAddr().String() + "->" + c.stream.RemoteAddr().String()
	}
	c.journalNow = now
	if c.journalNow == nil {
		c.journalNow = time.Now
	}
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
	// The DT5215 acknowledges an FCMD/DCMD request before the command's
	// scheduled execution time. FERSlib keeps the control transaction
	// serialized and waits for that delay before reading the reply. Without
	// the wait, a following command can be submitted while the previous one
	// is still pending; real hardware has returned CNC_STATUS_TIMEOUT in that
	// situation on the second consecutive run.
	op := "FCMD"
	if delayed {
		op = "DCMD"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 1; attempt <= tdlCommandMaxAttempts; attempt++ {
		response, err := c.exchangeAfterWriteDelayWithTimeoutLocked(ctx, request, 4, time.Duration(delay)*tdlCommandDelayUnit, defaultOperationTimeout)
		if err != nil {
			return fmt.Errorf("%s command 0x%02x: %w", op, command, err)
		}
		if err := DecodeStatusResponse(op, response); err == nil {
			return nil
		} else if attempt == tdlCommandMaxAttempts {
			return fmt.Errorf("%s command 0x%02x failed after %d attempts: %w", op, command, attempt, err)
		}
	}
	panic("unreachable")
}
func (c *Client) Synchronize(ctx context.Context) error {
	return c.simpleWithTimeout(ctx, "SNT0", syncOperationTimeout)
}
func (c *Client) ResetLinks(ctx context.Context) error {
	return c.simpleWithTimeout(ctx, "RLNK", resetOperationTimeout)
}
func (c *Client) ClearStream(ctx context.Context) error { return c.simple(ctx, "CLRS") }
func (c *Client) simple(ctx context.Context, operation string) error {
	return c.simpleWithTimeout(ctx, operation, defaultOperationTimeout)
}
func (c *Client) simpleWithTimeout(ctx context.Context, operation string, timeout time.Duration) error {
	request, err := EncodeSimpleRequest(operation)
	if err != nil {
		return err
	}
	response, err := c.exchangeWithTimeout(ctx, request, 4, timeout)
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

func (c *Client) Enumerate(ctx context.Context, chain uint16) (EnumerationInfo, error) {
	request, err := EncodeEnumerateRequest(chain)
	if err != nil {
		return EnumerationInfo{}, err
	}
	response, err := c.exchangeWithTimeout(ctx, request, 12, enumOperationTimeout)
	if err != nil {
		return EnumerationInfo{}, fmt.Errorf("chain %d ENUM: %w", chain, err)
	}
	return DecodeEnumerateResponse(response)
}

func (c *Client) ControlChain(ctx context.Context, chain uint16, enable bool, tokenInterval uint32) error {
	request, err := EncodeChainControlRequest(chain, enable, tokenInterval)
	if err != nil {
		return err
	}
	response, err := c.exchange(ctx, request, 4)
	if err != nil {
		return fmt.Errorf("chain %d CCNT: %w", chain, err)
	}
	return DecodeStatusResponse("CCNT", response)
}

func (c *Client) WriteConcentratorRegister(ctx context.Context, address, value uint32) error {
	response, err := c.exchange(ctx, EncodeConcentratorWriteRegisterRequest(address, value), 4)
	if err != nil {
		return fmt.Errorf("CWRG %d: %w", address, err)
	}
	return DecodeStatusResponse("CWRG", response)
}

// recoverTDL reproduces the connection-time sequence captured from JANUS.
// It clears a DT5215 state in which CINF still succeeds but every board RREG
// returns CNC status 26, so it must run before discovery reads board registers.
func (c *Client) recoverTDL(ctx context.Context, chains []int) error {
	for _, chain := range chains {
		// Capture-verified from pcap/allcards_janus.pcap. FERSlib selects the
		// link in the high byte and writes both synchronization parameters
		// before disabling the readout train.
		link := uint32(chain) << 24
		for _, setting := range []uint32{tdlSyncDelaySetting, tdlSyncAdjustment} {
			if err := c.WriteConcentratorRegister(ctx, VirtualRegisterSyncDelay, link|setting); err != nil {
				return fmt.Errorf("set TDlink %d synchronization parameter 0x%08x: %w", chain, setting, err)
			}
		}
	}
	for _, chain := range chains {
		if err := c.ControlChain(ctx, uint16(chain), false, 0); err != nil {
			return fmt.Errorf("disable TDlink %d readout train for recovery: %w", chain, err)
		}
	}
	for _, command := range []uint32{CommandSync, CommandResetTime, CommandResetPeriodic} {
		if err := c.SendCommand(ctx, 0xff, 0xff, command, TDLCommandDelay); err != nil {
			return fmt.Errorf("recover TDlinks with command 0x%02x: %w", command, err)
		}
	}
	for _, chain := range chains {
		if err := c.ControlChain(ctx, uint16(chain), true, 256); err != nil {
			return fmt.Errorf("enable TDlink %d readout train after recovery: %w", chain, err)
		}
	}
	return nil
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

func (c *Client) ConcentratorInfo(ctx context.Context) (ConcentratorInfo, error) {
	response, err := c.exchangeWithTimeout(ctx, []byte("VERS"), 68, versionOperationTimeout)
	if err != nil {
		return ConcentratorInfo{}, fmt.Errorf("VERS: %w", err)
	}
	return DecodeConcentratorVersionResponse(response)
}

func (c *Client) exchange(ctx context.Context, request []byte, responseSize int) ([]byte, error) {
	return c.exchangeAfterWriteDelay(ctx, request, responseSize, 0)
}

func (c *Client) exchangeWithTimeout(ctx context.Context, request []byte, responseSize int, timeout time.Duration) ([]byte, error) {
	return c.exchangeAfterWriteDelayWithTimeout(ctx, request, responseSize, 0, timeout)
}

func (c *Client) exchangeAfterWriteDelay(ctx context.Context, request []byte, responseSize int, delay time.Duration) ([]byte, error) {
	return c.exchangeAfterWriteDelayWithTimeout(ctx, request, responseSize, delay, defaultOperationTimeout)
}

func (c *Client) exchangeAfterWriteDelayWithTimeout(ctx context.Context, request []byte, responseSize int, delay, timeout time.Duration) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exchangeAfterWriteDelayWithTimeoutLocked(ctx, request, responseSize, delay, timeout)
}

func (c *Client) exchangeAfterWriteDelayWithTimeoutLocked(ctx context.Context, request []byte, responseSize int, delay, timeout time.Duration) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := c.control.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set control deadline: %w", err)
	}
	cancelDone := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() { _ = c.control.SetDeadline(time.Now()); close(cancelDone) })
	defer func() {
		if !stopCancel() {
			<-cancelDone
		}
		_ = c.control.SetDeadline(time.Time{})
	}()

	if err := writeAll(c.control, request); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, ctx.Err()
		}
	}
	response := make([]byte, responseSize)
	if _, err := io.ReadFull(c.control, response); err != nil {
		if ctxErr := operationContextError(ctx); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("read %d-byte response: %w", responseSize, err)
	}
	return response, nil
}

func operationContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
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
	Concentrator ConcentratorInfo
	Chains       [MaxChains]ChainInfo
	Enumerations [MaxChains]EnumerationInfo
	Boards       []BoardInfo
}

type DiscoveryStage string

const (
	DiscoveryIdentity      DiscoveryStage = "identity"
	DiscoveryScanning      DiscoveryStage = "scanning_links"
	DiscoveryResetting     DiscoveryStage = "resetting_links"
	DiscoveryEnumerating   DiscoveryStage = "enumerating_links"
	DiscoverySynchronizing DiscoveryStage = "synchronizing_links"
	DiscoveryRecovering    DiscoveryStage = "recovering_links"
	DiscoveryReadingBoards DiscoveryStage = "reading_boards"
	DiscoveryComplete      DiscoveryStage = "complete"
	DiscoveryFailed        DiscoveryStage = "failed"
)

type DiscoveryProgress struct {
	Stage            DiscoveryStage
	Chain            int
	Node             int
	ChainsCompleted  int
	ChainsTotal      int
	BoardsDiscovered int
	BoardsTotal      int
	Message          string
}

type DiscoveryObserver func(DiscoveryProgress)

// DiscoverProductionTopology verifies web provisioning, initializes enabled
// links when CINF reports a pre-enumeration state, and reads board identity and
// status registers.
func (c *Client) DiscoverProductionTopology(ctx context.Context, expected []janusconfig.Connection) (Topology, error) {
	return c.productionTopology(ctx, expected, true)
}

// InspectProductionTopology validates an already initialized production
// topology using only CINF and RREG requests. It never resets, enumerates, or
// synchronizes links, making it suitable for opt-in read-only hardware checks.
func (c *Client) InspectProductionTopology(ctx context.Context, expected []janusconfig.Connection) (Topology, error) {
	return c.productionTopology(ctx, expected, false)
}

// DiscoverEnabledTopology resets, enumerates, and synchronizes every TDlink
// enabled through the DT5215 web interface, then reads the identity and status
// registers of every enumerated node. It does not apply board configuration or
// change persistent link enablement.
func (c *Client) DiscoverEnabledTopology(ctx context.Context) (Topology, error) {
	return c.DiscoverEnabledTopologyWithObserver(ctx, nil)
}

func (c *Client) DiscoverEnabledTopologyWithObserver(ctx context.Context, observe DiscoveryObserver) (topology Topology, err error) {
	progress := DiscoveryProgress{Stage: DiscoveryIdentity, Chain: -1, Node: -1, ChainsTotal: MaxChains, Message: "Reading concentrator identity"}
	publish := func(next DiscoveryProgress) {
		progress = next
		if observe != nil {
			observe(next)
		}
	}
	publish(progress)
	defer func() {
		if err != nil {
			progress.Stage, progress.Message = DiscoveryFailed, err.Error()
			publish(progress)
		}
	}()
	concentrator, err := c.ConcentratorInfo(ctx)
	if err != nil {
		return Topology{}, fmt.Errorf("read DT5215 identity: %w", err)
	}
	topology = Topology{Concentrator: concentrator}
	enabled := make([]int, 0, MaxChains)
	for chain := 0; chain < MaxChains; chain++ {
		publish(DiscoveryProgress{
			Stage: DiscoveryScanning, Chain: chain, Node: -1, ChainsCompleted: chain,
			ChainsTotal: MaxChains, Message: fmt.Sprintf("Scanning TDlink %d", chain),
		})
		info, infoErr := c.ChainInfo(ctx, uint16(chain))
		if infoErr != nil {
			return Topology{}, infoErr
		}
		topology.Chains[chain] = info
		if info.Status != 0 {
			enabled = append(enabled, chain)
		}
		publish(DiscoveryProgress{
			Stage: DiscoveryScanning, Chain: chain, Node: -1, ChainsCompleted: chain + 1,
			ChainsTotal: MaxChains, Message: fmt.Sprintf("TDlink %d reports %d cards", chain, info.BoardCount),
		})
	}
	if len(enabled) == 0 {
		return Topology{}, fmt.Errorf("no TDlinks are enabled; enable the required links in the DT5215 web interface")
	}
	publish(DiscoveryProgress{
		Stage: DiscoveryResetting, Chain: -1, Node: -1, ChainsCompleted: 0,
		ChainsTotal: len(enabled), Message: "Resetting enabled TDlinks",
	})
	if err = c.ResetLinks(ctx); err != nil {
		return Topology{}, fmt.Errorf("initialize TDlinks: %w", err)
	}
	for index, chain := range enabled {
		publish(DiscoveryProgress{
			Stage: DiscoveryEnumerating, Chain: chain, Node: -1, ChainsCompleted: index,
			ChainsTotal: len(enabled), Message: fmt.Sprintf("Enumerating TDlink %d", chain),
		})
		enumeration, enumerateErr := c.Enumerate(ctx, uint16(chain))
		if enumerateErr != nil {
			return Topology{}, enumerateErr
		}
		topology.Enumerations[chain] = enumeration
		publish(DiscoveryProgress{
			Stage: DiscoveryEnumerating, Chain: chain, Node: -1, ChainsCompleted: index + 1,
			ChainsTotal: len(enabled), BoardsTotal: int(enumeration.NodeCount),
			Message: fmt.Sprintf("TDlink %d enumerated %d cards", chain, enumeration.NodeCount),
		})
	}
	publish(DiscoveryProgress{
		Stage: DiscoverySynchronizing, Chain: -1, Node: -1, ChainsCompleted: len(enabled),
		ChainsTotal: len(enabled), Message: "Synchronizing enumerated TDlinks",
	})
	if err = c.Synchronize(ctx); err != nil {
		return Topology{}, fmt.Errorf("synchronize enumerated TDlinks: %w", err)
	}
	publish(DiscoveryProgress{
		Stage: DiscoveryRecovering, Chain: -1, Node: -1, ChainsCompleted: len(enabled),
		ChainsTotal: len(enabled), Message: "Recovering TDlink readout trains",
	})
	if err = c.recoverTDL(ctx, enabled); err != nil {
		return Topology{}, fmt.Errorf("recover DT5215 TDlinks before board discovery: %w", err)
	}
	boardTotal := 0
	for _, chain := range enabled {
		info, infoErr := c.ChainInfo(ctx, uint16(chain))
		if infoErr != nil {
			return Topology{}, infoErr
		}
		topology.Chains[chain] = info
		if info.Status == 1 || info.Status == 2 {
			return Topology{}, fmt.Errorf("TDlink %d is not ready after discovery (status %d, boards %d)", chain, info.Status, info.BoardCount)
		}
		nodeCount := int(info.BoardCount)
		if nodeCount > janusconfig.MaxNodesPerChain {
			return Topology{}, fmt.Errorf("TDlink %d reports %d boards; maximum supported is %d", chain, nodeCount, janusconfig.MaxNodesPerChain)
		}
		if enumerated := int(topology.Enumerations[chain].NodeCount); enumerated != nodeCount {
			return Topology{}, fmt.Errorf("TDlink %d reports %d boards after enumerating %d nodes", chain, nodeCount, enumerated)
		}
		boardTotal += nodeCount
	}
	discovered := 0
	for _, chain := range enabled {
		nodeCount := int(topology.Chains[chain].BoardCount)
		for node := 0; node < nodeCount; node++ {
			publish(DiscoveryProgress{
				Stage: DiscoveryReadingBoards, Chain: chain, Node: node, ChainsCompleted: len(enabled),
				ChainsTotal: len(enabled), BoardsDiscovered: discovered, BoardsTotal: boardTotal,
				Message: fmt.Sprintf("Reading TDlink %d node %d identity", chain, node),
			})
			productID, readErr := c.ReadRegister(ctx, uint16(chain), uint16(node), RegisterProductID)
			if readErr != nil {
				return Topology{}, readErr
			}
			firmware, readErr := c.ReadRegister(ctx, uint16(chain), uint16(node), RegisterFirmwareRevision)
			if readErr != nil {
				return Topology{}, readErr
			}
			status, readErr := c.ReadRegister(ctx, uint16(chain), uint16(node), RegisterAcquisitionStatus)
			if readErr != nil {
				return Topology{}, readErr
			}
			topology.Boards = append(topology.Boards, BoardInfo{
				Chain: uint16(chain), Node: uint16(node), ProductID: productID,
				FirmwareRevision: firmware, AcquisitionState: status,
			})
			discovered++
			publish(DiscoveryProgress{
				Stage: DiscoveryReadingBoards, Chain: chain, Node: node, ChainsCompleted: len(enabled),
				ChainsTotal: len(enabled), BoardsDiscovered: discovered, BoardsTotal: boardTotal,
				Message: fmt.Sprintf("Discovered card %d of %d", discovered, boardTotal),
			})
		}
	}
	sort.Slice(topology.Boards, func(i, j int) bool {
		if topology.Boards[i].Chain != topology.Boards[j].Chain {
			return topology.Boards[i].Chain < topology.Boards[j].Chain
		}
		return topology.Boards[i].Node < topology.Boards[j].Node
	})
	publish(DiscoveryProgress{
		Stage: DiscoveryComplete, Chain: -1, Node: -1, ChainsCompleted: len(enabled),
		ChainsTotal: len(enabled), BoardsDiscovered: discovered, BoardsTotal: boardTotal,
		Message: fmt.Sprintf("Discovered %d cards", discovered),
	})
	return topology, nil
}

func (c *Client) productionTopology(ctx context.Context, expected []janusconfig.Connection, initialize bool) (Topology, error) {
	if err := janusconfig.ValidateProductionTopology(expected); err != nil {
		return Topology{}, fmt.Errorf("expected topology: %w", err)
	}
	expectedByChain := make(map[int][]janusconfig.Connection, MaxChains)
	for _, connection := range expected {
		expectedByChain[connection.Chain] = append(expectedByChain[connection.Chain], connection)
	}

	concentrator, err := c.ConcentratorInfo(ctx)
	if err != nil {
		return Topology{}, fmt.Errorf("read DT5215 identity: %w", err)
	}
	topology := Topology{Concentrator: concentrator}
	requiresEnumeration := false
	for chain := 0; chain < MaxChains; chain++ {
		info, err := c.ChainInfo(ctx, uint16(chain))
		if err != nil {
			return Topology{}, err
		}
		topology.Chains[chain] = info
		connections, wanted := expectedByChain[chain]
		// The concentrator reports disabled as status zero. Non-zero states also
		// include pre-enumeration states, which are enabled links that this client
		// must not mistake for disabled web provisioning.
		enabled := info.Status != 0
		if wanted && !enabled {
			return Topology{}, fmt.Errorf("TDlink %d is disabled; enable the configured link in the DT5215 web interface", chain)
		}
		if !wanted && enabled {
			return Topology{}, fmt.Errorf("unexpected enabled TDlink %d; disable the unused link in the DT5215 web interface or add its cards to the configuration", chain)
		}
		if !wanted {
			continue
		}
		if info.Status == 1 || info.Status == 2 {
			requiresEnumeration = true
		} else if int(info.BoardCount) != len(connections) {
			return Topology{}, fmt.Errorf("TDlink %d reports %d boards; configuration expects %d", chain, info.BoardCount, len(connections))
		}
	}

	if requiresEnumeration {
		if !initialize {
			return Topology{}, fmt.Errorf("one or more expected TDlinks require runtime initialization; read-only inspection will not reset, enumerate, or synchronize links")
		}
		if err := c.ResetLinks(ctx); err != nil {
			return Topology{}, fmt.Errorf("initialize TDlinks: %w", err)
		}
		for chain := 0; chain < MaxChains; chain++ {
			if _, wanted := expectedByChain[chain]; !wanted {
				continue
			}
			enumeration, err := c.Enumerate(ctx, uint16(chain))
			if err != nil {
				return Topology{}, err
			}
			topology.Enumerations[chain] = enumeration
			if int(enumeration.NodeCount) != len(expectedByChain[chain]) {
				return Topology{}, fmt.Errorf("TDlink %d enumerated %d nodes; configuration expects %d", chain, enumeration.NodeCount, len(expectedByChain[chain]))
			}
		}
		if err := c.Synchronize(ctx); err != nil {
			return Topology{}, fmt.Errorf("synchronize enumerated TDlinks: %w", err)
		}
		for chain := 0; chain < MaxChains; chain++ {
			info, err := c.ChainInfo(ctx, uint16(chain))
			if err != nil {
				return Topology{}, err
			}
			topology.Chains[chain] = info
		}
	}

	if initialize {
		chains := make([]int, 0, len(expectedByChain))
		for chain := 0; chain < MaxChains; chain++ {
			if _, wanted := expectedByChain[chain]; wanted {
				chains = append(chains, chain)
			}
		}
		if err := c.recoverTDL(ctx, chains); err != nil {
			return Topology{}, fmt.Errorf("recover DT5215 TDlinks before board discovery: %w", err)
		}
	}

	for chain := 0; chain < MaxChains; chain++ {
		connections, wanted := expectedByChain[chain]
		if !wanted {
			continue
		}
		info := topology.Chains[chain]
		if info.Status == 1 || info.Status == 2 || int(info.BoardCount) != len(connections) {
			return Topology{}, fmt.Errorf("TDlink %d is not ready after discovery (status %d, boards %d); expected %d boards", chain, info.Status, info.BoardCount, len(connections))
		}
		for _, connection := range connections {
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
	}
	sort.Slice(topology.Boards, func(i, j int) bool {
		if topology.Boards[i].Chain != topology.Boards[j].Chain {
			return topology.Boards[i].Chain < topology.Boards[j].Chain
		}
		return topology.Boards[i].Node < topology.Boards[j].Node
	})
	return topology, nil
}
