# SQLite Run Catalog Implementation Plan

## Status

Approved for implementation on 2026-07-22.

## Purpose

The backend currently keeps each run's configuration and metadata in its run
directory. This is a good durable evidence format, but scanning JSON manifests
does not provide efficient typed searches such as:

- runs acquired with a bias voltage in a specified range;
- runs using a particular acquisition mode;
- runs with a threshold override on a particular board or channel; and
- runs completed in a date range with a particular termination reason.

This project will add a SQLite catalog for these queries. The catalog is a
rebuildable index: run directories and their manifests remain the authoritative
record. Event data, raw wire data, and transport journals remain files.

The governing architectural decision is recorded in
`docs/adr/0008-sqlite-run-catalog.md`.

## Goals

1. Search run metadata and configuration values without scanning every run.
2. Preserve parameter types, units, scope, inheritance, and provenance.
3. Distinguish requested configuration from the effective configuration.
4. Make indexing idempotent and recoverable from the run directories.
5. Keep acquisition evidence valid when the catalog is missing or unhealthy.
6. Preserve static, CGO-disabled builds, including the Windows build.
7. Expose bounded, typed searches through the backend API and web interface.

## Non-goals

- Moving events, wire captures, transport journals, or other large artifacts
  into the database.
- Making SQLite the only copy of configuration or run metadata.
- Exposing arbitrary SQL to API clients.
- Building a multi-host catalog service in the first implementation.
- Supporting arbitrary Boolean query expressions before a concrete need exists.

## Target architecture

Each run stays self-contained:

```text
runs/
├── catalog.sqlite3
├── run-<id>/
│   ├── manifest.json
│   ├── events.jsonl
│   ├── wire.raw
│   └── transport.journal
└── ...
```

The backend writes and finalizes a run through the existing run-storage path.
Only after the authoritative manifest is finalized does it transactionally
upsert that run into the catalog. Startup reconciliation compares manifests
with catalog entries and repairs missing or stale entries. An explicit rebuild
can create a new catalog entirely from manifests.

The catalog implementation should live behind an internal interface, for
example:

```go
type Catalog interface {
	IndexManifest(ctx context.Context, manifest runstore.Manifest) error
	GetRun(ctx context.Context, runID string) (RunRecord, error)
	ListRuns(ctx context.Context, query RunQuery) ([]RunRecord, error)
	SearchRuns(ctx context.Context, query ConfigurationQuery) ([]RunRecord, error)
	Reconcile(ctx context.Context, runParent string) (ReconcileReport, error)
	Close() error
}
```

SQL-specific types and statements remain private. This boundary permits a
future PostgreSQL implementation without coupling acquisition and service code
to SQLite.

## Data model

### Runs

The `runs` table contains searchable summary metadata and the identity of the
manifest from which it was indexed. Suggested initial columns are:

```sql
CREATE TABLE runs (
    run_id                 TEXT PRIMARY KEY,
    schema_version         INTEGER NOT NULL,
    requested_by           TEXT,
    started_at             TEXT NOT NULL,
    completed_at           TEXT,
    termination_reason     TEXT,
    event_count            INTEGER NOT NULL,
    raw_batch_count        INTEGER NOT NULL,
    capture_raw            INTEGER NOT NULL,
    journal_transport      INTEGER NOT NULL,
    incomplete             INTEGER NOT NULL,
    available              INTEGER NOT NULL DEFAULT 1,
    manifest_path          TEXT NOT NULL,
    manifest_sha256        TEXT NOT NULL,
    configuration_sha256   TEXT,
    indexed_at             TEXT NOT NULL
) STRICT;
```

Timestamps use one canonical UTC representation. Boolean values are constrained
to zero or one in the migration. `manifest_path` is catalog information, never
an authorization mechanism for artifact download.

### Artifacts

```sql
CREATE TABLE artifacts (
    run_id       TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    name         TEXT NOT NULL,
    size_bytes   INTEGER NOT NULL,
    sha256       TEXT NOT NULL,
    PRIMARY KEY (run_id, name)
) STRICT;
```

Only artifact metadata is indexed. Artifact content stays in the run directory.

### Configuration values

```sql
CREATE TABLE configuration_values (
    run_id          TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
    layer           TEXT NOT NULL,
    parameter       TEXT NOT NULL,
    board_index     INTEGER NOT NULL DEFAULT -1,
    channel_index   INTEGER NOT NULL DEFAULT -1,
    value_type      TEXT NOT NULL,
    integer_value   INTEGER,
    real_value      REAL,
    text_value      TEXT,
    canonical_unit  TEXT,
    raw_value       TEXT NOT NULL,
    source_line     INTEGER,
    inherited       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (run_id, layer, parameter, board_index, channel_index)
) STRICT;
```

`-1` denotes global or not-applicable scope, avoiding nullable-key ambiguity.
Migrations add check constraints so exactly the appropriate typed value is set.
The initial indexes cover time-ordered run listing, typed value lookup, and
board/channel scope:

