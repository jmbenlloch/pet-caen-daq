# HDF5 counting events

This document describes the counting-mode datasets under `/events/counting`
in the decoded-event HDF5 format. The examples are taken from the real
hardware capture:

```text
pcap/run-61/run_61.0000.h5
```

Run 61 was configured for `COUNTING` acquisition and contains seven counting
events from three boards. Its HDF5 file also contains one service event.

## Structure

Counting data use a parent table and a variable-length child table:

```text
/events/index
    | kind = 3
    | kind_row
    v
/events/counting/events
    | count_offset, count_count
    v
/events/counting/counts
```

The tables contain:

| Dataset | Rows in run 61 | Purpose |
| --- | ---: | --- |
| `/events/index` | 8 | Global event order and board identity |
| `/events/counting/events` | 7 | One row per received counting event |
| `/events/counting/counts` | 141 | Per-channel counters belonging to those events |

The eighth index row is a service event. Counting parent rows therefore cannot
be assumed to have the same row numbers as the global index.

## Global event index

The common `/events/index` table supplies information that is not duplicated
in the counting parent:

| Field | Meaning for counting data |
| --- | --- |
| `sequence` | Global persisted-event sequence |
| `kind` | `3` for counting |
| `chain`, `node` | Physical board address |
| `qualifier` | Wire event qualifier |
| `kind_row` | Row in `/events/counting/events` |
| `trigger_id`, `timestamp` | Identity copied from the wire descriptor |
| `payload_offset_words` | Offset of the payload in its transport batch |
| `payload_size_words` | Payload size in 32-bit words |
| `crc_error` | Transport CRC-error flag |

All seven run-61 counting rows have qualifier `0x04`. Its low nibble selects
counting mode. Modifier bit `0x80`, which would add a relative timestamp word,
is not set.

Run 61 maps counting parents to boards as follows:

| Index row | Sequence | Board | Parent row | Trigger ID | Timestamp | Payload words |
| ---: | ---: | --- | ---: | ---: | ---: | ---: |
| 0 | 1 | chain 1, node 0 | 0 | 0 | 5 | 31 |
| 1 | 2 | chain 1, node 0 | 1 | 13 | 143 | 30 |
| 2 | 3 | chain 1, node 0 | 2 | 26 | 286 | 36 |
| 3 | 4 | chain 2, node 0 | 3 | 0 | 2,112 | 9 |
| 4 | 5 | chain 2, node 0 | 4 | 2 | 16,537 | 18 |
| 5 | 6 | chain 2, node 0 | 5 | 20 | 16,675 | 12 |
| 7 | 8 | chain 3, node 0 | 6 | 0 | 32,966 | 15 |

Index row 6 is the service event between the last events from chains 2 and 3.

## Parent dataset: `/events/counting/events`

This is an extensible one-dimensional compound dataset:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `trigger_id` | `uint64` | Board trigger counter at the event |
| `timestamp` | `uint64` | Raw hardware timestamp in 8 ns ticks |
| `validity` | `uint8` | Optional-field validity bitmap |
| `relative_timestamp_clock` | `uint32` | Optional relative timestamp value |
| `channel_mask` | `uint64` | Bits identifying channels present in `counts` |
| `count_offset` | `uint64` | First child row in `/events/counting/counts` |
| `count_count` | `uint32` | Number of child rows |
| `t_or_count` | `uint32` | T-OR assertions reported in this counting event |
| `q_or_count` | `uint32` | Q-OR assertions reported in this counting event |

### Trigger ID

`trigger_id` is copied without conversion from the normalized event
descriptor. Run 61 used:

```text
TrgIdMode = TRIGGER_CNT
```

In this mode it counts triggers received by the board, including triggers
that did not become stored events. It is not the HDF5 row number and is not
necessarily contiguous.

The observed sequences are:

```text
chain 1: 0, 13, 26
chain 2: 0, 2, 20
chain 3: 0
```

Assuming the counter has neither reset nor wrapped between adjacent rows, the
estimated number of missing triggers is:

```text
missing = current_trigger_id - previous_trigger_id - 1
```

This gives 24 for chain 1 and 18 for chain 2, matching the run manifest.

### Timestamp

The timestamp is stored exactly as decoded from the hardware descriptor. Its
clock period is 8 ns:

```python
timestamp_seconds = timestamp_ticks * 8e-9
timestamp_microseconds = timestamp_ticks * 0.008
```

It is a board-relative hardware time, not Unix time. Do not compare it
directly with `/run/started_at_unix_ns`.

The run-61 timestamps cover only hundreds of microseconds even though the
wall-clock run lasted about 15 seconds. Together with the trigger-ID gaps,
this shows that the file does not contain a continuous record of every
counting trigger. It is unsafe to use the first-to-last stored timestamp as
the total counting exposure.

### Optional relative timestamp

