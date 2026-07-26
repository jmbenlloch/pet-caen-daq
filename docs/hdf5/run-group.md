# HDF5 `/run` group

This document describes every dataset in the `/run` group of a decoded-event
HDF5 segment. It uses `pcap/run-54/run_54.0000.h5` as a concrete,
capture-verified example.

This document does not describe the datasets under `/configuration`. The
finalized `/run/manifest_json` necessarily contains copies of configuration
material because it is a self-contained run manifest; those fields are
identified here, but their hardware-setting semantics belong in a separate
configuration document.

## Purpose

The `/run` group makes an event segment self-describing. It records:

- the artifact format and logical run identifier;
- the selected HDF5 compression policy;
- an immutable creation-time identity snapshot; and
- a final manifest snapshot with lifecycle results and run statistics.

Run 54 has this layout:

```text
/run
|-- format
|-- run_id
|-- compression
|-- metadata_json
`-- manifest_json
```

All five datasets are one-dimensional arrays of little-endian `uint8` bytes.
They are not native HDF5 string datatypes. The text datasets contain UTF-8, and
the JSON datasets contain UTF-8 encoded JSON.

## Dataset summary and complete top-level values

| Dataset | HDF5 shape | Run-54 decoded value |
| --- | ---: | --- |
| `/run/format` | `(17,)` | `pet-caen-daq-hdf5` |
| `/run/run_id` | `(2,)` | `54` |
| `/run/compression` | `(27,)` | `blosc-lz4-level4-bitshuffle` |
| `/run/metadata_json` | `(2098,)` | Creation-time JSON shown below |
| `/run/manifest_json` | `(161581,)`, extensible | Finalized JSON described below |

The HDF5 extent is a byte count for these datasets, not a row count in the
usual tabular sense.

## `/run/format`

The complete run-54 value is:

```text
pet-caen-daq-hdf5
```

This identifies the file as a decoded-event artifact written by this DAQ. It
distinguishes the file from other HDF5 artifacts, for example the histogram
format identifier `pet-caen-daq-histograms`.

The format name and the root `schema_version` have different purposes:

- `/run/format` identifies the artifact family.
- Root `schema_version` identifies the physical layout version within that
  family.

## `/run/run_id`

The complete run-54 value is:

```text
54
```

The run ID is text, not an integer. Every decoded-event segment belonging to
the same run carries the same value. For example, both `run_48.0000.h5` and
`run_48.0001.h5` contain `48`.

The segment number is not encoded in this value. Segment identity is stored in
the root `segment_index` attribute.

## `/run/compression`

The complete run-54 value is:

```text
blosc-lz4-level4-bitshuffle
```

It means that the chunked event datasets use:

- HDF5 filter ID 32001, Blosc;
- LZ4 compression;
- compression level 4; and
- bit-shuffle preprocessing.

The other supported value is `none`. Compression changes the physical storage
and performance characteristics, not the logical decoded values.

The same value is repeated in
`metadata_json.execution_identity.storage.compression` so that the complete
execution identity remains self-contained.

## `/run/metadata_json`

`metadata_json` is written when an HDF5 segment is created. It is deliberately
bounded and immutable. It answers:

> What run, topology, software build, storage configuration, and runtime
> settings were believed to be active when this segment was opened?

It does not contain completion time, termination reason, final event count, or
end-of-run statistics.

The complete decoded value in run 54, formatted for readability, is:

```json
{
  "schema_version": 1,
  "run_id": "54",
  "requested_by": "operator",
  "started_at": "2026-07-25T16:47:09.355762467Z",
  "capture_raw": false,
  "journal_transport": false,
  "configuration_identity": {
    "parser_version": 1,
    "audit_schema_version": 1,
    "requested_configuration_sha256": "c717f330add4f3af908ae2ef0dfbdd415e32fc940a0a0c9c1696272e259bd6a1",
    "effective_configuration_sha256": "84f1f8b9da6eb49d9a231b4268a40fc57e092814543e123002bcc7c0064eaf06",
    "configuration_audit_sha256": "52deeefba4d6769cbb13fe0e0b0c5fec6789a058fb12b3c8d4bd6d1eb8fb048c"
  },
  "execution_identity": {
    "topology": {
      "concentrator": {
        "control_address": "172.16.0.11:9760",
        "stream_address": "172.16.0.11:9000",
        "product_id": null,
        "firmware_revision": null,
        "identity_evidence": "unknown-not-queried",
        "firmware_revision_evidence": "unknown-not-queried"
      },
      "boards": [
        {
          "board": 0,
          "chain": 0,
          "node": 0,
          "product_id": 64883,
          "firmware_revision": 2703427336,
          "acquisition_state": 9,
          "identity_evidence": "hardware-register-read",
          "firmware_evidence": "hardware-register-read"
        },
        {
          "board": 1,
          "chain": 1,
          "node": 0,
          "product_id": 64138,
          "firmware_revision": 2703427336,
          "acquisition_state": 9,
          "identity_evidence": "hardware-register-read",
          "firmware_evidence": "hardware-register-read"
        },
        {
          "board": 2,
          "chain": 2,
          "node": 0,
          "product_id": 64885,
          "firmware_revision": 2703427336,
          "acquisition_state": 9,
          "identity_evidence": "hardware-register-read",
          "firmware_evidence": "hardware-register-read"
        },
        {
          "board": 3,
          "chain": 3,
          "node": 0,
          "product_id": 64884,
          "firmware_revision": 2703427336,
          "acquisition_state": 9,
          "identity_evidence": "hardware-register-read",
          "firmware_evidence": "hardware-register-read"
        }
      ]
    },
    "software": {
      "revision": "ac8c4380fb1c461ce8796b1efcc77e8b90807f04",
      "modified": true,
      "go_version": "go1.25.0"
    },
    "storage": {
      "format": "hdf5",
      "writer_version": 1,
      "compression": "blosc-lz4-level4-bitshuffle"
    },
    "runtime": {
      "pipeline_capacity": 512,
      "backpressure_policy": "block",
      "capture_raw": false,
      "journal_transport": false,
      "persist_histograms": true,
      "energy_histogram_bins": 4096,
      "energy_histogram_channels": 8192,
      "toa_histogram_bins": 4096,
      "toa_histogram_rebin": 1,
      "toa_histogram_min_ns": 0,
      "tot_histogram_bins": 512,
      "hdf5_segment_size_bytes": 524288000
    }
  }
}
```

### Basic identity fields

| Field | Meaning |
| --- | --- |
| `schema_version` | JSON run-manifest schema version, not the HDF5 root schema attribute |
| `run_id` | Logical run identifier |
| `requested_by` | Actor that initiated the run |
| `started_at` | UTC wall-clock start time in RFC 3339 form |
| `capture_raw` | Whether byte-exact wire capture was enabled |
| `journal_transport` | Whether socket-read transport journaling was enabled |

`started_at` is an absolute UTC time. It is not related to the event
`timestamp` fields, which use relative 8 ns hardware ticks.

### Configuration identity hashes

The three SHA-256 values identify distinct representations:

- `requested_configuration_sha256` identifies the original operator/JANUS
  configuration text.
- `effective_configuration_sha256` identifies the normalized per-board plans.
- `configuration_audit_sha256` identifies the audit report explaining how the
  requested settings were handled.

The hashes make it possible to verify copies without embedding the large
configuration bodies in this creation-time snapshot.

### Topology identity

The concentrator identity records the control and stream endpoints. Its
product and firmware values are null in this creation-time snapshot and carry
the evidence label `unknown-not-queried`. Null is deliberate: the backend
records uncertainty instead of inventing a value.

Each board record gives:

- the logical board number;
- physical chain and node coordinates;
- product and firmware register values;
- acquisition state at discovery; and
- the evidence source for identity and firmware.

Run 54 used four boards on chains 0 through 3, all at node 0. Their identity
and firmware values came from hardware register reads.

### Software identity

| Field | Run-54 value | Meaning |
| --- | --- | --- |
| `revision` | `ac8c4380fb1c461ce8796b1efcc77e8b90807f04` | DAQ Git revision |
| `modified` | `true` | The source tree was dirty, so the revision alone is not a byte-exact source identity |
| `go_version` | `go1.25.0` | Go toolchain used to build the DAQ |

### Storage and runtime identity

Run 54 used HDF5 writer version 1 and Blosc/LZ4 compression. Its decoded-event
pipeline capacity was 512, with a `block` backpressure policy: a full bounded
queue blocks producers instead of silently discarding decoded events.

Histogram persistence was enabled with:

- 4,096 PHA bins over 8,192 ADC codes;
- 4,096 ToA bins;
- ToA rebin factor 1;
- ToA minimum 0 ns; and
- 512 ToT bins.

The segment rotation target was 524,288,000 bytes, exactly 500 MiB.

## `/run/manifest_json`

`manifest_json` is appended when a segment is finalized, before the root
`complete` marker is changed to 1. It contains the final run snapshot known at
that point.

The complete run-54 JSON is 161,581 bytes. It is much larger than
`metadata_json` because it includes:

- every field from the bounded execution identity;
- lifecycle completion values;
- full requested, effective, and audited configuration representations;
- final concentrator information;
- final run and per-board statistics, including three 64-element arrays per
  board.

Pasting the 14,822-character requested configuration, the complete normalized
configuration plans, the audit report, and all 768 per-channel statistic
values inline would obscure the `/run` schema and duplicate the future
configuration document. They are nevertheless part of the dataset's complete
value and are read and printed losslessly by the Python examples below. The
following sections enumerate every top-level field and give all scalar
run-54 values without truncation.

### Complete top-level field inventory

Run 54 contains these top-level keys:

```text
schema_version
run_id
requested_by
started_at
completed_at
termination_reason
event_count
capture_raw
journal_transport
persist_histograms
hdf5_segment_size_bytes
requested_configuration
effective_configuration
configuration_audit
configuration_identity
execution_identity
concentrator
statistics
```

There is no embedded `artifacts` key in this segment; the reason is explained
below.

### Lifecycle and acquisition values

The complete scalar lifecycle values are:

```json
{
  "schema_version": 1,
  "run_id": "54",
  "requested_by": "operator",
  "started_at": "2026-07-25T16:47:09.355762467Z",
  "completed_at": "2026-07-25T16:47:24.712016409Z",
  "termination_reason": "preset_time",
  "event_count": "799949",
  "capture_raw": false,
  "journal_transport": false,
  "persist_histograms": true,
  "hdf5_segment_size_bytes": 524288000
}
```

`event_count` is encoded as a JSON string to preserve the complete unsigned
64-bit range in JavaScript consumers. It equals the number of rows in
`/events/index` for this one-segment run.

`termination_reason = "preset_time"` means acquisition stopped because the
configured duration expired, not because of an operator stop or transport
failure.

The timestamp difference between `started_at` and `completed_at` agrees with
the stored `statistics.elapsed_milliseconds = "15356"` to normal wall-clock
rounding.

### Embedded configuration values

The manifest contains:

| Field | Run-54 representation |
| --- | --- |
| `requested_configuration` | One 14,822-character JANUS-format text string |
| `effective_configuration` | Array of four normalized board plans |
| `configuration_audit` | Audit object with schema version 1, `valid = true`, 103 setting results, and four board results |

The requested text begins:

```text
# ******************************************************************************************
# params File generated by Python
# ******************************************************************************************
# ------------------------------------------------------------------------------------------
# Connect
# ------------------------------------------------------------------------------------------
Open[0] usb:172.16.0.11:tdl:0:0
Open[1] usb:172.16.0.11:tdl:1:0
```

Each effective plan contains the keys:

```text
Board
Citiroc
Deferred
HV
Inactive
Pedestal
Writes
```

The exact identities of the three embedded values are:

```text
requested configuration:
c717f330add4f3af908ae2ef0dfbdd415e32fc940a0a0c9c1696272e259bd6a1

