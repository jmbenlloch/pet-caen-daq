package runstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindIncompleteReportsValidAndCorruptRunsWithoutMutation(t *testing.T) {
	parent := t.TempDir()
	valid, err := Create(parent, Manifest{RunID: "20", StartedAt: "2026-07-21T16:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := valid.Abort(); err != nil {
		t.Fatal(err)
	}
	complete, err := Create(parent, Manifest{RunID: "10"})
	if err != nil {
		t.Fatal(err)
	}
	if err := complete.Finalize("2026-07-21T16:01:00Z", "complete"); err != nil {
		t.Fatal(err)
	}
	corrupt, err := Create(parent, Manifest{RunID: "30"})
	if err != nil {
		t.Fatal(err)
	}
	if err := corrupt.Abort(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corrupt.Directory(), "manifest.json"), []byte("{"), 0o640); err != nil {
		t.Fatal(err)
	}

	runs, err := FindIncomplete(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].RunID != "20" || runs[1].RunID != "30" {
		t.Fatalf("runs = %+v", runs)
	}
	if runs[0].Manifest == nil || runs[0].Problem != "" || !strings.Contains(runs[1].Problem, "decode manifest") {
		t.Fatalf("runs = %+v", runs)
	}
	for _, run := range runs {
		if _, err := os.Stat(filepath.Join(run.Directory, "incomplete")); err != nil {
			t.Fatalf("discovery mutated %s: %v", run.RunID, err)
		}
	}
}

func TestFindIncompleteRejectsIdentityMismatchAndNonRegularMarker(t *testing.T) {
	parent := t.TempDir()
	run, err := Create(parent, Manifest{RunID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Abort(); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"schema_version":1,"run_id":"other","event_count":"0"}`)
	if err := os.WriteFile(filepath.Join(run.Directory(), "manifest.json"), manifest, 0o640); err != nil {
		t.Fatal(err)
	}
	runs, err := FindIncomplete(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || !strings.Contains(runs[0].Problem, "does not match") {
		t.Fatalf("identity mismatch runs = %+v", runs)
	}
	marker := filepath.Join(run.Directory(), "incomplete")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(marker, 0o750); err != nil {
		t.Fatal(err)
	}
	runs, err = FindIncomplete(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || !strings.Contains(runs[0].Problem, "not a regular file") {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestFindIncompleteRequiresReadableParent(t *testing.T) {
	if _, err := FindIncomplete(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("accepted missing storage parent")
	}
}
