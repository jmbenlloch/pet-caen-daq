# HDF5 timing events

This document describes timing-mode data under `/events/timing` in the
decoded-event HDF5 format. Real examples come from:

```text
pcap/run-2/run_2.0000.h5
```

Run 2 is the retained corpus's substantial real timing capture. It contains
more than 1.6 million timing events and almost 20 million hits.

## Run-2 artifacts

| File | Size | Purpose |
| --- | ---: | --- |
| `run_2.0000.h5` | 190,424,594 bytes | Decoded events |
| `run_2.histograms.h5` | 381,300 bytes | Run-wide ToA and ToT histograms |
| `manifest.json` | 315,460 bytes | Run metadata, configuration, and statistics |

The run used a 15-second preset and completed with termination reason
`preset_time`.

## Structure

Timing storage uses a parent table and a variable-length hit table:

```text
/events/index
    | kind = 2
    | kind_row
    v
/events/timing/events
    | hit_offset, hit_count
    v
/events/timing/hits
```

The parent contains event identity and a half-open slice into the child table.
Each child is one timing measurement. One channel can produce multiple
children in the same event.

## Dataset sizes

| Dataset | Rows |
| --- | ---: |
| `/events/index` | 1,608,127 |
| `/events/timing/events` | 1,608,067 |
| `/events/timing/hits` | 19,704,749 |
| `/events/service/events` | 60 |

The total is consistent:

```text
1,608,067 timing + 60 service = 1,608,127 total events
```

The file is compressed, which is why almost 20 million compound hit rows fit
in a 190 MB artifact.

## Global index

`/events/index` supplies source information not repeated in the timing
parent:

| Field | Meaning |
| --- | --- |
| `sequence` | Run-global persisted-event sequence |
| `kind` | `2` for timing |
| `chain`, `node` | Physical board address |
| `qualifier` | Authoritative timing wire layout |
| `kind_row` | Row in `/events/timing/events` |
| `trigger_id`, `timestamp` | Identity copied from the event descriptor |
| `payload_offset_words` | Payload position in the transport batch |
| `payload_size_words` | Payload size in 32-bit words |
| `crc_error` | Transport CRC-error flag |

For run 2, every inspected timing event has:

```text
qualifier = 34 = 0x22
```

The qualifier must be used to interpret hit words. Do not infer the layout
solely from the HDF5 group name.

## Parent dataset: `/events/timing/events`

This is an extensible one-dimensional compound dataset:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `trigger_id` | `uint64` | Board trigger identifier |
| `timestamp` | `uint64` | Coarse hardware timestamp in 8 ns ticks |
| `time_reference` | `uint64` | Coarse timestamp plus four fine bits |
| `hit_offset` | `uint64` | First row in `/events/timing/hits` |
| `hit_count` | `uint32` | Number of child hits |

### Trigger ID

The trigger ID is stored without conversion. Although run 2 requested
`TrgIdMode = TRIGGER_CNT`, all inspected timing parents and final per-board
statistics report trigger ID zero.

For this capture, trigger ID does not distinguish or order events. Use:

- `index.sequence` for persisted global order;
- `chain` and `node` for board identity; and
- wrap-aware hardware timestamps for board-relative time.

Do not group events from different boards using trigger ID alone.

### Coarse timestamp

The descriptor timestamp has a clock period of 8 ns:

```python
coarse_seconds = int(parent["timestamp"]) * 8e-9
```

It is a board-relative hardware counter, not Unix time.

### Combined time reference

Every timing payload begins with a reference word. Its low four bits refine
the descriptor timestamp:

```python
time_reference = (timestamp << 4) | fine
fine = time_reference & 0x0f
```

Because one coarse tick is 8 ns and it is divided into 16 sub-ticks:

```text
time-reference LSB = 8 ns / 16 = 0.5 ns
```

Conversions are:

```python
reference_seconds = int(parent["time_reference"]) * 0.5e-9
reference_ns = int(parent["time_reference"]) * 0.5
fine_ns = (int(parent["time_reference"]) & 0x0f) * 0.5
```

