# JANUS scan modes and native DAQ implementation

This note records the scan facilities exposed by JANUS 5202 5.0.0 and maps
them onto the current native Go DAQ. JANUS and FERSlib are used only as
reference implementations; the proposed production implementation does not
call either at runtime.

## Sources and evidence

- `STMP_UM7946_Janus_UserManual_rev3.pdf`, especially pp. 22–25 (Staircase
  and Hold Delay Scan controls), p. 41 (plot types), and pp. 61–63
  (discriminator settings).
- `WEB_UM7945_A5202-DT5202_rev4.pdf`, especially sections 10.7.2 and 10.7.3
  (pp. 89–92).
- Bundled JANUS 5202 5.0.0 source:
  - `src/FERSutils.c`, `ScanThreshold` and `ScanHoldDelay`
  - `src/JanusC.c`, scan dispatch and post-scan reconfiguration
  - `src/plot.c`, `PlotStaircase` and `PlotScanHoldDelay`
  - `src/outputfiles.c`, per-channel staircase output
  - `gui/ctrl.py`, the two scan buttons and their parameters
- The current DAQ implementation, principally
  `backend/internal/dt5202`, `backend/internal/acquisition`,
  `backend/internal/service`, `api/pet/caen/daq/v1/system.proto`, and the
  frontend.

Facts attributed to the manuals or implemented directly in bundled source are
**source-confirmed**. Proposed behavior for this DAQ is explicitly marked as
such. Register behavior has not yet been exercised on real hardware for a
native scan and is therefore not **hardware-verified**.

## Implementation status

The threshold staircase described below is implemented on
`feature/janus-scan-support`:

- `ScanService` starts, cancels, lists, and retrieves staircase scans.
- Scans are exclusive `SCANNING` lifecycle operations admitted only from
  `READY`.
- Each completed point is synchronously appended to `points.jsonl`, so an
  interrupted scan retains independently parseable evidence.
- Production `hdf5` builds finalize a canonical `staircase.h5` artifact with
  `staircase/points`, `staircase/channel_measurements`, and
  `staircase/metadata_json` datasets. Non-HDF5 development builds retain the
  JSON Lines artifact.
- Reconnect-safe telemetry carries the current scan and its completed points.
  The frontend Scans workspace plots live curves and reloads finalized scans
  through `GetStaircase`.
- The simulator returns deterministic threshold-dependent channel, T-OR, and
  Q-OR counters.
- Finalized scans are included in the common run-history contract with
  `run_type=STAIRCASE`; acquisitions use `run_type=DATA`. The run table shows
  distinct type badges, and search accepts All types, Data runs, or Staircase
  scans. Scan artifacts remain downloadable from the common artifact action.
- Completion and cancellation perform a full hard, non-HV production
  configuration restore with register readback. Scan or restoration failures
  enter `FAULT`; successful cancellation returns to `READY`.

The native sequence remains `source-confirmed` until it is compared with JANUS
on real hardware. Hold-delay scanning remains proposed work.

## What scans does JANUS implement?

JANUS exposes exactly two automated, operator-facing parameter scans:

| Scan | Scanned quantity | Measurement | Main use |
| --- | --- | --- | --- |
| Threshold scan (“Staircase”) | Common TD and QD coarse threshold | 64 channel TD hit rates plus T-OR and Q-OR rates | Find discriminator baseline/noise plateaus and choose trigger thresholds |
| Hold-delay scan | Peak-hold delay, in 8 ns units | HG pulse-height distribution for every channel at every delay | Choose the sampling time near the slow-shaper peak |

The conclusion is based both on the GUI, which has only `Staircase` and
`HoldScan` special-run buttons, and the C implementation, which has only
`ScanThreshold` and `ScanHoldDelay` scan routines. JANUS plot choices such as
PHA, ToA, ToT, trigger-rate, multi-channel scaler, waveform, and 2D charge are
views of ordinary acquisition data, not additional scans. Pedestal calibration
is an automated calibration procedure, but it does not sweep a parameter and
JANUS does not present it as a scan.

### Threshold scan / Staircase

