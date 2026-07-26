# HDF5 waveform events

This document describes waveform-mode data under `/events/waveform` in the
decoded-event HDF5 format. Examples come from the real hardware capture:

```text
pcap/run-70/
```

Run 70 is especially useful because it contains more than 72,000 waveform
events and crosses the configured HDF5 segment-size boundary.

## Run-70 artifacts

| File | Size | Purpose |
| --- | ---: | --- |
| `run_70.0000.h5` | 527,478,749 bytes | First decoded-event segment |
| `run_70.0001.h5` | 56,907,047 bytes | Second decoded-event segment |
| `run_70.histograms.h5` | 4,640 bytes | Run-wide histogram artifact |
| `wire.raw` | 233,557,880 bytes | Raw stream capture |
| `transport.journal` | 236,324,612 bytes | Transport journal |
| `manifest.json` | 314,735 bytes | Run metadata, configuration, and statistics |

The run used a 15-second preset and completed normally with termination reason
`preset_time`.

## Structure

Waveform storage uses a parent table and a flat variable-length sample table:

```text
/events/index
    | kind = 4
    | kind_row
    v
/events/waveform/events
    | sample_offset, sample_count
    v
/events/waveform/samples
```

The parent stores event-wide identity and locates a half-open slice in the
sample table. Each child row stores one simultaneous HG/LG/probe snapshot.

## Dataset sizes

| Segment | `/events/index` | Waveform parents | Samples | Service parents |
| --- | ---: | ---: | ---: | ---: |
| `0000` | 65,311 | 65,263 | 52,210,400 | 48 |
| `0001` | 7,000 | 6,995 | 5,596,000 | 5 |
| Total | 72,311 | 72,258 | 57,806,400 | 53 |

Every waveform parent has 800 samples:

```text
65,263 × 800 = 52,210,400
 6,995 × 800 =  5,596,000
72,258 × 800 = 57,806,400
```

The manifest's event count is also internally consistent:

```text
72,258 waveform + 53 service = 72,311 total events
```

## Global index

`/events/index` provides source information not repeated in the waveform
parent:

| Field | Meaning |
| --- | --- |
| `sequence` | Run-global persisted-event sequence |
| `kind` | `4` for waveform |
| `chain`, `node` | Physical board address |
| `qualifier` | Raw event qualifier; `0x08` in run 70 |
| `kind_row` | Segment-local row in `/events/waveform/events` |
| `trigger_id`, `timestamp` | Event identity copied from the descriptor |
| `payload_offset_words` | Payload position within its transport batch |
| `payload_size_words` | Number of 32-bit waveform words |
| `crc_error` | Transport CRC-error flag |

All run-70 waveform events use qualifier `0x08`, the waveform event type.
Their payload size is 800 words, equal to their sample count.

## Parent dataset: `/events/waveform/events`

This is an extensible one-dimensional compound dataset:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `trigger_id` | `uint64` | Board trigger counter |
| `timestamp` | `uint64` | Event timestamp in 8 ns hardware ticks |
| `sample_offset` | `uint64` | First child row in this segment's sample table |
| `sample_count` | `uint32` | Number of child samples |

### Trigger ID

The trigger ID is stored without conversion. Run 70 used:

```text
TrgIdMode = TRIGGER_CNT
```

It therefore counts triggers seen by each board, including triggers which do
not become stored waveform events. It is not a row number, is local to a
board, and can contain large gaps.

### Timestamp

The event timestamp is copied directly from the normalized DT5202 descriptor.
Its unit is an 8 ns hardware clock tick:

```python
event_time_seconds = int(parent["timestamp"]) * 8e-9
```

It is board-relative hardware time, not Unix time. Use the `/run` timestamps
for wall-clock run timing.

### Sample slice

The child slice is:

```python
start = int(parent["sample_offset"])
stop = start + int(parent["sample_count"])
samples = h5["/events/waveform/samples"][start:stop]
```

For run 70, adjacent parents have:

```text
parent 0 -> [0:800]
parent 1 -> [800:1600]
parent 2 -> [1600:2400]
...
```

The slices are contiguous within each file. Rotation occurs only after a
complete event, so an event and its samples are never divided between segment
files.

## Sample dataset: `/events/waveform/samples`