For parent 3:

```text
timestamp      = 45
time_reference = 725

45 × 16 + 5 = 725
```

Therefore:

```text
coarse time    = 45 × 8 ns = 360 ns
fine component = 5 × 0.5 ns = 2.5 ns
reference time = 362.5 ns
```

### Child slice

The event's hits occupy:

```python
start = int(parent["hit_offset"])
stop = start + int(parent["hit_count"])
hits = h5["/events/timing/hits"][start:stop]
```

Parent slices are contiguous and collectively cover the hit table.

## Child dataset: `/events/timing/hits`

This is another extensible one-dimensional compound dataset:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `parent_row` | `uint64` | Owner in `/events/timing/events` |
| `channel` | `uint8` | Physical channel, 0 through 63 |
| `toa` | `uint32` | Raw time-of-arrival field |
| `tot` | `uint16` | Raw time-over-threshold field or placeholder |

The exact bit widths and meaning of `toa` and `tot` depend on the index
qualifier.

## Timing qualifiers

The decoder supports three standalone timing qualifiers:

| Qualifier | Decoder name | Hit layout |
| ---: | --- | --- |
| `0x02` | timing/common start | 16-bit ToA plus 9-bit ToT |
| `0x12` | common stop | 16-bit ToA plus 9-bit ToT |
| `0x22` | streaming | 25-bit ToA; no ToT |

For `0x02` and `0x12`, a hit word is decoded as:

```python
channel = word >> 25
toa = word & 0xffff
tot = (word >> 16) & 0x01ff
```

For `0x22`, it is:

```python
channel = word >> 25
toa = word & 0x01ffffff
tot = 0
```

Run 2 uses only `0x22`. Consequently:

- its ToA field can occupy 25 bits;
- its HDF5 `tot` value is zero for every observed hit; and
- those zeros are placeholders, not measured zero-duration pulses.

## Requested mode versus actual qualifier

The requested configuration says:

```text
AcquisitionMode     = TIMING_CSTART
EnableToT           = 0
EnableListZeroSuppr = 0
TrefSource          = TLOGIC
TrefWindow          = 1.0 us
TrefDelay           = -500 ns
```

The actual wire qualifier is `0x22`, the wide-ToA/no-ToT layout. With ToT
disabled, the firmware emitted the 25-bit ToA representation. This is why the
stored layout is described by `0x22` even though the requested acquisition
mode is named `TIMING_CSTART`.

The wire qualifier is authoritative. A reader should inspect it per index row
rather than applying one interpretation to every dataset named `timing`.

## First eight real parents

| Parent | Trigger | Timestamp | Time reference | Hit slice | Hits |
| ---: | ---: | ---: | ---: | --- | ---: |
| 0 | 0 | `72,057,594,037,927,876` | `1,152,921,504,606,846,021` | `[0:25]` | 25 |
| 1 | 0 | 0 | 5 | `[25:215]` | 190 |
| 2 | 0 | `72,057,594,037,927,876` | `1,152,921,504,606,846,021` | `[215:469]` | 254 |
| 3 | 0 | 45 | 725 | `[469:723]` | 254 |
| 4 | 0 | 145 | 2,325 | `[723:977]` | 254 |
| 5 | 0 | 595 | 9,525 | `[977:977]` | 0 |
| 6 | 0 | 613 | 9,813 | `[977:1033]` | 56 |
| 7 | 0 | 738 | 11,813 | `[1033:1074]` | 41 |

Parent 5 is a real zero-hit event. It owns an empty half-open slice without
breaking child-table contiguity.

## Initial 56-bit timestamp wrap

The large timestamp in parents 0 and 2 is:

```text
72,057,594,037,927,876
= 0x00ffffffffffffc4
= 2^56 - 60
```

The following board timestamp is zero. This is consistent with a 56-bit
hardware counter beginning near its maximum and wrapping:

```text
... 2^56 - 60 -> 0 -> 45 -> 145 ...
```

