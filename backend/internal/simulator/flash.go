package simulator

import (
	"encoding/binary"
	"fmt"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
)

const (
	spiKeep        = uint32(0x100)
	spiPageRead    = byte(0xd2)
	spiPageProgram = byte(0x82)
)

type flashReadState struct {
	active     bool
	phase      int
	address    uint32
	offset     int
	finalClock bool
}

func (b *Board) writeSPI(value uint32) error {
	data := byte(value)
	if !b.spi.active {
		if data == spiPageProgram {
			return fmt.Errorf("protected flash programming is disabled")
		}
		if data == spiPageRead && value&spiKeep != 0 {
			b.spi = flashReadState{active: true, phase: 1}
			return nil
		}
		return nil
	}
	switch b.spi.phase {
	case 1:
		b.spi.address = uint32(data) << 16
		b.spi.phase++
	case 2:
		b.spi.address |= uint32(data) << 8
		b.spi.phase++
	case 3:
		b.spi.address |= uint32(data)
		b.spi.phase++
	case 4, 5, 6, 7:
		b.spi.phase++
	default:
		b.spi.finalClock = value&spiKeep == 0
	}
	return nil
}

func (b *Board) readSPI() uint32 {
	if !b.spi.active || b.spi.phase < 8 {
		return 0
	}
	page := uint16(b.spi.address >> 10)
	data := b.protectedFlash[page]
	value := byte(0xff)
	if b.spi.offset < len(data) {
		value = data[b.spi.offset]
	}
	b.spi.offset++
	if b.spi.finalClock {
		b.spi.active = false
	}
	return uint32(value)
}

func simulatorPedestalPage(board *Board) []byte {
	page := make([]byte, dt5202.PedestalFlashPageBytes)
	page[0], page[1] = 'P', 0
	binary.LittleEndian.PutUint16(page[2:], 2026)
	page[4], page[5] = 7, 21
	for index := 0; index < 4; index++ {
		binary.LittleEndian.PutUint16(page[6+index*2:], 2750)
	}
	for channel := 0; channel < dt5202.ChannelCount; channel++ {
		binary.LittleEndian.PutUint16(page[16+channel*2:], board.Pedestal.LowGain[channel])
		binary.LittleEndian.PutUint16(page[16+dt5202.ChannelCount*2+channel*2:], board.Pedestal.HighGain[channel])
	}
	return page
}