This is another extensible one-dimensional compound dataset:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `parent_row` | `uint64` | Segment-local owner in `waveform/events` |
| `sample_index` | `uint32` | Position within the waveform |
| `high_gain` | `uint16` | Raw 14-bit high-gain lane |
| `low_gain` | `uint16` | Raw 14-bit low-gain lane |
| `digital_probes` | `uint8` | Raw four-bit digital-probe snapshot |

For every run-70 event, `sample_index` runs from 0 through 799.

## Wire-word layout

The decoder interprets every little-endian 32-bit payload word as:

```text
31                         28 27             14 13              0
+----------------------------+----------------+------------------+
| digital probes, 4 bits     | low gain, 14  | high gain, 14    |
+----------------------------+----------------+------------------+
```

Equivalent decoding:

```python
high_gain = word & 0x3fff
low_gain = (word >> 14) & 0x3fff
digital_probes = (word >> 28) & 0x0f
```

The valid raw range for each analog lane is:

```text
0 through 16,383 (0x0000 through 0x3fff)
```

HDF5 uses `uint16` because it has no native 14-bit integer type. The two upper
bits of each stored value are unused.

## Units and conversions

`high_gain` and `low_gain` are raw ADC codes. The HDF5 writer does not convert
them to:

- volts or millivolts;
- deposited energy;
- charge;
- calibrated ADC units; or
- pedestal-subtracted values.

A physical conversion would require independently established ADC transfer
functions, probe routing, gain calibration, and pedestal information.

The `digital_probes` value is also written without conversion. Expanding it
into Boolean bits is a derived convenience:

```python
probe_bits = [
    bool(int(sample["digital_probes"]) & (1 << bit))
    for bit in range(4)
]
```

## First five real parents

The first segment begins with:

| Parent | Trigger ID | Timestamp ticks | Event time | Sample slice |
| ---: | ---: | ---: | ---: | --- |
| 0 | 0 | 7 | 56 ns | `[0:800]` |
| 1 | 54 | 4,094 | 32.752 µs | `[800:1600]` |
| 2 | 95 | 8,265 | 66.120 µs | `[1600:2400]` |
| 3 | 142 | 12,444 | 99.552 µs | `[2400:3200]` |
| 4 | 182 | 16,585 | 132.680 µs | `[3200:4000]` |

The corresponding index rows identify all five as chain 0, node 0.

## Complete linked example: first event

Index row 0 contains:

```text
sequence             = 1
kind                 = 4 (waveform)
chain                = 0
node                 = 0
qualifier            = 0x08
kind_row             = 0
trigger_id           = 0
timestamp            = 7
payload_offset_words = 0
payload_size_words   = 800
crc_error            = 0
```

Waveform parent row 0 contains:

```text
trigger_id    = 0
timestamp     = 7
sample_offset = 0
sample_count  = 800
```

The trigger ID and timestamp agree between index and parent. The parent owns
sample rows `[0:800]`.

Its first twelve samples are:

| Child row | Sample index | HG | LG | Probe decimal | Probe binary |
| ---: | ---: | ---: | ---: | ---: | --- |
| 0 | 0 | 16,383 | 16,383 | 1 | `0001` |
| 1 | 1 | 16,383 | 16,383 | 1 | `0001` |
| 2 | 2 | 16,383 | 16,383 | 1 | `0001` |
| 3 | 3 | 16,383 | 16,383 | 9 | `1001` |
| 4 | 4 | 16,383 | 16,383 | 9 | `1001` |
| 5 | 5 | 16,383 | 16,383 | 13 | `1101` |
| 6 | 6 | 16,383 | 16,383 | 9 | `1001` |
| 7 | 7 | 16,383 | 16,383 | 13 | `1101` |
| 8 | 8 | 16,383 | 16,383 | 5 | `0101` |
| 9 | 9 | 16,383 | 16,383 | 1 | `0001` |
| 10 | 10 | 16,383 | 16,383 | 5 | `0101` |
| 11 | 11 | 16,383 | 16,383 | 5 | `0101` |

`16,383` is positive full scale for a 14-bit unsigned value. The opening
portion of both analog lanes is therefore saturated at the representable
maximum. The file alone does not establish whether this is physical ADC
saturation, an inactive-probe encoding, or another waveform-mode behavior.

Later in the same event:

