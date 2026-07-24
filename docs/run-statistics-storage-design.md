# Final run statistics storage

## Goal

Preserve one cumulative statistics snapshot for every successfully finalized run.
The stored values are the final counters already shown in the live Statistics
tab; intermediate snapshots and their time evolution are intentionally not
retained.

This makes run-to-run comparison possible without replaying decoded events and
without treating the live telemetry stream as durable storage.

## Existing data flow

The run pipeline already accumulates the required values in
`runpipeline.BoardStats`:

- final event timestamp and trigger ID;
- received trigger count and estimated lost-trigger count;
- decoded payload bytes;
- per-channel trigger, timestamp, and PHA counts; and
- the T-OR count.

These counters are protected by the session lock and are exposed as a copied,
stable snapshot through `BoardStats()`. The acquisition coordinator drains and
closes the pipeline before calling `Session.Finalize`, which is the same point
where the final histogram snapshot is saved. No new event-side accumulation is
required.

Periodic telemetry converts these values into `StatisticsTelemetry`. That API
message is currently only attached to live `TelemetrySnapshot` messages and is
lost when the process exits.

## Implemented authoritative representation

An optional `statistics` object in `runstore.Manifest` contains
the elapsed run duration in milliseconds and a sorted list of per-board
statistics:

```json
{
  "statistics": {
    "elapsed_milliseconds": "15012",
    "boards": [
      {
        "chain": 0,
        "node": 0,
        "timestamp": "1876500",
        "trigger_id": "4210",
        "trigger_count": "4200",
        "lost_trigger_count": "10",
        "data_bytes": "1075200",
        "channel_trigger_counts": ["... 64 values ..."],
        "timestamp_counts": ["... 64 values ..."],
        "pha_counts": ["... 64 values ..."],
        "t_or_count": "4200"
      }
    ]
  }
}
```

All counters should use the manifest's existing JSON convention of decimal
strings for unsigned 64-bit values. This avoids precision loss in JavaScript
and stays consistent with `event_count` and `raw_batch_count`. Board order must
be deterministic: chain first, then node. Per-channel arrays must always have
exactly the DT5202 channel count so quiet channels retain their identity.

The elapsed value should be calculated from the recorded completion and start
timestamps, clamped at zero, rather than from the time of the last periodic
health update.

The field is omitted for old manifests and incomplete or aborted runs. Adding an
optional field is backward compatible with the current JSON decoder, so it does
not by itself require replacing old run evidence or changing the event-envelope
schema.

## Finalization

The storage writer boundary exposes
`SaveStatistics(runstore.RunStatistics)`. During `Session.Finalize`:

1. copy the final board counters after the pipeline has drained;
2. derive elapsed milliseconds from `completedAt` and the manifest start time;
3. save statistics to the in-memory manifest;
4. save histograms when enabled; and
5. perform the existing atomic manifest write and remove the incomplete marker.

Statistics should always be saved; unlike histograms, they are small and do not
need a run option. If statistics serialization fails, finalization must fail and
the incomplete marker must remain. The manifest remains the authoritative
record, and the values do not need a separate artifact hash because the catalog
already records the manifest hash.

Both the JSON and HDF5 event writers use the same JSON manifest, so this design
works for both storage builds without adding HDF5 datasets or duplicating
schema.

## API and all-runs view

An optional `StatisticsTelemetry final_statistics` field on `RunSummary`
provides the historical API representation.
Reusing `StatisticsTelemetry` and `BoardStatistics` keeps live and historical
counter semantics identical and avoids a second protobuf model.

`manifestSummary` should map the stored values into this field. Both
`ListRuns` and `SearchRuns` already load authoritative manifests and call
`manifestSummary`, so the frontend can display or compare final values for all
returned runs without another request per run. The stop response and
`latest_completed_run` should populate the same field from the just-finalized
pipeline so live completion and later history reads agree.

The expected payload is modest: four boards with three 64-element counter arrays
per run. The existing maximum page size of 100 is a useful bound. If the UI
later needs only aggregates, the API can add explicit run-level totals rather
than silently changing the meaning of board counters.

## SQLite catalog boundary

Do not put the complete arrays in the SQLite `runs` table or an opaque JSON
column. The catalog is rebuildable from manifests, and current list/search
responses already reopen each selected manifest.

If future requirements include server-side filters or plots across thousands of
runs, add normalized derived tables in a catalog migration:

- one row per run and board for scalar counters; and
- one row per run, board, channel, and counter family for channel values.

Only add those tables with concrete query requirements and indexes. Manifests
must remain authoritative and catalog rebuild must reproduce every row.

## Verification plan

- Run-storage unit tests verify JSON encoding, fixed channel-array lengths,
  deterministic board order, old-manifest compatibility, and omission for an
  incomplete run.
- Pipeline tests submit events, drain, finalize, and compare persisted counters
  with `BoardStats()`, including lost-trigger and channel counters.
- JSON and HDF5 session tests verify identical manifest statistics.
- Service tests verify `ListRuns`, `SearchRuns`, `StopRun`, and
  `latest_completed_run` expose the same final statistics.
- Protobuf generation, Go tests, frontend type checking, and existing run
  history tests remain part of the normal Task workflows.

## Alternatives considered

### Separate JSON or HDF5 statistics artifact

This would match histogram artifact handling but makes a small, frequently
listed metadata snapshot require another file open and download. It is useful
for large arrays, not for bounded run summary metadata.

### Store only in SQLite

This would make the derived catalog the only copy and would lose statistics
when the catalog is rebuilt. It conflicts with the existing decision that run
manifests are authoritative.

### Recompute from event files

Replay is slower, may require multiple HDF5 segments, and cannot necessarily
reproduce every online counter semantic. It is suitable for validation, not the
normal run-history path.

### Retain periodic telemetry

That stores time evolution the requirement explicitly excludes and creates much
larger retention and query concerns. A separate time-series design can be added
later if needed.
