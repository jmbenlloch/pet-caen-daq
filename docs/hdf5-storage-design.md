# HDF5 run-storage problem statement and proposed organization

Status: proposal for discussion

## Problem statement

The production storage format must preserve every project-owned decoded DT5202
event without losing integer precision, event order, optional-field state, or
the relationship to its DT5215 descriptor. It must also be efficient for the
two expected access patterns:

1. scan one event family or selected boards/channels for analysis; and
2. reconstruct the original decoded event stream in acquisition order for
   validation and replay.

The event model is heterogeneous. A run can interleave six event kinds, and
several kinds contain a variable number of child records. Storing the Go
`Event` union as one wide HDF5 compound record would produce many irrelevant
columns, nested variable-length values, and fragile language bindings. Storing
one opaque JSON value per event would retain the current size and parsing costs
and would not make numeric columns directly useful to HDF5 analysis tools.

The file must remain understandable without the running DAQ or its SQLite run
catalog. In particular, a result is not reproducible from decoded samples
alone: it also depends on the exact requested JANUS configuration, how that
configuration was interpreted, the effective values applied to every board,
calibration data, firmware/topology evidence, and the software/schema version.

HDF5 is not itself a transaction log. A process can stop after extending one
dataset but before extending related datasets. The existing run-directory
`incomplete` marker and final manifest therefore remain necessary. A completed
file must also carry internal counts and consistency information so readers can
reject a partial or incompatible file.

## Data that must be represented

Every decoded event has a common storage envelope derived from the DT5215
stream event:

| Field | Type | Notes |
| --- | --- | --- |
| `sequence` | `uint64` | Monotonic run-wide decoded-event order, starting at 1. |
| `kind` | enum stored as `uint8` | Spectroscopy, timing, counting, waveform, service, or test. |
| `kind_row` | `uint64` | Row in the corresponding kind-specific event dataset. |
| `chain`, `node` | `uint8` | Physical source identity. |
| `qualifier` | `uint8` | Original DT5202 qualifier; do not infer it from kind. |
| `trigger_id`, `timestamp` | `uint64` | Values from the DT5215 descriptor. Service events still retain the descriptor values. |
| `payload_offset_words`, `payload_size_words` | `uint32` | Descriptor evidence useful for correlation and diagnostics. |
| `crc_error` | boolean stored as `uint8` | Original descriptor CRC flag. |

The typed payloads are:

- **Spectroscopy:** `trigger_id uint64`, `timestamp uint64`, optional
  `relative_timestamp_clock uint32`, `channel_mask uint64`, zero or more
  energies, zero or more timings, and optional `time_reference uint32`.
  Each energy has `channel uint8`, low/high gain `uint16`, explicit
  `has_low_gain`, `has_high_gain`, and `discriminator` flags. Each timing has
  `channel uint8`, `toa uint32`, and `tot uint16`.
- **Timing:** `trigger_id uint64`, `timestamp uint64`, `time_reference uint64`,
  and zero or more hits with `channel uint8`, `toa uint32`, and `tot uint16`.
- **Counting:** `trigger_id uint64`, `timestamp uint64`, optional
  `relative_timestamp_clock uint32`, `channel_mask uint64`, zero or more
  `(channel uint8, value uint32)` counts, plus `t_or_count uint32` and
  `q_or_count uint32`.
- **Waveform:** `trigger_id uint64`, `timestamp uint64`, and a variable number
  of samples. Each sample contains high gain `uint16`, low gain `uint16`, and
  digital probes `uint8`. The current model does not attach a channel number to
  a waveform sample, so the HDF5 schema must not invent one.
- **Service:** `timestamp uint64`, `version uint8`, `format uint8`; optional
  `float64` FPGA, board, HV, and detector temperatures; optional `float64` HV
  voltage and current; four HV boolean flags; optional status `uint16`; zero or
  more `(channel uint8, value uint32)` counters; `t_or_count uint32` and
  `q_or_count uint32`; and arbitrary unknown payload bytes which must survive
  decoding and storage unchanged.
- **Test:** `trigger_id uint64`, `timestamp uint64`, and zero to four `uint32`
  words.

The typed event trigger ID and timestamp currently duplicate descriptor
values. Version one should store both in the common envelope and typed table,
then validate that they agree. This preserves the current public event model
and makes each kind-specific table independently useful. A later schema may
remove the duplication only through an explicit compatibility decision.