```sql
CREATE INDEX runs_started_at
ON runs(started_at DESC, run_id);

CREATE INDEX config_integer_search
ON configuration_values(parameter, integer_value, run_id)
WHERE integer_value IS NOT NULL;

CREATE INDEX config_real_search
ON configuration_values(parameter, real_value, run_id)
WHERE real_value IS NOT NULL;

CREATE INDEX config_text_search
ON configuration_values(parameter, text_value, run_id)
WHERE text_value IS NOT NULL;

CREATE INDEX config_scope_search
ON configuration_values(parameter, board_index, channel_index, run_id);
```

Indexes should be validated with realistic query plans before additional
indexes are introduced.

## Configuration semantics

The catalog indexes separate layers:

- `requested`: assignments explicitly present in the submitted JANUS document;
- `resolved`: values applicable to each board or channel after defaults,
  inheritance, and overrides; and
- `register`: optional low-level register plan values, deferred until operators
  need register-level search.

Operator searches default to `resolved`. Requested values remain available for
provenance and reproducing intent.

Every indexed value retains the parameter name, original text, parsed type,
canonical value and unit, board/channel scope, source line when available, and
whether the value was inherited. Known physical quantities are normalized to a
stable canonical unit. Exact quantities use scaled integers where their range
fits SQLite's signed 64-bit integer, for example nanoseconds, microvolts, and
nanoamperes. Enumerations use canonical text; masks and counters use integers.
Floating-point equality is not offered as a query operation.

Normalization rules must be versioned. Manifests should eventually include a
resolved parameter snapshot, requested-configuration hash, normalization
version, and software revision. That snapshot ensures future catalog rebuilds
do not reinterpret old runs with newer parser behavior. Until then, legacy
manifests may be parsed with an explicitly recorded parser version.

## Write, consistency, and failure model

The finalization sequence is:

1. Finish writing and synchronize run artifacts.
2. Calculate artifact sizes and hashes.
3. Atomically write the finalized manifest.
4. Remove the incomplete marker according to existing storage semantics.
5. In one database transaction, replace the run, artifact, and configuration
   rows derived from that manifest.

Manifest finalization precedes catalog indexing. Consequently, a database error
cannot invalidate or remove finalized evidence. It instead produces a
structured diagnostic and degraded catalog-health status. Reconciliation can
retry the index operation.

Indexing is idempotent. If `manifest_sha256` is unchanged, no replacement is
needed. If the hash changes, all rows for that run are replaced within one
transaction so readers never see mixed manifest versions.

Incomplete runs may be indexed to support current-run visibility, but their
state must be clearly marked and refreshed after completion or abort.

## Migrations and SQLite configuration

Schema changes use ordered, transactional migrations and an explicit schema
version. Tests cover migration from an empty database, every released version,
and rollback after an injected migration failure.

The selected driver must be pure Go and support `CGO_ENABLED=0`. On every
connection the catalog enables foreign keys and configures a bounded busy
timeout. Connection counts stay small because the application has one writer.

WAL mode may be used for concurrent readers after tests validate checkpoint,
backup, and shutdown behavior. The live SQLite database must be stored on local
storage, not accessed by multiple hosts through a network filesystem.

## Reconciliation and rebuild

At startup the backend:

1. Opens the catalog and applies migrations.
2. Scans run directories using the existing bounded, symlink-safe reader.
3. Validates and hashes each manifest.
4. Inserts manifests absent from the catalog.
5. Replaces entries whose recorded hash is stale.
6. Reports invalid, corrupt, or oversized manifests without indexing them.
7. Marks entries whose run directories disappeared as unavailable.

Missing directories are not silently erased from the catalog because that can
conceal evidence loss. Explicit maintenance policy can later remove confirmed
orphans.

Two operational commands are added:

- `catalog:check` compares the catalog with manifests without changing either;
- `catalog:rebuild` builds and validates a temporary database, then atomically
  replaces the old catalog.

Rebuild must never delete the active database before its replacement has been
created and validated.

Operator procedures for consistency checking, stopped-backend atomic rebuild,
backup, restoration, and failure diagnosis are documented in
`docs/run-catalog-operations.md`.

## Backend and API integration

The backend receives an explicit catalog path, with a default adjacent to the
run directories. Startup opens, migrates, and reconciles the catalog. Run
finalization upserts the final manifest. Existing artifact authorization keeps
using validated manifest contents rather than database-provided paths.

Existing list behavior should be preserved while its implementation moves to
the catalog. A separate typed `SearchRuns` RPC accepts:

- start/end timestamps and termination reason;
- minimum event count and other bounded run metadata filters;
- one or more typed configuration predicates;
- requested or resolved layer;
- optional board and channel scope; and
- limit and opaque pagination token.

Initial configuration comparisons are integer equality/range, real range,
text/enum equality, and Boolean equality. Predicates are combined with AND.
Input parameter names, value types, ranges, limits, and page tokens are
validated before SQL is built. The API never accepts raw SQL.

