# HDF5 run-storage problem statement and proposed organization

Status: accepted design; decoded-event writer implementation in progress

The first implementation slice lives in `backend/internal/hdf5store` behind
the `hdf5` build tag. It creates the appendable `/events/index` and all six
kind-specific parent/child layouts described below, committing children before
the typed parent and run-wide index. It embeds the exact requested
configuration plus audit, effective-configuration, run-metadata, and finalized
manifest snapshots. Finalization flushes the file before changing its internal
`complete` attribute. The adapter is not yet selected by `runpipeline`, so
normal runs continue to use the JSON development writer during this
intermediate state.

Concrete logical dataset examples are collected in
[`hdf5-schema-examples.md`](hdf5-schema-examples.md).

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

## Observed run corpus and event examples

The JSON corpus under `pcap/runs` was inventoried before fixing the proposed
schema. It contains six manifests and six `events.jsonl` files. Five event
files are empty. All six manifests request `SPECT_TIMING`; only
`run-go-native-detector-hvon-003` contains decoded events:

| Run | Manifest event count | Event kinds observed |
| --- | ---: | --- |
| `run-go-native-001` | 0 | none |
| `run-go-native-detector-001` | 0 | none |
| `run-go-native-detector-hvon-001` | 0 | none |
| `run-go-native-detector-hvon-002` | 0 | none |
| `run-go-native-detector-hvon-003` | 87,989 | spectroscopy only |
| `run-go-native-ptrg-001` | 0 | none |

The examples below have two evidence levels. The spectroscopy example is
abbreviated from the populated real run. The other five examples are concrete
outputs implied by the byte-level golden decoder fixtures in
`backend/internal/dt5202/event_test.go`; they demonstrate the implemented data
model but were not observed in this retained run corpus. Production acceptance
still requires real-board fixtures for those modes.

### Spectroscopy: observed real-run example

```json
{
  "schema_version": 1,
  "kind": "spectroscopy",
  "sequence": "1",
  "payload": {
    "chain": 1,
    "node": 0,
    "qualifier": 51,
    "trigger_id": "0",
    "timestamp": "26865",
    "event": {
      "kind": "spectroscopy",
      "qualifier": 51,
      "spectroscopy": {
        "trigger_id": "0",
        "timestamp": "26865",
        "channel_mask": "18446744073709551615",
        "energies": [
          {
            "channel": 0,
            "low_gain": 263,
            "high_gain": 2225,
            "has_low_gain": true,
            "has_high_gain": true,
            "discriminator": true
          }
        ],
        "timings": [
          {"channel": 63, "toa": 861, "tot": 0}
        ],
        "time_reference": 428870
      }
    }
  }
}
```

The arrays are abbreviated: the original record has 64 energy entries and 13
timing entries. Qualifier 51 (`0x33`) identifies the captured combined
spectroscopy/timing form with both energy gains. The all-ones channel mask says
that all 64 energy entries are present; it does not say that all 64 sensors
caused the trigger.

The energy path and timing path must remain conceptually separate in the HDF5
schema:

```text
detector pulse
   +-- slow shaping --> peak hold --> ADC --> energy value
   +-- charge discriminator (QD) --> energy.discriminator
   +-- time discriminator (TD) --> TDC --> timing hit

enabled channel trigger signals
   --> trigger logic --> bunch trigger --> accepted spectroscopy event
```

`energy.discriminator` is packed energy bit 15. The bundled FERSlib decoder
places the corresponding channel bit in `qdmask`, so it represents assertion
of the channel's charge discriminator. It is not proof that the channel alone
caused the final trigger. Trigger acceptance also depends on trigger source,
masks, logic/majority, signal overlap, validation, and veto.

An energy entry is present because its channel is included in the event's
energy mask. It may contain a pulse, baseline, noise, or unrelated activity.
The retained run reads both gains for all enabled channels. Its relevant
settings include `GainSelect BOTH`, full channel masks, QD coarse threshold
250, board-dependent TD coarse thresholds around 178--183, `TriggerLogic
MAJ64`, and `MajorityLevel 4`.

A `timings` entry is a separate TDC observation from the time-discriminator
path. `toa` is the encoded channel-edge time relative to the hardware timing
reference convention; `tot` is time over threshold when enabled. The event
timestamp, payload `time_reference`, and per-channel `toa` have different
roles and must remain separate fields.