Optional numeric values need an explicit validity representation. NaN is not a
sufficient absence marker because NaN can itself be a measured or diagnostic
value. Each event row therefore has validity bits for its optional scalars.

Raw evidence remains separate from decoded HDF5 data in version one:
`wire.raw` preserves complete batches and `transport.journal` preserves
pre-framing evidence. Embedding them in HDF5 would couple evidence recovery to
the HDF5 library and make a damaged container a single point of failure.

## Proposed file layout

The decoded artifact is `events.h5`. Dataset names and numeric enum values are
part of the schema and must be golden-tested.

```text
/
  attributes:
    format = "pet-caen-daq-hdf5"
    schema_version = 1
    writer_version
    run_id
    complete = 0|1

  events/
    index                    # common envelope, one row per event
    spectroscopy/
      events                 # scalar header + child offsets/counts
      energies               # flat Energy rows
      timings                # flat Timing rows
    timing/
      events
      hits                   # flat Timing rows
    counting/
      events
      counts                 # flat Count rows
    waveform/
      events
      samples                # flat WaveformSample rows
    service/
      events
      counters               # flat ServiceCounter rows
      unknown_payload        # flat uint8 byte pool
    test/
      events
      words                  # flat uint32 pool

  configuration/
    requested_janus          # exact UTF-8 bytes submitted by the operator
    audit_json               # canonical versioned audit snapshot
    effective/
      boards                 # board/chain/node/firmware identity
      fpga_writes            # board, ordinal, address, value
      citiroc_streams         # board, chip, 36 uint32 words, bit_count=1144
      citiroc_channels        # optional analysis-friendly expanded fields
      citiroc_common_json     # versioned lossless common-field snapshot
      hv_plans               # requested/effective scalar HV values
      hv_transactions        # board, ordinal, register, data_type, data
      pedestal_plans         # per-board scalar plan values
      pedestal_thresholds    # board/channel LG/HG effective thresholds
      pedestal_calibration   # board/channel LG/HG calibration + provenance
      inactive_settings      # board, name, reason

  run/
    manifest_json            # finalized manifest snapshot, excluding self-hash
    metadata_json            # extensible topology/software/run metadata snapshot
```

### Flat child tables instead of HDF5 variable-length types

Each parent event stores a `child_offset uint64` and `child_count uint32` for
each child collection. Child records are appended to ordinary one-dimensional,
chunked datasets. For example, a spectroscopy row points to contiguous ranges
in `energies` and `timings`.

This layout is preferred over HDF5 variable-length compound fields because it
is easier to append, compress, inspect from C, Python/h5py, Julia, MATLAB, and
ROOT-oriented conversion tools, and recover after interruption. It also avoids
allocator/reclaim behavior that differs among HDF5 bindings. Offsets and counts
must be checked for overflow and bounds by the reader.

The run-wide `events/index` preserves interleaving. Analysts interested only in
one event kind can read its table directly without scanning an all-kinds union.
Readers reconstruct order by walking the index and resolving `(kind, kind_row)`.

### HDF5 physical types

- Use fixed-width little-endian standard integer and IEEE floating-point HDF5
  types, never native Go/C layout types.
- Store booleans and enums as `uint8`, with enum mappings documented as schema
  constants. Do not rely on implementation-dependent HDF5 enum bindings.
- Use fixed-layout compound rows only for scalar event headers and small child
  records. Define every field offset explicitly and test it from an independent
  reader.
- Store arbitrary text or bytes as one-dimensional `uint8` datasets with an
  encoding/content-type attribute. Avoid variable-length strings in the core
  schema.
- All physical quantities include units in dataset/field documentation and,
  where practical, HDF5 attributes (`C`, `V`, `A`, clock ticks). Stored values
  remain the unconverted decoder values unless the field already has a physical
  type such as service telemetry.

## Complete configuration representation

“The configuration” is not a single structure. The file needs four layers:

1. **Requested source:** `configuration/requested_janus` is the exact byte
   sequence accepted by `StartRun`, including comments, ordering, spelling,
   units, global assignments, indexed overrides, and final newline state. Its
   SHA-256 is stored as an attribute. This is the primary answer to “what did
   the operator request?” and must never be regenerated from parsed values.
2. **Interpretation and audit:** `configuration/audit_json` contains the
   versioned `configaudit.Report`: validity, board firmware evidence, and every
   setting's name, optional board index, source line, owner, requested text,
   applied/inactive/rejected status, effective values, and reason. JSON is
   appropriate here because this data is bounded, heterogeneous metadata rather
   than the high-volume event stream. It should be canonicalized for stable
   hashing and accompanied by its schema version and SHA-256.