User parameters are board, minimum threshold, maximum threshold, step, and
dwell time in milliseconds. The manuals suggest approximately 150–500 DAC
units and note that zero is not the TD baseline. A longer dwell improves rate
precision. The board manual describes the intended measurement as the dark
count rate with the light source off and HV on.

The JANUS routine:

1. Temporarily configures the selected board for single-channel counting:
   acquisition mode `COUNTING`, singles counting, the requested dwell time,
   configured T-logic masks, direct/non-latched Q discriminator, 25 ns LG/HG
   shaping, software trigger only, and debug output masks.
2. Walks from the maximum threshold down to the minimum. It writes the same
   value to both `QD_CoarseThreshold` and `TD_CoarseThreshold`.
3. Programs both Citiroc chips with `CMD_CFG_ASIC`.
4. Enables the periodic trigger, sends `CMD_RES_PTRG`, waits one dwell plus a
   200 ms margin, and reads `T_OR_CNT`, `Q_OR_CNT`, and all 64 individual hit
   counters.
5. Divides each count by the requested dwell in seconds and writes
   `ScanThr.txt`: threshold, 64 channel cps values, T-OR cps, and Q-OR cps.
6. Reapplies the complete configured board state after the scan.

JANUS performs one unrecorded warm-up interval at `max + step` before the
recorded maximum point. This is implementation behavior, not a documented
requirement. A native implementation should use an explicit warm-up if
hardware testing proves it necessary, rather than reproduce the off-by-one
loop accidentally.

Although the user-facing name and documentation describe a *TD* threshold
scan, JANUS writes both the TD and QD coarse DACs at every point. Consequently
the channel values and T-OR curve are a TD staircase, while the Q-OR curve is
also measured at a simultaneously changing QD threshold. This coupling should
be explicit in our API and result metadata. A later native extension could
offer independently selected TD-only or QD-only scans, but that would go
beyond JANUS parity and would require hardware validation.

### Hold-delay scan

User parameters are board, minimum delay, maximum delay, step in nanoseconds,
and the number of acquired points per delay. The scan is valid only for
`SPECTROSCOPY`; the trigger source must match the intended physics run.
Minimum step and resolution are 8 ns. JANUS enforces at least 10 samples and
caps the scan at 64 delay points.

The JANUS routine:

1. Stops acquisition and configures spectroscopy with both gains, the
   configured trigger mask, run mask, and common pedestal.
2. For each delay, writes `HoldDelay = delay / 8`, starts acquisition, and
   obtains the requested number of spectroscopy events.
3. For every event and every channel, bins HG energy into 512 bins (by shifting
   the ADC code and saturating at bin 511).
4. Stops acquisition, flushes data, and advances to the next delay.
5. Retains a per-board/per-channel 2D histogram
   `(hold delay, HG pulse-height bin)`.
6. Reapplies the complete configured board state after the scan.

The manuals call the parameter “Num averaged points”, but the implementation
does not calculate an average. It accumulates a two-dimensional pulse-height
histogram. Our API should use an unambiguous name such as `events_per_delay`.
Storing sufficient statistics (count, sum, and optionally sum of squares) may
be useful, but must not replace the source distribution if JANUS-compatible
plots are required.

## What the current DAQ already has

Several low-level pieces needed by both scans already exist:

- Native register read/write and command transport through the DT5215.
- Source-confirmed constants for `DwellTime`, `TimeCoarseThreshold`,
  `ChargeCoarseThreshold`, `HoldDelay`, `TimeORCount`, `ChargeORCount`,
  `HitCounter`, `CommandResetPeriodicTrigger`, and
  `CommandConfigureASIC`.
- `IndividualRegister(HitCounter, channel)` address construction.
- Production configuration planning, ordered writes, Citiroc programming,
  readback validation, and full hard reconfiguration.
- Explicit acquisition lifecycle, synchronized start/stop/drain, decoded
  counting and spectroscopy events, storage abstractions, telemetry streaming,
  histogram support, and a CI simulator.

The missing part is not basic protocol support. It is orchestration for a
bounded diagnostic job, its public contract, result persistence, progress
telemetry, cancellation, restoration, and operator UI.

## Proposed native design

### 1. Model scans as exclusive diagnostic jobs

