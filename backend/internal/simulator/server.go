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
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

type Board struct {
	ProductID        uint32
	FirmwareRevision uint32
	Status           uint32
	Registers        map[uint32]uint32
	CitirocLoads     [2]uint32
	HVRegisters      map[uint32]uint32
	hvSelector       uint32
	CommonPedestal   uint16
	Pedestal         dt5202.PedestalCalibration
	protectedFlash   map[uint16][]byte
	spi              flashReadState
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
			Registers:        monitorRegisters(),
			CommonPedestal:   50,
		}}
		for channel := range topology.Chains[chain][0].Pedestal.LowGain {
			topology.Chains[chain][0].Pedestal.LowGain[channel] = 50
			topology.Chains[chain][0].Pedestal.HighGain[channel] = 50
		}
		topology.Chains[chain][0].protectedFlash = map[uint16][]byte{dt5202.PedestalFlashPage: simulatorPedestalPage(&topology.Chains[chain][0])}
	}
	return topology
}

type Server struct {
	control       net.Listener
	stream        net.Listener
	topology      Topology
	done          chan struct{}
	wg            sync.WaitGroup
	once          sync.Once
	mu            sync.Mutex
	synchronized  bool
	streamData    chan streamItem
	eventSequence uint64
	faults        []Fault
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
	server := &Server{control: control, stream: stream, topology: topology, done: make(chan struct{}), streamData: make(chan streamItem, 16)}
	server.wg.Add(2)
	go server.acceptControl()
	go server.acceptStream()
	return server, nil
}

func (s *Server) ControlAddress() string { return s.control.Addr().String() }
func (s *Server) StreamAddress() string  { return s.stream.Addr().String() }
func (s *Server) QueueStreamBatch(batch []byte) {
	s.mu.Lock()
	item, drop := s.prepareStreamItem(batch, false, false)
	s.mu.Unlock()
	if !drop {
		s.streamData <- item
	}
}