effective configuration:
84f1f8b9da6eb49d9a231b4268a40fc57e092814543e123002bcc7c0064eaf06

configuration audit:
52deeefba4d6769cbb13fe0e0b0c5fec6789a058fb12b3c8d4bd6d1eb8fb048c
```

These embedded fields are complete copies, not references to
`/configuration`. Their setting-level interpretation will be documented with
the `/configuration` group.

### Execution identity

`manifest_json.execution_identity` has the same run-54 value shown in full
under `metadata_json`: topology, software, storage, and runtime identity. It is
repeated so that the finalized manifest is independently meaningful.

### Final concentrator information

The finalized manifest adds:

```json
{
  "software_revision": "2025.11.24.1",
  "fpga_revision": "25.11.24.01-2-2",
  "product_id": 66643
}
```

This later information is distinct from the null creation-time concentrator
identity. The final manifest records what became available during the run
lifecycle without retroactively changing immutable `metadata_json`.

### Final run statistics

The top-level statistics object contains:

```json
{
  "elapsed_milliseconds": "15356",
  "boards": [
    "... four board-statistics objects ..."
  ]
}
```

Each board object has:

| Field | Meaning |
| --- | --- |
| `chain`, `node` | Hardware source coordinates |
| `timestamp` | Latest event timestamp in 8 ns hardware ticks |
| `trigger_id` | Latest observed trigger identifier |
| `trigger_count` | Count of received, decoded non-service events |
| `lost_trigger_count` | Estimate derived from trigger-ID gaps |
| `data_bytes` | Decoded payload bytes attributed to the board |
| `channel_trigger_counts[64]` | Accumulated per-channel fast-discriminator trigger counts |
| `timestamp_counts[64]` | Per-channel decoded timing-hit counts |
| `pha_counts[64]` | Per-channel decoded pulse-height counts |
| `t_or_count` | Accumulated T-OR count |

All large counter values are JSON strings to preserve their `uint64` range.

The complete scalar values for the four boards are:

| Chain/node | Timestamp | Trigger ID | Trigger count | Lost-trigger estimate | Data bytes | T-OR |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 0/0 | 1,879,711,836 | 18,705,388 | 394,212 | 18,311,177 | 104,402,356 | 0 |
| 1/0 | 1,880,823,863 | 3,832,711 | 68,284 | 3,764,428 | 18,247,696 | 1,601,548 |
| 2/0 | 1,880,861,357 | 83,230,725 | 295,380 | 82,935,346 | 78,242,248 | 7,413,452 |
| 3/0 | 1,880,791,535 | 565,843 | 42,013 | 523,831 | 11,499,416 | 479,173 |

The four hardware timestamps are approximately 15.05 seconds in their 8 ns
clock domains. They are not Unix timestamps.

The lost-trigger values are estimates based on trigger-ID discontinuities.
They are not proven missing HDF5 rows and should not automatically be
interpreted as transport packet loss.

The full 64-element arrays are accessible without loss through:

```python
board["channel_trigger_counts"]
board["timestamp_counts"]
board["pha_counts"]
```

For example, all three arrays can be printed channel by channel with the code
below instead of relying on a visually error-prone 768-number transcription.

### Why `artifacts` is absent

The embedded run-54 manifest has no `artifacts` field because of finalization
order:

1. The HDF5 segment embeds `manifest_json`.
2. The segment is flushed, marked complete, and closed.
3. Only then can its final byte size and SHA-256 hash be calculated.
4. The external `manifest.json` is updated with the artifact list.

Embedding the segment's own final size and hash inside itself would change the
file and invalidate those values. The external run manifest is therefore
authoritative for artifact filenames, sizes, and hashes.

## `metadata_json` versus `manifest_json`

| Property | `metadata_json` | `manifest_json` |
| --- | --- | --- |
| Written | At segment creation | At segment finalization |
| Run-54 size | 2,098 bytes | 161,581 bytes |
| Immutable bounded identity | Yes | Included |
| Completion time | No | Yes |
| Termination reason | No | Yes |
| Final event count | No | Yes |
| Final statistics | No | Yes |
| Full configuration bodies | No, hashes only | Yes |
| Available in an interrupted file | Normally yes | May be empty |

This distinction preserves useful provenance even if acquisition is
interrupted before finalization.

## Related root attributes

These attributes are outside `/run`, but are required to interpret a segment:

| Root attribute | Run-54 value | Meaning |
| --- | ---: | --- |
| `schema_version` | 2 | Physical decoded-event HDF5 schema |
| `complete` | 1 | Segment finalized successfully |
| `segment_index` | 0 | Zero-based segment number |
| `first_event_sequence` | 1 | First global event sequence in this segment |

A reader should not treat `manifest_json` or event extents as finalized unless
`complete == 1`.

For a rotated run, every segment repeats the run identity. `segment_index` and
`first_event_sequence` distinguish the segments and preserve global event
ordering.

## Reading `/run` with Python

### Read and pretty-print every complete value

```python
from pathlib import Path
import json