Naively converting the first value gives hundreds of millions of seconds and
is not a meaningful elapsed run time. Timestamp-difference code must handle
wrap:

```python
TIMESTAMP_BITS = 56
TIMESTAMP_MODULUS = 1 << TIMESTAMP_BITS

delta_ticks = (
    int(current_timestamp) - int(previous_timestamp)
) % TIMESTAMP_MODULUS
delta_seconds = delta_ticks * 8e-9
```

Wrap handling must be performed independently for each board.

The combined reference uses the same coarse counter shifted left by four.
For the initial row:

```text
(0x00ffffffffffffc4 << 4) | 5
= 0x0ffffffffffffc45
= 1,152,921,504,606,846,021
```

## Complete linked example: parent row 0

Index row 0 contains:

```text
sequence             = 1
kind                 = 2 (timing)
chain                = 0
node                 = 0
qualifier            = 0x22
kind_row             = 0
trigger_id           = 0
timestamp            = 0x00ffffffffffffc4
payload_offset_words = 0
payload_size_words   = 26
crc_error            = 0
```

Parent row 0 contains:

```text
trigger_id     = 0
timestamp      = 0x00ffffffffffffc4
time_reference = 0x0ffffffffffffc45
hit_offset     = 0
hit_count      = 25
```

The payload relation is:

```text
1 reference word + 25 hit words = 26 words
```

The first hits are:

| Child row | Channel | ToA | ToA × 0.5 ns | ToT |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 0 | 1,007 | 503.5 ns | 0, invalid |
| 1 | 58 | 1,008 | 504.0 ns | 0, invalid |
| 2 | 3 | 1,015 | 507.5 ns | 0, invalid |
| 3 | 57 | 1,009 | 504.5 ns | 0, invalid |
| 4 | 5 | 1,015 | 507.5 ns | 0, invalid |
| 5 | 53 | 1,011 | 505.5 ns | 0, invalid |
| 6 | 16 | 1,016 | 508.0 ns | 0, invalid |
| 7 | 55 | 1,012 | 506.0 ns | 0, invalid |
| 8 | 2 | 1,018 | 509.0 ns | 0, invalid |
| 9 | 56 | 1,012 | 506.0 ns | 0, invalid |

The 0.5 ns scale is supported by the persisted ToA histogram metadata.
However, whether a ToA coordinate should be added to, subtracted from, or
otherwise related to `time_reference` depends on the firmware mode. Do not
invent an absolute-hit-time formula from the storage schema alone.

## Zero-hit example

Index row 5 and parent row 5 represent:

```text
qualifier          = 0x22
payload_size_words = 1
timestamp          = 595
time_reference     = 9,525
hit_offset         = 977
hit_count          = 0
```

Only the required reference word is present:

```text
1 reference word + 0 hit words = 1 payload word
```

Because `EnableListZeroSuppr = 0`, retaining zero-hit events is expected.
An empty slice is a valid event, not evidence of damaged HDF5 structure.

## Multiple hits from one channel

The child table is a list of timing edges, not a one-row-per-channel table.
Parent 1 includes:

```text
channel 29 -> ToA 66
channel 29 -> ToA 184

channel 63 -> ToA 78
channel 63 -> ToA 156
```

These are separate valid hits. A dictionary comprehension that keeps only one
row per channel loses information:

```python
# Incorrect for timing events with repeated channels:
one_hit_per_channel = {
    int(hit["channel"]): hit
    for hit in hits
}
```

Group into lists instead:

```python
from collections import defaultdict


hits_by_channel = defaultdict(list)
for hit in hits:
    hits_by_channel[int(hit["channel"])].append(hit)
```

Unlike counting payloads, standalone timing payloads do not canonicalize
repeated channels with last-value-wins behavior.

## Hit ordering

Child rows preserve decoded wire order. The first event begins:

```text
channels: 0, 58, 3, 57, 5, 53, 16, 55, 2, 56, ...
ToA:      1007, 1008, 1015, 1009, 1015, 1011, ...
```

