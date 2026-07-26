# Spectroscopy events (decoded-event schema v2)

The decoded-event HDF5 schema version 2 stores spectroscopy data in two
datasets:

```text
/events/index
    |
    | kind = 1, kind_row
    v
/events/spectroscopy/events
    |
    | observation_offset, observation_count
    v
/events/spectroscopy/observations
```

`events` remains a compact, one-row-per-packet table. It preserves efficient
event indexing and lets `/events/index.kind_row` identify a spectroscopy event.
`observations` is the analysis-oriented table: one row contains the event,
source, channel, energy, and timing information needed for most work. An
analysis therefore normally reads only `observations`; it does not need to join
separate energy and timing tables.

Schema v2 replaces the schema-v1
`/events/spectroscopy/energies` and `/events/spectroscopy/timings` datasets.
Files identify the layout with the root attribute `schema_version = 2`.

## `/events/spectroscopy/events`

Each row describes one decoded spectroscopy packet.

| Field | Type | Meaning |
|---|---:|---|
| `trigger_id` | `uint64` | Trigger identifier from the packet |
| `timestamp` | `uint64` | Raw device timestamp; no unit conversion is applied |
| `channel_mask` | `uint64` | Bit `n` is set when channel `n` has energy data |
| `observation_offset` | `uint64` | First associated row in `observations` |
| `relative_timestamp_clock` | `uint32` | Raw relative timestamp clock |
| `time_reference` | `uint32` | Raw time-reference value |
| `observation_count` | `uint32` | Number of associated observation rows |
| `validity` | `uint8` | Packet validity bit mask |

The child slice for parent row `p` is:

```python
event = events[p]
rows = observations[
    event["observation_offset"]:
    event["observation_offset"] + event["observation_count"]
]
```

Offsets are contiguous and every observation belongs to exactly one parent.
`observation_count` is always at least one because an empty event is represented
by a sentinel row.

## `/events/spectroscopy/observations`

An ordinary row represents one channel in one spectroscopy event. Energy and
timing values for the same channel are merged into the same row. A channel that
has only timing data also gets a row.

| Field | Type | Meaning |
|---|---:|---|
| `sequence` | `uint64` | Global event sequence, repeated from `/events/index` |
| `parent_row` | `uint64` | Row in `/events/spectroscopy/events` |
| `trigger_id` | `uint64` | Repeated event trigger ID |
| `timestamp` | `uint64` | Repeated raw device timestamp |
| `relative_timestamp_clock` | `uint32` | Repeated raw relative clock |
| `time_reference` | `uint32` | Repeated raw time reference |
| `toa` | `uint32` | Raw time-of-arrival value; meaningful if `has_timing = 1` |
| `low_gain` | `uint16` | Raw low-gain ADC value; meaningful if `has_energy = 1` and `has_low_gain = 1` |
| `high_gain` | `uint16` | Raw high-gain ADC value; meaningful if `has_energy = 1` and `has_high_gain = 1` |
| `tot` | `uint16` | Raw time-over-threshold value; meaningful if `has_timing = 1` |
| `chain` | `uint8` | Source chain, repeated from `/events/index` |
| `node` | `uint8` | Source node, repeated from `/events/index` |
| `qualifier` | `uint8` | Packet qualifier, repeated from `/events/index` |
| `validity` | `uint8` | Repeated packet validity mask |
| `channel` | `uint8` | Channel number, 0 through 63 |
| `channel_valid` | `uint8` | `1` for a channel row, `0` for an empty-event sentinel |
| `has_energy` | `uint8` | Energy fields belong to this observation |
| `has_low_gain` | `uint8` | Low-gain ADC sample is present |
| `has_high_gain` | `uint8` | High-gain ADC sample is present |
| `discriminator` | `uint8` | Raw discriminator flag |
| `has_timing` | `uint8` | `toa` and `tot` belong to this observation |

All stored numbers are the values decoded by the backend. The HDF5 writer does
not convert timestamp, ADC, ToA, or ToT units. Interpretation of clock ticks
depends on the hardware configuration and firmware.

The repeated fields deliberately make each observation self-contained. They
compress well because adjacent rows usually repeat the same event and source
values. Keeping `parent_row` and the compact parent table still provides exact
event boundaries and an efficient link from the global event index.

### Validity rules