| Sample index | HG | LG | Probe |
| ---: | ---: | ---: | ---: |
| 380 | 7,706 | 7,746 | 0 |
| 390 | 7,868 | 7,919 | 0 |
| 400 | 8,030 | 8,084 | 0 |
| 410 | 8,201 | 8,269 | 0 |
| 419 | 8,350 | 8,434 | 0 |
| 790 | 14,188 | 14,613 | 0 |
| 791 | 14,204 | 14,625 | 0 |
| 798 | 14,306 | 14,742 | 0 |
| 799 | 14,330 | 14,763 | 0 |

This is a structured, ramp-like trace after the initial full-scale region,
not a constant placeholder waveform.

## Digital-probe interpretation

The four-bit value is authoritative:

```python
value = int(sample["digital_probes"])

bit_0 = bool(value & 0x01)
bit_1 = bool(value & 0x02)
bit_2 = bool(value & 0x04)
bit_3 = bool(value & 0x08)
```

The requested configuration lists signals such as peak HG/LG, hold,
conversion start, data commit, data valid, clock, validation window, T-OR,
and Q-OR as possible digital-probe selections.

However, the complete mapping of the four packed bits to physical internal
signals has not been verified for this real capture. Analysis should retain
the raw nibble and avoid assigning signal names based only on bit position.

## Probe configuration caveat

Run 70 requested:

```text
AnalogProbe0  = OFF
DigitalProbe0 = OFF
ProbeChannel0 = 0

AnalogProbe1  = OFF
DigitalProbe1 = OFF
ProbeChannel1 = 32
```

Despite the `OFF` selections, the hardware transmitted nonconstant analog
lanes and nonzero digital nibbles. This is an important real-data result.
Possible explanations include:

- fixed waveform paths selected by firmware;
- waveform-specific registers not represented by the JANUS text;
- incomplete native waveform-source configuration;
- `OFF` affecting external probe routing rather than payload capture; or
- firmware-specific waveform semantics.

Run 70 proves that data were transmitted, but not which explanation is
correct. The raw values should be preserved until the routing is
hardware-verified.

## Why there is no channel field

A waveform payload contains two packed analog lanes and four digital bits. It
is not a spectroscopy-style list with one record per hit channel. Therefore,
neither the parent nor sample dataset contains a channel number.

`ProbeChannel0 = 0` and `ProbeChannel1 = 32` occur in the requested
configuration, but because both analog probe selections are `OFF`, this is
not sufficient evidence that the stored HG lane is channel 0 and the LG lane
is channel 32. The field names describe the source-confirmed wire lanes, not a
verified per-run channel assignment.

## Sample timing caveat

`sample_index` establishes ordering:

```text
0, 1, 2, ..., 799
```

The decoded HDF5 schema does not store a waveform sampling period. Therefore:

```text
sample_index = 400
```

means “the 401st sample,” not 400 ns, 3.2 µs, or any other physical time.

Do not compute a time axis without independently verifying the DT5202 sample
period for the relevant firmware and waveform mode. This is separate from the
parent event timestamp, whose 8 ns unit is established.

## HDF5 segment rotation

The root attributes show:

```text
run_70.0000.h5:
    segment_index        = 0
    first_event_sequence = 1
    complete             = 1

run_70.0001.h5:
    segment_index        = 1
    first_event_sequence = 65,312
    complete             = 1
```

Global index sequences continue across files, but table row numbers restart.
The first event in segment 1 is:

```text
sequence      = 65,312
kind          = 4
chain         = 3
node          = 0
kind_row      = 0
trigger_id    = 457,107
timestamp     = 1,518,706,729
sample_offset = 0
sample_count  = 800
```

Its first sample also has:

```text
parent_row  = 0
sample_index = 0
```

The meanings of these identifiers are:

| Identifier | Scope |
| --- | --- |
| `index.sequence` | Run-global across ordered segments |
| `index.kind_row` | Local to one segment and event kind |
| `samples.parent_row` | Local to one segment |
| `events.sample_offset` | Local to one segment's sample dataset |
| `samples.sample_index` | Local to one waveform |

Never concatenate sample tables and then apply the original offsets without
adjusting them. A safer approach is to resolve parents and samples while each
segment file is open.

## Trigger gaps and received-event totals

Run-level statistics report:

| Board | Received waveform events | Final trigger ID | Estimated missing triggers |
| --- | ---: | ---: | ---: |
| chain 0, node 0 | 19,113 | 17,861,913 | 17,842,801 |
| chain 1, node 0 | 17,617 | 3,907,953 | 3,890,337 |
| chain 2, node 0 | 18,100 | 79,745,148 | 79,727,049 |
| chain 3, node 0 | 17,428 | 517,721 | 500,294 |