The retained run uses `TrefSource TLOGIC`, `TrefWindow 1.0 us`, `TrefDelay
-500 ns`, and `EnableToT 0`. Only 26,277 events (29.9%) contain at least one
timing entry; 61,712 (70.1%) contain none. An accepted spectroscopy event
therefore cannot be modeled as requiring TDC children. QD and TD thresholds and
masks differ, an edge may fall outside the reference window, and timing-path
holdoff/dead time may suppress a measurement. In general neither QD assertion
nor event acceptance implies a TDC hit.

Other configuration needed to interpret spectroscopy includes analog HG/LG
gain, shaping times, hold delay, multiplexer period, pedestal calibration,
energy zero suppression, QD/TD fine thresholds and masks, fast-shaper input,
validation/veto settings, HV bias and individual adjustment, temperature
feedback, trigger-ID mode, synchronization, topology, and firmware revision.
These dependencies reinforce the requirement to embed requested, audited, and
effective configuration in the HDF5 file.

### Timing-only: golden-fixture example, not observed in the corpus

```json
{
  "kind": "timing",
  "qualifier": 2,
  "timing": {
    "trigger_id": "7",
    "timestamp": "291",
    "time_reference": "4666",
    "hits": [
      {"channel": 3, "toa": 13398, "tot": 18}
    ]
  }
}
```

This common-start fixture shows a fine reference derived from the coarse
timestamp and one channel hit. The same event type also covers common-stop and
streaming qualifiers. Qualifier, ToA allocation, ToT availability, and timing
reference semantics must therefore remain explicit rather than inferred from
the HDF5 group name.

### Counting: golden-fixture example, not observed in the corpus

```json
{
  "kind": "counting",
  "qualifier": 132,
  "counting": {
    "trigger_id": "11",
    "timestamp": "12",
    "relative_timestamp_clock": 99,
    "channel_mask": "4",
    "counts": [
      {"channel": 2, "value": 123}
    ],
    "t_or_count": 456,
    "q_or_count": 789
  }
}
```

The channel mask is derived from the channels whose counters are present.
T-OR and Q-OR counters use reserved wire channel identifiers and are stored as
dedicated scalar fields, not ordinary channel rows.

### Waveform: golden-fixture example, not observed in the corpus

```json
{
  "kind": "waveform",
  "qualifier": 8,
  "waveform": {
    "trigger_id": "1",
    "timestamp": "2",
    "samples": [
      {"high_gain": 111, "low_gain": 222, "digital_probes": 10}
    ]
  }
}
```

Each packed word contains one high-gain sample, one low-gain sample, and four
digital-probe bits. The current decoded type does not attach a channel number
or sample interval to each sample, so storage must not invent either. The exact
waveform acquisition configuration provides the missing interpretation.

### Service: golden-fixture example, not observed in the corpus

```json
{
  "kind": "service",
  "qualifier": 47,
  "service": {
    "timestamp": "55",
    "version": 1,
    "format": 3,
    "fpga_temperature_c": -21.1625,
    "board_temperature_c": 25,
    "detector_temperature_c": 25.6,
    "hv_temperature_c": 51.2,
    "hv_voltage_v": 45.4,
    "hv_current_a": 0.001,
    "hv_on": true,
    "hv_ramping": false,
    "hv_over_current": true,
    "hv_over_voltage": false,
    "status": 17185,
    "counters": [
      {"channel": 7, "value": 88}
    ],
    "t_or_count": 99,
    "q_or_count": 111
  }
}
```

Optional telemetry requires explicit validity bits. A service version newer than the
implemented decoder retains its remaining bytes as `unknown_payload`; those
bytes belong in a flat byte pool so evidence is not discarded.

### Test: golden-fixture example, not observed in the corpus

```json
{
  "kind": "test",
  "qualifier": 255,
  "test": {
    "trigger_id": "8",
    "timestamp": "9",
    "words": [287454020, 2864434397]
  }
}
```

The decimal words are `0x11223344` and `0xaabbccdd`. Test events retain up to
four opaque 32-bit words. They should be stored losslessly without assigning a
meaning not established by the event producer.

## Field semantics and resulting schema requirements

### Physical source: `chain` and `node`