Add a scan coordinator alongside the run coordinator. A scan must be admitted
only from `READY`, must be mutually exclusive with configuration, HV changes,
and normal runs, and must return to `READY` only after restoration and
readback validation.

Prefer explicit lifecycle states such as `SCANNING` (with scan kind and phase
in telemetry), or a general `DIAGNOSTIC_RUNNING` state, rather than representing
a scan as a normal physics run. The existing state machine and protobuf
`SystemState` need matching additions. Transitional phases should include at
least preparing, scanning, restoring, completed, cancelled, and failed.

The coordinator must:

- serialize the operation with existing configuration/run control;
- own a cancellable context;
- publish current point, total points, board, and phase;
- stop any board acquisition and drain/flush where appropriate;
- always restore the previously effective production configuration in a
  `defer`-style cleanup path;
- validate restoration by readback;
- enter `FAULT`, preserving both the primary and restoration errors, whenever
  board state cannot be proven restored.

Do not merely restore the handful of registers believed to have changed.
JANUS itself uses full `CFG_HARD` reconfiguration after either scan. Reapplying
the cached effective `ConfigurationPlan` is safer and easier to audit. HV must
not be toggled or rewritten as part of restoration; restoration should use the
non-HV configuration path.

### 2. Add a protobuf scan contract

Add a dedicated `ScanService` (or clearly separated scan RPCs under
`RunService`) with:

- `StartThresholdScan`
  - target board (chain/node or the stable board identity used by discovery)
  - minimum/maximum coarse DAC value
  - positive step
  - dwell duration
  - optional explicit warm-up duration/point, default off until verified
  - actor
- `StartHoldDelayScan`
  - target board
  - minimum/maximum delay in ns
  - step, validated as a positive multiple of 8 ns
  - events per delay, minimum 10 for JANUS parity
  - actor
- `CancelScan`
- `GetScan` / `ListScans` and either a scan snapshot stream or scan fields in
  the existing reconnect-safe telemetry snapshot.

Use a server-created scan ID. Reject reversed or empty ranges, arithmetic
overflow, thresholds outside the supported DAC range, more than an explicit
maximum number of points, excessive dwell/event counts, a missing board,
incorrect acquisition mode, or a trigger source unsuitable for the requested
operation.

The request and stored result must state that JANUS-parity threshold scans
change both TD and QD coarse thresholds. This prevents an operator from
mistaking Q-OR values for a fixed-QD measurement.

### 3. Implement the threshold scan in `backend/internal/dt5202`

Create a small hardware-neutral scan engine around an interface containing
`ReadRegister`, `WriteRegister`, and `SendCommand`. Keep orchestration and
storage out of this package.

For each requested point:

1. write TD and QD coarse thresholds;
2. issue `CommandConfigureASIC` for each Citiroc selection using the same
   sequence already used by native configuration;
3. issue `CommandResetPeriodicTrigger`;
4. wait for the dwell interval using an injected/context-aware clock;
5. read all 64 individual hit counters and the T-OR/Q-OR counters;
6. report raw counts, the actual elapsed interval, and derived rates.

The exact temporary setup writes used by JANUS must be ported as named,
validated register fields rather than unexplained literals. In particular,
acquisition-control singles counting and `CitirocConfig = 0x00070f20` need
byte/bit-level conformance tests and then hardware verification. The existing
register constants are source-confirmed; the complete native scan sequence is
not yet hardware-verified.

Use actual monotonic elapsed time for rate calculation and retain raw counts
and requested dwell. JANUS divides by requested dwell despite adding 200 ms
host wait margin; retaining both makes results reproducible and exposes timing
uncertainty.

### 4. Implement hold-delay acquisition through the normal decoder

Use the existing acquisition stream/decoder rather than a second event parser.
For each delay:

1. write and read back `HoldDelay = delay_ns / 8`;
2. start a bounded single-board spectroscopy acquisition using the production
   trigger source and both gains;
3. collect exactly `events_per_delay` successfully decoded spectroscopy
   events, subject to a per-point timeout;
4. stop, drain, and flush before changing the delay;
5. accumulate per-channel HG histograms keyed by the actual delay.

