package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
)

func TestRunRequiresConfigurationBeforeNetworkAccess(t *testing.T) {
	err := run(context.Background(), nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-config is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestListenHTTPExplainsHowToSelectAnotherPort(t *testing.T) {
	_, err := listenHTTP("invalid-address")
	if err == nil || !strings.Contains(err.Error(), "-listen 127.0.0.1:8081") {
		t.Fatalf("error = %v", err)
	}
}

func TestTopologySnapshotIncludesEnabledAndDisabledChains(t *testing.T) {
	var topology dt5215.Topology
	topology.Chains[0] = dt5215.ChainInfo{Status: 3, BoardCount: 1}
	topology.Boards = []dt5215.BoardInfo{{Chain: 0, Node: 0, ProductID: 5202, FirmwareRevision: 0x050100}}
	snapshot := topologySnapshot(topology)
	if snapshot.State != daqv1.SystemState_SYSTEM_STATE_IDLE || len(snapshot.Chains) != dt5215.MaxChains {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if !snapshot.Chains[0].Enabled || snapshot.Chains[0].Health != daqv1.HealthStatus_HEALTH_STATUS_OK || len(snapshot.Chains[0].Boards) != 1 {
		t.Fatalf("enabled chain = %+v", snapshot.Chains[0])
	}
	if snapshot.Chains[1].Enabled || snapshot.Chains[1].Health != daqv1.HealthStatus_HEALTH_STATUS_UNKNOWN {
		t.Fatalf("disabled chain = %+v", snapshot.Chains[1])
	}
}

func TestPrintDiscoveredDevices(t *testing.T) {
	topology := dt5215.Topology{Boards: []dt5215.BoardInfo{
		{Chain: 0, Node: 0, ProductID: 64883, FirmwareRevision: 0x0800a707, AcquisitionState: 9},
		{Chain: 3, Node: 0, ProductID: 64884, FirmwareRevision: 0x0800a707, AcquisitionState: 1},
	}}
	var output bytes.Buffer
	printDiscoveredDevices(&output, topology)
	want := "devices found=2\n" +
		"device chain=0 node=0 product_id=64883 fpga_firmware=0x0800a707 acquisition_status=0x00000009\n" +
		"device chain=3 node=0 product_id=64884 fpga_firmware=0x0800a707 acquisition_status=0x00000001\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestInstanceIDIsNonEmptyAndChanges(t *testing.T) {
	first, second := instanceID(), instanceID()
	if first == "" || second == "" || first == second {
		t.Fatalf("instance IDs = %q %q", first, second)
	}
}