They are not sorted by channel or ToA. Retain original order for protocol
diagnostics. Sort a copy only when required by a particular analysis.

## Payload-size invariant

For all supported standalone timing layouts, one reference word precedes the
hit words:

```text
payload_size_words = 1 + hit_count
```

Real examples:

| Parent | Payload words | Hit count |
| ---: | ---: | ---: |
| 0 | 26 | 25 |
| 1 | 191 | 190 |
| 2 | 255 | 254 |
| 5 | 1 | 0 |
| 6 | 57 | 56 |
| 7 | 42 | 41 |

The repeatedly observed 254-hit parents have a 255-word payload: one
reference plus 254 hits.

## Per-board event counts

The run manifest reports:

| Board | Timing parents | Final timestamp | Final trigger ID |
| --- | ---: | ---: | ---: |
| chain 0, node 0 | 6,544 | 1,730,606,328 | 0 |
| chain 1, node 0 | 660,324 | 1,881,176,166 | 0 |
| chain 2, node 0 | 542,675 | 1,881,144,277 | 0 |
| chain 3, node 0 | 398,524 | 1,881,138,418 | 0 |

The event counts sum exactly:

```text
6,544 + 660,324 + 542,675 + 398,524 = 1,608,067
```

The large imbalance between chain 0 and the other boards is present in the
real received data. The HDF5 structure does not explain its detector or
hardware cause.

## ToA histogram artifact

Run 2 contains populated run-wide ToA histograms:

```text
/histograms/toa/spectra: 256 rows
/histograms/toa/bins:    1,048,576 rows
```

There is one spectrum per physical board and channel:

```text
4 boards × 64 channels = 256 spectra
```

Every spectrum has:

```text
minimum   = 0
bin_width = 0.5 ns
bin_count = 4,096
```

The nominal represented range is 0 through 2,048 ns. Values at or beyond the
upper edge contribute to `overflow`.

For chain 0, node 0, channel 0:

```text
entries   = 7,266
underflow = 0
overflow  = 0
minimum   = 0
bin_width = 0.5 ns
bin_count = 4,096
```

`entries` is the number of histogrammed hits, not the number of parent events.

## ToT histogram caveat

The file also contains:

```text
/histograms/tot/spectra: 256 rows
/histograms/tot/bins:    131,072 rows
```

Each spectrum has 512 bins of width 1. Nevertheless, run 2 used qualifier
`0x22`, and its `tot` fields are decoder-inserted zero placeholders.

The ToT histogram entries therefore count those placeholder zeros. The
existence of populated ToT histogram metadata does not make run-2 ToT a
physical measurement.

For run 2:

```text
ToA: meaningful raw timing coordinate
ToT: unavailable; stored zero is invalid
```

A generic histogram consumer must use event qualifiers or acquisition
configuration when deciding whether ToT products are meaningful.

## Reading one event with Python

Install:

```sh
python -m pip install h5py hdf5plugin
```

Import `hdf5plugin` before opening compressed datasets:

```python
from pathlib import Path

import h5py
import hdf5plugin  # noqa: F401; registers compression filters


path = Path("pcap/run-2/run_2.0000.h5")

with h5py.File(path, "r") as h5:
    index = h5["/events/index"]
    parents = h5["/events/timing/events"]
    hit_table = h5["/events/timing/hits"]

    source = index[0]
    if int(source["kind"]) != 2:
        raise ValueError("selected index row is not timing")

    parent_number = int(source["kind_row"])
    parent = parents[parent_number]

    start = int(parent["hit_offset"])
    stop = start + int(parent["hit_count"])
    hits = hit_table[start:stop]

    qualifier = int(source["qualifier"])
    tot_valid = qualifier in (0x02, 0x12)

    print({
        "sequence": int(source["sequence"]),
        "chain": int(source["chain"]),
        "node": int(source["node"]),
        "qualifier": f"0x{qualifier:02x}",
        "timestamp_ticks": int(parent["timestamp"]),
        "time_reference": int(parent["time_reference"]),
        "hit_count": len(hits),
    })

    for hit in hits:
        print({
            "channel": int(hit["channel"]),
            "toa": int(hit["toa"]),
            "toa_ns": int(hit["toa"]) * 0.5,
            "tot": int(hit["tot"]) if tot_valid else None,
        })
```