Define whether events from other boards are disabled or drained and discarded.
For an initial implementation, scanning one board while all other readout
trains are disabled is simplest and matches JANUS’s selected-board behavior.
Record discarded/invalid frames and retain raw evidence when scan raw capture
is enabled.

Do not hard-code 512 bins into the domain model. For JANUS parity, offer a
512-bin HG view with the documented ADC-range mapping, while storing raw
decoded energy samples or a lossless/configured histogram sufficient to
regenerate plots. The point timeout must be explicit; JANUS’s polling loop
amounts to roughly five seconds per event but aborts a point silently after a
timeout.

### 5. Persist scans as first-class artifacts

Store a scan manifest containing:

- scan ID, type, requested-by, timestamps, completion/cancellation reason;
- board identity and firmware/topology;
- exact requested and effective scan parameters;
- exact production configuration before and after the scan;
- evidence classification and software version;
- per-point elapsed timing, missing/invalid event counts, and errors;
- whether restoration was validated.

Use independently parseable records (JSON Lines for development and dedicated
HDF5 datasets for production) through a scan-storage interface. Suggested
records are:

- threshold: one record per threshold containing 64 raw counts, dwell,
  64 rates, T-OR/Q-OR raw counts and rates;
- hold delay: one record per delay and channel histogram, or lossless event
  samples plus derived histograms.

An optional `ScanThr.txt` exporter can provide JANUS interoperability, but the
67-column text file should not be the canonical store. Integrate scan
summaries/artifacts with the run catalog only if the catalog is generalized to
distinguish `physics_run` from `threshold_scan` and `hold_delay_scan`.

### 6. Frontend

Add a Diagnostics/Scans workspace, separate from normal run controls:

- board selector and validated inputs;
- estimated duration and point count before start;
- explicit reminders that threshold scanning expects HV on and the light
  source off, without changing either automatically;
- explicit reminder that hold-delay scanning uses the physics trigger source;
- progress and cancellable status resilient to reconnect;
- staircase overlay selection for channels plus T-OR/Q-OR curves;
- hold-delay heat map with channel selection and delay/pulse-height axes;
- downloadable canonical result and optional JANUS-compatible export.

The UI must disable conflicting run/configuration/HV actions based on server
state, while the server remains authoritative.

### 7. Simulator and tests

Extend the simulator with deterministic responses:

- threshold-dependent channel rates with configurable baselines/plateaus,
  T-OR/Q-OR counters, counter reset behavior, and a seeded Poisson-like option;
- hold-delay-dependent HG spectra with a known peak versus delay;
- failures during preparation, a point, stop/drain, and restoration;
- cancellation and no-event timeout behavior.

Required tests:

- range, 8 ns alignment, point-count, overflow, and duration validation;
- exact register/command order and both-Citiroc programming;
- descending/ascending point semantics and absence of the JANUS off-by-one
  unless a deliberate warm-up is selected;
- rate calculation and counter address golden tests;
- hold-delay event routing and histogram binning;
- exclusive lifecycle transitions and reconnect-safe progress snapshots;
- cancellation at every phase;
- restoration after success, cancellation, timeout, transport failure, and
  storage failure;
- a fault when restoration/readback fails;
- storage round trips and artifact checksums;
- ConnectRPC integration and Playwright coverage of both operator workflows.

Finally add an opt-in hardware test that first runs a very small, conservative
range and compares native results and register traces against JANUS. Until
that succeeds, label the end-to-end scan sequence `source-confirmed`, not
`hardware-verified`.

## Recommended delivery order

1. Threshold-scan engine, simulator behavior, restore semantics, JSONL result,
   API, telemetry, and a basic staircase plot.
2. Hardware conformance run against JANUS and promotion of verified facts.
3. Hold-delay bounded acquisition, persistent 2D results, API, and heat map.
4. HDF5 scan datasets, catalog integration, JANUS text export, and richer
   comparison/analysis tools.

The threshold scan is the lower-risk first slice: all required register
constants and counter reads already exist, and it does not require integrating
a repeated start/read/stop acquisition loop with the stream pipeline. The
non-negotiable foundation for either scan is exclusive orchestration and
validated full restoration of production board state.