Counting qualifier bit `0x80` indicates that a 32-bit relative timestamp word
is present. The HDF5 validity bitmap uses bit zero:

| Validity bit | Mask | Meaning |
| ---: | ---: | --- |
| 0 | `0x01` | `relative_timestamp_clock` is valid |

Use:

```python
has_relative_clock = bool(int(parent["validity"]) & 0x01)
```

All run-61 parent rows have:

```text
validity                = 0
relative_timestamp_clock = 0
```

The numeric zero is only a placeholder. It must not be interpreted as a valid
relative-clock measurement.

### Channel mask and child slice

Bit `n` in `channel_mask` represents physical channel `n`:

```python
channels = [
    channel for channel in range(64)
    if int(parent["channel_mask"]) & (1 << channel)
]
```

The corresponding child rows form a half-open slice:

```python
start = int(parent["count_offset"])
stop = start + int(parent["count_count"])
event_counts = h5["/events/counting/counts"][start:stop]
```

For a valid file:

- `count_count` equals the number of set mask bits;
- every sliced child's `parent_row` equals the parent row number;
- each child channel has its corresponding mask bit set; and
- the parent slices are contiguous and cover the entire child table.

Child rows retain wire order and need not be sorted by channel.

### T-OR and Q-OR counters

The counting wire format uses identifiers:

```text
0–63: individual channels
64:   T-OR aggregate
65:   Q-OR aggregate
```

The decoder places identifiers 64 and 65 in the parent as `t_or_count` and
`q_or_count`, rather than creating ordinary channel children.

T-OR is the aggregate time-discriminator OR counter. Q-OR is the aggregate
charge-discriminator OR counter. They are independent measurements; neither
value is required to equal:

- the number of populated channel rows;
- the sum of individual channel counts;
- the event trigger ID; or
- the number of stored events.

The values are raw integer counts. No counts-per-second conversion is
performed by the HDF5 writer.

## Child dataset: `/events/counting/counts`

This is another extensible one-dimensional compound dataset:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `parent_row` | `uint64` | Owner row in `/events/counting/events` |
| `channel` | `uint8` | Physical channel number, 0 through 63 |
| `counter_value` | `uint32` | Raw count reported for this channel |

On the wire the counter occupies 24 bits:

```text
bits 31:24 = channel identifier
bits 23:0  = counter value
```

HDF5 uses `uint32` for convenient native storage, but the decoded value range
is therefore:

```text
0 through 16,777,215
```

If malformed input repeats a channel in one payload, the decoder follows the
source-confirmed FERSlib behavior: the last value wins. The stored typed event
contains at most one child row per physical channel.

## All run-61 parents

| Parent | Board | Trigger | Timestamp ticks | Timestamp µs | Mask | Child slice | T-OR | Q-OR |
| ---: | --- | ---: | ---: | ---: | --- | --- | ---: | ---: |
| 0 | 1:0 | 0 | 5 | 0.040 | `0x00000002f87fffff` | `[0:29]` | 1 | 0 |
| 1 | 1:0 | 13 | 143 | 1.144 | `0x0056003ffe008fff` | `[29:59]` | 0 | 0 |
| 2 | 1:0 | 26 | 286 | 2.288 | `0xf10000003fffdfff` | `[59:93]` | 0 | 0 |
| 3 | 2:0 | 0 | 2,112 | 16.896 | `0x0490115000000000` | `[93:100]` | 0 | 0 |
| 4 | 2:0 | 2 | 16,537 | 132.296 | `0x03fff44100008000` | `[100:118]` | 0 | 0 |
| 5 | 2:0 | 20 | 16,675 | 133.400 | `0xfc00030300000000` | `[118:128]` | 1 | 0 |
| 6 | 3:0 | 0 | 32,966 | 263.728 | `0x0000000004002fef` | `[128:141]` | 0 | 0 |

The board notation `1:0` means chain 1, node 0.

## Complete linked example: parent row 0

The index row is:

```text
sequence             = 1
kind                 = 3 (counting)
chain                = 1
node                 = 0
qualifier            = 0x04
kind_row             = 0
trigger_id           = 0
timestamp            = 5
payload_offset_words = 0
payload_size_words   = 31
crc_error            = 0
```

The parent is:

```text
trigger_id               = 0
timestamp                = 5
validity                 = 0
relative_timestamp_clock = invalid
channel_mask             = 0x00000002f87fffff
count_offset             = 0
count_count              = 29
t_or_count               = 1
q_or_count               = 0
```

The mask selects:

```text
0–22, 27–31, 33
```

Child rows 0 through 28 contain exactly those channels, each with
`counter_value = 1`. Thus this received interval reports one count on each of
29 channels, one aggregate T-OR assertion, and no Q-OR assertions.

The payload has 31 words:

```text
29 channel words + 1 T-OR word + 1 Q-OR word = 31 words
```

