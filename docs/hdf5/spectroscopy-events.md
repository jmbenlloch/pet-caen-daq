# HDF5 spectroscopy events

This document describes the `/events/spectroscopy` datasets in the
decoded-event HDF5 format. Spectroscopy is the only event mode represented by
substantial real detector data in the retained run corpus. Examples below are
capture-verified against `pcap/run-54/run_54.0000.h5`.

## Structure

Spectroscopy uses one parent table and two flat child tables:

```text
/events/index
    | kind = 1
    | kind_row
    v
/events/spectroscopy/events
    |-- energy_offset, energy_count --> /events/spectroscopy/energies
    `-- timing_offset, timing_count --> /events/spectroscopy/timings
```

The parent contains event-wide scalar values. Energy and timing measurements
have variable cardinality, so they are concatenated into ordinary
one-dimensional child tables. Every parent records a half-open slice into each
child table.

Run 54 contains:

| Dataset | Rows |
| --- | ---: |
| `/events/spectroscopy/events` | 799,889 |
| `/events/spectroscopy/energies` | 50,154,153 |
| `/events/spectroscopy/timings` | 107,709 |

The same file contains 60 service events:

```text
799,889 spectroscopy + 60 service = 799,949 /events/index rows
```

## Parent dataset

`/events/spectroscopy/events` is an extensible one-dimensional compound
dataset:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `trigger_id` | `uint64` | Board trigger or validation identifier |
| `timestamp` | `uint64` | Coarse event timestamp in 8 ns hardware ticks |
| `validity` | `uint8` | Optional-field validity bitmap |
| `relative_timestamp_clock` | `uint32` | Optional relative timestamp |
| `channel_mask` | `uint64` | Channels for which energy entries are present |
| `energy_offset` | `uint64` | First child row in `energies` |
| `energy_count` | `uint32` | Number of energy children |
| `timing_offset` | `uint64` | First child row in `timings` |
| `timing_count` | `uint32` | Number of timing children |
| `time_reference` | `uint32` | Optional raw 31-bit timing-reference value |

### Validity bitmap

| Bit | Mask | Optional field |
| ---: | ---: | --- |
| 0 | `0x01` | `relative_timestamp_clock` |
| 1 | `0x02` | `time_reference` |

The stored numeric value of an invalid optional field must be ignored:

```text
validity = 0 --> neither optional field is present
validity = 1 --> relative timestamp is present
validity = 2 --> time reference is present
validity = 3 --> both are present
```

### First five run-54 parents

| Parent | Trigger ID | Timestamp | Validity | Channel mask | Energy range | Timing range | Time reference |
| ---: | ---: | ---: | ---: | --- | --- | --- | ---: |
| 0 | 0 | 6 | 0 | `0xffffffffffffffff` | `[0:64]` | `[0:0]` | invalid |
| 1 | 0 | 70 | 0 | `0xffffffffffffffff` | `[64:128]` | `[0:0]` | invalid |
| 2 | 131 | 1,385 | 2 | `0xffffffffffffffff` | `[128:192]` | `[0:40]` | 2,147,482,693 |
| 3 | 303 | 2,960 | 2 | `0xffffffffffffffff` | `[192:256]` | `[40:71]` | 21,190 |
| 4 | 452 | 4,403 | 2 | `0xffffffffffffffff` | `[256:320]` | `[71:102]` | 46,390 |

A zero-length timing range is valid. Its offset is the current end of the
timing pool.

## Complete linked run-54 example

Parent row 2 is:

```text
trigger_id                = 131
timestamp                 = 1385
validity                  = 2
relative_timestamp_clock  = 0, invalid
channel_mask              = 0xffffffffffffffff
energy_offset             = 128
energy_count              = 64
timing_offset             = 0
timing_count              = 40
time_reference            = 2147482693 = 0x7ffffc45
```

Interpretation:

- The coarse hardware time is `1385 * 8 ns = 11.080 us`.
- Validity bit 1 is set, so `time_reference` is present.
- Validity bit 0 is clear, so the stored zero in
  `relative_timestamp_clock` is only a placeholder.
- All 64 channel-mask bits are set.
- The event owns `energies[128:192]`.
- The event owns `timings[0:40]`.

### Corresponding global index row

`/events/index[2]` contains:

| Field | Value |
| --- | ---: |
| `sequence` | 3 |
| `kind` | 1, spectroscopy |
| `chain` | 2 |
| `node` | 0 |
| `qualifier` | 51 (`0x33`) |
| `kind_row` | 2 |
| `trigger_id` | 131 |
| `timestamp` | 1,385 |
| `payload_offset_words` | 0 |
| `payload_size_words` | 265 |
| `crc_error` | 0 |

The index supplies source identity and global ordering. Those fields are not
duplicated in the spectroscopy parent.

The following identities must agree:

```text
index.kind_row   = spectroscopy parent row
index.trigger_id = parent.trigger_id
index.timestamp  = parent.timestamp
```

`payload_offset_words` and `payload_size_words` are evidence from the DT5215
descriptor. They are not offsets into an HDF5 dataset.

## Spectroscopy qualifier

The original eight-bit wire qualifier is stored in `/events/index`.
Run-54 examples use `0x33`:

```text
0x03 = spectroscopy plus timing
0x10 = both energy gains present
0x20 = leading-edge timing format
--------------------------------
0x33
```

Thus the example contains energy, attached timing, both gain measurements, and
leading-edge timing without ToT.

Other supported spectroscopy modifiers include:

- `0x10`: both gains;
- `0x20`: leading-edge timing form; and
- `0x80`: relative timestamp present.

The broad HDF5 `kind` is convenient for queries, while `qualifier` is the
authoritative packed variant. A reader must not reconstruct the qualifier from
the group name.

## Energy children

`/events/spectroscopy/energies` has:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `parent_row` | `uint64` | Owning spectroscopy parent |
| `channel` | `uint8` | Physical channel 0 through 63 |
| `low_gain` | `uint16` | Low-gain raw ADC code |
| `high_gain` | `uint16` | High-gain raw ADC code |
| `has_low_gain` | `uint8` | Low-gain field is valid |
| `has_high_gain` | `uint8` | High-gain field is valid |
| `discriminator` | `uint8` | Packed charge-discriminator/QD bit |

### Children of parent row 2

The first eight rows in `energies[128:192]` are:

| Child row | Parent | Channel | Low gain | High gain | Has LG | Has HG | QD |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 128 | 2 | 0 | 214 | 283 | 1 | 1 | 0 |
| 129 | 2 | 1 | 205 | 201 | 1 | 1 | 0 |
| 130 | 2 | 2 | 154 | 298 | 1 | 1 | 0 |
| 131 | 2 | 3 | 177 | 210 | 1 | 1 | 0 |
| 132 | 2 | 4 | 166 | 271 | 1 | 1 | 0 |
| 133 | 2 | 5 | 142 | 148 | 1 | 1 | 0 |
| 134 | 2 | 6 | 186 | 263 | 1 | 1 | 0 |
| 135 | 2 | 7 | 160 | 152 | 1 | 1 | 0 |

The values are raw ADC codes, not calibrated energies such as keV.

### Gain flags and wire packing

Gain validity is explicit:

```text
has_low_gain  = 1 --> low_gain is meaningful
has_high_gain = 1 --> high_gain is meaningful
```

For both-gain payloads, one 32-bit word is used per channel:

```text
bits 13:0  = high-gain ADC code
bit  15    = discriminator
bits 29:16 = low-gain ADC code
```

For single-gain payloads, entries are 16 bits:

```text
bits 13:0 = ADC code
bit  14   = low-versus-high gain selector
bit  15   = discriminator
```

An absent gain column can contain zero as a storage placeholder. Its
`has_*_gain` flag must be checked before using it.

### Channel mask

Bit `c` of `channel_mask` means that an energy entry exists for channel `c`:

```python
channels = [
    channel
    for channel in range(64)
    if channel_mask & (1 << channel)
]
```

The expected relationship is:

```text
energy_count = popcount(channel_mask)
```

For parent row 2:

```text
channel_mask = 0xffffffffffffffff
popcount     = 64
energy_count = 64
```

An all-ones mask does not mean that all channels caused the trigger. It only
means all 64 energy values were included. Full-readout events can include
baseline, noise, or pulse activity unrelated to the accepted trigger.

### Charge discriminator

`discriminator` is the charge-discriminator, or QD, bit packed with the energy
word. It does not prove that the channel alone generated the accepted board
trigger. Trigger acceptance can depend on:

- enabled discriminator masks;
- majority/trigger logic;
- signal overlap;
- external trigger sources;
- validation; and
- veto.

For parent row 0:

| Channel | Low gain | High gain | QD |
| ---: | ---: | ---: | ---: |
| 0 | 211 | 355 | 0 |
| 1 | 184 | 194 | 1 |
| 2 | 163 | 142 | 1 |
| 3 | 186 | 214 | 1 |
| 10 | 153 | 242 | 0 |
| 20 | 127 | 298 | 0 |

The parent contains energy values for every channel even though only some
carry the QD bit.

## Timing children

`/events/spectroscopy/timings` has:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `parent_row` | `uint64` | Owning spectroscopy parent |
| `channel` | `uint8` | Physical channel |
| `toa` | `uint32` | Raw time-of-arrival code |
| `tot` | `uint16` | Raw 9-bit time-over-threshold code when present |

The first ten timing children of parent row 2 are:

| Timing row | Parent | Channel | ToA | ToT |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 2 | 27 | 1,029 | 0 |
| 1 | 2 | 60 | 1,032 | 0 |
| 2 | 2 | 1 | 1,033 | 0 |
| 3 | 2 | 61 | 968 | 0 |
| 4 | 2 | 20 | 1,033 | 0 |
| 5 | 2 | 63 | 968 | 0 |
| 6 | 2 | 30 | 1,033 | 0 |
| 7 | 2 | 59 | 988 | 0 |
| 8 | 2 | 10 | 1,034 | 0 |
| 9 | 2 | 18 | 1,034 | 0 |

The histogram implementation uses the source-confirmed conversion:

```text
ToA time = toa * 0.5 ns
```

Thus `toa = 1029` corresponds nominally to 514.5 ns in the configured timing
reference convention. It is not an absolute time since run start.

Run 54 uses the leading-edge form and has ToT disabled, so `tot = 0` is a
placeholder rather than a measured zero-duration pulse.

### Timing children are optional

An accepted spectroscopy event can have no timing children. QD assertion and
event acceptance do not guarantee a TDC measurement because the timing path is
separate:

```text
detector pulse
|-- slow shaping --> peak hold --> ADC --> energy row
|-- charge discriminator --> QD bit
`-- time discriminator --> TDC --> timing row
```

