package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestRebuildCheckAndBackup(t *testing.T) {
	runs := t.TempDir()
	writer, err := runstore.Create(runs, runstore.Manifest{RunID: "42", StartedAt: "2026-07-22T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Finalize("2026-07-22T10:01:00Z", "operator_stop"); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(runs, "catalog.sqlite3")
	var output bytes.Buffer
	if err := rebuild(context.Background(), runs, catalog, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "1 run(s) indexed") {
		t.Fatalf("unexpected rebuild output: %q", output.String())
	}
	output.Reset()
	if err := check(context.Background(), runs, catalog, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "check passed") {
		t.Fatalf("unexpected check output: %q", output.String())
	}
	backupPath := filepath.Join(t.TempDir(), "catalog-backup.sqlite3")
	output.Reset()
	if err := backup(catalog, backupPath, &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatal(err)
	}
	if err := check(context.Background(), runs, backupPath, &output); err != nil {
		t.Fatalf("backup is not equivalent: %v", err)
	}
}

func TestCheckReportsMissingCatalogEntry(t *testing.T) {
	runs := t.TempDir()
	writer, err := runstore.Create(runs, runstore.Manifest{RunID: "missing", StartedAt: "2026-07-22T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(runs, "catalog.sqlite3")
	// Opening an empty catalog through rebuild against another empty run parent
	// gives check a valid database whose contents intentionally differ.
	emptyRuns := t.TempDir()
	if err := rebuild(context.Background(), emptyRuns, catalog, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = check(context.Background(), runs, catalog, &output)
	if err == nil || !strings.Contains(output.String(), "run missing: missing from catalog") {
		t.Fatalf("err=%v output=%q", err, output.String())
	}
}

func TestRebuildRefusesCorruptManifest(t *testing.T) {
	runs := t.TempDir()
	directory := filepath.Join(runs, "run-bad")
	if err := os.Mkdir(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), []byte("{bad"), 0o640); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(runs, "catalog.sqlite3")
	err := rebuild(context.Background(), runs, catalog, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "refusing to replace catalog") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(catalog); !os.IsNotExist(statErr) {
		t.Fatalf("catalog unexpectedly published: %v", statErr)
	}
}

func TestBackupDoesNotOverwriteDestination(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "catalog.sqlite3")
	destination := filepath.Join(directory, "backup.sqlite3")
	emptyRuns := t.TempDir()
	if err := rebuild(context.Background(), emptyRuns, source, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := backup(source, destination, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "publish catalog backup") {
		t.Fatalf("unexpected error: %v", err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil || string(contents) != "preserve" {
		t.Fatalf("destination changed: contents=%q err=%v", contents, err)
	}
}
