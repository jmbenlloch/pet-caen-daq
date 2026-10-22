package holddelaystore

import (
	"crypto/sha256"
	"fmt"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/holddelay"
	"github.com/jmbenlloch/pet-caen-daq/backend/internal/scanrecord"
	"path/filepath"
	"testing"
	"time"
)

func TestEvidenceSurvivesCancelledScanWithoutPoints(t *testing.T) {
	parent := t.TempDir()
	writer, err := Create(parent, NewManifest("42", 0, "operator", holddelay.Request{}, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if err := scanrecord.Emit(writer.RecordEvidence, scanrecord.Record{Type: "baseline"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finalize(time.Now().UTC().Format(time.RFC3339Nano), "cancelled", "context canceled", true); err != nil {
		t.Fatal(err)
	}
	manifest, _, err := Read(parent, "42")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(parent, "hold-scan-42")
	data, err := scanrecord.Read(dir)
	if err != nil || len(data) == 0 {
		t.Fatalf("evidence missing: %v", err)
	}
	if manifest.EvidenceArtifact == nil || manifest.EvidenceArtifact.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("evidence checksum missing or wrong: %+v", manifest.EvidenceArtifact)
	}
	if err := writer.RecordEvidence(scanrecord.Record{}); err == nil {
		t.Fatal("accepted evidence after finalization")
	}
}