A timing row proves that an accepted TDC observation was emitted. Absence does
not prove that the analog time discriminator never crossed. A crossing can be
masked, outside the reference window, suppressed by holdoff/dead time, or
excluded by event-building rules.

The decoder retains the first timing word for a channel and ignores later
duplicates in the same spectroscopy payload.

There is no primary TD boolean equivalent to the QD bit. Analysis can derive:

```text
has_timing_hit = channel appears in the parent's timing slice
```

That means an accepted timing measurement exists; it must not be labeled
`td_discriminator_asserted`.

## Three different time concepts

### Event timestamp

The parent and index `timestamp` is the coarse descriptor timestamp:

```text
1 tick = 8 ns
```

For row 2:

```text
1385 ticks = 11.080 us
```

It is hardware-relative, not UTC.

### Relative timestamp clock

`relative_timestamp_clock` is optional and is present when qualifier bit
`0x80` is set. Parent validity bit 0 controls whether the stored value can be
used. The run-54 `0x33` examples do not include it.

### Payload time reference

A spectroscopy timing-reference word uses wire bit 31 as a marker. The stored
value is:

```text
wire_word & 0x7fffffff
```

Parent row 2 stores:

```text
0x7ffffc45
```

This is preserved as a raw 31-bit payload value. It is not the same quantity
as the coarse event timestamp, relative timestamp clock, or channel ToA. Its
interpretation depends on timing-reference source, window, delay, and firmware
conventions.

