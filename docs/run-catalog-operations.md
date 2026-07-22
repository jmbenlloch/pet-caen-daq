# Run Catalog Operations

The SQLite catalog is a searchable, rebuildable index. The authoritative run
evidence remains in each `run-*/manifest.json` and its referenced artifact
files. Catalog maintenance never replaces or repairs those manifests.

The examples below use the default `./runs/catalog.sqlite3`. Set `RUNS` and
`CATALOG` when the deployment uses different paths. Keep the live catalog on
local storage rather than a network filesystem.

## Consistency check

Stop neither acquisition nor the backend for an ordinary read-only comparison:

```sh
task catalog:check
task catalog:check RUNS=/srv/pet-caen/runs CATALOG=/srv/pet-caen/runs/catalog.sqlite3
```

The command validates bounded manifests, compares their SHA-256 digests with
catalog rows, and reports missing, stale, unavailable, or unexpectedly
available entries. It exits nonzero if it finds any discrepancy. It does not
reconcile or rewrite catalog rows. Opening the database can apply a pending
schema migration, so deploy new binaries through the normal maintenance
procedure before using them against an older catalog.

## Rebuild

Stop the backend before replacing its open database, then run:

```sh
task catalog:rebuild
```

The command builds a new database beside the target, reconciles every valid
manifest into it, reopens it for validation, and renames it over the target.
It never deletes the active catalog first. If any manifest is invalid, corrupt,
oversized, or otherwise unreadable, replacement is refused and the existing
catalog is retained. Resolve or explicitly preserve the problematic run
evidence before retrying.

After rebuilding, start the backend and run `task catalog:check`.

## Backup and restore

Stop the backend so the database is quiescent, then choose a new destination:

```sh
task catalog:backup DEST=/srv/backups/catalog-2026-07-22.sqlite3
```

The backup is written to a restrictive-permission partial file, synchronized,
opened and queried for validation, then renamed to the requested destination.
Existing backup destinations are not overwritten. The command refuses a
catalog with a WAL sidecar because copying only the main file could omit
committed data. Current catalog settings do not enable WAL; if WAL is enabled
later, checkpoint it through the deployment's SQLite maintenance procedure
before backup.

To restore, stop the backend, preserve the suspect catalog for diagnosis, copy
the validated backup into the configured catalog path using the deployment's
atomic file-management procedure, start the backend, and run
`task catalog:check`. Because manifests are authoritative, `catalog:rebuild` is
usually safer than restoring an old backup and cannot lose newer indexed runs.

## Troubleshooting

- `manifest hash differs`: the manifest changed after indexing. Investigate why
  immutable finalized evidence changed, then rebuild only after resolving it.
- `missing from catalog`: run reconciliation through a normal backend restart,
  or perform a stopped-backend rebuild.
- `catalog says available but run directory is absent`: treat this as possible
  evidence loss. Locate or restore the run directory before rebuilding.
- `invalid manifest`: retain the directory unchanged and inspect the reported
  parse, size, identity, or file-type error.
- SQLite busy/locked errors: ensure no backend process is using the catalog for
  rebuild, backup, or restore operations.

Do not authorize artifact downloads or delete evidence based only on catalog
contents. Manifest validation remains the security and evidence boundary.