- `channel_valid = 1` means `channel` is in `[0, 63]`.
- At least one of `has_energy` or `has_timing` is set for an ordinary row.
- There is at most one ordinary observation per channel per event.
- `has_energy = 0` means all energy fields and flags are zero.
- `has_timing = 0` means `toa` and `tot` are zero.
- The parent `channel_mask` is reconstructed from rows with `has_energy = 1`.
  Timing-only channels do not set bits in this mask.
- Boolean fields are stored as `uint8` values `0` or `1`.

### Empty-event sentinel

An event with neither energy nor timing data still needs a row so that it is not
lost when an analyst reads only the flat observation dataset. Its only child is
a sentinel with:

```text
channel_valid = 0
has_energy    = 0
has_timing    = 0
```

The repeated event/source fields remain populated. Analysis code that wants
only channel measurements should filter on `channel_valid != 0`.

## Example rows

The following readable example shows one event with energy and timing on
channel 0 and timing only on channel 2:

```text
/events/spectroscopy/events[0]
trigger_id=101 timestamp=5000 channel_mask=0x0000000000000001
observation_offset=0 observation_count=2
relative_timestamp_clock=17 time_reference=9 validity=3

/events/spectroscopy/observations[0]
sequence=1 parent_row=0 chain=2 node=4 qualifier=19
trigger_id=101 timestamp=5000 channel=0 channel_valid=1
has_energy=1 low_gain=1200 high_gain=800
has_low_gain=1 has_high_gain=1 discriminator=0
has_timing=1 toa=300 tot=25

/events/spectroscopy/observations[1]
sequence=1 parent_row=0 chain=2 node=4 qualifier=19
trigger_id=101 timestamp=5000 channel=2 channel_valid=1
has_energy=0 low_gain=0 high_gain=0
has_low_gain=0 has_high_gain=0 discriminator=0
has_timing=1 toa=310 tot=26
```

The parent mask contains only bit 0: channel 2 is timing-only.

## Reading with Python and h5py

### Read the analysis table directly

```python
import h5py

with h5py.File("run_42.0000.h5", "r") as h5:
    if int(h5.attrs["schema_version"]) != 2:
        raise ValueError("this reader expects decoded-event schema v2")

    observations = h5["/events/spectroscopy/observations"]
    rows = observations[:]

    channel_rows = rows[rows["channel_valid"] != 0]
    energy_rows = channel_rows[channel_rows["has_energy"] != 0]
    timing_rows = channel_rows[channel_rows["has_timing"] != 0]

    for row in energy_rows[:10]:
        print(
            int(row["sequence"]),
            int(row["chain"]),
            int(row["node"]),
            int(row["channel"]),
            int(row["low_gain"]),
            int(row["high_gain"]),
        )
```

### Select one event

```python
import h5py

with h5py.File("run_42.0000.h5", "r") as h5:
    events = h5["/events/spectroscopy/events"]
    observations = h5["/events/spectroscopy/observations"]

    parent_row = 0
    event = events[parent_row]
    begin = int(event["observation_offset"])
    end = begin + int(event["observation_count"])
    rows = observations[begin:end]

    assert (rows["parent_row"] == parent_row).all()
    print(event)
    print(rows)
```

### Find spectroscopy events through the global index

```python
import h5py

SPECTROSCOPY = 1

with h5py.File("run_42.0000.h5", "r") as h5:
    index = h5["/events/index"]
    events = h5["/events/spectroscopy/events"]
    observations = h5["/events/spectroscopy/observations"]

    for source in index[index["kind"] == SPECTROSCOPY]:
        parent = int(source["kind_row"])
        event = events[parent]
        begin = int(event["observation_offset"])
        end = begin + int(event["observation_count"])
        rows = observations[begin:end]
        print(int(source["sequence"]), source["chain"], source["node"], rows)
```

### Build a pandas DataFrame

```python
import h5py
import pandas as pd

with h5py.File("run_42.0000.h5", "r") as h5:
    frame = pd.DataFrame.from_records(
        h5["/events/spectroscopy/observations"][:]
    )

measurements = frame[frame.channel_valid != 0]
energies = measurements[measurements.has_energy != 0]
timings = measurements[measurements.has_timing != 0]
```

No merge is needed to associate energy and timing measurements from the same
event and channel.

## Validation

The backend validates schema-v2 files when finalizing them. The independent
h5py validator performs the same structural checks:

```bash
python3 scripts/validate-hdf5.py run_42.0000.h5
```

For direct inspection:

```bash
h5dump -A run_42.0000.h5
h5dump -H -d /events/spectroscopy/events run_42.0000.h5
h5dump -H -d /events/spectroscopy/observations run_42.0000.h5
```