## Child slices and integrity rules

For parent row `p`:

```python
event = parents[p]

energy_start = int(event["energy_offset"])
energy_stop = energy_start + int(event["energy_count"])
event_energies = energies[energy_start:energy_stop]

timing_start = int(event["timing_offset"])
timing_stop = timing_start + int(event["timing_count"])
event_timings = timings[timing_start:timing_stop]
```

The slices are half-open. Readers should validate:

```text
energy_offset <= len(energies)
energy_count  <= len(energies) - energy_offset
timing_offset <= len(timings)
timing_count  <= len(timings) - timing_offset
every energy child parent_row equals p
every timing child parent_row equals p
energy child channels match channel_mask
energy_count equals popcount(channel_mask)
```

The redundant `parent_row` makes direct child inspection and corruption checks
possible without first searching parent offsets.

## Global ordering and source identity

`/events/spectroscopy/events` contains only spectroscopy parents. Interleaving
with service or other event kinds is preserved in `/events/index`.

For spectroscopy:

```text
index.kind     = 1
index.kind_row = spectroscopy parent row
```

The index additionally provides sequence, chain, node, qualifier, descriptor
payload evidence, and CRC status. `sequence` is the global stored-event order;
it must not be confused with the hardware `trigger_id`.

## Storage commit order

