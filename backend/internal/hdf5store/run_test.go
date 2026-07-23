//go:build hdf5

package hdf5store

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	hdf5 "github.com/next-exp/hdf5-go"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5202"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/dt5215"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestRunWriterFinalizesHDF5ArtifactAndExternalManifest(t *testing.T) {
	parent := t.TempDir()
	writer, err := CreateRun(parent, runstore.Manifest{
		RunID: "42", StartedAt: "2026-07-23T10:00:00Z", RequestedBy: "operator",
		RequestedConfiguration: "AcquisitionMode TEST\r\n",
		ConfigurationIdentity:  runstore.ConfigurationIdentity{ParserVersion: 1},
		ExecutionIdentity: runstore.ExecutionIdentity{
			Topology: runstore.TopologyIdentity{Boards: []runstore.BoardIdentity{{Board: 0, Chain: 1, Node: 2, FirmwareRevision: 0x0708}}},
			Software: runstore.SoftwareIdentity{Revision: "abc123", GoVersion: "go-test"},
			Storage:  runstore.StorageIdentity{Format: "hdf5", WriterVersion: 1, Compression: "none"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wire := dt5215.StreamEvent{Chain: 1, Descriptor: dt5215.Descriptor{
		Node: 2, Qualifier: dt5202.QualifierTest, TriggerID: 8, Timestamp: 9,
	}}
	event := dt5202.Event{Kind: dt5202.EventTest, Test: &dt5202.TestEvent{
		TriggerID: 8, Timestamp: 9, Words: []uint32{0x11223344},
	}}
	if err := writer.AppendEvent(wire, event); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finalize("2026-07-23T10:01:00Z", "operator_stop"); err != nil {
		t.Fatal(err)
	}
	manifest, err := runstore.ReadManifest(writer.Directory(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.EventCount != 1 || manifest.CompletedAt == "" || len(manifest.Artifacts) != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
	artifact := manifest.Artifacts[0]
	if artifact.Name != "events.h5" || artifact.Kind != "decoded_events" || artifact.SizeBytes == 0 || len(artifact.SHA256) != 64 {
		t.Fatalf("artifact = %+v", artifact)
	}
	opened, _, err := runstore.OpenArtifact(parent, "42", "events.h5")
	if err != nil {
		t.Fatal(err)
	}
	opened.Close()
	if _, err := os.Stat(filepath.Join(writer.Directory(), "events.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("JSONL artifact exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(writer.Directory(), "incomplete")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete marker remains: %v", err)
	}
	if err := Validate(filepath.Join(writer.Directory(), "events.h5"), true); err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(source), "..", "..", "..", "scripts", "validate-hdf5.py")
	command := exec.Command("python3", script, filepath.Join(writer.Directory(), "events.h5"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("independent h5py validation: %v\n%s", err, output)
	}
	file, err := hdf5.OpenFile(filepath.Join(writer.Directory(), "events.h5"), hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	embedded, err := file.OpenDataset("run/manifest_json")
	if err != nil {
		t.Fatal(err)
	}
	defer embedded.Close()
	space := embedded.Space()
	if space == nil {
		t.Fatal("manifest dataspace is missing")
	}
	dimensions, _, err := space.SimpleExtentDims()
	space.Close()
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, dimensions[0])
	if err := embedded.Read(&data); err != nil {
		t.Fatal(err)
	}
	var snapshot runstore.Manifest
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.RunID != "42" || snapshot.EventCount != 1 || len(snapshot.Artifacts) != 0 {
		t.Fatalf("embedded manifest = %+v", snapshot)
	}
	if snapshot.ConfigurationIdentity.ParserVersion != 1 ||
		snapshot.ExecutionIdentity.Software.Revision != "abc123" ||
		len(snapshot.ExecutionIdentity.Topology.Boards) != 1 ||
		snapshot.ExecutionIdentity.Storage.Format != "hdf5" {
		t.Fatalf("embedded metadata = %+v", snapshot)
	}
}

func TestRunWriterAbortRetainsIncompleteHDF5(t *testing.T) {
	writer, err := CreateRun(t.TempDir(), runstore.Manifest{RunID: "failed", StartedAt: "now"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(writer.Directory(), "incomplete")); err != nil {
		t.Fatal(err)
	}
	file, err := hdf5.OpenFile(filepath.Join(writer.Directory(), "events.h5"), hdf5.F_ACC_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	root, err := file.OpenGroup("/")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	attribute, err := root.OpenAttribute("complete")
	if err != nil {
		t.Fatal(err)
	}
	defer attribute.Close()
	var complete uint8
	if err := attribute.Read(&complete, hdf5.T_STD_U8LE); err != nil {
		t.Fatal(err)
	}
	if complete != 0 {
		t.Fatalf("complete = %d", complete)
	}
	if err := Validate(filepath.Join(writer.Directory(), "events.h5"), false); err != nil {
		t.Fatal(err)
	}
	if err := Validate(filepath.Join(writer.Directory(), "events.h5"), true); err == nil {
		t.Fatal("expected incomplete validation failure")
	}
}

func TestRunWriterFinalizationFailureClosesHDF5AndRetainsIncomplete(t *testing.T) {
	writer, err := CreateRun(t.TempDir(), runstore.Manifest{RunID: "finalize-failure", StartedAt: "now"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.events.manifestJSON.dataset.Close(); err != nil {
		t.Fatal(err)
	}
	err = writer.Finalize("later", "operator_stop")
	if err == nil {
		t.Fatal("expected finalization failure")
	}
	if _, statErr := os.Stat(filepath.Join(writer.Directory(), "incomplete")); statErr != nil {
		t.Fatalf("incomplete marker missing after %v: %v", err, statErr)
	}
	file, openErr := hdf5.OpenFile(filepath.Join(writer.Directory(), "events.h5"), hdf5.F_ACC_RDONLY)
	if openErr != nil {
		t.Fatalf("reopen failed artifact after %v: %v", err, openErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if abortErr := writer.Abort(); abortErr != nil {
		t.Fatalf("abort after failed finalization: %v", abortErr)
	}
}
