package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	daqv1 "github.com/jmbenlloch/pet-caen-daq/backend/gen/pet/caen/daq/v1"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/janusconfig"
)

func TestRunRequiresConfigurationBeforeNetworkAccess(t *testing.T) {
	err := run(context.Background(), nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-config is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestInspectOnlyStillRequiresConfiguration(t *testing.T) {
	err := run(context.Background(), []string{"-inspect-only"}, io.Discard)
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
	topology := dt5215.Topology{Concentrator: dt5215.ConcentratorInfo{
		SoftwareRevision: "2026.4.1.1", FPGARevision: "25.11.24.01-2-2", ProductID: 66643,
	}}
	topology.Chains[0] = dt5215.ChainInfo{Status: 3, BoardCount: 1}
	topology.Boards = []dt5215.BoardInfo{{
		Chain: 0, Node: 0, ProductID: 5202, FirmwareRevision: 0x050100,
		HVModuleFirmwareRaw: 0x3f99999a, HVModuleFirmwareVersion: 1.2, HVModuleFirmwareAvailable: true,
	}}
	snapshot := topologySnapshot(topology, []janusconfig.Connection{{Board: 7, Chain: 0, Node: 0}})
	if snapshot.State != daqv1.SystemState_SYSTEM_STATE_IDLE || len(snapshot.Chains) != dt5215.MaxChains {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Chains[0].Boards[0].GetLogicalIndex() != 7 {
		t.Fatalf("logical board = %d, want 7", snapshot.Chains[0].Boards[0].GetLogicalIndex())
	}
	if snapshot.Concentrator.GetSoftwareRevision() != "2026.4.1.1" || snapshot.Concentrator.GetFpgaRevision() != "25.11.24.01-2-2" || snapshot.Concentrator.GetProductId() != 66643 {
		t.Fatalf("concentrator = %+v", snapshot.Concentrator)
	}
	if !snapshot.Chains[0].Enabled || snapshot.Chains[0].Health != daqv1.HealthStatus_HEALTH_STATUS_OK || len(snapshot.Chains[0].Boards) != 1 {
		t.Fatalf("enabled chain = %+v", snapshot.Chains[0])
	}
	board := snapshot.Chains[0].Boards[0]
	if !board.HvModuleFirmwareAvailable || board.HvModuleFirmwareRaw != 0x3f99999a || board.HvModuleFirmwareVersion != 1.2 {
		t.Fatalf("HV firmware metadata = %+v", board)
	}
	if snapshot.Chains[1].Enabled || snapshot.Chains[1].Health != daqv1.HealthStatus_HEALTH_STATUS_UNKNOWN {
		t.Fatalf("disabled chain = %+v", snapshot.Chains[1])
	}
}

func TestExecutionIdentityPreservesDiscoveredHardwareEvidence(t *testing.T) {
	topology := dt5215.Topology{Boards: []dt5215.BoardInfo{{
		Chain: 3, Node: 1, ProductID: 5202, FirmwareRevision: 0xa1230708, AcquisitionState: 9,
	}}}
	identity := executionIdentity(topology, []janusconfig.Connection{{Board: 7, Chain: 3, Node: 1}}, "10.0.0.1:9760", "10.0.0.1:9000")
	if identity.Topology.Concentrator.ControlAddress != "10.0.0.1:9760" ||
		identity.Topology.Concentrator.FirmwareRevision != nil ||
		identity.Topology.Concentrator.FirmwareRevisionEvidence != "unknown-not-queried" ||
		len(identity.Topology.Boards) != 1 {
		t.Fatalf("identity = %+v", identity)
	}
	board := identity.Topology.Boards[0]
	if board.Board != 7 || board.Chain != 3 || board.Node != 1 || board.ProductID != 5202 ||
		board.FirmwareRevision != 0xa1230708 || board.AcquisitionState != 9 ||
		board.FirmwareEvidence != "hardware-register-read" {
		t.Fatalf("board identity = %+v", board)
	}
	if identity.Software.Revision == "" || identity.Software.GoVersion == "" {
		t.Fatalf("software identity = %+v", identity.Software)
	}
}

func TestPrintDiscoveredDevices(t *testing.T) {
	topology := dt5215.Topology{Concentrator: dt5215.ConcentratorInfo{
		SoftwareRevision: "2026.4.1.1", FPGARevision: "25.11.24.01-2-2", ProductID: 66643,
	}, Boards: []dt5215.BoardInfo{
		{Chain: 0, Node: 0, ProductID: 64883, FirmwareRevision: 0x0800a707, HVModuleFirmwareRaw: 0x3f99999a, HVModuleFirmwareVersion: 1.2, HVModuleFirmwareAvailable: true, AcquisitionState: 9},
		{Chain: 3, Node: 0, ProductID: 64884, FirmwareRevision: 0x0800a707, AcquisitionState: 1},
	}}
	var output bytes.Buffer
	printDiscoveredDevices(&output, topology)
	want := "concentrator product_id=66643 software_revision=2026.4.1.1 fpga_revision=25.11.24.01-2-2\n" +
		"devices found=2\n" +
		"device chain=0 node=0 product_id=64883 fpga_firmware=0x0800a707 hv_module_firmware=1.2 hv_module_firmware_raw=0x3f99999a acquisition_status=0x00000009\n" +
		"device chain=3 node=0 product_id=64884 fpga_firmware=0x0800a707 hv_module_firmware=unavailable hv_module_firmware_raw=0x00000000 acquisition_status=0x00000001\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

type failingHVFirmwareHardware struct{}

func (failingHVFirmwareHardware) WriteRegister(context.Context, uint16, uint16, uint32, uint32) error {
	return errors.New("injected")
}

func (failingHVFirmwareHardware) ReadRegister(context.Context, uint16, uint16, uint32) (uint32, error) {
	return 0, nil
}

func TestHVModuleFirmwareFailureWarnsAndPreservesUnavailableMetadata(t *testing.T) {
	topology := dt5215.Topology{Boards: []dt5215.BoardInfo{{Chain: 2, Node: 0}}}
	var output bytes.Buffer
	readHVModuleFirmware(context.Background(), failingHVFirmwareHardware{}, &topology, &output)
	board := topology.Boards[0]
	if board.HVModuleFirmwareAvailable || board.HVModuleFirmwareRaw != 0 || board.HVModuleFirmwareVersion != 0 {
		t.Fatalf("board metadata = %+v", board)
	}
	if !strings.Contains(output.String(), "device warning chain=2 node=0 hv_module_firmware=unavailable") {
		t.Fatalf("warning = %q", output.String())
	}
}

var _ dt5202.HVHardware = failingHVFirmwareHardware{}

func TestInstanceIDIsNonEmptyAndChanges(t *testing.T) {
	first, second := instanceID(), instanceID()
	if first == "" || second == "" || first == second {
		t.Fatalf("instance IDs = %q %q", first, second)
	}
}