func (s *Server) BoardSnapshot(chain, node int) (Board, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if chain < 0 || chain >= len(s.topology.Chains) || node < 0 || node >= len(s.topology.Chains[chain]) {
		return Board{}, fmt.Errorf("invalid board %d:%d", chain, node)
	}
	board := s.topology.Chains[chain][node]
	board.Registers = make(map[uint32]uint32, len(s.topology.Chains[chain][node].Registers))
	for address, value := range s.topology.Chains[chain][node].Registers {
		board.Registers[address] = value
	}
	board.HVRegisters = make(map[uint32]uint32, len(s.topology.Chains[chain][node].HVRegisters))
	for address, value := range s.topology.Chains[chain][node].HVRegisters {
		board.HVRegisters[address] = value
	}
	return board, nil
}

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
				case item := <-s.streamData:
					if item.fault != nil {
						switch item.fault.Kind {
						case FaultStreamDelay:
							delay := item.fault.Delay
							if delay == 0 {
								delay = 5 * time.Second
							}
							timer := time.NewTimer(delay)
							select {
							case <-timer.C:
							case <-s.done:
								timer.Stop()
								return
							}
						case FaultStreamDisconnect:
							return
						case FaultStreamTruncation:
							n := item.fault.AfterBytes
							if n == 0 {
								n = 1
							}
							if n > len(item.data) {
								n = len(item.data)
							}
							_ = writeAll(connection, item.data[:n])
							return
						}
					}
					if writeAll(connection, item.data) != nil {
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
		s.mu.Lock()
		fault, hasFault := s.takeFault(func(f Fault) bool {
			return isControlFault(f.Kind) && (f.Operation == "" || f.Operation == string(opcode))
		})
		s.mu.Unlock()
		if hasFault {
			switch fault.Kind {
			case FaultControlDelay:
				delay := fault.Delay
				if delay == 0 {
					delay = 5 * time.Second
				}
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-s.done:
					timer.Stop()
					return
				}
			case FaultControlTimeout:
				delay := fault.Delay
				if delay == 0 {
					delay = 5 * time.Second
				}
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
					return
				case <-s.done:
					timer.Stop()
					return
				}
			case FaultControlDisconnect:
				return
			}
		}
		writer := net.Conn(connection)
		if hasFault && fault.Kind == FaultPartialReply {
			writer = &faultWriter{Conn: connection, fault: fault}
		}
		switch string(opcode) {
		case "CINF":
			err = s.handleChainInfo(writer)
		case "ENUM":
			err = s.handleEnumerate(writer)
		case "CCNT":
			err = s.handleChainControl(writer)
		case "RREG":
			err = s.handleReadRegister(writer)
		case "WREG":
			err = s.handleWriteRegister(writer)
		case "FCMD", "DCMD":
			err = s.handleCommand(writer, string(opcode))
		case "SNT0":
			s.mu.Lock()
			s.synchronized = true
			s.mu.Unlock()
			err = writeStatus(writer, 0)
		case "RLNK":
			s.mu.Lock()
			s.synchronized = false
			for chain := range s.topology.LinkStatuses {
				if s.topology.LinkStatuses[chain] != 0 {
					s.topology.LinkStatuses[chain] = 1
				}
			}
			s.mu.Unlock()
			err = writeStatus(writer, 0)
		case "CLRS":
			err = writeStatus(writer, 0)
		default:
			return
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) handleChainControl(connection net.Conn) error {
	rest := make([]byte, 8)
	if _, err := io.ReadFull(connection, rest); err != nil {
		return err
	}
	chain := binary.LittleEndian.Uint16(rest[0:2])
	if chain >= dt5215.MaxChains {
		return writeStatus(connection, 2)
	}
	return writeStatus(connection, 0)
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
	response := make([]byte, 12)
	s.mu.Lock()
	defer s.mu.Unlock()
	boards := s.topology.Chains[chain]
	if len(boards) == 0 {
		binary.LittleEndian.PutUint32(response[0:4], dt5215.StatusChainDisabled)
	} else {
		binary.LittleEndian.PutUint32(response[4:8], uint32(len(boards)))
		binary.LittleEndian.PutUint32(response[8:12], 60+uint32(chain))
		s.topology.LinkStatuses[chain] = 4
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
	board := &s.topology.Chains[chain][node]
	var value uint32
	switch address {
	case dt5215.RegisterProductID:
		value = board.ProductID
	case dt5215.RegisterFirmwareRevision:
		value = board.FirmwareRevision
	case dt5215.RegisterAcquisitionStatus:
		value = board.Status
	case uint32(dt5202.SPIData):
		value = board.readSPI()
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
	if address == uint32(dt5202.SPIData) {
		if err := board.writeSPI(value); err != nil {
			return writeStatus(connection, 22)
		}
	}
	if board.Registers == nil {
		board.Registers = make(map[uint32]uint32)
	}
	board.Registers[address] = value
	if address == uint32(dt5202.HVRegisterAddress) {
		board.hvSelector = value
	} else if address == uint32(dt5202.HVRegisterData) && board.hvSelector != 0x2001 {
		if board.HVRegisters == nil {
			board.HVRegisters = make(map[uint32]uint32)
		}
		board.HVRegisters[board.hvSelector] = value
		if board.hvSelector == 0x200 {
			status := board.Registers[uint32(dt5202.HVStatus)] &^ (uint32(1) << 26)
			if value != 0 {
				status |= 1 << 26
				board.Registers[uint32(dt5202.HVVoltageMonitor)] = board.HVRegisters[0x102]
			} else {
				board.Registers[uint32(dt5202.HVVoltageMonitor)] = 0
			}
			board.Registers[uint32(dt5202.HVStatus)] = status
		}
	}
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
	type target struct {
		chain, node int
		board       *Board
	}
	targets := []target{}
	if chain == 0xff && node == 0xff {
		for c := range s.topology.Chains {
			for n := range s.topology.Chains[c] {
				targets = append(targets, target{c, n, &s.topology.Chains[c][n]})
			}
		}
	} else if chain >= dt5215.MaxChains || int(node) >= len(s.topology.Chains[chain]) {
		return writeStatus(connection, 2)
	} else {
		targets = append(targets, target{int(chain), int(node), &s.topology.Chains[chain][node]})
	}
	if command == dt5215.CommandAcquisitionStart && !s.synchronized {
		return writeStatus(connection, 10)
	}
	for _, target := range targets {
		board := target.board
		switch command {
		case dt5215.CommandAcquisitionStart:
			board.Status = uint32(dt5202.StatusRunning)
			if board.Registers[uint32(dt5202.AcquisitionControl)]&(3<<18) != 0 {
				s.eventSequence++
				batch, err := generatedBatch(uint8(target.chain), uint8(target.node), s.eventSequence, dt5202.QualifierService, board)
				if err != nil {
					return writeStatus(connection, 22)
				}
				item, drop := s.prepareStreamItem(batch, true, false)
				if !drop {
					select {
					case s.streamData <- item:
					default:
						return writeStatus(connection, 11)
					}
				}
			}
		case dt5215.CommandAcquisitionStop:
			if dt5202.Status(board.Status).Has(dt5202.StatusRunning) {
				board.Status = 1
				s.eventSequence++
				batch, err := generatedBatch(uint8(target.chain), uint8(target.node), s.eventSequence, dt5202.QualifierService, board)
				if err != nil {
					return writeStatus(connection, 22)
				}
				item, drop := s.prepareStreamItem(batch, true, true)
				if drop {
					break
				}
				select {
				case s.streamData <- item:
				default:
					return writeStatus(connection, 11)
				}
			}
		case dt5215.CommandGlobalReset:
			board.Status = 1
			board.Registers = monitorRegisters()
			board.CitirocLoads = [2]uint32{}
			board.HVRegisters = make(map[uint32]uint32)
			board.hvSelector = 0
			board.spi = flashReadState{}
		case dt5215.CommandTestPulse:
			if !dt5202.Status(board.Status).Has(dt5202.StatusRunning) {
				return writeStatus(connection, 10)
			}
			s.eventSequence++
			batch, err := generatedBatch(uint8(target.chain), uint8(target.node), s.eventSequence, 0, board)
			if err != nil {
				return writeStatus(connection, 22)
			}
			item, drop := s.prepareStreamItem(batch, false, false)
			if drop {
				break
			}
			select {
			case s.streamData <- item:
			default:
				return writeStatus(connection, 11)
			}
		case dt5215.CommandResetTime, dt5215.CommandResetPeriodic, dt5215.CommandSoftwareTrigger, dt5215.CommandClearData, dt5215.CommandSync:
		case uint32(dt5202.CommandConfigureASIC):
			chip := (board.Registers[uint32(dt5202.CitirocSlowControl)] >> 9) & 1
			board.CitirocLoads[chip]++
		default:
			return writeStatus(connection, 22)
		}
	}
	return writeStatus(connection, 0)
}
func eventBatch(chain, node, qualifier uint8, sequence uint64, payload []byte) []byte {
	batch := make([]byte, 12+32+len(payload))
	binary.LittleEndian.PutUint32(batch, 0xffffffff)
	binary.LittleEndian.PutUint32(batch[4:], 0xffffffff)
	binary.LittleEndian.PutUint32(batch[8:], uint32(chain)|(1<<8))
	binary.LittleEndian.PutUint32(batch[12:], uint32(len(payload)/4))
	binary.LittleEndian.PutUint32(batch[24:], uint32(sequence<<16))
	binary.LittleEndian.PutUint32(batch[40:], uint32(node)|uint32(qualifier)<<8)
	copy(batch[44:], payload)
	return batch
}

func monitorRegisters() map[uint32]uint32 {
	return map[uint32]uint32{
		uint32(dt5202.FPGATemperature):  2500,
		uint32(dt5202.BoardTemperature): 100,
		uint32(dt5202.HVStatus):         1000 | 1200<<13,
	}
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
