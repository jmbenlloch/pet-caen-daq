// Package simulator provides a deterministic DT5215/DT5202 TCP simulator.
package simulator

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"sync"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

type Board struct {
	ProductID        uint32
	FirmwareRevision uint32
	Status           uint32
	Registers        map[uint32]uint32
}

type Topology struct {
	Chains       [dt5215.MaxChains][]Board
	LinkStatuses [dt5215.MaxChains]uint16
}

// ProductionTopology returns deterministic equivalents of the four production
// boards, one on each of chains 0-3.
func ProductionTopology() Topology {
	var topology Topology
	pids := []uint32{64883, 64138, 64885, 64884}
	for chain, pid := range pids {
		topology.LinkStatuses[chain] = 3
		topology.Chains[chain] = []Board{{
			ProductID:        pid,
			FirmwareRevision: 0x07080000 | uint32(chain),
			Status:           1,
		}}
	}
	return topology
}

type Server struct {
	control      net.Listener
	stream       net.Listener
	topology     Topology
	done         chan struct{}
	wg           sync.WaitGroup
	once         sync.Once
	mu           sync.Mutex
	synchronized bool
	streamData   chan []byte
}

func Start(controlAddress, streamAddress string, topology Topology) (*Server, error) {
	control, err := net.Listen("tcp", controlAddress)
	if err != nil {
		return nil, fmt.Errorf("listen control: %w", err)
	}
	stream, err := net.Listen("tcp", streamAddress)
	if err != nil {
		control.Close()
		return nil, fmt.Errorf("listen stream: %w", err)
	}
	server := &Server{control: control, stream: stream, topology: topology, done: make(chan struct{}), streamData: make(chan []byte, 16)}
	server.wg.Add(2)
	go server.acceptControl()
	go server.acceptStream()
	return server, nil
}

func (s *Server) ControlAddress() string        { return s.control.Addr().String() }
func (s *Server) StreamAddress() string         { return s.stream.Addr().String() }
func (s *Server) QueueStreamBatch(batch []byte) { s.streamData <- append([]byte(nil), batch...) }

func (s *Server) Close() error {
	var closeErr error
	s.once.Do(func() {
		close(s.done)
		controlErr := s.control.Close()
		streamErr := s.stream.Close()
		if controlErr != nil && !errors.Is(controlErr, net.ErrClosed) {
			closeErr = controlErr
		} else if streamErr != nil && !errors.Is(streamErr, net.ErrClosed) {
			closeErr = streamErr
		}
		s.wg.Wait()
	})
	return closeErr
}

func (s *Server) acceptControl() {
	defer s.wg.Done()
	for {
		connection, err := s.control.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer connection.Close()
			s.serveControl(connection)
		}()
	}
}

func (s *Server) acceptStream() {
	defer s.wg.Done()
	for {
		connection, err := s.stream.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer connection.Close()
			for {
				select {
				case data := <-s.streamData:
					if writeAll(connection, data) != nil {
						return
					}
				case <-s.done:
					return
				}
			}
		}()
	}
}

