//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/acquisition"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/rawcapture"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/simulator"
)

func TestPersistedTestPulseRun(t *testing.T) {
	server, err := simulator.Start("127.0.0.1:0", "127.0.0.1:0", simulator.ProductionTopology())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := dt5215.Dial(ctx, server.ControlAddress(), server.StreamAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	run, err := runstore.Create(t.TempDir(), runstore.Manifest{RunID: "simulated-test-pulse", StartedAt: "2026-07-20T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err = run.EnableRawCapture(); err != nil {
		t.Fatal(err)
	}
	if err = acquisition.RunTestPulse(ctx, client, run, 4); err != nil {
		run.Abort()
		t.Fatal(err)
	}
	if err = run.Finalize("2026-07-20T00:00:01Z", "completed"); err != nil {
		t.Fatal(err)
	}
	eventsFile, err := os.Open(filepath.Join(run.Directory(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer eventsFile.Close()
	eventReader := runstore.NewReader(eventsFile, 0)
	for sequence := uint64(1); sequence <= 4; sequence++ {
		event, err := eventReader.Next()
		if err != nil {
			t.Fatal(err)
		}
		if event.Sequence != sequence {
			t.Fatalf("sequence = %d, want %d", event.Sequence, sequence)
		}
	}
	if _, err = eventReader.Next(); err != io.EOF {
		t.Fatalf("events end = %v", err)
	}
	rawFile, err := os.Open(filepath.Join(run.Directory(), "wire.raw"))
	if err != nil {
		t.Fatal(err)
	}
	defer rawFile.Close()
	replay, err := rawcapture.NewReader(rawFile)
	if err != nil {
		t.Fatal(err)
	}
	for chain := 0; chain < 4; chain++ {
		batch, err := replay.Next()
		if err != nil {
			t.Fatal(err)
		}
		events, err := dt5215.DecodeStreamBatch(batch)
		if err != nil || len(events) != 1 || int(events[0].Chain) != chain {
			t.Fatalf("raw chain %d: %#v %v", chain, events, err)
		}
	}
	if _, err = os.Stat(filepath.Join(run.Directory(), "incomplete")); !os.IsNotExist(err) {
		t.Fatalf("incomplete marker: %v", err)
	}
}

func TestControlAndPartialStreamWorkflow(t *testing.T) {
	server, err := simulator.Start("127.0.0.1:0", "127.0.0.1:0", simulator.ProductionTopology())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := dt5215.Dial(ctx, server.ControlAddress(), server.StreamAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	const register = uint32(0x01000050)
	if err = client.WriteRegister(ctx, 0, 0, register, 42); err != nil {
		t.Fatal(err)
	}
	value, err := client.ReadRegister(ctx, 0, 0, register)
	if err != nil || value != 42 {
		t.Fatalf("read value %d: %v", value, err)
	}
	if err = client.SendCommand(ctx, 0, 0, dt5215.CommandAcquisitionStart, 0); err == nil {
		t.Fatal("start succeeded before synchronization")
	}
	if err = client.Synchronize(ctx); err != nil {
		t.Fatal(err)
	}
	if err = client.SendCommand(ctx, 0xff, 0xff, dt5215.CommandAcquisitionStart, 0); err != nil {
		t.Fatal(err)
	}
	status, err := client.ReadRegister(ctx, 3, 0, dt5215.RegisterAcquisitionStatus)
	if err != nil || status != 2 {
		t.Fatalf("running status %d: %v", status, err)
	}
	batch := testBatch()
	server.QueueStreamBatch(batch[:7])
	server.QueueStreamBatch(batch[7:41])
	server.QueueStreamBatch(batch[41:])
	events, err := client.ReadStreamBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Chain != 2 || events[0].Descriptor.TriggerID != 99 {
		t.Fatalf("events = %#v", events)
	}
	decoded, err := dt5202.DecodeSpectroscopy(events[0].Descriptor.Qualifier, events[0].Descriptor.TriggerID, events[0].Descriptor.Timestamp, events[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Energies) != 1 || decoded.Energies[0].HighGain != 123 || decoded.Energies[0].LowGain != 456 || len(decoded.Timings) != 1 || decoded.Timings[0].ToA != 789 {
		t.Fatalf("decoded event = %#v", decoded)
	}
	if err = client.SendCommand(ctx, 0xff, 0xff, dt5215.CommandTestPulse, 0); err != nil {
		t.Fatal(err)
	}
	var captured bytes.Buffer
	capture, err := rawcapture.NewWriter(&captured)
	if err != nil {
		t.Fatal(err)
	}
	for chain := 0; chain < 4; chain++ {
		raw, events, err := client.ReadRawStreamBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = capture.Append(raw); err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || int(events[0].Chain) != chain {
			t.Fatalf("test-pulse chain %d events = %#v", chain, events)
		}
		event, err := dt5202.DecodeSpectroscopy(events[0].Descriptor.Qualifier, events[0].Descriptor.TriggerID, events[0].Descriptor.Timestamp, events[0].Payload)
		if err != nil {
			t.Fatal(err)
		}
		if event.Energies[0].HighGain != uint16(101+chain) || event.Energies[0].LowGain != uint16(201+chain) {
			t.Fatalf("chain %d event = %#v", chain, event)
		}
	}
	if err = capture.Close(); err != nil {
		t.Fatal(err)
	}
	replay, err := rawcapture.NewReader(bytes.NewReader(captured.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	for chain := 0; chain < 4; chain++ {
		raw, err := replay.Next()
		if err != nil {
			t.Fatal(err)
		}
		events, err := dt5215.DecodeStreamBatch(raw)
		if err != nil || len(events) != 1 || int(events[0].Chain) != chain {
			t.Fatalf("replay chain %d: %#v %v", chain, events, err)
		}
	}
	if err = client.SendCommand(ctx, 0xff, 0xff, dt5215.CommandAcquisitionStop, 0); err != nil {
		t.Fatal(err)
	}
}
func testBatch() []byte {
	payload := make([]byte, 16)
	binary.LittleEndian.PutUint64(payload, 1)
	binary.LittleEndian.PutUint32(payload[8:], 123|(456<<16))
	binary.LittleEndian.PutUint32(payload[12:], 7<<16|789)
	b := make([]byte, 12+32+len(payload))
	binary.LittleEndian.PutUint32(b, 0xffffffff)
	binary.LittleEndian.PutUint32(b[4:], 0xffffffff)
	binary.LittleEndian.PutUint32(b[8:], 2|(1<<8))
	binary.LittleEndian.PutUint32(b[12:], uint32(len(payload)/4))
	binary.LittleEndian.PutUint32(b[24:], uint32(99<<16))
	binary.LittleEndian.PutUint32(b[40:], uint32(dt5202.QualifierSpectroscopy|dt5202.QualifierTiming|dt5202.QualifierBothGains)<<8)
	copy(b[44:], payload)
	return b
}

func TestProductionConfigurationDiscoversSimulatedTopology(t *testing.T) {
	server, err := simulator.Start("127.0.0.1:0", "127.0.0.1:0", simulator.ProductionTopology())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	fixture := filepath.Join("..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt")
	file, err := os.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	document, err := janusconfig.Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	connections, err := document.Connections()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := dt5215.Dial(ctx, server.ControlAddress(), server.StreamAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	topology, err := client.DiscoverProductionTopology(ctx, connections)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(topology.Boards); got != 4 {
		t.Fatalf("board count = %d, want 4", got)
	}
	wantPIDs := []uint32{64883, 64138, 64885, 64884}
	for index, board := range topology.Boards {
		if board.Chain != uint16(index) || board.Node != 0 || board.ProductID != wantPIDs[index] {
			t.Fatalf("board %d = %#v", index, board)
		}
	}
}

func TestProductionConfigurationAppliesAndValidatesFourBoards(t *testing.T) {
	server, err := simulator.Start("127.0.0.1:0", "127.0.0.1:0", simulator.ProductionTopology())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	file, err := os.Open(filepath.Join("..", "..", "test", "fixtures", "janus", "config_same4_v3_good.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	document, err := janusconfig.Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := dt5215.Dial(ctx, server.ControlAddress(), server.StreamAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	wantThreshold := []uint32{181, 183, 179, 178}
	for board := range 4 {
		plan, err := dt5202.PlanProductionConfiguration(document, board)
		if err != nil {
			t.Fatalf("plan board %d: %v", board, err)
		}
		calibration := dt5202.PedestalCalibration{Source: "deterministic simulator calibration"}
		for channel := range dt5202.ChannelCount {
			calibration.LowGain[channel] = 50
			calibration.HighGain[channel] = 50
		}
		plan, err = plan.WithPedestalCalibration(calibration)
		if err != nil {
			t.Fatalf("complete board %d calibration: %v", board, err)
		}
		if len(plan.Deferred) != 0 {
			t.Fatalf("board %d deferred settings = %#v", board, plan.Deferred)
		}
		if err := dt5202.ApplyConfiguration(ctx, client, uint16(board), 0, plan, true); err != nil {
			t.Fatalf("apply board %d: %v", board, err)
		}
		if err := dt5202.ApplyHVConfiguration(ctx, client, uint16(board), 0, plan.HV); err != nil {
			t.Fatalf("apply board %d HV: %v", board, err)
		}
		snapshot, err := server.BoardSnapshot(board, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := snapshot.Registers[uint32(dt5202.TimeCoarseThreshold)]; got != wantThreshold[board] {
			t.Errorf("board %d TD threshold = %d, want %d", board, got, wantThreshold[board])
		}
		if got := snapshot.Registers[uint32(dt5202.IndividualRegister(dt5202.HighGain, 63))]; got != 55 {
			t.Errorf("board %d channel 63 HG = %d", board, got)
		}
		if snapshot.CitirocLoads != [2]uint32{1, 1} {
			t.Errorf("board %d Citiroc loads = %v", board, snapshot.CitirocLoads)
		}
		for selector, want := range map[uint32]uint32{0x21e: 1, 0x102: 454000, 0x105: 10000, 0x108: 500000, 0x11c: 0xfffaa8d0, 0x001: 0} {
			if got := snapshot.HVRegisters[selector]; got != want {
				t.Errorf("board %d HV selector %#x = %#x, want %#x", board, selector, got, want)
			}
		}
	}
}

func TestDiscoveryRejectsUnexpectedEnabledLink(t *testing.T) {
	topology := simulator.ProductionTopology()
	topology.Chains[4] = []simulator.Board{{ProductID: 1, FirmwareRevision: 1, Status: 1}}
	topology.LinkStatuses[4] = 3
	server, err := simulator.Start("127.0.0.1:0", "127.0.0.1:0", topology)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	connections := make([]janusconfig.Connection, 4)
	for chain := range connections {
		connections[chain] = janusconfig.Connection{Board: chain, Interface: "usb", Host: "172.16.0.11", Chain: chain, Node: 0}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := dt5215.Dial(ctx, server.ControlAddress(), server.StreamAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.DiscoverProductionTopology(ctx, connections); err == nil {
		t.Fatal("discovery succeeded with unexpected enabled chain")
	}
}

func TestDiscoveryEnumeratesEnabledPreEnumerationState(t *testing.T) {
	topology := simulator.ProductionTopology()
	for chain := 0; chain < 4; chain++ {
		topology.LinkStatuses[chain] = 2
	}
	server, err := simulator.Start("127.0.0.1:0", "127.0.0.1:0", topology)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	connections := make([]janusconfig.Connection, 4)
	for chain := range connections {
		connections[chain] = janusconfig.Connection{Board: chain, Interface: "usb", Host: "172.16.0.11", Chain: chain, Node: 0}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := dt5215.Dial(ctx, server.ControlAddress(), server.StreamAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.DiscoverProductionTopology(ctx, connections); err != nil {
		t.Fatalf("discover pre-enumeration topology: %v", err)
	}
}
