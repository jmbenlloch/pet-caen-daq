# Read configuration and telemetry from saved data

Use the saved configuration to recover acquisition settings, and service telemetry
to recover measured temperatures, voltage and current. This page maps the columns
in `CITIROC_Detailed_Measurements_Oct26_Logbook.ods` to the output files and provides
a reader you can run on your own run or scan directory.

The normal-run examples were checked against completed runs **26 and 34 from
October 22, 2026**. Those files were written by revision `716d093`, which does not include the new
scan evidence writer. The updated scan examples were checked using
the merged production code and the TCP simulator, not a new hardware measurement.

## Start with the directory and manifest

An ordinary run has `manifest.json`, one or more numbered event files such as
`run_34.0000.h5`, and optionally `run_34.histograms.h5`. Staircase scans use
`scan-N/staircase.h5`; hold-delay scans use `hold-scan-N/hold-delay.h5`. Both also
save `manifest.json` and `points.jsonl`. Updated scans add `evidence.jsonl`.

`manifest.json` identifies the run, UTC start/end, completion state and artifacts.
For normal runs it also includes configuration and software/hardware identity.
Read every numbered HDF5 segment if the run rotated; the companion histogram
file is not an event segment and has no temperature time series.

| Information | Normal run | Updated scan |
| --- | --- | --- |
| Imported settings | `manifest.json` → `requested_configuration`; HDF5 `/configuration/requested_janus` | `evidence.jsonl` → `configuration.requested_assignments` on the `configuration` record |
| Resolved settings and application status | Manifest `configuration_audit`, `effective_configuration`; HDF5 `/configuration/audit_json`, `/configuration/effective/…` | `configuration.configuration_audit`, `configuration.effective_configuration`; selected board's `baseline_plan` on the `baseline` record |
| Measured telemetry | HDF5 `/events/service/events`, joined through `/events/index` | HDF5 `/telemetry/samples`; full `monitor` and `service` records in the journal |
| Temporary sweep settings | Not applicable | `overrides` records plus the point tables and manifest `request` |
| Restoration outcome | Not applicable | Manifest `restored` and the `restoration` record |

Updated scan HDF5 files embed the journal at `/evidence/records_jsonl`, so the
artifact is self-contained. These are paths **inside the HDF5 file**, not folders
on disk. The scan manifest and updated scan HDF5 schema are version 2; evidence
records have their own version 1. Normal-run HDF5 schema version 2 is a separate
format version and does not prove that a scan contains evidence.

## Map the logbook columns

| Logbook column | How to recover or interpret it |
| --- | --- |
| Date, time | Manifest `started_at` and `completed_at` are UTC. Convert to the operator's timezone. |
| Run, file type | `run_id` or `scan_id`, scan type, artifact kind and directory contents. |
| Step, such as 1.1 or 2.2 | Procedure labels are operator annotations; they are not encoded by the acquisition settings. |
| Board | Logical board number. For normal-run telemetry, map chain/node to board before selecting rows. |
| `HG_ShapingTime`, `LG_ShapingTime` | Resolved configuration audit values in ns; use the applied baseline and temporary overrides for scans. |
| `HoldDelay` | Resolved run setting; hold scans additionally save requested `delay_ns` and read-back `effective_delay_ns` at every point. |
| `HV_Vbias` | Supply setpoint from configuration. Record Vmon and estimated channel bias separately. |
| `TD_CoarseThreshold` | Board-resolved baseline threshold. Staircase points sweep both TD and QD thresholds, so one baseline number does not describe the whole scan. |
| Shooting channel | Internal pulser configuration may identify a programmed destination. External injection/cabling and the intended shooting channel require an operator record. |
| Temperature | Select a named sensor, board and sample interval; four different temperatures are available. |
| Events | Keep the requested preset, actual spectroscopy event count and total decoded event count distinct. Hold scans save `events_collected` per point; staircases save discriminator counters. |
| Problems | Quantitative symptoms may be visible in the data. Intent, observations and causal explanations still need annotations. |

A configured `TestPulseDestination = ALL` with `TestPulseSource = OFF` does not
prove that all channels were stimulated. Likewise, the presence of all channels
in a histogram does not identify external cabling.

## Resolve board and channel overrides

Do not take the first matching line in the imported text as the effective value.
A common setting can be superseded by a board or channel assignment. The audit
lists each assignment's `requested` value, `status` and resolved `effective`
values, including reasons for inactive settings. A saved setting that is inactive
must not be reported as an applied hardware value.

Run 34 is a concrete example:

```text
TD_CoarseThreshold       183
TD_CoarseThreshold[0]    181
TD_CoarseThreshold[2]    179
TD_CoarseThreshold[3]    178
```

