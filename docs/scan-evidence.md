# Scan configuration and telemetry evidence

Staircase and hold-delay scans now persist provenance alongside their existing
point results. This closes the gap where ordinary runs saved configuration and
service telemetry but scans saved only the sweep request and aggregate results.
No changes to the scan RPC contract or existing point dataset columns are needed.

## Baseline and compatibility

The implementation retains the calibrated analog DC-offset fix from deployed
commit `716d09355275438e9bc41ba269947b2185c944b5`. The branch was rebased onto
GitHub `main` at `fce8014586fc09f5146dbb0f978775b016a0e85b`. Since that main
revision does not include the deployed DC-offset correction, both the
calibration correction and scan persistence are included in this change.
No firmware or deployed service changes are part of this patch.

Scan JSON manifest schema is now version 2. Existing version-1 scan files remain
readable, and old point JSON/HDF5 layouts are unchanged. The added evidence is
optional when reading historical files; absence means unavailable, not zero or
an inferred copy of a neighboring run's conditions.

## Output files

Each new production scan directory contains:

```text
scan-N/                          # or hold-scan-N/
    manifest.json
    points.jsonl
    evidence.jsonl
    staircase.h5                 # or hold-delay.h5, with HDF5 enabled
```

`evidence.jsonl` is written synchronously and synced after each record, before
continuing the scan. It survives cancellation and failures before final HDF5
creation. There is one scan-owned writer and no asynchronous telemetry queue.
A storage error propagates to the scan, whose coordinator still attempts the
existing production configuration restoration. When finalization succeeds,
`manifest.json/evidence_artifact` records its filename, size and SHA256.
The evidence journal is also embedded byte-for-byte in the finalized HDF5 file
at `/evidence/records_jsonl`. The HDF5 scan schema is version 2 when the new
writer supplies evidence. Normal acquisition HDF5 schema is unchanged.

Non-HDF5 builds retain the same evidence journal and manifest reference; their
primary point artifact remains JSON Lines. No new frontend screen is required.
Evidence is available in the output directory and inside the existing HDF5
artifact download. There is no new RPC for individual telemetry samples.

## Evidence records

Every record has `schema_version` (1), `type`, `observed_at` (UTC host time),
logical `board`, `chain` and `node`. `point_index`, when present, is zero-based in
**acquisition order**, matching the saved point row. A configuration record
covers the complete applied baseline; its topology is the authority for device
addresses rather than the record's default chain/node fields.

| Type | Contents |
| --- | --- |
| `configuration` | Ordered requested assignments, full effective configuration plans, per-setting audit, discovered topology and embedded build software identity |
| `baseline` | The selected board's configuration plan and logical-board to chain/node mapping |
| `overrides` | Temporary scan register write intentions, distinguished by `prepare_intent` or `point_intent` phase |
| `monitor` | Direct register telemetry before the scan, after each completed point and after attempted restoration, or an explicit read error |
| `service` | Hold-delay service events decoded from received stream batches, including their hardware timestamp, point identity and CRC flag |
| `restoration` | Actual restoration success/failure and its error text |

Requested assignments preserve their order, names, board/channel indices,
values and source line numbers. Source comments and original whitespace are not
retained. They are the cached **applied** document, not a potentially edited UI
configuration. Effective plans retain resolved defaults, channel settings,
CITIROC settings, register writes, pedestal calibration and HV plans. Software
identity may explicitly be `unknown` for builds without embedded VCS metadata.
The topology snapshot is the discovered identity, not a fresh firmware query at
every scan point.

Override records describe intended writes before execution, so they survive
partial preparation failures. They are not an independent hardware readback of
every setting. Staircase preparation records its temporary shaping and ASIC
configuration overrides; each threshold point records both TD and QD threshold
intentions. Hold scans record preparation overrides and each requested hold
register value; the existing point output additionally contains its read-back
effective delay. ASIC-loading commands, selector writes and trigger arming retain
the existing source-confirmed algorithm; the evidence does not claim to be a
byte-exact transport journal.

`restoration/restored` means that the existing non-HV configuration restoration
and its normal checks succeeded. It does not claim a new authorization to
change HV or a separate physical validation of every analog parameter.

## Telemetry sources and timing

Staircase monitor reads run before counting starts, after a point's counters
have been collected and after restoration. They do not run inside the requested
counting dwell. Hold scans take the same direct monitor samples between points,
after the point acquisition has stopped, and additionally preserve service
events encountered during the stream read. Service events that follow the last
requested spectroscopy event in the same received batch are retained without
increasing the spectroscopy event total.