Pagination uses a stable newest-first key composed of start time and run ID.
Queries have bounded result counts, execution deadlines, and cancellation.

## User interface

The first search interface provides:

- a parameter selector sourced from known JANUS definitions;
- operators appropriate for the selected parameter type;
- value and unit controls;
- global, board, and channel scope controls;
- requested/resolved layer selection;
- multiple AND filters; and
- date, status, and run metadata filters.

Search results explain why each run matched, including parameter, scope, layer,
value, and canonical unit. The interface also handles no-results and catalog
unavailable states without affecting access to authoritative run evidence.

## Security and operational controls

- Reject symlinks and unsafe paths using the existing run-store policy.
- Do not use catalog paths to authorize file access.
- Bound manifest size, filter count, result count, and query duration.
- Avoid logging complete configurations at normal log levels.
- Export catalog availability, schema version, last successful reconciliation,
  stale/missing entry counts, and reconciliation errors.
- Provide a consistent SQLite backup operation and document WAL sidecar handling
  if WAL mode is enabled.
- Treat database corruption as degraded search, not corrupted acquisition data.

## Verification strategy

### Unit tests

- Empty and incremental schema migrations, including rollback.
- Type and unit normalization for every parameter category.
- Global, board, and channel inheritance and sparse overrides.
- Masks, signed-range boundaries, and values too large for SQLite integers.
- Unknown and invalid parameters.
- Idempotent upsert and transactional replacement.
- Typed multi-predicate SQL generation and pagination stability.
- Context cancellation and busy/locked database handling.

### Storage integration tests

- Finalized and incomplete runs are represented correctly.
- Catalog failure leaves all run artifacts and manifests intact.
- Reconciliation inserts missing and refreshes stale runs.
- Invalid, oversized, and symlinked manifests are rejected and reported.
- Missing run directories are marked unavailable.
- Rebuild produces equivalent catalog contents and search results.
- Readers continue safely while a run is finalized.

### Service and browser tests

- Typed RPC filters return the expected runs and structured validation errors.
- Search results agree with authoritative manifests.
- Artifact download remains manifest-authorized.
- Restart preserves and reconciles search results.
- Browser searches cover enum equality, numeric ranges, board overrides, no
  results, and unavailable-catalog behavior.

All milestones finish with repository formatting, static checks, unit tests,
integration tests appropriate to that milestone, `git diff --check`, and the
project's full CI task before release.

## Milestones

### Milestone 1: decision and compatibility spike

- Record the architecture decision and this plan.
- Select and prove a pure-Go SQLite driver under Linux and the CGO-disabled
  Windows build.
- Establish catalog package boundaries and error taxonomy.

Exit criterion: the driver builds on supported targets and the design has no
unresolved storage-authority or failure-semantics questions.

### Milestone 2: schema and typed normalization

- Implement the catalog package, migrations, schema, and SQLite settings.
- Implement requested and resolved configuration flattening.
- Add canonical type/unit normalization and focused unit tests.

Exit criterion: an in-memory or temporary catalog can migrate and index a
representative manifest with deterministic typed rows.

### Milestone 3: indexing and reconciliation

- Implement transactional, idempotent manifest indexing.
- Add startup check/reconciliation and orphan visibility.
- Add safe check and atomic rebuild commands.
- Add catalog health telemetry.

Exit criterion: deleting the catalog and rebuilding it yields equivalent rows
and queries, while injected database failures leave run evidence unchanged.

### Milestone 4: lifecycle integration

- Integrate incomplete-run and finalized-run indexing into backend lifecycle.
- Add explicit catalog configuration.
- Move run listing to catalog-backed metadata without changing API behavior.

Exit criterion: acquisition, completion, abort, restart, and download integration
tests pass with and without a temporarily available catalog.

### Milestone 5: typed search API

- Add protobuf definitions and generated clients.
- Implement predicate validation, indexed SQL, pagination, and cancellation.
- Add service integration tests and query-plan checks.

Exit criterion: all supported metadata and configuration searches work through
the public API with stable pagination and bounded resource use.

### Milestone 6: web search and operations

- Add search controls and match explanations to the web application.
- Add end-to-end browser coverage.
- Document rebuild, health checks, backup, restoration, and troubleshooting.
- Run the complete CI and release verification matrix.

Exit criterion: operators can find and inspect runs by typed configuration, and
the catalog can be backed up, restored, checked, and rebuilt using documented
procedures.

## Definition of done

- Existing artifact formats remain unchanged.
- Catalog deletion and deterministic rebuilding are supported and tested.
- Global, board, and channel configuration values are searchable.
- Requested and resolved values are distinguishable and auditable.
- Database failure cannot cause loss or invalidation of run evidence.
- Static `CGO_ENABLED=0` builds remain supported.
- Search is typed, bounded, paginated, and does not expose SQL.
- Migration, reconciliation, recovery, backup, and query semantics are
  documented.
- Full CI and end-to-end tests pass.