Using `None` for invalid ToT prevents placeholder zero from being mistaken for
a measurement.

## Decoding qualifier-dependent hit semantics

```python
def interpret_hit(source, hit):
    qualifier = int(source["qualifier"])

    if qualifier == 0x22:
        return {
            "channel": int(hit["channel"]),
            "toa_raw": int(hit["toa"]),
            "toa_ns": int(hit["toa"]) * 0.5,
            "tot_raw": None,
        }

    if qualifier in (0x02, 0x12):
        return {
            "channel": int(hit["channel"]),
            "toa_raw": int(hit["toa"]),
            "toa_ns": int(hit["toa"]) * 0.5,
            "tot_raw": int(hit["tot"]),
        }

    raise ValueError(
        f"unsupported timing qualifier 0x{qualifier:02x}"
    )
```

The 0.5 ns ToA conversion is supported by the run-2 histogram metadata and
the time-reference construction. A calibrated or mode-specific relative-time
interpretation remains a separate analysis step.

## Grouping hits without losing duplicates

```python
from collections import defaultdict


with h5py.File(path, "r") as h5:
    parent = h5["/events/timing/events"][1]
    start = int(parent["hit_offset"])
    stop = start + int(parent["hit_count"])
    hits = h5["/events/timing/hits"][start:stop]

    by_channel = defaultdict(list)

    for hit in hits:
        by_channel[int(hit["channel"])].append({
            "toa": int(hit["toa"]),
            "tot": None,  # run 2 uses qualifier 0x22
        })

    print("channel 29:", by_channel[29])
    print("channel 63:", by_channel[63])
```

## Unwrapping timestamps per board

Do not unwrap the globally interleaved index as one clock. Maintain state for
each `(chain, node)`:

```python
TIMESTAMP_MODULUS = 1 << 56


last_raw = {}
wrap_count = {}

with h5py.File(path, "r") as h5:
    index = h5["/events/index"]

    for source in index:
        if int(source["kind"]) != 2:
            continue

        board = (int(source["chain"]), int(source["node"]))
        raw = int(source["timestamp"])

        previous = last_raw.get(board)
        if previous is not None and raw < previous:
            # Use contextual checks in production to distinguish a genuine
            # wrap from a reset or out-of-order event.
            wrap_count[board] = wrap_count.get(board, 0) + 1

        last_raw[board] = raw
        unwrapped = raw + (
            wrap_count.get(board, 0) * TIMESTAMP_MODULUS
        )

        print(board, unwrapped, unwrapped * 8e-9)
```

The comment is important: a decrease may also indicate a reset or reordering.
Run 2 provides strong boundary evidence for the initial wrap, but a generic
reader should apply run and board context.

## Validating parent/child relationships

The file is large, so production validation should operate in batches.
This direct version states the invariants:

```python
import numpy as np


with h5py.File(path, "r") as h5:
    parents = h5["/events/timing/events"]
    hits = h5["/events/timing/hits"]

    expected_offset = 0

    for parent_number in range(len(parents)):
        parent = parents[parent_number]
        offset = int(parent["hit_offset"])
        count = int(parent["hit_count"])

        if offset != expected_offset:
            raise ValueError(
                f"parent {parent_number}: offset {offset}, "
                f"expected {expected_offset}"
            )
        if offset > len(hits) or count > len(hits) - offset:
            raise ValueError(
                f"parent {parent_number}: hit slice out of bounds"
            )

        children = hits[offset:offset + count]

        if np.any(children["parent_row"] != parent_number):
            raise ValueError(
                f"parent {parent_number}: incorrect hit ownership"
            )
        if np.any(children["channel"] >= 64):
            raise ValueError(
                f"parent {parent_number}: channel out of range"
            )

        expected_offset += count

    if expected_offset != len(hits):
        raise ValueError("unreferenced timing-hit rows")
```