Its resolved thresholds for boards 0–4 are **181, 183, 179, 178 and 183**. HG shaping
is 25 ns, LG shaping is 87.5 ns, hold delay is 0 ns and the fast-shaper input is
`LG-PA`. These values are available from the audit without decoding shaping-time
register codes. The effective HDF5 `citiroc_chips` table stores those codes rather
than human-readable nanoseconds; do not interpret code 5 as 5 ns.

For channel-specific analysis, also inspect `/configuration/effective/channels`
and the resolved plans. The audit reader below summarizes the logbook's common
and board settings; it is not a replacement for a full per-channel configuration
export. Parameters with channel overrides are omitted from the reader's board
summary and listed under `per_channel_parameters_omitted`; inspect the per-channel
tables for their actual values. Provenance records what the software requested and compiled/applied,
not an independent physical measurement of every analog setting.

## Distinguish supply voltage and channel bias

In runs 26 and 34, `HV_Vbias = 40.7`, `HV_Adjust_Range = 4.5` and the common
`HV_IndivAdj = 0`. Board 0 in run 34 measures Vmon at approximately
**40.6994–40.7001 V**. This is the board supply output.

For that channel code, the software's nominal-bias estimate is:

```text
estimated offset = 4.2 × (255 − 0) / 255 = 4.2 V
estimated Vnom   = 40.7 − 4.2 = 36.5 V
```

Thus a logbook value of 36.5 V can describe the estimated channel bias while
40.7 V describes the supply setting. This does not establish the original
operator's intended definition, and Vnom is not a measured SiPM voltage. Apply
any channel override before estimating its bias. The full formula and its
limitations are explained in [HV bias and channel offset](hv-bias-and-offset.md).

## Read all four temperatures and HV telemetry

The saved temperature and HV columns are already converted to engineering units.

| Field | Unit and meaning | Presence bit in `validity` |
| --- | --- | --- |
| `fpga_temperature_c` | °C, front-end FPGA | 0 (`0x01`) |
| `board_temperature_c` | °C, front-end board | 1 (`0x02`) |
| `detector_temperature_c` | °C, configured detector sensor reported through the HV subsystem | 2 (`0x04`) |
| `hv_temperature_c` | °C, HV module | 3 (`0x08`) |
| `hv_voltage_v` | V, measured supply output | 4 (`0x10`) |
| `hv_current_a` | A, measured current | 5 (`0x20`) |
| `status` | Board status word when supplied | 6 (`0x40`) |

Test `validity & (1 << bit)` before using a value. An unavailable field may have
a zero-filled slot. `0x7f` means all seven fields are present; direct scan monitor
rows use `0x3f` because they do not include the separate board status word.
Reject CRC-flagged samples. Presence does not independently certify sensor health
or calibration. Multiply stored current by 1,000 for mA or 1,000,000 for µA.

The detector reading depends on the configured sensor conversion. A plausible
reading does not establish sensor placement or calibration. There is no separate
ambient-room or concentrator temperature series in these outputs.

### Select the board before calculating ranges

For normal runs, service rows have no board column. Select `/events/index` rows
with `kind == 5`, use `kind_row` to select the row in `/events/service/events`, then
map the index's `(chain, node)` using `/configuration/effective/boards`.
These row references are local to their numbered segment.

Run 34 contains five boards and ten service samples per board. Its measured
ranges illustrate why a single undifferentiated “Temperature” column is ambiguous:

| Board | FPGA °C | Board °C | Detector °C | HV module °C |
| --- | --- | --- | --- | --- |
| 0 | 56.60–56.85 | 48.5 | 24.858–24.883 | 47.718–47.872 |
| 1 | 68.66–69.03 | 56.0 | 23.987 | 55.603 |
| 2 | 66.20–66.44 | 54.5–55.0 | 24.397 | 52.173 |
| 3 | 70.63–71.00 | 57.0 | 24.218 | 56.090 |
| 4 | 68.04–68.41 | 57.5 | 24.243 | 55.757 |

These are observed values, not approved operating limits. Record the sensor name
and board alongside the value or range you copy into a logbook.

## Use the read-only Python example

<a href="/docs/examples/read_saved_measurements.py" download>Download <code>read_saved_measurements.py</code></a>.
Install its reader packages in your analysis Python environment, then pass a
completed run or scan directory:

```sh
python -m pip install numpy h5py hdf5plugin
python read_saved_measurements.py path/to/run-34 > run-34-summary.json
python read_saved_measurements.py path/to/scan-100 > scan-100-summary.json
python read_saved_measurements.py path/to/hold-scan-101 > hold-101-summary.json
```

