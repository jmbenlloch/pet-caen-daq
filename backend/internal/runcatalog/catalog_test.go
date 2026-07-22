package runcatalog

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
)

func TestOpenMigratesAndIndexesManifestAtomically(t *testing.T) {
	catalog, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	integer, real, text := int64(220), 45.4, "SPECT_TIMING"
	request := IndexRequest{
		Manifest: runstore.Manifest{
			SchemaVersion: 1, RunID: "run-42", RequestedBy: "operator",
			StartedAt: "2026-07-22T10:00:00Z", CompletedAt: "2026-07-22T10:01:00Z",
			TerminationReason: "operator_stop", EventCount: 12, RawBatchCount: 3,
			CaptureRaw: true, JournalTransport: true,
			Artifacts: []runstore.Artifact{{Kind: "decoded_events", Name: "events.jsonl", SizeBytes: 123, SHA256: "events-hash"}},
		},
		ManifestPath: "/runs/run-run-42/manifest.json", ManifestSHA256: "manifest-hash",
		ConfigurationSHA256: "configuration-hash",
		Configuration: []ConfigurationValue{
			{Layer: "resolved", Parameter: "TD_CoarseThreshold", Board: 2, Channel: -1, Type: ValueInteger, Integer: &integer, RawValue: "220"},
			{Layer: "resolved", Parameter: "HV_Vbias", Board: -1, Channel: -1, Type: ValueReal, Real: &real, CanonicalUnit: "V", RawValue: "45.4"},
			{Layer: "requested", Parameter: "AcquisitionMode", Board: -1, Channel: -1, Type: ValueText, Text: &text, RawValue: text, SourceLine: 10},
		},
	}
	if err := catalog.IndexManifest(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	board := 2
	runs, err := catalog.List(context.Background(), Query{Limit: 10, Configuration: []Predicate{
		{Layer: "resolved", Parameter: "TD_CoarseThreshold", Board: &board, IntegerMinimum: pointer(int64(200))},
		{Layer: "requested", Parameter: "AcquisitionMode", TextEqual: pointer("SPECT_TIMING")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].RunID != "run-42" || !runs[0].CaptureRaw {
		t.Fatalf("unexpected indexed runs: %+v", runs)
	}

	var version int
	if err := catalog.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %d, want %d", version, schemaVersion)
	}
	var artifacts, values int
	if err := catalog.db.QueryRow(`SELECT COUNT(*) FROM artifacts WHERE run_id = ?`, "run-42").Scan(&artifacts); err != nil {
		t.Fatal(err)
	}
	if err := catalog.db.QueryRow(`SELECT COUNT(*) FROM configuration_values WHERE run_id = ?`, "run-42").Scan(&values); err != nil {
		t.Fatal(err)
	}
	if artifacts != 1 || values != 3 {
		t.Fatalf("artifact/value counts = %d/%d, want 1/3", artifacts, values)
	}
}

func TestIndexManifestRollsBackInvalidReplacement(t *testing.T) {
	catalog, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()

	base := IndexRequest{
		Manifest:     runstore.Manifest{SchemaVersion: 1, RunID: "stable", StartedAt: "2026-07-22T10:00:00Z", EventCount: 7},
		ManifestPath: "/runs/run-stable/manifest.json", ManifestSHA256: "first",
	}
	if err := catalog.IndexManifest(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	invalid := base
	invalid.Manifest.EventCount = 99
	invalid.ManifestSHA256 = "second"
	invalid.Configuration = []ConfigurationValue{{Layer: "resolved", Parameter: "bad", Board: -1, Channel: -1, Type: ValueInteger, RawValue: "missing typed value"}}
	if err := catalog.IndexManifest(context.Background(), invalid); err == nil {
		t.Fatal("invalid replacement succeeded")
	}
	runs, err := catalog.List(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].EventCount != 7 || runs[0].ManifestSHA256 != "first" {
		t.Fatalf("transaction did not roll back: %+v", runs)
	}
}

func TestListValidatesPredicatesAndLimits(t *testing.T) {
	catalog, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	if _, err := catalog.List(context.Background(), Query{Limit: 1001}); err == nil {
		t.Fatal("oversized limit succeeded")
	}
	if _, err := catalog.List(context.Background(), Query{Configuration: []Predicate{{Parameter: "HV_Vbias"}}}); err == nil {
		t.Fatal("predicate without comparison succeeded")
	}
}

func pointer[T any](value T) *T { return &value }