Even a zero T-OR or Q-OR value can be present on the wire. These aggregate
words are stored in the parent rather than in the child count.

## Example with different channel values: parent row 1

Parent row 1 owns child rows `[29:59]`. The beginning of that slice is:

| Child row | Channel | Counter value |
| ---: | ---: | ---: |
| 29 | 0 | 2 |
| 30 | 1 | 3 |
| 31 | 2 | 2 |
| 32 | 3 | 3 |
| 33 | 4 | 3 |
| 34 | 5 | 3 |
| 35 | 6 | 3 |
| 36 | 7 | 3 |
| 37 | 8 | 2 |
| 38 | 9 | 3 |
| 39 | 10 | 3 |
| 40 | 11 | 3 |

Later rows include:

```text
channel 33 -> 2
channel 34 -> 1
channel 35 -> 2
channel 49 -> 1
channel 50 -> 1
channel 52 -> 1
channel 54 -> 1
```

This illustrates that `counter_value` is not merely a Boolean hit flag.

## Example of wire-order children: parent row 2

Parent row 2 owns rows `[59:93]`. Its first children are:

```text
channel 56 -> 1
channel 60 -> 1
channel 61 -> 1
channel 62 -> 1
channel 63 -> 1
channel 0  -> 1
channel 1  -> 1
channel 2  -> 1
channel 3  -> 2
```

The channel sequence wraps from 63 to 0 because the table preserves decoded
payload order. Analysis code should index or group by `channel`, not assume
ascending child order.

## Configuration context

The run manifest records:

```text
AcquisitionMode      COUNTING
CountingMode         SINGLES
EnableCntZeroSuppr   1
BunchTrgSource       TLOGIC
PtrgPeriod           1 ms
TrgIdMode            TRIGGER_CNT
StopRunMode          PRESET_TIME
PresetTime           15 s
```

`SINGLES` means every physical channel counts its own self-trigger. It is not
the paired-channel coincidence mode.

Because `EnableCntZeroSuppr` is enabled, channels with zero counts are omitted
from the payload. In this specific configuration, an absent channel normally
represents a zero count for that received interval. In general-purpose code,
also consult channel-enable and acquisition configuration before equating
absence with a functioning channel that measured zero.

`BunchTrgSource = TLOGIC` means TLOGIC, not the internal periodic trigger,
causes counting-event readout. Consequently, the configured
`PtrgPeriod = 1 ms` must not automatically be used as the exposure time for
these rows.

## Per-event values and run totals

The HDF5 parent and child rows store the values from individual received
events. Run statistics accumulate those values by board and channel.

For chain 1, channel 0:

```text
parent 0: 1
parent 1: 2
parent 2: 1
run sum: 4
```

The manifest's chain-1 `channel_trigger_counts[0]` is therefore `4`.

T-OR accumulation works the same way:

```text
chain 1: 1 + 0 + 0 = 1
chain 2: 0 + 0 + 1 = 1
chain 3: 0           = 0
```

These totals describe received data. They do not reconstruct counters from
the missing trigger IDs.

## Counts are not rates

`counter_value`, `t_or_count`, and `q_or_count` have the unit “counts.” The
writer performs no normalization or conversion.

A rate requires a known integration duration:

```python
rate_cps = count / integration_seconds
```

The counting parent does not explicitly store that duration. For run 61:

- the run lasted about 15.245 seconds;
- the file contains only seven counting events;
- trigger IDs show 42 estimated missing triggers;
- stored hardware timestamps cover only hundreds of microseconds; and
- TLOGIC, rather than PTRG, is the bunch-trigger source.

Do not divide an individual count by 15.245 seconds or by the configured 1 ms
PTRG period without separate evidence that this is the integration gate for
that event. The HDF5 data reliably preserve counts, but this capture alone
does not establish a defensible per-row rate denominator.

## Reading one counting event with Python

The decoded-event files use compressed HDF5 datasets. Install the reader
packages:

```sh
python -m pip install h5py hdf5plugin
```

Import `hdf5plugin` before opening the file so its filters are registered:

```python
from pathlib import Path

import h5py
import hdf5plugin  # noqa: F401; registers compression filters


path = Path("pcap/run-61/run_61.0000.h5")

with h5py.File(path, "r") as h5:
    parents = h5["/events/counting/events"]
    children = h5["/events/counting/counts"]

    parent_number = 1
    parent = parents[parent_number]

    start = int(parent["count_offset"])
    stop = start + int(parent["count_count"])
    event_counts = children[start:stop]

    print("trigger ID:", int(parent["trigger_id"]))
    print("timestamp ticks:", int(parent["timestamp"]))
    print("timestamp seconds:", int(parent["timestamp"]) * 8e-9)
    print("T-OR:", int(parent["t_or_count"]))
    print("Q-OR:", int(parent["q_or_count"]))

    for child in event_counts:
        print(
            "channel", int(child["channel"]),
            "count", int(child["counter_value"]),
        )
```