3. **Effective machine state:** tables under `configuration/effective` record
   what the DAQ planned and verified for each physical board. At minimum this
   includes ordered FPGA register writes; both complete 1,144-bit Citiroc
   streams; the expanded channel/common Citiroc values or a lossless versioned
   snapshot; HV scalar plans and ordered peripheral transactions; pedestal
   mode, thresholds, effective per-channel values, and protected-flash
   calibration/provenance; plus inactive and unresolved settings. The packed
   Citiroc stream and register/transaction tables are the authoritative
   hardware-facing representation; expanded tables are analysis conveniences.
4. **Execution identity:** topology mapping (board index, chain, node and any
   discovered identifiers), DT5202 and DT5215 firmware revisions, DAQ software
   revision/dirty state, configuration parser/audit version, HDF5 writer
   version, and relevant runtime choices such as raw capture, transport journal,
   backpressure policy, and histogram settings. These belong in bounded run
   metadata and should also be reflected in the external manifest.

The current manifest already preserves the requested document, effective
`ConfigurationPlan` values, and audit report. It does not yet contain all of the
topology and software identity described by the architecture. That gap should
be closed in the project-owned run metadata before the HDF5 adapter is written,
so JSON and HDF5 do not develop different notions of a run.

Storing only the requested JANUS file is insufficient: defaults, overrides,
firmware-dependent packing, calibration-derived writes, and inactive settings
would be ambiguous. Storing only register writes is also insufficient: it loses
operator intent, units, inactive requests, and the provenance needed to explain
why a value was applied. Both views, plus the audit connecting them, are
required.

## Chunking, compression, and append protocol

All event and child datasets are one-dimensional, unlimited, and chunked.
Initial chunk targets should be selected by bytes rather than a universal row
count: approximately 1--4 MiB of uncompressed data per chunk, then tuned using
the retained real run. Use no compression as the correctness baseline and
benchmark a broadly supported built-in filter such as deflate before choosing
a production default. A filter requiring a third-party HDF5 plugin should not
be the only readable production representation.

Append one logical event in this order:

1. append its child ranges;
2. append its kind-specific parent row referencing those ranges;
3. append the common index row last.

The index is the commit point visible to readers. On periodic flush, write and
flush child datasets before parent datasets and the index. Maintain committed
length attributes or a small checkpoint dataset. Recovery may truncate
unreferenced tails to the last internally consistent checkpoint, but must never
mark the run complete automatically.

At successful finalization, flush all datasets, validate index references and
counts, write final run/manifest metadata, set the internal `complete` marker,
close the file, calculate its external size and SHA-256, atomically update
`manifest.json`, and only then remove the run-directory `incomplete` marker.
The external manifest remains authoritative for artifact discovery and hashes;
the internal snapshot makes a copied HDF5 file intelligible by itself.

## Compatibility and validation

Schema version 1 is append-only: new optional datasets or metadata may be added,
but existing field meanings, enum numbers, units, and signedness do not change.
An incompatible change creates schema version 2 and a converter. Readers reject
unknown major schema versions rather than guessing.

Acceptance should compare JSONL-to-HDF5 conversion and direct HDF5 writing from
the same golden stream. Tests must cover every event kind, empty and maximum
child collections, all optional fields present/absent, full-width integers,
NaN/Inf telemetry, unknown service bytes, interrupted appends at every stage,
configuration byte identity, effective-plan equality, and independent reading
with at least one non-Go HDF5 client. Performance tests should use the retained
675 MB real JSONL run and report throughput, compression ratio, peak memory,
flush latency, and representative analysis query latency.

## Decisions still requiring measured input

- Which analysis clients are mandatory (h5py, MATLAB, ROOT, Julia, or others)?
- Are queries normally event-ordered, board-ordered, channel-ordered, or
  waveform-heavy?
- Expected peak event rate, maximum run duration/file size, and acceptable
  writer CPU and flush latency.
- Whether production writes HDF5 only or HDF5 plus JSONL. The proposal favors
  HDF5 as the sole decoded production artifact while retaining the converter,
  raw capture option, and lightweight JSONL development writer.
- Whether SWMR live reading is required. It should not be enabled until a real
  live-analysis requirement and binding support are demonstrated.
