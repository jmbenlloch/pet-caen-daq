package runcatalog

import (
	"context"
	"database/sql"
	"fmt"
)

func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read catalog schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("run catalog schema version %d is newer than supported version %d", version, schemaVersion)
	}
	if version == schemaVersion {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin catalog migration: %w", err)
	}
	defer tx.Rollback()
	if version < 1 {
		if _, err := tx.ExecContext(ctx, schemaV1); err != nil {
			return fmt.Errorf("apply run catalog schema version 1: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return fmt.Errorf("record catalog schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit catalog migration: %w", err)
	}
	return nil
}

const schemaV1 = `
CREATE TABLE runs (
 run_id TEXT PRIMARY KEY,
 schema_version INTEGER NOT NULL,
 requested_by TEXT NOT NULL DEFAULT '',
 started_at TEXT NOT NULL,
 completed_at TEXT,
 termination_reason TEXT,
 event_count INTEGER NOT NULL CHECK(event_count >= 0),
 raw_batch_count INTEGER NOT NULL CHECK(raw_batch_count >= 0),
 capture_raw INTEGER NOT NULL CHECK(capture_raw IN (0,1)),
 journal_transport INTEGER NOT NULL CHECK(journal_transport IN (0,1)),
 incomplete INTEGER NOT NULL CHECK(incomplete IN (0,1)),
 manifest_path TEXT NOT NULL,
 manifest_sha256 TEXT NOT NULL,
 configuration_sha256 TEXT,
 indexed_at TEXT NOT NULL
) STRICT;
CREATE TABLE artifacts (
 run_id TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
 kind TEXT NOT NULL,
 name TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
 sha256 TEXT NOT NULL,
 PRIMARY KEY(run_id, name)
) STRICT;
CREATE TABLE configuration_values (
 run_id TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
 layer TEXT NOT NULL,
 parameter TEXT NOT NULL,
 board_index INTEGER NOT NULL DEFAULT -1 CHECK(board_index >= -1),
 channel_index INTEGER NOT NULL DEFAULT -1 CHECK(channel_index >= -1),
 value_type TEXT NOT NULL CHECK(value_type IN ('integer','real','text','boolean')),
 integer_value INTEGER,
 real_value REAL,
 text_value TEXT,
 canonical_unit TEXT,
 raw_value TEXT NOT NULL,
 source_line INTEGER,
 inherited INTEGER NOT NULL DEFAULT 0 CHECK(inherited IN (0,1)),
 CHECK((value_type IN ('integer','boolean') AND integer_value IS NOT NULL AND real_value IS NULL AND text_value IS NULL)
    OR (value_type = 'real' AND integer_value IS NULL AND real_value IS NOT NULL AND text_value IS NULL)
    OR (value_type = 'text' AND integer_value IS NULL AND real_value IS NULL AND text_value IS NOT NULL)),
 PRIMARY KEY(run_id, layer, parameter, board_index, channel_index)
) STRICT;
CREATE INDEX runs_started_at ON runs(started_at DESC, run_id DESC);
CREATE INDEX config_integer_search ON configuration_values(parameter, integer_value, run_id) WHERE integer_value IS NOT NULL;
CREATE INDEX config_real_search ON configuration_values(parameter, real_value, run_id) WHERE real_value IS NOT NULL;
CREATE INDEX config_text_search ON configuration_values(parameter, text_value, run_id) WHERE text_value IS NOT NULL;
CREATE INDEX config_scope_search ON configuration_values(parameter, board_index, channel_index, run_id);
`
