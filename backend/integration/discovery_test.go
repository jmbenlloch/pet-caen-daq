//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/simulator"
)

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