Do not add a uniqueness check for child channels: repeated channels are valid.

## Validating index identities and payload sizes

```python
with h5py.File(path, "r") as h5:
    index = h5["/events/index"]
    parents = h5["/events/timing/events"]

    for source in index:
        if int(source["kind"]) != 2:
            continue

        parent_number = int(source["kind_row"])
        if parent_number >= len(parents):
            raise ValueError("timing kind_row out of bounds")

        parent = parents[parent_number]

        if int(source["trigger_id"]) != int(parent["trigger_id"]):
            raise ValueError("trigger ID mismatch")
        if int(source["timestamp"]) != int(parent["timestamp"]):
            raise ValueError("timestamp mismatch")

        expected_words = 1 + int(parent["hit_count"])
        if int(source["payload_size_words"]) != expected_words:
            raise ValueError(
                f"parent {parent_number}: payload has "
                f"{int(source['payload_size_words'])} words, "
                f"expected {expected_words}"
            )

        qualifier = int(source["qualifier"])
        if qualifier not in (0x02, 0x12, 0x22):
            raise ValueError(
                f"unsupported qualifier 0x{qualifier:02x}"
            )
```

## Reading ToA histogram metadata

```python
histogram_path = Path("pcap/run-2/run_2.histograms.h5")

with h5py.File(histogram_path, "r") as h5:
    spectra = h5["/histograms/toa/spectra"]
    bins = h5["/histograms/toa/bins"]

    spectrum = spectra[0]
    offset = int(spectrum["bin_offset"])
    count = int(spectrum["bin_count"])
    values = bins[offset:offset + count]

    minimum = float(spectrum["minimum"])
    width = float(spectrum["bin_width"])
    edges = minimum + np.arange(count + 1) * width

    print({
        "chain": int(spectrum["chain"]),
        "node": int(spectrum["node"]),
        "channel": int(spectrum["channel"]),
        "entries": int(spectrum["entries"]),
        "underflow": int(spectrum["underflow"]),
        "overflow": int(spectrum["overflow"]),
        "bin_width_ns": width,
    })
```

For run 2, treat the analogous ToT bins as a histogram of invalid placeholder
zeros, not as physical ToT.

## Command-line inspection

Show the timing schemas:

```sh
h5dump -H -g /events/timing \
  pcap/run-2/run_2.0000.h5
```

Show the first eight parents:

```sh
h5dump -d /events/timing/events -s 0 -c 8 \
  pcap/run-2/run_2.0000.h5
```

Show the first 80 hits:

```sh
h5dump -d /events/timing/hits -s 0 -c 80 \
  pcap/run-2/run_2.0000.h5
```

Show the corresponding index rows:

```sh
h5dump -d /events/index -s 0 -c 12 \
  pcap/run-2/run_2.0000.h5
```

Show the histogram schemas:

```sh
h5dump -H pcap/run-2/run_2.histograms.h5
```

## Interpretation summary

- Run 2 contains 1,608,067 timing events and 19,704,749 hits.
- Board identity and qualifier come from `/events/index`.
- The actual qualifier is `0x22`, selecting 25-bit ToA without ToT.
- Stored ToT zeros are invalid placeholders.
- The persisted ToA histogram establishes a 0.5 ns bin scale.
- Parent timestamps have an 8 ns coarse unit.
- `time_reference` adds four fine bits, giving a 0.5 ns reference LSB.
- Initial real rows demonstrate a 56-bit timestamp wrap.
- Zero-hit timing events are valid and retained.
- One channel can have multiple hits in one event.
- Hit order is wire order, not channel or time order.
- Trigger IDs remain zero and are not useful for ordering run-2 events.
- ToT histogram rows exist but do not represent valid ToT measurements for
  this qualifier.