import h5py


def read_utf8_bytes(h5: h5py.File, path: str) -> str:
    dataset = h5[path]
    if dataset.dtype.kind != "u" or dataset.dtype.itemsize != 1:
        raise TypeError(f"{path} is not a uint8 byte dataset")
    if dataset.ndim != 1:
        raise ValueError(f"{path} has shape {dataset.shape}, expected 1-D")
    return bytes(dataset[:]).decode("utf-8")


path = Path("pcap/run-54/run_54.0000.h5")

with h5py.File(path, "r") as h5:
    format_name = read_utf8_bytes(h5, "/run/format")
    run_id = read_utf8_bytes(h5, "/run/run_id")
    compression = read_utf8_bytes(h5, "/run/compression")
    metadata_text = read_utf8_bytes(h5, "/run/metadata_json")
    manifest_text = read_utf8_bytes(h5, "/run/manifest_json")

    metadata = json.loads(metadata_text)
    manifest = json.loads(manifest_text)

    print("format:", format_name)
    print("run_id:", run_id)
    print("compression:", compression)
    print("metadata_json:")
    print(json.dumps(metadata, indent=2, sort_keys=True))
    print("manifest_json:")
    print(json.dumps(manifest, indent=2, sort_keys=True))
```

This prints the complete 161,581-byte manifest value, including the original
configuration text, all four effective plans, the complete audit, and every
per-channel statistic. It does not truncate arrays or replace nested objects
with summaries.

The `/run` datasets themselves are uncompressed, so reading only this group
does not require the Blosc filter plugin. Reading compressed event tables from
the same run does require filter support.

### Validate internal consistency

```python
import json
from datetime import datetime