`chain` is the DT5215 TDlink number, in the protocol range 0--7. `node` is the
board address within that chain, in the protocol range 0--15. Their pair is the
protocol-native source address for an event. The present production topology
has one DT5202 at node 0 on each of chains 0--3. No additional logical-board-ID
scheme is required for schema version 1; configuration still records the
observed `(chain,node)` and firmware for each configured board.

### Payload discriminator: `qualifier`

`qualifier` is the original eight-bit DT5202 data qualifier. It determines both
the broad event kind and the precise packed payload variant:

| Value | Meaning |
| ---: | --- |
| `0x01` | spectroscopy |
| `0x02` | timing/common start |
| `0x04` | counting |
| `0x08` | waveform |
| `0x12` | timing/common stop |
| `0x22` | timing streaming |
| `0x2f` | service |
| `0xff` | test |

Spectroscopy and counting qualifiers also use modifier bits. For example,
`0x33` is the captured spectroscopy-with-timing form with both gains and its
leading-edge format modifier; `0x81` adds a relative timestamp to
spectroscopy; and `0x84` adds one to counting. The HDF5 file stores both a
query-friendly `kind` and the exact `qualifier`. A reader must not reconstruct
the latter from the former.

### QD assertion versus a timing hit

The packed spectroscopy energy word contains an explicit QD bit, exposed as
`energy.discriminator`. There is no equivalent primary TD boolean in the
decoded spectroscopy format. A row in `spectroscopy/timings` proves that an
accepted TDC measurement was emitted for that channel; the absence of a row
does not prove that the analog TD never crossed. A crossing might be masked,
outside the reference window, suppressed by holdoff/dead time, or omitted by
the event-building rules. Combined spectroscopy/timing decoding retains the
first timing measurement per channel.

Analysis may derive `has_timing_hit` by joining an energy row with its event's
timing child range. It must not store or label that derived value as
`td_discriminator_asserted`.

### T-OR, Q-OR, and service counters

T-OR is the aggregate logical OR of enabled time-discriminator signals; Q-OR
is the corresponding aggregate charge-discriminator OR. Their 24-bit counter
values use reserved wire identifiers 64 and 65. They count aggregate OR
occurrences, not the number of asserted channels in one event, and they need
not equal accepted-event or trigger-ID counts.

Service counter child rows with identifiers 0--63 are 24-bit per-channel
trigger-counter values (`ch_trg_cnt` in FERSlib). They are monitoring values,
not energy samples. Rates can be derived from differences between consecutive
service snapshots and their timestamps, with wrap/reset handling. Available
source evidence does not establish whether every firmware version reports a
value cumulative since reset or cleared on an interval, nor the precise
service emission interval. Schema version 1 therefore uses the conservative
name `counter_value`, records the service timestamp/version/format, and does
not claim a `total_since_run_start` semantic.

### Waveform samples and probes

Each waveform word contains high-gain bits 0--13, low-gain bits 14--27, and a
four-bit digital-probe snapshot in bits 28--31. A digital probe is a configured
internal logic signal such as peak HG/LG, hold, conversion start, data commit,
data valid, clock, validation window, T-OR, or Q-OR. The four-bit value is
authoritative; expanded probe booleans are derived conveniences.

The waveform event does not repeat a channel number because waveform capture
observes configured probe paths rather than carrying an ordinary per-channel
list. `AnalogProbe0/1`, `ProbeChannel0/1`, `DigitalProbe0/1`, waveform length,
and any effective waveform-source selection provide the interpretation.
Probe channel 0 selects channels 0--31 and probe channel 1 selects channels
32--63 in the current DT5202 configuration model.

Configuration is immutable during a run, so waveform parents do not need a
per-event configuration ID. The file must instead contain a complete
per-board `waveform_configuration` table. The mapping of all four packed
digital bits and the effective `WaveformSource` behavior remain to be verified
with a real waveform capture. Until then, raw probe bits and configuration are
preserved without inventing channel or signal identities.

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
      channels                # per-board/per-channel effective settings
      frontend_boards         # settings exposed at board scope by JANUS
      citiroc_chips           # actual per-chip common packed-field values
      board_trigger           # board trigger/validation/veto logic
      timing_reference        # board Tref window/delay and ToT mode
      waveform_configuration  # board probe sources/channels and length
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

### Effective configuration scope

Expanded tables are organized by the scope exposed to the operator and the
scope actually packed into hardware. This distinction matters because a
global JANUS value may be overridden per board, while fine discriminator and
gain values may be overridden per channel.