Direct monitor reads use the existing native, firmware-4+ register reader and
have a two-second deadline per sample. A read failure is written with `error`
and no `monitor` object; it does not fabricate zero temperatures or independently
abort the measurement. Failure to persist that error does abort the scan.
After cancellation, restoration and its monitor sample use a context detached
from the cancellation; the sample still has its two-second deadline. Additional
reads and journal syncs increase overall scan duration, outside the staircase
dwell. The preexisting background monitor remains unchanged.

`observed_at` is the host time after a telemetry read or service-event decode.
It is not an exact detector measurement time. Stream service records separately
preserve the hardware counter in `hardware_timestamp`. The fields must not be
combined into an exact UTC conversion without a clock correlation. Short hold
scans may receive no service events; the direct monitor samples still provide
before/after-point measurements. Service events of other boards are outside
the selected board's scan evidence.

Malformed service events retain a decode error and their base64 JSON
`raw_payload`. CRC-flagged service events also retain the raw payload and
`crc_error = true`; their decoded values must not be treated as trusted readings.
Unknown service versions preserve their undecoded payload through the service
event structure. This is diagnostic preservation of service payloads, not full
raw capture of all scan traffic.

## HDF5 telemetry table

The finalized scan includes `/telemetry/samples`, with one compound row per
successfully decoded service event or successful direct monitor read.
Errors and configuration records remain in `/evidence/records_jsonl`; failed
monitor reads do not create all-zero measurement rows.

| Field | Meaning |
| --- | --- |
| `record_index` | Zero-based line in `/evidence/records_jsonl`; join here for phase and the complete original record |
| `observed_at_unix_ns` | Signed UTC Unix nanoseconds at the host observation time |
| `board`, `chain`, `node` | Source identity |
| `point_index`, `has_point` | Point row association; ignore the index when `has_point == 0` |
| `hardware_timestamp`, `has_hardware_timestamp` | Original hardware counter when available; not a Unix timestamp |
| `source` | 1 = stream service event, 2 = direct register monitor |
| `crc_error` | Stream descriptor CRC indication; reject flagged readings for analysis |
| `validity` | Presence mask described below |
| `fpga_temperature_c` | Front-end FPGA temperature in degrees Celsius, bit 0 (`0x01`) |
| `board_temperature_c` | Front-end board temperature in degrees Celsius, bit 1 (`0x02`) |
| `detector_temperature_c` | Configured detector sensor reading through HV subsystem, degrees Celsius, bit 2 (`0x04`) |
| `hv_temperature_c` | HV module temperature in degrees Celsius, bit 3 (`0x08`) |
| `hv_voltage_v` | Measured HV-module output in volts, bit 4 (`0x10`) |
| `hv_current_a` | Measured current in amperes, bit 5 (`0x20`) |
| `status` | Board status word when present, bit 6 (`0x40`) |
| `hv_on`, `hv_ramping`, `hv_over_current`, `hv_over_voltage` | Decoded HV flags |

Service-field validity follows the normal run writer's presence semantics.
Monitor rows set bits 0–5 after all direct register reads succeed; they do not
provide the separate board status word. These flags indicate successful reads
and decoding, not an independent calibration or sensor-health assessment. The
existing monitor decoder's handling of special raw values remains unchanged.
For an unfamiliar service version, consult the evidence record before using
flags; the schema does not assign separate HV-flag validity bits.

An unavailable field's stored numeric zero is not a measurement. The detector,
board, FPGA and HV temperatures are distinct; no ambient-room or concentrator
temperature is inferred.

```python
import json
import h5py
import hdf5plugin  # Register the filters needed by compressed scan point tables.

with h5py.File("scan-N/staircase.h5", "r") as f:
    records = [json.loads(line) for line in
               f["evidence/records_jsonl"][:].tobytes().decode().splitlines()]
    baseline = next(r["configuration"] for r in records
                    if r["type"] == "configuration")
    telemetry = f["telemetry/samples"][:]
    valid = ((telemetry["validity"] & 0x02) != 0) & (telemetry["crc_error"] == 0)
    print(telemetry["board_temperature_c"][valid])
```

## Validation

Run `task test:scan-evidence`, or `./scripts/docker-run task test:scan-evidence`
with the existing build image. This runs race-enabled tests for scan loops,
configuration/restoration coordinators, services, evidence journals, HDF5
round trips and the native runtime against the TCP simulator. No physical
hardware is required.

Tests cover newly applied rather than stale baseline settings, all four monitor
temperatures, preservation of service events after the event quota, malformed
service data, absent measurements, failed writes, cancellation, failed
restoration, zero-point cancelled artifacts, journal checksums and byte-exact
HDF5 embedding. The patch cannot fill missing metadata in historical scans.
