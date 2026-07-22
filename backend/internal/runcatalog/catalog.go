// Package runcatalog provides a rebuildable SQLite index of run manifests.
// Run manifests and their artifacts remain the authoritative records.
package runcatalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jmbenlloch/pet-caen-daq/backend/internal/runstore"
	_ "modernc.org/sqlite"
)

const schemaVersion = 2
const timestampFormat = "2006-01-02T15:04:05.000000000Z07:00"

type Catalog struct{ db *sql.DB }

// Open opens or creates a catalog and applies all schema migrations. The
// modernc driver is pure Go so this package remains buildable with CGO disabled.
func Open(path string) (*Catalog, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open run catalog: %w", err)
	}
	// A single writer is sufficient for run finalization and ensures connection-
	// local pragmas are consistently applied.
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure run catalog: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Catalog{db: db}, nil
}

func (c *Catalog) Close() error { return c.db.Close() }

type ValueType string

const (
	ValueInteger ValueType = "integer"
	ValueReal    ValueType = "real"
	ValueText    ValueType = "text"
	ValueBoolean ValueType = "boolean"
)

type ConfigurationValue struct {
	Layer, Parameter string
	Board, Channel   int // -1 denotes global/not applicable.
	Type             ValueType
	Integer          *int64
	Real             *float64
	Text             *string
	CanonicalUnit    string
	RawValue         string
	SourceLine       int
	Inherited        bool
}

type IndexRequest struct {
	Manifest            runstore.Manifest
	ManifestPath        string
	ManifestSHA256      string
	ConfigurationSHA256 string
	Incomplete          bool
	Configuration       []ConfigurationValue
}

// IndexManifest atomically replaces all catalog rows belonging to one run.
// It is safe to call repeatedly with the same run and manifest.
func (c *Catalog) IndexManifest(ctx context.Context, request IndexRequest) (err error) {
	if err := validateIndexRequest(request); err != nil {
		return err
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin catalog index transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	m := request.Manifest
	startedAt, err := canonicalTimestamp(m.StartedAt)
	if err != nil {
		return fmt.Errorf("index run %q start time: %w", m.RunID, err)
	}
	completedAt := ""
	if m.CompletedAt != "" {
		completedAt, err = canonicalTimestamp(m.CompletedAt)
		if err != nil {
			return fmt.Errorf("index run %q completion time: %w", m.RunID, err)
		}
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO runs(run_id, schema_version, requested_by, started_at, completed_at,
 termination_reason, event_count, raw_batch_count, capture_raw, journal_transport,
 incomplete, available, manifest_path, manifest_sha256, configuration_sha256, indexed_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
 schema_version=excluded.schema_version, requested_by=excluded.requested_by,
 started_at=excluded.started_at, completed_at=excluded.completed_at,
 termination_reason=excluded.termination_reason, event_count=excluded.event_count,
 raw_batch_count=excluded.raw_batch_count, capture_raw=excluded.capture_raw,
 journal_transport=excluded.journal_transport, incomplete=excluded.incomplete,
 available=1,
 manifest_path=excluded.manifest_path, manifest_sha256=excluded.manifest_sha256,
 configuration_sha256=excluded.configuration_sha256, indexed_at=excluded.indexed_at`,
		m.RunID, m.SchemaVersion, m.RequestedBy, startedAt, nullable(completedAt),
		nullable(m.TerminationReason), int64(m.EventCount), int64(m.RawBatchCount),
		m.CaptureRaw, m.JournalTransport, request.Incomplete, request.ManifestPath,
		request.ManifestSHA256, nullable(request.ConfigurationSHA256), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("index run %q: %w", m.RunID, err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM artifacts WHERE run_id = ?`, m.RunID); err != nil {
		return fmt.Errorf("replace artifacts for run %q: %w", m.RunID, err)
	}
	for _, artifact := range m.Artifacts {
		if artifact.SizeBytes > math.MaxInt64 {
			return fmt.Errorf("artifact %q size exceeds SQLite integer range", artifact.Name)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO artifacts(run_id, kind, name, size_bytes, sha256) VALUES(?, ?, ?, ?, ?)`, m.RunID, artifact.Kind, artifact.Name, int64(artifact.SizeBytes), artifact.SHA256); err != nil {
			return fmt.Errorf("index artifact %q: %w", artifact.Name, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM configuration_values WHERE run_id = ?`, m.RunID); err != nil {
		return fmt.Errorf("replace configuration for run %q: %w", m.RunID, err)
	}
	for _, value := range request.Configuration {
		if err = validateValue(value); err != nil {
			return fmt.Errorf("configuration %q: %w", value.Parameter, err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO configuration_values
(run_id, layer, parameter, board_index, channel_index, value_type, integer_value,
 real_value, text_value, canonical_unit, raw_value, source_line, inherited)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, m.RunID, value.Layer,
			value.Parameter, value.Board, value.Channel, value.Type, value.Integer,
			value.Real, value.Text, nullable(value.CanonicalUnit), value.RawValue,
			nullableInt(value.SourceLine), value.Inherited); err != nil {
			return fmt.Errorf("index configuration %q: %w", value.Parameter, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit run catalog: %w", err)
	}
	return nil
}

func canonicalTimestamp(value string) (string, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", err
	}
	return parsed.UTC().Format(timestampFormat), nil
}

func validateIndexRequest(r IndexRequest) error {
	if strings.TrimSpace(r.Manifest.RunID) == "" {
		return errors.New("run ID is required")
	}
	if r.Manifest.EventCount > math.MaxInt64 || r.Manifest.RawBatchCount > math.MaxInt64 {
		return errors.New("run counters exceed SQLite integer range")
	}
	if r.ManifestPath == "" || r.ManifestSHA256 == "" {
		return errors.New("manifest path and SHA-256 are required")
	}
	return nil
}

func validateValue(v ConfigurationValue) error {
	if v.Layer == "" || v.Parameter == "" {
		return errors.New("layer and parameter are required")
	}
	if v.Board < -1 || v.Channel < -1 {
		return errors.New("board and channel must be -1 or non-negative")
	}
	wantInteger, wantReal, wantText := v.Type == ValueInteger || v.Type == ValueBoolean, v.Type == ValueReal, v.Type == ValueText
	if v.Type != ValueInteger && v.Type != ValueBoolean && v.Type != ValueReal && v.Type != ValueText {
		return fmt.Errorf("unsupported value type %q", v.Type)
	}
	if (v.Integer != nil) != wantInteger || (v.Real != nil) != wantReal || (v.Text != nil) != wantText {
		return errors.New("exactly the typed value column must be populated")
	}
	if v.Type == ValueBoolean && *v.Integer != 0 && *v.Integer != 1 {
		return errors.New("boolean value must be 0 or 1")
	}
	return nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