The writer commits spectroscopy records in this order:

```text
1. energy children
2. timing children
3. spectroscopy parents
4. global index rows
```

Consecutive spectroscopy events are batched for efficient HDF5 writes. Child
offsets, parent rows, and global sequences are calculated before the batch is
appended.

## Reading with Python

The run-54 event tables use Blosc/LZ4 compression. The HDF5 environment must
provide filter ID 32001; one common Python setup registers it by importing
`hdf5plugin` before reading compressed datasets.

### Read one complete spectroscopy event

```python
from pathlib import Path

import h5py
import hdf5plugin  # noqa: F401


path = Path("pcap/run-54/run_54.0000.h5")
parent_number = 2

with h5py.File(path, "r") as h5:
    parents = h5["/events/spectroscopy/events"]
    energy_table = h5["/events/spectroscopy/energies"]
    timing_table = h5["/events/spectroscopy/timings"]

    parent = parents[parent_number]

    energy_start = int(parent["energy_offset"])
    energy_stop = energy_start + int(parent["energy_count"])
    energies = energy_table[energy_start:energy_stop]

    timing_start = int(parent["timing_offset"])
    timing_stop = timing_start + int(parent["timing_count"])
    timings = timing_table[timing_start:timing_stop]

    relative_timestamp = (
        int(parent["relative_timestamp_clock"])
        if int(parent["validity"]) & 0x01
        else None
    )
    time_reference = (
        int(parent["time_reference"])
        if int(parent["validity"]) & 0x02
        else None
    )

    print("parent:", parent)
    print("relative timestamp:", relative_timestamp)
    print("time reference:", time_reference)
    print("energies:", energies)
    print("timings:", timings)
```

### Find source and qualifier through the index

```python
with h5py.File(path, "r") as h5:
    index = h5["/events/index"][:]

matches = index[
    (index["kind"] == 1)
    & (index["kind_row"] == parent_number)
]

if len(matches) != 1:
    raise ValueError(
        f"expected one index row for parent {parent_number}, "
        f"found {len(matches)}"
    )

source = matches[0]
print(
    "sequence", int(source["sequence"]),
    "chain", int(source["chain"]),
    "node", int(source["node"]),
    "qualifier", f"0x{int(source['qualifier']):02x}",
)
```

### Validate all parent/child relationships

```python
import numpy as np


with h5py.File(path, "r") as h5:
    parents = h5["/events/spectroscopy/events"][:]
    energies = h5["/events/spectroscopy/energies"]
    timings = h5["/events/spectroscopy/timings"]

    energy_length = len(energies)
    timing_length = len(timings)

    for parent_number, parent in enumerate(parents):
        energy_start = int(parent["energy_offset"])
        energy_count = int(parent["energy_count"])
        timing_start = int(parent["timing_offset"])
        timing_count = int(parent["timing_count"])

        if energy_start > energy_length:
            raise ValueError(
                f"parent {parent_number}: energy offset out of bounds"
            )
        if energy_count > energy_length - energy_start:
            raise ValueError(
                f"parent {parent_number}: energy slice out of bounds"
            )
        if timing_start > timing_length:
            raise ValueError(
                f"parent {parent_number}: timing offset out of bounds"
            )
        if timing_count > timing_length - timing_start:
            raise ValueError(
                f"parent {parent_number}: timing slice out of bounds"
            )

        event_energies = energies[
            energy_start:energy_start + energy_count
        ]
        event_timings = timings[
            timing_start:timing_start + timing_count
        ]

        if np.any(event_energies["parent_row"] != parent_number):
            raise ValueError(
                f"parent {parent_number}: incorrect energy ownership"
            )
        if np.any(event_timings["parent_row"] != parent_number):
            raise ValueError(
                f"parent {parent_number}: incorrect timing ownership"
            )

        mask = int(parent["channel_mask"])
        expected_channels = [
            channel for channel in range(64)
            if mask & (1 << channel)
        ]
        actual_channels = [
            int(channel) for channel in event_energies["channel"]
        ]

        if actual_channels != expected_channels:
            raise ValueError(
                f"parent {parent_number}: "
                f"mask channels {expected_channels} "
                f"do not match energy rows {actual_channels}"
            )
```