The received counts sum to 72,258. Large trigger-ID gaps are expected to be
visible when `TRIGGER_CNT` counts far more board triggers than the DAQ stores
as waveform events.

These are inferred missing-trigger counts, not proof that the HDF5 writer
dropped those events. Dead time, board acceptance, transport behavior, and
run-control timing can all affect which triggers become received payloads.

## Reading one waveform with Python

Install the required reader packages:

```sh
python -m pip install h5py hdf5plugin
```

Import `hdf5plugin` before opening the compressed file:

```python
from pathlib import Path

import h5py
import hdf5plugin  # noqa: F401; registers HDF5 compression filters


path = Path("pcap/run-70/run_70.0000.h5")

with h5py.File(path, "r") as h5:
    parents = h5["/events/waveform/events"]
    sample_table = h5["/events/waveform/samples"]

    parent_number = 0
    parent = parents[parent_number]

    start = int(parent["sample_offset"])
    stop = start + int(parent["sample_count"])
    samples = sample_table[start:stop]

    print("trigger ID:", int(parent["trigger_id"]))
    print("timestamp ticks:", int(parent["timestamp"]))
    print("event time seconds:", int(parent["timestamp"]) * 8e-9)

    for sample in samples[:12]:
        probes = int(sample["digital_probes"])
        print({
            "sample_index": int(sample["sample_index"]),
            "high_gain": int(sample["high_gain"]),
            "low_gain": int(sample["low_gain"]),
            "digital_probes": probes,
            "probe_bits": [
                bool(probes & (1 << bit))
                for bit in range(4)
            ],
        })
```

## Joining parents to board identities

```python
with h5py.File(path, "r") as h5:
    index = h5["/events/index"][:]
    parents = h5["/events/waveform/events"]

    waveform_sources = index[index["kind"] == 4]

    for source in waveform_sources:
        parent_number = int(source["kind_row"])
        parent = parents[parent_number]

        if int(source["trigger_id"]) != int(parent["trigger_id"]):
            raise ValueError("index/parent trigger ID mismatch")
        if int(source["timestamp"]) != int(parent["timestamp"]):
            raise ValueError("index/parent timestamp mismatch")

        print({
            "sequence": int(source["sequence"]),
            "chain": int(source["chain"]),
            "node": int(source["node"]),
            "parent_row": parent_number,
            "trigger_id": int(parent["trigger_id"]),
        })
```

Do not identify boards using `trigger_id`: trigger counters are board-local.

## Reading every segment safely

```python
from pathlib import Path

import h5py
import hdf5plugin  # noqa: F401


run_directory = Path("pcap/run-70")
segments = sorted(run_directory.glob("run_70.[0-9][0-9][0-9][0-9].h5"))

previous_sequence = None

for segment_path in segments:
    with h5py.File(segment_path, "r") as h5:
        segment_index = int(h5.attrs["segment_index"])
        first_sequence = int(h5.attrs["first_event_sequence"])

        index = h5["/events/index"][:]
        parents = h5["/events/waveform/events"]
        sample_table = h5["/events/waveform/samples"]

        if len(index) and int(index[0]["sequence"]) != first_sequence:
            raise ValueError(f"{segment_path}: first sequence mismatch")

        for source in index[index["kind"] == 4]:
            sequence = int(source["sequence"])
            if previous_sequence is not None and sequence <= previous_sequence:
                raise ValueError("non-increasing global sequence")
            previous_sequence = sequence

            parent_number = int(source["kind_row"])
            parent = parents[parent_number]

            start = int(parent["sample_offset"])
            stop = start + int(parent["sample_count"])
            samples = sample_table[start:stop]

            # Consume or analyze the segment-local sample slice here.
            process = {
                "segment": segment_index,
                "sequence": sequence,
                "chain": int(source["chain"]),
                "node": int(source["node"]),
                "samples": samples,
            }
```

If retaining waveforms after closing a file, copy the sample array instead of
retaining an HDF5 dataset view.

## Building plot arrays