import h5py


def read_text(h5, path):
    return bytes(h5[path][:]).decode("utf-8")


with h5py.File("pcap/run-54/run_54.0000.h5", "r") as h5:
    if int(h5.attrs["complete"]) != 1:
        raise ValueError("segment is not finalized")

    run_id = read_text(h5, "/run/run_id")
    metadata = json.loads(read_text(h5, "/run/metadata_json"))
    manifest = json.loads(read_text(h5, "/run/manifest_json"))

    if metadata["run_id"] != run_id:
        raise ValueError("metadata run ID does not match /run/run_id")
    if manifest["run_id"] != run_id:
        raise ValueError("manifest run ID does not match /run/run_id")
    if manifest["configuration_identity"] != metadata["configuration_identity"]:
        raise ValueError("configuration identity changed within the segment")
    if manifest["execution_identity"] != metadata["execution_identity"]:
        raise ValueError("execution identity changed within the segment")

    event_rows = int(h5["/events/index"].shape[0])
    manifest_events = int(manifest["event_count"])
    if event_rows != manifest_events:
        raise ValueError(
            f"event count mismatch: index={event_rows}, manifest={manifest_events}"
        )

    started = datetime.fromisoformat(
        manifest["started_at"].replace("Z", "+00:00")
    )
    completed = datetime.fromisoformat(
        manifest["completed_at"].replace("Z", "+00:00")
    )
    if completed < started:
        raise ValueError("completion precedes start")
