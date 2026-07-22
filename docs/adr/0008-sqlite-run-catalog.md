# ADR 0008: Use SQLite as a Rebuildable Run Catalog

- Status: Accepted
- Date: 2026-07-22
- Decision owners: PET CAEN DAQ maintainers

## Context

Every acquisition produces a self-contained run directory. Its manifest records
run metadata, requested configuration, effective configuration plans, artifact
metadata, and completion state. This layout is durable, inspectable, portable,
and suitable for preserving acquisition evidence.

Operators also need cross-run queries over typed configuration and metadata,
including numeric ranges and board/channel-specific effective values. Repeatedly
opening and parsing every manifest is inefficient, makes pagination difficult,
and does not provide stable indexes. Storing only an opaque JSON document in a
database would still make scope, units, types, and common query plans awkward.

The backend is currently the single writer, metadata volume is small relative to
event data, and the DAQ should operate without a separately administered
database service. Supported builds include CGO-disabled targets.

## Decision

Add a local SQLite database as a searchable catalog of run metadata, artifact
metadata, and typed configuration values.

Run directories and finalized manifests remain authoritative. SQLite is a
derived index and must be reproducible from those manifests. Event streams,
wire captures, transport journals, and other large artifacts remain files.

Configuration values are stored in normalized rows with:

- requested or resolved layer;
- parameter name and original value;
- typed canonical value and unit;
- global, board, or channel scope;
- source provenance and inheritance state; and
- the manifest hash and normalization version used to produce the row.

Catalog updates occur in transactions after the authoritative manifest is
finalized. Startup reconciliation repairs missing and stale rows. Explicit check
and atomic rebuild operations are provided. A catalog failure degrades search
and emits health information but does not invalidate a completed run.

The implementation is isolated behind an internal catalog interface. A pure-Go
SQLite driver is required so `CGO_ENABLED=0` builds remain supported. The live
database is placed on local storage; it is not shared by multiple hosts over a
network filesystem.

Public search uses validated, typed predicates with bounded pagination and
execution time. Arbitrary SQL is not exposed. Artifact access continues to be
authorized and resolved through validated manifests, never through a path read
from the catalog.

## Consequences

### Positive

- Indexed equality and range searches become fast and predictable.
- SQLite adds no external database service to deploy or operate.
- Typed rows make units, scope, requested intent, and effective values explicit.
- Acquisition evidence remains independently readable and portable.
- Corrupt or deleted catalogs can be rebuilt without rewriting run evidence.
- The catalog interface leaves a migration path to PostgreSQL if requirements
  become multi-host or write-concurrent.

### Negative

- The backend must maintain migrations, reconciliation, and database health.
- Run metadata exists in both authoritative manifests and a derived catalog, so
  temporary divergence is possible and must be detected.
- Configuration normalization rules require versioning and extensive tests.
- SQLite backup and WAL checkpoint behavior require operational documentation.
- Normalized configuration rows consume more space than storing one JSON value,
  though the volume remains small compared with run artifacts.

### Risks and mitigations

- **Catalog differs from a manifest:** store the manifest hash and reconcile at
  startup and on demand.
- **Catalog write fails during finalization:** finalize the manifest first,
  report degraded health, and retry through reconciliation.
- **Parser behavior changes:** version normalization and add resolved snapshots
  to future manifests.
- **Unsafe artifact path from SQL:** never authorize downloads using catalog
  paths; retain manifest-based validation.
- **SQLite locking:** use one writer, a small connection pool, a bounded busy
  timeout, cancellation, and realistic concurrency tests.
- **Database corruption:** treat the catalog as disposable and provide an atomic
  rebuild procedure.
- **Network filesystem behavior:** require local storage for the live database.

## Alternatives considered

### Continue scanning JSON manifests

This preserves simplicity but does not provide efficient typed cross-run search,
stable pagination, or reusable indexes. It remains the recovery path, not the
primary query path.

### Store manifests only as SQLite JSON

SQLite JSON functions can support ad hoc queries, but frequently searched
values still need expression indexes and careful type/unit handling. Scoped and
inherited parameters are clearer and safer as typed relational rows. The full
manifest may still be retained as evidence in its run directory.

### PostgreSQL

PostgreSQL is preferable for several DAQ writers, remote concurrent clients,
central aggregation, database-level access control, replication, or high
availability. Those requirements do not currently justify operating another
service. The catalog interface preserves this future option.

### DuckDB

DuckDB is strong for offline analytics but is not the best fit for the backend's
small transactional updates and operational run listing.

### MongoDB or Elasticsearch/OpenSearch

These systems add operational complexity without improving the core typed,
relational range-query use case enough to justify it.

### HDF5 or database BLOB storage

HDF5 is suitable for scientific arrays, not an operational cross-run metadata
catalog. Putting large evidence artifacts in database BLOBs would reduce their
portability and complicate streaming, hashing, recovery, and backups.

## Follow-up

Implementation and verification milestones are defined in
`docs/sqlite-run-catalog-implementation-plan.md`. Revisit this decision if the
system gains multiple concurrent writers, requires a remotely shared catalog,
or needs database-level replication and access control.
