package runstore

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestListManifestsAndOpenArtifact(t *testing.T) {
	parent := t.TempDir()
	first, err := Create(parent, Manifest{RunID: "41", StartedAt: "2026-07-20T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Append(Envelope{Kind: "test", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := first.Finalize("2026-07-20T10:01:00Z", "operator_stop"); err != nil {
		t.Fatal(err)
	}
	second, err := Create(parent, Manifest{RunID: "42", StartedAt: "2026-07-21T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Abort(); err != nil {
		t.Fatal(err)
	}

	manifests, err := ListManifests(parent, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) != 2 || manifests[0].RunID != "42" || manifests[1].RunID != "41" {
		t.Fatalf("manifests = %+v", manifests)
	}
	file, artifact, err := OpenArtifact(parent, "41", "events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil || len(data) == 0 || artifact.SizeBytes != uint64(len(data)) {
		t.Fatalf("data=%q artifact=%+v err=%v", data, artifact, err)
	}
	if _, _, err := OpenArtifact(parent, "41", "../manifest.json"); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("traversal error = %v", err)
	}
}

func TestHistoryRejectsCorruptManifestAndSymlinkArtifact(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "run-bad")
	if err := os.Mkdir(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), []byte(`{`), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := ListManifests(parent, 10); err == nil {
		t.Fatal("expected corrupt manifest error")
	}

	good, err := Create(parent, Manifest{RunID: "good", StartedAt: "2026-07-21T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := good.Append(Envelope{Kind: "test", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := good.Finalize("2026-07-21T10:01:00Z", "stop"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(good.Directory(), "events.jsonl")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("manifest.json", path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenArtifact(parent, "good", "events.jsonl"); err == nil {
		t.Fatal("expected symlink rejection")
	}
}