```

The event-count equality in this example assumes a one-segment run. For a
rotated run, compare the final run event count with the sum of
`/events/index` rows across all ordered segments.

### Print every per-channel statistic

```python
import json

import h5py


with h5py.File("pcap/run-54/run_54.0000.h5", "r") as h5:
    manifest = json.loads(
        bytes(h5["/run/manifest_json"][:]).decode("utf-8")
    )

for board in manifest["statistics"]["boards"]:
    chain = board["chain"]
    node = board["node"]
    channel_triggers = list(map(int, board["channel_trigger_counts"]))
    timing_hits = list(map(int, board["timestamp_counts"]))
    pha_hits = list(map(int, board["pha_counts"]))

    if not (len(channel_triggers) == len(timing_hits) == len(pha_hits) == 64):
        raise ValueError(f"chain {chain}/{node} does not have 64 channels")

    for channel in range(64):
        print(
            f"{chain=}, {node=}, {channel=}, "
            f"channel_triggers={channel_triggers[channel]}, "
            f"timing_hits={timing_hits[channel]}, "
            f"pha_hits={pha_hits[channel]}"
        )
```

### Extract the complete JSON without `h5py`

The HDF5 command-line tools can write the raw `uint8` dataset bytes directly:

```sh
h5dump -d /run/metadata_json \
  -o run54-metadata.json -b LE \
  pcap/run-54/run_54.0000.h5

h5dump -d /run/manifest_json \
  -o run54-manifest.json -b LE \
  pcap/run-54/run_54.0000.h5

jq . run54-metadata.json
jq . run54-manifest.json
```

This is preferable to reading the default `h5dump` numeric display, where the
UTF-8 JSON appears as a long list of decimal byte values.