## Recovering board identity

Use `kind_row` to join the global index to the counting parent:

```python
with h5py.File(path, "r") as h5:
    index = h5["/events/index"][:]
    parents = h5["/events/counting/events"]

    counting_index = index[index["kind"] == 3]

    for source in counting_index:
        parent_number = int(source["kind_row"])
        parent = parents[parent_number]

        if int(source["trigger_id"]) != int(parent["trigger_id"]):
            raise ValueError("index/parent trigger ID mismatch")
        if int(source["timestamp"]) != int(parent["timestamp"]):
            raise ValueError("index/parent timestamp mismatch")

        print({
            "chain": int(source["chain"]),
            "node": int(source["node"]),
            "parent_row": parent_number,
            "trigger_id": int(parent["trigger_id"]),
        })
```

Do not group counting events by `trigger_id` alone: each board has its own
counter and multiple boards can report the same value.

## Building channel dictionaries

A convenient representation for analysis is one dictionary per event:

```python
with h5py.File(path, "r") as h5:
    parents = h5["/events/counting/events"]
    children = h5["/events/counting/counts"]

    for parent_number, parent in enumerate(parents):
        start = int(parent["count_offset"])
        stop = start + int(parent["count_count"])

        counts_by_channel = {
            int(child["channel"]): int(child["counter_value"])
            for child in children[start:stop]
        }

        # This zero fill is appropriate for run 61 because zero
        # suppression was enabled and all channels were configured.
        dense_counts = [
            counts_by_channel.get(channel, 0)
            for channel in range(64)
        ]

        print(parent_number, dense_counts)
```

Keep the sparse dictionary if disabled channels and zero-count channels must
remain distinguishable.

## Validating the parent/child structure

```python
import numpy as np


with h5py.File(path, "r") as h5:
    parents = h5["/events/counting/events"][:]
    children = h5["/events/counting/counts"]

    expected_offset = 0

    for parent_number, parent in enumerate(parents):
        offset = int(parent["count_offset"])
        count = int(parent["count_count"])

        if offset != expected_offset:
            raise ValueError(
                f"parent {parent_number}: non-contiguous offset "
                f"{offset}, expected {expected_offset}"
            )
        if offset > len(children) or count > len(children) - offset:
            raise ValueError(
                f"parent {parent_number}: child slice out of bounds"
            )

        event_counts = children[offset:offset + count]

        if np.any(event_counts["parent_row"] != parent_number):
            raise ValueError(
                f"parent {parent_number}: incorrect child ownership"
            )

        channels = [int(value) for value in event_counts["channel"]]
        if len(channels) != len(set(channels)):
            raise ValueError(
                f"parent {parent_number}: duplicate stored channel"
            )
        if any(channel >= 64 for channel in channels):
            raise ValueError(
                f"parent {parent_number}: channel out of range"
            )

        mask = int(parent["channel_mask"])
        mask_channels = {
            channel for channel in range(64)
            if mask & (1 << channel)
        }
        if set(channels) != mask_channels:
            raise ValueError(
                f"parent {parent_number}: child/mask mismatch"
            )

        if count != mask.bit_count():
            raise ValueError(
                f"parent {parent_number}: count/popcount mismatch"
            )

        expected_offset += count

    if expected_offset != len(children):
        raise ValueError("unreferenced child rows")
```

## Command-line inspection

Show the schemas and row counts:

```sh
h5dump -H -g /events/counting \
  pcap/run-61/run_61.0000.h5
```

Show every parent:

```sh
h5dump -d /events/counting/events \
  pcap/run-61/run_61.0000.h5
```

Show all channel children:

```sh
h5dump -d /events/counting/counts \
  pcap/run-61/run_61.0000.h5
```

Show the global source index:

```sh
h5dump -d /events/index \
  pcap/run-61/run_61.0000.h5
```

## Histogram file

Run 61 also has:

```text
pcap/run-61/run_61.histograms.h5
```

Its `/histograms` group is empty. Counting events were persisted in the
decoded-event file, but this run did not write a counting/MCS histogram
dataset. The absence of a histogram does not imply absence of counting event
rows.

## Interpretation summary

- A parent row is one received counting event.
- A child row is one raw per-channel count in that event.
- `count_offset` and `count_count` locate the children.
- `channel_mask` describes the same set of physical channels.
- Zero suppression explains omitted zero-count channels in run 61.
- T-OR and Q-OR are separate aggregate discriminator counters.
- Raw counts are stored without conversion.
- Board identity comes from `/events/index`.
- Trigger IDs can contain gaps and are local to each board.
- Hardware timestamps use 8 ns ticks but do not represent wall-clock time.
- A defensible rate requires an independently established integration time.
