# HDF5 `/configuration` group

This document describes every dataset under `/configuration` in the
decoded-event HDF5 format. It uses
`pcap/run-54/run_54.0000.h5` as a concrete, capture-verified example and
includes Python code for reading every representation without truncation.

The central point is that “the configuration” is not one value. The file
preserves operator intent, the DAQ's executable interpretation, and an audit
that connects the two.

## Requested, plan, effective, and audit

The configuration lifecycle is:

```text
byte-exact requested JANUS document
        |
        | parse assignments, units, scopes, defaults, and overrides
        v
one ConfigurationPlan per board
        |
        | pack FPGA, Citiroc, HV, and pedestal operations
        v
effective_json plus typed effective tables
        |
        | account for every requested assignment
        v
audit_json: applied, inactive, or rejected, with reasons
```

These terms have precise meanings.

### Requested configuration

The requested configuration is the exact text supplied by the operator. It
preserves comments, whitespace, spelling, units, assignment order, global
values, indexed overrides, line endings, and final-newline state.

It answers:

> What exactly did the operator submit?

It does not resolve defaults or tell an analyst whether a request was active.

### Configuration plan

A `ConfigurationPlan` is an in-memory, normalized, per-board recipe generated
from the requested document. It contains:

```text
ConfigurationPlan
|-- Board
|-- Writes[]       ordered FPGA register writes
|-- Citiroc[2]     complete configuration of both front-end chips
|-- HV             high-level HV plan and ordered peripheral transactions
|-- Pedestal       pedestal, zero-suppression, and calibration plan
|-- Deferred[]     settings awaiting a later planning stage
`-- Inactive[]     valid settings that intentionally have no effect
```

Planning resolves:

- global values and board/channel overrides;
- supported defaults;
- named choices and boolean values;
- physical units and hardware clock units;
- range checks;
- firmware-dependent behavior;
- channel and discriminator masks;
- complete Citiroc programming streams;
- ordered FPGA writes;
- ordered HV peripheral transactions;
- pedestal thresholds and protected-flash calibration; and
- settings made irrelevant by another selected mode.

A plan is the DAQ's executable interpretation, not another copy of the
operator's text.

### Effective configuration

The effective configuration is the persisted plan selected for the run after
planning has resolved defaults, overrides, units, packing, conditional
behavior, and calibration.

It is stored as:

```text
/configuration/effective_json
/configuration/effective/*
```

`effective_json` is the lossless serialization of all per-board plans. The
typed datasets under `effective/` are efficient, fixed-width views derived
from the same plans.

“Effective” primarily means “resolved and used by the DAQ.” It does not mean
that every value is a live measurement or a complete post-write hardware
snapshot. Ordered FPGA writes can be validated against register readback, and
configuration application must succeed before acquisition, but live HV and
temperature measurements belong to service events rather than this group.

### Configuration audit

The audit accounts for every parsed assignment and explains how it contributed
to the run:

```text
/configuration/audit_json
```

Every setting is classified as:

| Status | Meaning |
| --- | --- |
| `applied` | The responsible hardware or software subsystem honored the request |
| `inactive` | The request was valid but intentionally had no effect under the selected mode |
| `rejected` | The request could not be represented or supported safely/correctly |

A valid audit may contain inactive settings. It cannot contain a rejected
setting. Run 54 has:

```text
valid    = true
applied  = 69
inactive = 34
rejected = 0
```

The short distinction is:

```text
requested = operator intent
plan      = normalized executable recipe
effective = persisted plan selected for the run
audit     = explanation connecting intent to effective behavior
```

## Group hierarchy

Run 54 contains:

```text
/configuration
|-- requested_janus
|-- audit_json
|-- effective_json
`-- effective
    |-- boards
    |-- channels
    |-- citiroc_chips
    |-- citiroc_stream_words
    |-- fpga_writes
    |-- hv_plans
    |-- hv_transactions
    |-- pedestal_plans
    `-- pedestal_channels
```

The current extents are:

| Dataset | Extent | Content |
| --- | ---: | --- |
| `/configuration/requested_janus` | 14,822 bytes | Byte-exact requested text |
| `/configuration/audit_json` | 20,662 bytes | Versioned audit report |
| `/configuration/effective_json` | 115,333 bytes | Four complete board plans |
| `/configuration/effective/boards` | 4 rows | Hardware identity and mapping |
| `/configuration/effective/channels` | 256 rows | Per-board/per-channel effective values |
| `/configuration/effective/citiroc_chips` | 8 rows | Per-board/per-chip common values |
| `/configuration/effective/citiroc_stream_words` | 288 rows | Packed 1,144-bit chip streams |
| `/configuration/effective/fpga_writes` | 1,416 rows | Ordered register-write plans |
| `/configuration/effective/hv_plans` | 4 rows | High-level HV plans |
| `/configuration/effective/hv_transactions` | 60 rows | Ordered low-level HV operations |
| `/configuration/effective/pedestal_plans` | 4 rows | High-level pedestal plans |
| `/configuration/effective/pedestal_channels` | 256 rows | Effective thresholds and calibration |

The three top-level datasets are one-dimensional `uint8` byte arrays. The
datasets below `effective/` are one-dimensional compound tables with
fixed-width little-endian fields.

## `/configuration/requested_janus`

This dataset is the primary evidence of operator intent. It is not a native
HDF5 string; its 14,822 bytes must be decoded as UTF-8.

The run-54 document begins:

```text
# ******************************************************************************************
# params File generated by Python
# ******************************************************************************************
# ------------------------------------------------------------------------------------------
# Connect
# ------------------------------------------------------------------------------------------
Open[0] usb:172.16.0.11:tdl:0:0
Open[1] usb:172.16.0.11:tdl:1:0
Open[2] usb:172.16.0.11:tdl:2:0
Open[3] usb:172.16.0.11:tdl:3:0
```

Assignments without an index are global. Board-indexed assignments override a
global value for one board. Board/channel-indexed assignments override a
value for one physical channel. The exact source remains necessary even when
the plan is available because normalized plans do not preserve comments,
original units, textual spelling, or overwritten assignments.

The run-54 requested-document SHA-256 recorded in `/run` metadata is:

```text
c717f330add4f3af908ae2ef0dfbdd415e32fc940a0a0c9c1696272e259bd6a1
```

## `/configuration/effective_json`

This is a JSON array containing four complete `ConfigurationPlan` objects.
The array has one plan for each logical board.

Every plan has these keys:

```text
Board
Writes
Citiroc
HV
Pedestal
Deferred
Inactive
```

Board 0 has:

```text
Board          = 0
Writes         = 354 ordered FPGA writes
Citiroc        = 2 complete chip configurations
Deferred       = empty
```

An empty `Deferred` list is significant. A hardware-owned setting still
deferred when the audit is built is rejected because the DAQ cannot prove how
it will be handled.

Board 0 records these inactive plan entries:

| Name | Reason |
| --- | --- |
| `ZS_Threshold_LG` | JANUS applies energy zero suppression only in spectroscopy mode |
| `ZS_Threshold_HG` | JANUS applies energy zero suppression only in spectroscopy mode |
| `TestPulseAmplitude` | Test-pulse source is off; effective DAC is zero |
| `TestPulseDestination` | Test-pulse source is off |
| `TestPulsePreamp` | Test-pulse source is off |
| `ProbeChannel0` | Analog probe 0 is off |
| `ProbeChannel1` | Analog probe 1 is off |

The effective JSON also retains heterogeneous details that do not fit cleanly
in a flat table, including calibration source strings and the inactive/deferred
lists.

The run-54 effective-plan SHA-256 is:

```text
84f1f8b9da6eb49d9a231b4268a40fc57e092814543e123002bcc7c0064eaf06
```

## `/configuration/audit_json`

The audit root is:

```json
{
  "schema_version": 1,
  "valid": true,
  "boards": [
    "... board evidence ..."
  ],
  "settings": [
    "... one entry per parsed assignment ..."
  ]
}
```

### Audit board evidence

Run 54 records:

| Board | FPGA firmware | HV firmware raw | HV version | Available |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 2,703,427,336 | 1,087,163,597 | 6.4 | 1 |
| 1 | 2,703,427,336 | 1,087,163,597 | 6.4 | 1 |
| 2 | 2,703,427,336 | 1,087,163,597 | 6.4 | 1 |
| 3 | 2,703,427,336 | 1,087,163,597 | 6.4 | 1 |

Firmware evidence is included because some packing and supported features can
depend on the installed firmware.

### Audit setting fields

| Field | Meaning |
| --- | --- |
| `name` | JANUS setting name |
| `index` | Optional board index |
| `line` | Source line in the requested document |
| `owner` | Responsible subsystem |
| `requested` | Original requested value, trimmed for audit display |
| `status` | `applied`, `inactive`, or `rejected` |
| `effective` | Effective values and optional target boards |
| `reason` | Explanation for inactivity, rejection, or partial targeting |

Ownership values include:

| Owner | Responsibility |
| --- | --- |
| `hardware` | FPGA, Citiroc, HV, pedestal, and device configuration |
| `topology` | Connections and board addressing |
| `run_control` | Run lifecycle and stop conditions |
| `storage` | Persistence/output choices |
| `analysis` | Analysis and histogram-related behavior |

An applied setting can target all boards:

```json
{
  "name": "GainSelect",
  "owner": "hardware",
  "requested": "BOTH",
  "status": "applied",
  "effective": [
    {"board": 0, "value": "BOTH"},
    {"board": 1, "value": "BOTH"},
    {"board": 2, "value": "BOTH"},
    {"board": 3, "value": "BOTH"}
  ]
}
```

The exact register bits resulting from this setting are found in the effective
plan. The audit intentionally remains at the operator-facing setting level.

Representative run-54 inactive entries are:

```json
{
  "name": "TstampCoincWindow",
  "line": 34,
  "owner": "run_control",
  "requested": "0",
  "status": "inactive",
  "reason": "event building is disabled"
}
```

```json
{
  "name": "PresetCounts",
  "line": 36,
  "owner": "run_control",
  "requested": "1000",
  "status": "inactive",
  "reason": "stop mode does not use preset counts"
}
```

```json
{
  "name": "DataAnalysis",
  "line": 42,
  "owner": "analysis",
  "requested": "ALL",
  "status": "inactive",
  "reason": "Phase 1 persists decoded events without an online analysis pipeline"
}
```

```json
{
  "name": "DataFilePath",
  "line": 43,
  "owner": "storage",
  "requested": "..\\..\\..\\Documents\\CITIROCDATA\\smallPET\\4 crystals",
  "status": "inactive",
  "reason": "the runstore parent directory is supplied by the service, not the imported JANUS path"
}
```

The audit SHA-256 is:

```text
52deeefba4d6769cbb13fe0e0b0c5fec6789a058fb12b3c8d4bd6d1eb8fb048c
```

## `/configuration/effective/boards`

One row binds every logical board plan to discovered hardware:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `board` | `uint32` | Logical board index |
| `chain` | `uint16` | DT5215 chain |
| `node` | `uint16` | Node on the chain |
| `product_id` | `uint32` | Hardware product ID |
| `firmware_revision` | `uint32` | Raw firmware revision |
| `acquisition_state` | `uint32` | State observed during configuration/discovery |

Run-54 rows are:

| Board | Chain | Node | Product ID | Firmware | State |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 0 | 0 | 64,883 | 2,703,427,336 | 9 |
| 1 | 1 | 0 | 64,138 | 2,703,427,336 | 9 |
| 2 | 2 | 0 | 64,885 | 2,703,427,336 | 9 |
| 3 | 3 | 0 | 64,884 | 2,703,427,336 | 9 |

## `/configuration/effective/channels`

There is one row per physical board/channel: 4 boards times 64 channels gives
256 rows.

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `board` | `uint32` | Logical board |
| `channel` | `uint8` | Physical channel 0 through 63 |
| `chip` | `uint8` | Citiroc chip 0 or 1 |
| `chip_channel` | `uint8` | Channel 0 through 31 within the chip |
| `readout_enabled` | `uint8` | Included in readout |
| `qd_enabled` | `uint8` | Charge discriminator enabled |
| `td_enabled` | `uint8` | Time discriminator enabled |
| `qd_fine` | `uint8` | Fine charge-threshold code |
| `td_fine` | `uint8` | Fine time-threshold code |
| `high_gain` | `uint8` | High-gain preamplifier code |
| `low_gain` | `uint8` | Low-gain preamplifier code |
| `hv_adjustment` | `uint16` | Per-channel HV adjustment code |
| `calibrate_high_gain` | `uint8` | High-gain calibration flag |
| `calibrate_low_gain` | `uint8` | Low-gain calibration flag |
| `preamplifier_disabled` | `uint8` | Preamplifier disable flag |

Run-54 board 0, channel 0 is:

```text
board                  = 0
channel                = 0
chip                   = 0
chip_channel           = 0
readout_enabled        = 1
qd_enabled             = 1
td_enabled             = 1
qd_fine                = 0
td_fine                = 0
high_gain              = 55
low_gain               = 55
hv_adjustment          = 256
calibrate_high_gain    = 0
calibrate_low_gain     = 0
preamplifier_disabled  = 0
```

Most numeric values are hardware codes, not calibrated physical quantities.

## `/configuration/effective/citiroc_chips`

There are two chip rows per board and eight rows total.

| Field | Meaning |
| --- | --- |
| `board`, `chip` | Target hardware |
| `discriminator_mask` | 32-channel discriminator mask |
| `charge_coarse_threshold` | Common charge threshold code |
| `time_coarse_threshold` | Common time threshold code |
| `low_shaping_time_code` | Low-gain shaping code |
| `high_shaping_time_code` | High-gain shaping code |
| `fast_shaper_on_low_gain` | Fast-shaper input selection |
| `enable_input_dac` | Input DAC enable |
| `input_dac_reference_45v` | Input DAC reference selection |
| `enable_digital_output` | Digital output enable |
| `enable_or32` | OR32 enable |
| `enable_open_collector_or32` | Open-collector OR32 enable |
| `negative_trigger_polarity` | Trigger polarity |
| `enable_open_collector_time_or` | Open-collector time-OR enable |
| `enable_channel_triggers` | Channel-trigger output enable |

Run-54 board 0, chip 0 begins:

```text
discriminator_mask       = 0xffffffff
charge_coarse_threshold  = 250
time_coarse_threshold    = 181
low_shaping_time_code    = 0
high_shaping_time_code   = 0
fast_shaper_on_low_gain  = 1
```

The shaping fields are codes. Physical shaping times require the applicable
Citiroc mapping.

## `/configuration/effective/citiroc_stream_words`

This table preserves the exact packed Citiroc programming streams:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `board` | `uint32` | Target board |
| `chip` | `uint8` | Target chip |
| `word_index` | `uint8` | Position in packed stream |
| `bit_count` | `uint16` | Valid total stream length, 1,144 bits |
| `word` | `uint32` | Packed word |

There are:

```text
4 boards * 2 chips * 36 words = 288 rows
```

Run-54 first row:

```text
board      = 0
chip       = 0
word_index = 0
bit_count  = 1144
word       = 0x00000000
```

The stream is authoritative hardware-facing evidence. The expanded chip and
channel tables are more convenient to query but do not replace the packed
bits.

## `/configuration/effective/fpga_writes`

This table stores every planned FPGA register write in order:

| Field | HDF5 type | Meaning |
| --- | --- | --- |
| `board` | `uint32` | Target board |
| `ordinal` | `uint32` | Write order |
| `address` | `uint32` | Register address |
| `value` | `uint32` | Raw value |

Run-54 first row:

```text
board    = 0
ordinal  = 0
address  = 0x01000100
value    = 0xffffffff
```

There are 354 writes per board and 1,416 total. Order must be preserved because
one register can be written more than once. For final register readback, the
last planned value at an address is the expected value; intermediate writes
remain important evidence of the programming sequence.

## `/configuration/effective/hv_plans`

One high-level HV plan is stored per board:

| Field | Unit or meaning |
| --- | --- |
| `board` | Logical board |
| `voltage_v` | Volts |
| `current_limit_ma` | Milliamperes |
| `temperature_feedback` | Boolean |
| `feedback_mv_per_c` | Millivolts per degree Celsius |
| `coefficient_0..2` | Temperature model coefficients |

Run-54 board 0:

```text
voltage_v            = 45.4
current_limit_ma     = 1
temperature_feedback = 0
feedback_mv_per_c    = 35
coefficient_0        = 0
coefficient_1        = 50
coefficient_2        = 0
```

This is a configuration plan, not measured HV telemetry.

## `/configuration/effective/hv_transactions`

The high-level HV plan is converted into ordered peripheral transactions:

| Field | Meaning |
| --- | --- |
| `board` | Target board |
| `ordinal` | Transaction order |
| `register` | HV peripheral register |
| `data_type` | Peripheral transaction encoding |
| `data` | Raw encoded data |

Run-54 first transaction:

```text
board     = 0
ordinal   = 0
register  = 30
data_type = 2
data      = 1
```

There are 15 transactions per board and 60 total.

The distinction is:

```text
hv_plans        = physical, analysis-friendly intent
hv_transactions = exact ordered programming recipe
```

## `/configuration/effective/pedestal_plans`

One high-level pedestal plan is stored per board:

| Field | Meaning |
| --- | --- |
| `board` | Logical board |
| `common` | Common pedestal setting |
| `acquisition_mode` | Effective mode code |
| `zero_suppress_low_gain` | Default low-gain threshold |
| `zero_suppress_high_gain` | Default high-gain threshold |
| `per_channel` | Per-channel thresholds are active |
| `calibration_present` | Calibration values are available |

Run-54 board 0:

```text
common                    = 50
acquisition_mode          = 3
zero_suppress_low_gain    = 50
zero_suppress_high_gain   = 50
per_channel               = 1
calibration_present       = 1
```

## `/configuration/effective/pedestal_channels`

There is one row per board/channel and 256 total:

| Field | Meaning |
| --- | --- |
| `board`, `channel` | Hardware coordinate |
| `zero_suppress_low_gain` | Effective low-gain threshold |
| `zero_suppress_high_gain` | Effective high-gain threshold |
| `calibration_present` | Calibration values exist |
| `low_gain_pedestal` | Low-gain pedestal code |
| `high_gain_pedestal` | High-gain pedestal code |

Run-54 board 0, channel 0:

```text
zero_suppress_low_gain   = 50
zero_suppress_high_gain  = 50
calibration_present      = 1
low_gain_pedestal        = 162
high_gain_pedestal       = 171
```

The corresponding source retained in `effective_json` is:

```text
DT5202 protected flash page 4 at chain 0 node 0
```

The typed table preserves queryable numbers; the JSON preserves heterogeneous
provenance.

## Which representation should an analysis use?

| Question | Best source |
| --- | --- |
| What exactly did the operator submit? | `requested_janus` |
| How was each submitted assignment handled? | `audit_json` |
| What complete board plans did the DAQ derive? | `effective_json` |
| What board was connected to each chain/node? | `effective/boards` |
| What channel values are convenient to query? | `effective/channels` |
| What exact Citiroc bits were generated? | `effective/citiroc_stream_words` |
| What FPGA operations were planned and in what order? | `effective/fpga_writes` |
| What high-level HV values were selected? | `effective/hv_plans` |
| What exact HV operations were generated? | `effective/hv_transactions` |
| What pedestal/calibration values were used? | `effective/pedestal_*` |
| What voltage/current was measured during acquisition? | Service events, not `/configuration` |

Storing only the requested document would lose defaults, overrides, packing,
calibration, and inactive behavior. Storing only the writes would lose
operator intent and physical units. Storing both without an audit would leave
no explicit explanation of why a request did or did not take effect.

## Reading everything with Python

The run-54 effective tables use the same Blosc/LZ4 compression selected for
the event file. The Python HDF5 environment must have filter ID 32001
available. One common setup uses `hdf5plugin`:

```python
import hdf5plugin  # Registers HDF5 compression filters before h5py opens data.
import h5py
```

The exact plugin-loading mechanism depends on the analysis environment. The
three top-level byte datasets are not compressed, but reading the typed
effective tables requires the filter.

### Read the requested text and both JSON documents

```python
from pathlib import Path
import json

import h5py
import hdf5plugin  # noqa: F401


def read_utf8_bytes(h5: h5py.File, path: str) -> str:
    dataset = h5[path]
    if dataset.ndim != 1:
        raise ValueError(f"{path} has shape {dataset.shape}, expected 1-D")
    if dataset.dtype.kind != "u" or dataset.dtype.itemsize != 1:
        raise TypeError(f"{path} is not a uint8 byte dataset")
    return bytes(dataset[:]).decode("utf-8")


path = Path("pcap/run-54/run_54.0000.h5")

with h5py.File(path, "r") as h5:
    requested_text = read_utf8_bytes(
        h5, "/configuration/requested_janus"
    )
    audit = json.loads(
        read_utf8_bytes(h5, "/configuration/audit_json")
    )
    effective = json.loads(
        read_utf8_bytes(h5, "/configuration/effective_json")
    )

print("REQUESTED CONFIGURATION")
print(requested_text)

print("AUDIT")
print(json.dumps(audit, indent=2, sort_keys=True))

print("EFFECTIVE PLANS")
print(json.dumps(effective, indent=2, sort_keys=True))
```

This prints all 14,822 requested bytes, all 103 audit entries, and every field
of all four board plans without truncation.

### Read every typed table

```python
from pathlib import Path

import h5py
import hdf5plugin  # noqa: F401


path = Path("pcap/run-54/run_54.0000.h5")
base = "/configuration/effective"
table_names = [
    "boards",
    "channels",
    "citiroc_chips",
    "citiroc_stream_words",
    "fpga_writes",
    "hv_plans",
    "hv_transactions",
    "pedestal_plans",
    "pedestal_channels",
]

with h5py.File(path, "r") as h5:
    for name in table_names:
        dataset = h5[f"{base}/{name}"]
        print(f"\n{name}: shape={dataset.shape}, dtype={dataset.dtype}")
        rows = dataset[:]
        for row_number, row in enumerate(rows):
            values = {
                field: row[field].item()
                for field in dataset.dtype.names
            }
            print(row_number, values)
```

This reads and prints every row and field, including all 1,416 FPGA writes,
288 packed Citiroc words, 60 HV transactions, and 512 combined channel and
pedestal-channel rows.

### Convert a compound table to ordinary dictionaries

```python
def table_as_dicts(dataset):
    if dataset.dtype.names is None:
        raise TypeError(f"{dataset.name} is not a compound table")
    result = []
    for row in dataset[:]:
        result.append({
            field: row[field].item()
            for field in dataset.dtype.names
        })
    return result


with h5py.File(path, "r") as h5:
    channels = table_as_dicts(
        h5["/configuration/effective/channels"]
    )

board_0_channel_0 = next(
    row for row in channels
    if row["board"] == 0 and row["channel"] == 0
)
print(board_0_channel_0)
```

### Summarize the audit

```python
from collections import Counter, defaultdict


status_counts = Counter(
    setting["status"] for setting in audit["settings"]
)
owner_counts = defaultdict(Counter)

for setting in audit["settings"]:
    owner_counts[setting["owner"]][setting["status"]] += 1

print("valid:", audit["valid"])
print("status counts:", dict(status_counts))

for owner, counts in sorted(owner_counts.items()):
    print(owner, dict(counts))

print("\nINACTIVE OR REJECTED")
for setting in audit["settings"]:
    if setting["status"] != "applied":
        print(
            f"line={setting['line']} "
            f"name={setting['name']} "
            f"owner={setting['owner']} "
            f"requested={setting['requested']!r} "
            f"status={setting['status']} "
            f"reason={setting.get('reason', '')}"
        )
```

### Reconstruct each packed Citiroc stream

```python
with h5py.File(path, "r") as h5:
    rows = h5[
        "/configuration/effective/citiroc_stream_words"
    ][:]

for board in range(4):
    for chip in range(2):
        selected = [
            row for row in rows
            if int(row["board"]) == board
            and int(row["chip"]) == chip
        ]
        selected.sort(key=lambda row: int(row["word_index"]))

        if len(selected) != 36:
            raise ValueError(
                f"board {board} chip {chip}: "
                f"expected 36 words, got {len(selected)}"
            )

        indices = [int(row["word_index"]) for row in selected]
        if indices != list(range(36)):
            raise ValueError(
                f"board {board} chip {chip}: non-contiguous indices"
            )

        bit_counts = {int(row["bit_count"]) for row in selected}
        if bit_counts != {1144}:
            raise ValueError(
                f"board {board} chip {chip}: "
                f"unexpected bit counts {bit_counts}"
            )

        words = [int(row["word"]) for row in selected]
        print(
            f"board={board} chip={chip} "
            f"words={[f'0x{word:08x}' for word in words]}"
        )
```

### Preserve and inspect FPGA write order

```python
with h5py.File(path, "r") as h5:
    writes = h5["/configuration/effective/fpga_writes"][:]

for board in range(4):
    selected = [
        row for row in writes
        if int(row["board"]) == board
    ]
    selected.sort(key=lambda row: int(row["ordinal"]))

    ordinals = [int(row["ordinal"]) for row in selected]
    if ordinals != list(range(len(selected))):
        raise ValueError(f"board {board}: non-contiguous write order")

    print(f"\nBOARD {board}")
    for row in selected:
        print(
            f"{int(row['ordinal']):4d} "
            f"address=0x{int(row['address']):08x} "
            f"value=0x{int(row['value']):08x}"
        )

    # Expected final readback value at each address:
    final_values = {}
    for row in selected:
        final_values[int(row["address"])] = int(row["value"])
```

Do not replace the ordered table with `final_values` when preserving evidence;
that dictionary intentionally loses repeated/intermediate writes.

### Join channel settings with pedestal calibration

```python
with h5py.File(path, "r") as h5:
    channels = h5["/configuration/effective/channels"][:]
    pedestals = h5[
        "/configuration/effective/pedestal_channels"
    ][:]

channel_index = {
    (int(row["board"]), int(row["channel"])): row
    for row in channels
}
pedestal_index = {
    (int(row["board"]), int(row["channel"])): row
    for row in pedestals
}

if channel_index.keys() != pedestal_index.keys():
    raise ValueError("channel and pedestal coordinates differ")

for coordinate in sorted(channel_index):
    channel = channel_index[coordinate]
    pedestal = pedestal_index[coordinate]
    print(
        coordinate,
        "HG gain", int(channel["high_gain"]),
        "LG gain", int(channel["low_gain"]),
        "HG pedestal", int(pedestal["high_gain_pedestal"]),
        "LG pedestal", int(pedestal["low_gain_pedestal"]),
    )
```

### Cross-check table cardinalities and coordinates

```python
with h5py.File(path, "r") as h5:
    effective_group = h5["/configuration/effective"]

    expected_rows = {
        "boards": 4,
        "channels": 4 * 64,
        "citiroc_chips": 4 * 2,
        "citiroc_stream_words": 4 * 2 * 36,
        "fpga_writes": 4 * 354,
        "hv_plans": 4,
        "hv_transactions": 4 * 15,
        "pedestal_plans": 4,
        "pedestal_channels": 4 * 64,
    }

    for name, expected in expected_rows.items():
        actual = int(effective_group[name].shape[0])
        if actual != expected:
            raise ValueError(
                f"{name}: expected {expected} rows, got {actual}"
            )

    board_ids = {
        int(row["board"])
        for row in effective_group["boards"][:]
    }
    if board_ids != {0, 1, 2, 3}:
        raise ValueError(f"unexpected board IDs: {board_ids}")

    channel_coordinates = {
        (int(row["board"]), int(row["channel"]))
        for row in effective_group["channels"][:]
    }
    expected_coordinates = {
        (board, channel)
        for board in range(4)
        for channel in range(64)
    }
    if channel_coordinates != expected_coordinates:
        raise ValueError("effective channel coverage is incomplete")
```

The fixed cardinalities above describe run 54's four-board topology. A generic
reader should derive expected board coordinates from `effective/boards` rather
than hard-code four boards.

### Verify stored configuration hashes

The configuration hashes are recorded in `/run/metadata_json` and
`/run/manifest_json`. Hash verification must use the same canonical input used
by the writer. The requested document is byte-exact and can be checked
directly:

```python
import hashlib
import json

import h5py


with h5py.File(path, "r") as h5:
    requested_bytes = bytes(
        h5["/configuration/requested_janus"][:]
    )
    run_metadata = json.loads(
        bytes(h5["/run/metadata_json"][:]).decode("utf-8")
    )

expected = run_metadata["configuration_identity"][
    "requested_configuration_sha256"
]
actual = hashlib.sha256(requested_bytes).hexdigest()

if actual != expected:
    raise ValueError(
        f"requested configuration hash mismatch: "
        f"stored={expected}, calculated={actual}"
    )
```

The effective and audit hashes require the writer's canonical JSON
serialization rules; reformatting parsed JSON with arbitrary indentation or
key sorting changes its bytes and therefore changes a byte-level hash.

## Reading with command-line HDF5 tools

Extract the complete byte datasets:

```sh
h5dump -d /configuration/requested_janus \
  -o requested-janus.txt -b LE \
  pcap/run-54/run_54.0000.h5

h5dump -d /configuration/audit_json \
  -o configuration-audit.json -b LE \
  pcap/run-54/run_54.0000.h5

h5dump -d /configuration/effective_json \
  -o effective-configuration.json -b LE \
  pcap/run-54/run_54.0000.h5
```

Pretty-print the JSON:

```sh
jq . configuration-audit.json
jq . effective-configuration.json
```

Inspect a typed table:

```sh
h5dump -d /configuration/effective/channels \
  -s 0 -c 4 \
  pcap/run-54/run_54.0000.h5
```

Show only the physical schema:

```sh
h5dump -H -g /configuration \
  pcap/run-54/run_54.0000.h5
```

## Interpretation cautions

- Do not treat requested text as proof that a setting took effect; consult the
  audit and effective plan.
- Do not treat `inactive` as malformed configuration. It means the setting was
  valid but irrelevant under the selected mode.
- Do not treat an effective plan as live telemetry.
- Do not discard ordered writes or transactions by converting them only into
  final-value maps.
- Do not infer physical units for hardware codes unless the field explicitly
  names a unit or the appropriate hardware mapping is known.
- Do not regenerate operator intent from effective tables; normalization loses
  textual provenance.
- Do not use expanded convenience tables as a replacement for packed Citiroc
  streams when exact hardware-facing evidence is required.
- Do not assume every future run has four boards. Use the stored board table.

Together, the requested source, effective plans, and audit provide a
reproducible explanation of both intent and execution:

```text
requested: what was asked
effective: what the DAQ resolved and used
audit:     why every request did or did not contribute
```