func (s *Server) serveControl(connection net.Conn) {
	for {
		opcode := make([]byte, 4)
		if _, err := io.ReadFull(connection, opcode); err != nil {
			return
		}
		var err error
		switch string(opcode) {
		case "CINF":
			err = s.handleChainInfo(connection)
		case "ENUM":
			err = s.handleEnumerate(connection)
		case "RREG":
			err = s.handleReadRegister(connection)
		case "WREG":
			err = s.handleWriteRegister(connection)
		case "FCMD", "DCMD":
			err = s.handleCommand(connection, string(opcode))
		case "SNT0":
			s.mu.Lock()
			s.synchronized = true
			s.mu.Unlock()
			err = writeStatus(connection, 0)
		case "RLNK":
			s.mu.Lock()
			s.synchronized = false
			s.mu.Unlock()
			err = writeStatus(connection, 0)
		case "CLRS":
			err = writeStatus(connection, 0)
		default:
			return
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) readChain(reader io.Reader) (uint16, error) {
	field := make([]byte, 2)
	if _, err := io.ReadFull(reader, field); err != nil {
		return 0, err
	}
	chain := binary.LittleEndian.Uint16(field)
	if chain >= dt5215.MaxChains {
		return 0, fmt.Errorf("invalid chain %d", chain)
	}
	return chain, nil
}

func (s *Server) handleChainInfo(connection net.Conn) error {
	chain, err := s.readChain(connection)
	if err != nil {
		return err
	}
	boards := s.topology.Chains[chain]
	response := make([]byte, 40)
	status := s.topology.LinkStatuses[chain]
	if status == 0 && len(boards) > 0 {
		status = 3
	}
	if status != 0 {
		binary.LittleEndian.PutUint16(response[0:2], status)
		binary.LittleEndian.PutUint16(response[2:4], uint16(len(boards)))
		binary.LittleEndian.PutUint32(response[4:8], math.Float32bits(10+float32(chain)))
	}
	return writeAll(connection, response)
}

func (s *Server) handleEnumerate(connection net.Conn) error {
	chain, err := s.readChain(connection)
	if err != nil {
		return err
	}
	response := make([]byte, 8)
	boards := s.topology.Chains[chain]
	if len(boards) == 0 {
		binary.LittleEndian.PutUint32(response[0:4], dt5215.StatusChainDisabled)
	} else {
		binary.LittleEndian.PutUint32(response[4:8], uint32(len(boards)))
	}
	return writeAll(connection, response)
}

func (s *Server) handleReadRegister(connection net.Conn) error {
	rest := make([]byte, 8)
	if _, err := io.ReadFull(connection, rest); err != nil {
		return err
	}
	chain := binary.LittleEndian.Uint16(rest[0:2])
	node := binary.LittleEndian.Uint16(rest[2:4])
	address := binary.LittleEndian.Uint32(rest[4:8])
	response := make([]byte, 8)
	if chain >= dt5215.MaxChains || int(node) >= len(s.topology.Chains[chain]) {
		binary.LittleEndian.PutUint32(response[0:4], 2)
		return writeAll(connection, response)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	board := s.topology.Chains[chain][node]
	var value uint32
	switch address {
	case dt5215.RegisterProductID:
		value = board.ProductID
	case dt5215.RegisterFirmwareRevision:
		value = board.FirmwareRevision
	case dt5215.RegisterAcquisitionStatus:
		value = board.Status
	default:
		value = board.Registers[address]
	}
	binary.LittleEndian.PutUint32(response[4:8], value)
	return writeAll(connection, response)
}

func (s *Server) handleWriteRegister(connection net.Conn) error {
	rest := make([]byte, 12)
	if _, err := io.ReadFull(connection, rest); err != nil {
		return err
	}
	chain := binary.LittleEndian.Uint16(rest)
	node := binary.LittleEndian.Uint16(rest[2:])
	address := binary.LittleEndian.Uint32(rest[4:])
	value := binary.LittleEndian.Uint32(rest[8:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if chain >= dt5215.MaxChains || int(node) >= len(s.topology.Chains[chain]) {
		return writeStatus(connection, 2)
	}
	if address == dt5215.RegisterProductID || address == dt5215.RegisterFirmwareRevision || address == dt5215.RegisterAcquisitionStatus {
		return writeStatus(connection, 22)
	}
	board := &s.topology.Chains[chain][node]
	if board.Registers == nil {
		board.Registers = make(map[uint32]uint32)
	}
	board.Registers[address] = value
	return writeStatus(connection, 0)
}
func (s *Server) handleCommand(connection net.Conn, operation string) error {
	rest := make([]byte, 16)
	if _, err := io.ReadFull(connection, rest); err != nil {
		return err
	}
	chain := binary.LittleEndian.Uint16(rest)
	node := binary.LittleEndian.Uint16(rest[2:])
	command := binary.LittleEndian.Uint32(rest[4:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if operation == "DCMD" {
		return writeStatus(connection, 0)
	}
	targets := []*Board{}
	if chain == 0xff && node == 0xff {
		for c := range s.topology.Chains {
			for n := range s.topology.Chains[c] {
				targets = append(targets, &s.topology.Chains[c][n])
			}
		}
	} else if chain >= dt5215.MaxChains || int(node) >= len(s.topology.Chains[chain]) {
		return writeStatus(connection, 2)
	} else {
		targets = append(targets, &s.topology.Chains[chain][node])
	}
	if command == dt5215.CommandAcquisitionStart && !s.synchronized {
		return writeStatus(connection, 10)
	}
	for _, board := range targets {
		switch command {
		case dt5215.CommandAcquisitionStart:
			board.Status = 2
		case dt5215.CommandAcquisitionStop:
			board.Status = 1
		case dt5215.CommandGlobalReset:
			board.Status = 1
			board.Registers = make(map[uint32]uint32)
		case dt5215.CommandResetTime, dt5215.CommandSoftwareTrigger, dt5215.CommandTestPulse, dt5215.CommandClearData, dt5215.CommandSync:
		default:
			return writeStatus(connection, 22)
		}
	}
	return writeStatus(connection, 0)
}
func writeStatus(w io.Writer, status uint32) error {
	response := make([]byte, 4)
	binary.LittleEndian.PutUint32(response, status)
	return writeAll(w, response)
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