The script reads local files only. It resolves board-level audit values, joins
normal-run telemetry to its source, checks field presence/CRC flags and reports
per-board ranges. It reads all numbered run segments. Updated scan summaries
include the saved request, override records, restoration flag and evidence
errors. No hardware connection or configuration write is involved.

For run 34, the output includes:

```json
{
  "id": "34",
  "started_at_utc": "2026-10-22T11:07:42.648396255Z",
  "total_decoded_events": 50110,
  "spectroscopy_events": 50060,
  "service_rows": 50
}
```

The requested preset is 50,000, while the stored event total is 50,110:
50,060 spectroscopy events plus 50 service events. Batching can overshoot the
preset. Neither the total nor the preset should be labelled “events per board”
without selecting the board. `service_rows` in this reader counts service rows
accepted after its CRC check.

To inspect the table directly:

```python
import h5py
import hdf5plugin  # Registers the filters used by the compressed datasets.

with h5py.File("path/to/run-34/run_34.0000.h5", "r") as f:
    service = f["events/service/events"][:]
    print(service.dtype.names)
    index = f["events/index"][:]
    boards = f["configuration/effective/boards"][:]
```

JSON/text HDF5 datasets are UTF-8 byte arrays. For example,
`json.loads(f["configuration/audit_json"][:].tobytes().decode("utf-8"))`
loads a normal-run audit after importing `json`.

## Read updated scans

A scan has an **applied baseline**, **temporary overrides**, and an attempted
**restoration**. Read all three. Staircase preparation changes shaping and ASIC
settings; using the imported HG/LG baseline alone would misdescribe its
measurement. Thresholds change at each staircase point. Hold-delay points save
both requested and read-back delays.

In the finalized scan HDF5 file:

```python
import json
import h5py
import hdf5plugin

with h5py.File("path/to/scan-100/staircase.h5", "r") as f:
    evidence = f["evidence/records_jsonl"][:].tobytes().decode("utf-8")
    records = [json.loads(line) for line in evidence.splitlines()]
    baseline = next(r["configuration"] for r in records
                    if r["type"] == "configuration")
    audit = baseline["configuration_audit"]
    overrides = [r for r in records if r["type"] == "overrides"]
    telemetry = f["telemetry/samples"][:]
    valid = ((telemetry["validity"] & 0x02) != 0) & (telemetry["crc_error"] == 0)
    print(telemetry["board_temperature_c"][valid])
```

Updated scan telemetry rows include `board`, `chain` and `node` directly. Their
`record_index` joins to the zero-based evidence line; `point_index` joins to the
point table only when `has_point == 1`. `source == 1` means a stream service event;
`source == 2` means a direct monitor read. The `phase` in its evidence record
distinguishes `before_scan`, `after_point`, `during_point` and `after_restore`.
Do not mix restored-baseline samples into a point-specific result accidentally.

Monitor reads are bounded and occur before the scan, between points and after
restoration. Staircase reads happen outside the counting dwell. Hold scans also
retain service events encountered in their stream batches. Short scans may
have no stream service event; direct monitor samples remain available. A failed
monitor read produces an explicit error record, not an all-zero telemetry row.
The summary reader needs the finalized HDF5 file for scan telemetry ranges;
for an interrupted or non-HDF5 scan, inspect the journal's `monitor`/`service`
records directly.

The `overrides` records describe write intentions, not independent readback of
every register. `restored = true` reports the existing non-HV restoration checks,
not physical validation of every analog value. A scan state of “completed” means
that its points were collected; it does not certify the measurement's quality.
See [scan evidence schema](../scan-evidence.md) for the complete layout.

## Time and older files

Manifest times ending in `Z` are UTC. For example, run 34 began at 11:07:42 UTC,
which was 13:07:42 in Madrid on October 22. Use a timezone-aware conversion:

```python
from datetime import datetime
from zoneinfo import ZoneInfo

utc = datetime.fromisoformat("2026-10-22T11:07:42.648396255Z")
print(utc.astimezone(ZoneInfo("Europe/Madrid")))
```

Normal service `timestamp` is a hardware counter, not Unix time. Updated scans
also save host `observed_at` and `observed_at_unix_ns`; these are observation times,
not an exact clock correlation to the detector. Do not add hardware ticks directly
to the manifest start time and label the result an exact UTC measurement time.

Scans created before the evidence update—including the original scans 10–12 and
the October 22 repeats 23–25—have no full baseline snapshot or temperature/HV
series. A newer documentation or application build cannot add that information
to old artifacts. The reader reports missing evidence instead of borrowing a
configuration from a neighboring run. Keep procedure labels, external cabling,
shooting channel and operator notes in the logbook even with the updated writer.

For the wider output layout, see [HDF5 examples](../hdf5-schema-examples.md),
[scan evidence](../scan-evidence.md) and [run catalog operations](../run-catalog-operations.md).