For very large files, validate in parent batches rather than retaining all
parent rows at once.

### Build a channel-level analysis table

```python
records = []

with h5py.File(path, "r") as h5:
    parents = h5["/events/spectroscopy/events"]
    energy_table = h5["/events/spectroscopy/energies"]
    timing_table = h5["/events/spectroscopy/timings"]

    for parent_number in range(len(parents)):
        parent = parents[parent_number]

        e0 = int(parent["energy_offset"])
        e1 = e0 + int(parent["energy_count"])
        t0 = int(parent["timing_offset"])
        t1 = t0 + int(parent["timing_count"])

        timing_by_channel = {
            int(row["channel"]): row
            for row in timing_table[t0:t1]
        }

        for energy in energy_table[e0:e1]:
            channel = int(energy["channel"])
            timing = timing_by_channel.get(channel)
            records.append({
                "parent_row": parent_number,
                "trigger_id": int(parent["trigger_id"]),
                "timestamp_ticks": int(parent["timestamp"]),
                "timestamp_seconds": int(parent["timestamp"]) * 8e-9,
                "channel": channel,
                "low_gain": (
                    int(energy["low_gain"])
                    if int(energy["has_low_gain"])
                    else None
                ),
                "high_gain": (
                    int(energy["high_gain"])
                    if int(energy["has_high_gain"])
                    else None
                ),
                "qd": bool(energy["discriminator"]),
                "has_timing_hit": timing is not None,
                "toa": int(timing["toa"]) if timing is not None else None,
                "toa_ns": (
                    int(timing["toa"]) * 0.5
                    if timing is not None
                    else None
                ),
                "tot": int(timing["tot"]) if timing is not None else None,
            })
```

`has_timing_hit` is deliberately named as an observed TDC result, not as a
time-discriminator assertion.

### Select events from one physical board

```python
import numpy as np


with h5py.File(path, "r") as h5:
    index = h5["/events/index"][:]
    parents = h5["/events/spectroscopy/events"]

    selected_index = index[
        (index["kind"] == 1)
        & (index["chain"] == 2)
        & (index["node"] == 0)
    ]

    parent_rows = selected_index["kind_row"].astype(np.int64)
    selected_parents = parents[parent_rows]
```

Do not group events by `trigger_id` alone. Trigger identifiers are local to
hardware behavior and may reset, wrap, or use a validation-counter mode.
Use chain/node plus stored sequence and timestamps as appropriate.

## Command-line inspection

Show the spectroscopy schema:

```sh
h5dump -H -g /events/spectroscopy \
  pcap/run-54/run_54.0000.h5
```

Show the first five parents:

```sh
h5dump -d /events/spectroscopy/events -s 0 -c 5 \
  pcap/run-54/run_54.0000.h5
```

Show parent row 2's 64 energies:

```sh
h5dump -d /events/spectroscopy/energies -s 128 -c 64 \
  pcap/run-54/run_54.0000.h5
```

Show parent row 2's 40 timings:

```sh
h5dump -d /events/spectroscopy/timings -s 0 -c 40 \
  pcap/run-54/run_54.0000.h5
```

Show its global index row:

```sh
h5dump -d /events/index -s 2 -c 1 \
  pcap/run-54/run_54.0000.h5
```

## Interpretation summary

| Value | Correct interpretation | Incorrect shortcut |
| --- | --- | --- |
| `channel_mask` bit | Energy value was included | Channel caused the trigger |
| `low_gain`, `high_gain` | Raw ADC codes | Calibrated energy |
| `has_*_gain` | Corresponding ADC value is valid | Gain necessarily caused triggering |
| `discriminator` | QD bit | Explicit TD state or sole trigger source |
| Timing child | Accepted TDC measurement exists | Analog TD crossed exactly once |
| `toa` | Raw timing code, 0.5 ns per tick in this mode | Absolute event time |
| `tot` | Raw 9-bit ToT code when present | Nanoseconds without a conversion |
| `timestamp` | Coarse 8 ns hardware clock | Unix timestamp |
| `time_reference` | Raw 31-bit payload value | Same quantity as event timestamp |
| `trigger_id` | Hardware trigger/validation identifier | Global run event number |
| Index `sequence` | Global stored-event order | Hardware trigger identifier |

Physical interpretation also depends on `/configuration`: gain, shaping,
pedestal calibration, masks, discriminator thresholds, timing window/delay,
ToT enablement, trigger logic, validation/veto, topology, and firmware all
affect the recorded spectroscopy values.