```python
import numpy as np


with h5py.File(path, "r") as h5:
    parent = h5["/events/waveform/events"][0]
    start = int(parent["sample_offset"])
    stop = start + int(parent["sample_count"])
    samples = h5["/events/waveform/samples"][start:stop]

    sample_number = samples["sample_index"].astype(np.uint32)
    high_gain_adc = samples["high_gain"].astype(np.uint16)
    low_gain_adc = samples["low_gain"].astype(np.uint16)
    probe_nibble = samples["digital_probes"].astype(np.uint8)

    probe_bit_0 = (probe_nibble & 0x01) != 0
    probe_bit_1 = (probe_nibble & 0x02) != 0
    probe_bit_2 = (probe_nibble & 0x04) != 0
    probe_bit_3 = (probe_nibble & 0x08) != 0
```

Label the horizontal axis “sample index” until a verified sampling period is
available. Label the analog axes “raw ADC code,” not voltage or energy.

## Validating parent and sample relationships

For very large files, validate parents in batches. This straightforward
version emphasizes the invariants:

```python
import numpy as np


with h5py.File(path, "r") as h5:
    parents = h5["/events/waveform/events"]
    samples = h5["/events/waveform/samples"]

    expected_offset = 0

    for parent_number in range(len(parents)):
        parent = parents[parent_number]
        offset = int(parent["sample_offset"])
        count = int(parent["sample_count"])

        if offset != expected_offset:
            raise ValueError(
                f"parent {parent_number}: offset {offset}, "
                f"expected {expected_offset}"
            )
        if offset > len(samples) or count > len(samples) - offset:
            raise ValueError(
                f"parent {parent_number}: sample slice out of bounds"
            )

        children = samples[offset:offset + count]

        if np.any(children["parent_row"] != parent_number):
            raise ValueError(
                f"parent {parent_number}: incorrect sample ownership"
            )

        expected_indices = np.arange(count, dtype=np.uint32)
        if not np.array_equal(children["sample_index"], expected_indices):
            raise ValueError(
                f"parent {parent_number}: incorrect sample indices"
            )

        if np.any(children["high_gain"] > 0x3fff):
            raise ValueError(
                f"parent {parent_number}: HG exceeds 14 bits"
            )
        if np.any(children["low_gain"] > 0x3fff):
            raise ValueError(
                f"parent {parent_number}: LG exceeds 14 bits"
            )
        if np.any(children["digital_probes"] > 0x0f):
            raise ValueError(
                f"parent {parent_number}: probe value exceeds 4 bits"
            )

        expected_offset += count

    if expected_offset != len(samples):
        raise ValueError("unreferenced sample rows")
```

## Command-line inspection

Show the schemas:

```sh
h5dump -H -g /events/waveform \
  pcap/run-70/run_70.0000.h5
```

Show the first five parents:

```sh
h5dump -d /events/waveform/events -s 0 -c 5 \
  pcap/run-70/run_70.0000.h5
```

Show the first twelve samples:

```sh
h5dump -d /events/waveform/samples -s 0 -c 12 \
  pcap/run-70/run_70.0000.h5
```

Show samples 790 through 799:

```sh
h5dump -d /events/waveform/samples -s 790 -c 10 \
  pcap/run-70/run_70.0000.h5
```

Show segment attributes:

```sh
h5dump -A pcap/run-70/run_70.0000.h5
h5dump -A pcap/run-70/run_70.0001.h5
```

## Histogram artifact

`run_70.histograms.h5` has an empty `/histograms` group. The waveform events
and samples were persisted in the decoded-event segments, but no waveform
histogram was generated. This is expected because the current histogram
artifact represents supported run-wide histogram products, not raw waveform
traces.

## Interpretation summary

- One waveform parent represents one received waveform event.
- Run 70 contains 72,258 waveform parents across two segments.
- Every parent owns exactly 800 samples.
- Each sample contains raw 14-bit HG, raw 14-bit LG, and four probe bits.
- No ADC-to-voltage, energy, pedestal, or gain conversion is applied.
- The first real waveform begins at full scale and then contains a structured
  ramp-like trace.
- Event timestamps use 8 ns hardware ticks.
- Sample indices have no physical time unit in the HDF5 schema.
- The payload contains no explicit channel number.
- Requested probes are `OFF`, but real nonconstant data are present; source
  routing therefore remains hardware-dependent and unresolved.
- `kind_row`, `parent_row`, and sample offsets restart in each segment.
- `/events/index.sequence` remains global across segment files.
