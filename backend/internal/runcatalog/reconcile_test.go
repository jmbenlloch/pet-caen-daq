package runcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestReconcileIndexesHashesReportsAndMarksUnavailable(t *testing.T) {
	parent := t.TempDir()
	complete, err := runstore.Create(parent, runstore.Manifest{RunID: "complete", StartedAt: "2026-07-22T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := complete.Finalize("2026-07-22T10:01:00Z", "operator_stop"); err != nil {
		t.Fatal(err)
	}
	unfinished, err := runstore.Create(parent, runstore.Manifest{RunID: "unfinished", StartedAt: "2026-07-22T11:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := unfinished.Abort(); err != nil {
		t.Fatal(err)
	}
	corruptDir := filepath.Join(parent, "run-corrupt")
	if err := os.Mkdir(corruptDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, "manifest.json"), []byte("{broken\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	catalog, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	report, err := catalog.Reconcile(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	if report.Indexed != 2 || report.Unchanged != 0 || len(report.Problems) != 1 || report.Problems[0].RunID != "corrupt" || !strings.Contains(report.Problems[0].Error, "decode manifest") {
		t.Fatalf("unexpected first report: %+v", report)
	}
	runs, err := catalog.List(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || !runs[0].Incomplete || runs[1].Incomplete {
		t.Fatalf("unexpected runs: %+v", runs)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(parent, "run-complete", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256(manifestBytes)
	if runs[1].ManifestSHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("hash = %q", runs[1].ManifestSHA256)
	}

	report, err = catalog.Reconcile(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	if report.Unchanged != 2 || report.Indexed != 0 {
		t.Fatalf("unexpected idempotent report: %+v", report)
	}

	if err := os.Rename(filepath.Join(parent, "run-complete"), filepath.Join(t.TempDir(), "moved-run")); err != nil {
		t.Fatal(err)
	}
	report, err = catalog.Reconcile(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	if report.MarkedUnavailable != 1 {
		t.Fatalf("unexpected disappearance report: %+v", report)
	}
	available, err := catalog.List(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	all, err := catalog.List(context.Background(), Query{Limit: 10, IncludeUnavailable: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 1 || len(all) != 2 || all[1].Available {
		t.Fatalf("availability not retained: available=%+v all=%+v", available, all)
	}
}

func TestReconcileRefreshesStaleAndRestoresAvailability(t *testing.T) {
	parent := t.TempDir()
	writer, err := runstore.Create(parent, runstore.Manifest{RunID: "42", StartedAt: "2026-07-22T10:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	if err := catalog.IndexManifest(context.Background(), IndexRequest{Manifest: runstore.Manifest{SchemaVersion: 1, RunID: "42", StartedAt: "old"}, ManifestPath: "old", ManifestSHA256: "stale"}); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.db.Exec(`UPDATE runs SET available = 0 WHERE run_id = '42'`); err != nil {
		t.Fatal(err)
	}
	report, err := catalog.Reconcile(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	if report.Indexed != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	runs, err := catalog.List(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || !runs[0].Available || runs[0].ManifestSHA256 == "stale" {
		t.Fatalf("stale row not refreshed: %+v", runs)
	}
}