The discriminator interface and current production planner establish this
hierarchy:

- `TD_CoarseThreshold`, `QD_CoarseThreshold`, and `FastShaperInput` are global
  defaults with optional board-indexed overrides. The current planner applies
  one effective coarse TD and QD value to both Citiroc chips on a board.
- `TD_FineThreshold` and `QD_FineThreshold` are 64-element per-channel values.
  A global or board value supplies the default and a channel-indexed assignment
  can override an individual channel. The JANUS board/channel selectors expose
  these overrides in groups of eight channels, but grouping in the interface
  does not make the value group-wide.
- TD and QD masks are effective 64-bit board masks. They are displayed and
  packed as chip 0 channels 0--31 and chip 1 channels 32--63. The QD halves are
  also present in the two packed Citiroc common streams; TD masks are applied
  through FPGA registers in the current implementation.
- HG/LG gain, individual HV adjustment, calibration flags, and preamplifier
  disable are per-channel Citiroc fields.
- Shaping, power/bias controls, coarse discriminator DAC fields, and some
  trigger-output controls are physically Citiroc-chip-common. The public
  configuration may intentionally apply identical values to both chips; the
  expanded `citiroc_chips` table records the actual value for each chip.
- Majority logic, bunch source, validation, veto, trigger widths/holdoff, and
  time-reference selection are FPGA/board-level settings.

For example, a global `TD_CoarseThreshold` of 220 combined with a board-1
override of 123 gives effective coarse values 220, 123, 220, and 220 for boards
0--3. The value 123 applies to all 64 channels and both Citiroc chips on board
1. Each of those channels then retains its separate `TD_FineThreshold`. Coarse
and fine are separate hardware DAC fields, not values that analysis should
simply add together.

`configuration/effective/channels` contains query-friendly rows such as:

| board | channel | chip | chip_channel | readout_enabled | qd_enabled | td_enabled | qd_fine | td_fine | hg_gain | lg_gain | hv_adjustment |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 0 | 0 | 0 | 1 | 1 | 1 | 0 | 0 | 55 | 55 | 256 |
| 0 | 31 | 0 | 31 | 1 | 1 | 1 | 0 | 0 | 55 | 55 | 256 |
| 0 | 32 | 1 | 0 | 1 | 1 | 1 | 0 | 0 | 55 | 55 | 256 |

This records trigger-path participation for each channel without duplicating
the board trigger algorithm 64 times. `qd_enabled` and `td_enabled` come from
the effective masks. A separate `trigger_enabled` convenience column may be
added only after its exact relationship to TD masks and TLOGIC inputs is
verified; masks and packed writes remain authoritative.

`configuration/effective/frontend_boards` contains settings exposed at board
scope:

| board | qd_coarse | td_coarse | fast_shaper_input | hold_delay_ns | mux_period_ns | hit_holdoff_ns |
| ---: | ---: | ---: | --- | ---: | ---: | ---: |
| 0 | 250 | 181 | `LG-PA` | 300 | 300 | 0 |
| 1 | 250 | 183 | `LG-PA` | 300 | 300 | 0 |

`configuration/effective/citiroc_chips` contains actual chip-common values,
including the chip half of the QD mask and every power, shaping, threshold, and
trigger-output field represented by `CitirocCommon`. A fixed versioned compound
type is preferable to a JSON-only query path, while `citiroc_streams` remains
the authoritative packed 1,144-bit evidence.

`configuration/effective/board_trigger` contains:

| board | bunch_source | logic | majority | logic_width_ns | validation_mode | validation_source | veto_source | holdoff_ns |
| ---: | --- | --- | ---: | ---: | --- | --- | --- | ---: |
| 0 | `TLOGIC` | `MAJ64` | 4 | 0 | `DISABLED` | `T0-IN` | `DISABLED` | 0 |

`configuration/effective/timing_reference` contains:

| board | source | window_ns | delay_ns | tot_enabled |
| ---: | --- | ---: | ---: | ---: |
| 0 | `TLOGIC` | 1000 | -500 | 0 |

`configuration/effective/waveform_configuration` contains at least waveform
length, effective waveform source when supported, both analog-probe selections
and channels, both named digital-probe selections, and the verified mapping of
the four packed digital bits. Unknown mappings are stored as unknown, never
filled from assumptions.

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
