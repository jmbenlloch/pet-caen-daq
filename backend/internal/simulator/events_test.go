package simulator

import (
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

func TestPeriodicEventsAdvanceForEveryRunningBoard(t *testing.T) {
	topology := ProductionTopology()
	for chain := range topology.Chains {
		for node := range topology.Chains[chain] {
			topology.Chains[chain][node].Status = uint32(dt5202.StatusRunning)
		}
	}
	server := &Server{topology: topology, done: make(chan struct{}), streamData: make(chan streamItem, 16)}
	defer func() {
		close(server.done)
		server.wg.Wait()
	}()
	if err := server.EnablePeriodicEvents(time.Millisecond); err != nil {
		t.Fatal(err)
	}

	sequences := make([]uint64, 0, 8)
	deadline := time.After(time.Second)
	for len(sequences) < 8 {
		select {
		case item := <-server.streamData:
			wire, decodeErr := dt5215.DecodeStreamBatch(item.data)
			if decodeErr != nil || len(wire) != 1 {
				t.Fatalf("decode periodic batch: events=%d err=%v", len(wire), decodeErr)
			}
			if wire[0].Descriptor.Timestamp == 0 {
				t.Fatal("periodic event timestamp is zero")
			}
			sequences = append(sequences, wire[0].Descriptor.TriggerID)
		case <-deadline:
			t.Fatalf("received %d periodic events, want 8", len(sequences))
		}
	}
	for index := 1; index < 4; index++ {
		if sequences[index] != sequences[0] {
			t.Fatalf("first tick trigger IDs = %v", sequences[:4])
		}
	}
	if sequences[4] != sequences[0]+1 {
		t.Fatalf("successive tick trigger IDs = %d then %d", sequences[0], sequences[4])
	}
}

func configuredBoard(mode uint32, mask uint64) Board {
	board := Board{Status: uint32(dt5202.StatusRunning), Registers: map[uint32]uint32{
		uint32(dt5202.AcquisitionControl): mode,
		uint32(dt5202.ChannelMaskLow):     uint32(mask),
		uint32(dt5202.ChannelMaskHigh):    uint32(mask >> 32),
	}, CommonPedestal: 50}
	for channel := range board.Pedestal.LowGain {
		board.Pedestal.LowGain[channel] = 50
		board.Pedestal.HighGain[channel] = 50
	}
	return board
}

func decodeGenerated(t *testing.T, qualifier uint8, board *Board) dt5202.Event {
	t.Helper()
	batch, err := generatedBatch(2, 0, 1, qualifier, board)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := dt5215.DecodeStreamBatch(batch)
	if err != nil || len(wire) != 1 {
		t.Fatalf("wire = %#v, %v", wire, err)
	}
	event, err := dt5202.DecodeEvent(wire[0].Descriptor.Qualifier, wire[0].Descriptor.TriggerID, wire[0].Descriptor.Timestamp, wire[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if wire[0].Descriptor.TriggerID != 1 || wire[0].Descriptor.Timestamp != 1 {
		t.Fatalf("descriptor trigger ID/timestamp = %d/%d, want 1/1", wire[0].Descriptor.TriggerID, wire[0].Descriptor.Timestamp)
	}
	return event
}

func TestGeneratedSpectroscopyUsesConfigurationAndPedestal(t *testing.T) {
	board := configuredBoard(3|3<<12, 1<<1|1<<2)
	board.Registers[uint32(dt5202.ChargeDiscriminatorMaskLow)] = 1 << 1
	board.Registers[uint32(dt5202.TimeCoarseThreshold)] = 100
	board.Registers[uint32(dt5202.IndividualRegister(dt5202.HighGain, 1))] = 5
	board.Registers[uint32(dt5202.IndividualRegister(dt5202.LowGain, 1))] = 3
	board.Registers[uint32(dt5202.IndividualRegister(dt5202.TimeFineThreshold, 1))] = 4
	board.Registers[uint32(dt5202.IndividualRegister(dt5202.ZeroSuppressionHighGain, 1))] = 120
	board.Registers[uint32(dt5202.IndividualRegister(dt5202.ZeroSuppressionLowGain, 1))] = 220
	board.Pedestal.HighGain[1], board.Pedestal.LowGain[1] = 70, 80
	event := decodeGenerated(t, 0, &board).Spectroscopy
	if event.ChannelMask != 1<<1 || len(event.Energies) != 1 || len(event.Timings) != 1 {
		t.Fatalf("event = %#v", event)
	}
	if event.Energies[0].HighGain != 141 || event.Energies[0].LowGain != 243 {
		t.Fatalf("raw energies = %#v", event.Energies)
	}
	corrected := dt5202.ApplyPedestalCalibration(*event, board.CommonPedestal, board.Pedestal)
	if corrected.Energies[0].HighGain != 121 || corrected.Energies[0].LowGain != 213 {
		t.Fatalf("corrected energies = %#v", corrected.Energies)
	}

	board.Registers[uint32(dt5202.IndividualRegister(dt5202.ZeroSuppressionHighGain, 1))] = 142
	board.Registers[uint32(dt5202.IndividualRegister(dt5202.ZeroSuppressionLowGain, 1))] = 244
	if got := decodeGenerated(t, 0, &board).Spectroscopy.ChannelMask; got != 0 {
		t.Fatalf("zero-suppressed mask = %#x", got)
	}
}

func TestGeneratedTimingModesUseEnablesAndThresholds(t *testing.T) {
	for _, qualifier := range []uint8{dt5202.QualifierTiming, dt5202.QualifierCommonStop} {
		board := configuredBoard(uint32(qualifier), 1<<4|1<<5)
		board.Registers[uint32(dt5202.TimeCoarseThreshold)] = 250
		board.Registers[uint32(dt5202.IndividualRegister(dt5202.TimeFineThreshold, 4))] = 1
		board.Registers[uint32(dt5202.IndividualRegister(dt5202.TimeFineThreshold, 5))] = 20
		event := decodeGenerated(t, 0, &board).Timing
		if len(event.Hits) != 1 || event.Hits[0].Channel != 4 || event.Hits[0].ToT != 14 {
			t.Fatalf("qualifier %#x event = %#v", qualifier, event)
		}
	}
}

func TestGeneratedCountingWaveformAndService(t *testing.T) {
	counting := configuredBoard(uint32(dt5202.QualifierCounting), 1|1<<63)
	counts := decodeGenerated(t, 0, &counting).Counting
	if counts.ChannelMask != 1|1<<63 || len(counts.Counts) != 2 || counts.Counts[0].Value != 2 || counts.Counts[1].Value != 65 || counts.TORCount != 4 || counts.QORCount != 6 {
		t.Fatalf("counting = %#v", counts)
	}

	waveform := configuredBoard(uint32(dt5202.QualifierWaveform), 1)
	waveform.Registers[uint32(dt5202.WaveformLength)] = 3
	wave := decodeGenerated(t, 0, &waveform).Waveform
	if len(wave.Samples) != 3 || wave.Samples[2].HighGain != 103 || wave.Samples[2].LowGain != 203 || wave.Samples[2].DigitalProbes != 2 {
		t.Fatalf("waveform = %#v", wave)
	}

	service := configuredBoard(3<<18, 1<<2)
	status := decodeGenerated(t, dt5202.QualifierService, &service).Service
	if status.Version != 1 || status.Format != 3 || status.HVVoltage == nil || len(status.Counters) != 1 || status.Counters[0].Channel != 2 || status.TORCount != 11 || status.QORCount != 21 {
		t.Fatalf("service = %#v", status)
	}
}

func TestGeneratedEventRejectsUnsupportedModeAndOversizedWaveform(t *testing.T) {
	board := configuredBoard(0x55, 1)
	if _, err := generatedBatch(0, 0, 1, 0, &board); err == nil {
		t.Fatal("accepted unsupported acquisition mode")
	}
	board.Registers[uint32(dt5202.AcquisitionControl)] = uint32(dt5202.QualifierWaveform)
	board.Registers[uint32(dt5202.WaveformLength)] = dt5202.MaxEventPayloadBytes/4 + 1
	if _, err := generatedBatch(0, 0, 1, 0, &board); err == nil {
		t.Fatal("accepted oversized waveform")
	}
}
