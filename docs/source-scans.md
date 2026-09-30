# XY radioactive-source scans

The **Scans → Source XY scan** panel acquires one normal PET data run per
stationary source position. Select/load the PET configuration and output options
in **Acquisition**, then expand **Source XY scan** in **Scans** to choose the grid
and motor settings. Collapsing the panel preserves the entered settings.
Millimetre inputs use 0.01 mm increments for their arrow controls.
The backend owns the entire sequence, so closing/reloading the browser does not
stop a scan. Cancel it using **Cancel source scan**.

The original `XY_raster_scan_with_JANUS_trigger_handshake_over_Serial.ino`
was the motion/handshake reference. It drives step/direction motors (not hobby
servos), starts moving at boot, and has a compiled-in raster and calibration.
It cannot accept a frontend-selected grid. Install the replacement
[`pet_xy_stage.ino`](../firmware/pet_xy_stage/pet_xy_stage.ino) for this integration.
The original sketch is preserved and is **not compatible with PETXY/1**.

## Configuration and operation

- X/Y minimum and maximum, pitch and travel per revolution are in **mm**.
- Full steps/revolution and microsteps must match the motor and driver DIP setting.
  The current firmware uses the same calibration for both axes, as the original.
- Direction polarity and high/low pulse times (µs) are sent to the device.
- The raster starts at `(x_min, y_max)` and proceeds in serpentine order, down Y.
  Both endpoints are included. Each span must be an integral multiple of pitch.
  At most 10,000 positions are accepted.
- Absolute coordinates are rounded to the nearest motor step (halves away from
  zero). Requested mm, calculated mm and logical step counts are all preserved.
  Pitch smaller than a step and coordinates outside ±10,000,000 steps are rejected.
- Choose a positive acquisition duration **or** event count for each point.
  The point criterion overrides the PET document's `StopRunMode` for orchestration;
  the original PET document remains preserved in run metadata. Counts refer to
  the same aggregate decoded-event count used by the existing DAQ preset counter,
  not coincidences, triggers on one board or per-channel events. They can overshoot
  because polling, in-flight buffers and stop/drain retain all received events.
- Settling starts after the device acknowledges the exact target step counts.
  Acquisition timing starts after run start succeeds. Point timeout includes
  travel, settling, configuration and acquisition, and bounds no-event runs.
- The backend waits for run finalization/drain before the next move. Any device,
  acquisition or persistence failure stops the sequence; no automatic retry occurs.
- External run starts/stops, diagnostic scans, connection/discovery operations and
  HV changes are rejected while the source scan owns hardware, including during
  movement and gaps between runs. Read APIs remain available.

There are no encoders or homing/limit switches in the supplied wiring. Before
**each** scan, physically establish the desired logical `(0,0)` and confirm it in
the UI. Check that the configured grid fits the stage. Opening the serial port
may reset an Arduino, so position from a previous session is never trusted.
Completion/cancellation leaves the stage where it stopped; there is no automatic
return to origin. After interrupted movement the origin must be re-established.

## Run without hardware

Use four terminals from the repository root:

```sh
task build
task simulator:caen
```

```sh
task simulator:source
```

```sh
task dev:source
```

```sh
task dev:frontend
```

Open `http://localhost:5173`. For a plain Go/JSON build, disable **Persist
histograms** in Acquisition → RunCtrl (that existing feature requires HDF5).
Try a small 2×2 grid: X/Y 0–0.5 mm, pitch 0.5 mm, acquisition 0.1 s.
Demo runs are written under `.cache/source-demo-runs`.

Alternatively, `task docker:local:up` starts the stage simulator alongside the
existing local CAEN simulator and HDF5 backend.

If the default ports are occupied, set `PET_CAEN_API_PORT` and
`PET_CAEN_FRONTEND_PORT` before starting Compose. For example, on Linux:

```sh
PET_CAEN_API_PORT=8082 PET_CAEN_FRONTEND_PORT=5174 task docker:local:up
```

The backend also serves the built frontend at `http://localhost:8082` in this
example; the development frontend is at `http://localhost:5174`.

The stage simulator is a separate executable/process; production device code
does not call it. Its TCP endpoint is configured on the server, not supplied by
browser requests. Inject faults when testing recovery:

```sh
task simulator:source -- -fault disconnect
task simulator:source -- -fault malformed
task simulator:source -- -fault timeout
```

Only run one stage simulator on a given port at once.

## Connect an Arduino

Upload `firmware/pet_xy_stage/pet_xy_stage.ino`. Wiring remains X PUL D2 / DIR D3,
Y PUL D5 / DIR D6, common GND, as in the provided sketch. Add
`-source-stage /dev/ttyACM0` (or the appropriate `/dev/serial/by-id/...` path) to
the backend's existing startup command. The backend user needs access to that
device. Serial is fixed at 115200, 8 data bits, no parity, 1 stop bit, no flow
control. Native serial currently supports **Linux**; simulator TCP works on
other Go platforms. In containers, explicitly pass through the device and its
required group permissions. An empty `-source-stage` disables source scans.

No real Arduino or stage was available during implementation. Firmware behavior
is tested with deterministic host I/O, and serial I/O with a Linux pseudo-terminal;
these tests do not establish physical direction, distance, missed-step behavior,
AVR board compilation, or mechanical travel limits.

### Windows USB Arduino with a Docker backend

Run `source-stage-bridge.exe` on the **Windows host**, outside Docker. It opens
the Arduino COM port exclusively and forwards bytes to/from one TCP controller.
The Linux backend connects over TCP; USB passthrough is not needed. The Arduino
must run `firmware/pet_xy_stage/pet_xy_stage.ino`, not the original raster sketch.

Build the Windows amd64 executable from the repository root (Go and Task required;
cross-compilation from Linux is supported):

```sh
task build:source-bridge:windows
task test:source-bridge
```

`task build-win` also includes the bridge. Copy `bin/source-stage-bridge.exe` to
the Windows host. Find the Arduino's COM number in Device Manager, close Arduino
Serial Monitor and other applications using that port, then run in PowerShell:

```powershell
.\source-stage-bridge.exe -serial COM3 -listen 127.0.0.1:19010
```

Configure the backend with `-source-stage tcp://host.docker.internal:19010`.
For the local Compose workflow, set this environment variable in the PowerShell
session launching Task, then recreate the stack:

```powershell
$env:PET_CAEN_SOURCE_STAGE = "tcp://host.docker.internal:19010"
task docker:local:up
```

**This replaces the simulated stage with real motor control**, while the local
Compose stack still uses simulated CAEN acquisition. The unused stage simulator
still starts. Remove the environment variable to return to simulated motion:
`Remove-Item Env:PET_CAEN_SOURCE_STAGE`, then run `task docker:local:up` again.

If Docker cannot reach the loopback listener with your networking backend,
explicitly bind the bridge to a reachable Windows host-interface IP. As a fallback,
`-listen 0.0.0.0:19010` listens on all IPv4 interfaces, but **first configure Windows
Firewall to allow only the required Docker/host traffic to this executable/port**.
Do not expose this port to the LAN or Internet: the bridge has no authentication
or encryption, and anyone able to connect can command motion. Do not disable the
firewall or add an unrestricted inbound rule.

The bridge fixes serial settings at 115200 baud, 8N1, without flow control. DTR
and RTS are asserted; opening the port may reset an Arduino. The bridge waits
two seconds after opening COM before forwarding commands (`-reset-wait 2s`).
This also covers TCP connections, for which the backend does not wait for reset.
Serial configuration follows
the Windows [DCB](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-dcb)
and [COMMTIMEOUTS](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-commtimeouts)
APIs. Each connection gets a fresh serial handle and purged receive/transmit
buffers; additional clients are immediately closed. COM-port open failures are
logged and close the TCP client. Disconnect, I/O failure, or Ctrl+C closes both
sides. There is no automatic replay, serial reconnect, or scan resume. Reconnect
requires a new scan and a newly established physical/logical origin.

The bridge never generates keepalives or a synthetic stop reply. On loss of the
controller, the firmware's motion watchdog is the fallback; disconnect is **not
an instantaneous emergency stop**. Keep an independent means of stopping motion.
TCP clients that stop reading time out on bridge writes after one second; idle
connections during acquisition remain valid. Windows serial reads use a
100 ms timeout, and writes have a 500 ms timeout. The port is opened with
`FILE_FLAG_OVERLAPPED`, with a separate event for each read/write. Close calls
`CancelIoEx` and joins pending operations before releasing their buffers/handles.
This prevents a pending serial read from serializing outgoing keepalive writes.

Verification: portable forwarding/lifecycle tests run without hardware and the
Windows executable is cross-compiled. Actual Windows COM access, driver timeout
behaviour, USB removal/reconnect, Arduino reset timing, and physical stage movement
remain hardware acceptance tests. Test on a secured stage without a radioactive
source before operational use.

### Instrumented test release 20260930.2

The first physical scan returned `ERROR motion_interrupted` before acquisition.
The one-second watchdog or an explicit stop produces that old firmware response;
the old logs cannot prove which transport hop delayed a keepalive. The synchronous
Windows I/O implementation was a suspect, not a hardware-confirmed root cause.

The new bridge remains compatible with the existing PETXY/1 firmware. It adds
overlapped I/O, Arduino boot waiting, a `-version` flag, and opt-in byte-chunk timing:

```powershell
.\source-stage-bridge.exe -version
.\source-stage-bridge.exe -serial COM4 -listen 127.0.0.1:19010 -debug -log-file .\bridge-test.jsonl
```

Stop the old bridge first, only while no scan is active. Do not run two bridges
against the same COM port. Logs append and are not automatically rotated: enable
debug for bounded tests and archive the file after each test. Each session includes
direction, bytes received, inter-chunk gap, write duration, and final byte totals.
Debug previews are limited to 96 bytes and JSON-escaped. A `K\n` received from TCP
but not promptly forwarded points to serial write latency; a late TCP arrival
points upstream. These are host-side timings, not proof of physical USB delivery.
Writes taking at least 100 ms warn even without debug. Synchronous diagnostic
output can itself add latency: avoid pausing/selecting the console during tests.

The updated backend logs each exchange duration, sent keepalive count, and maximum
send gap; gaps of 500 ms or more warn, and TCP command/keepalive writes have a 500 ms
deadline. `keepalives_sent` means accepted by the host transport, not acknowledged
by the Arduino. Docker configuration and PETXY/1 handshake are unchanged.

The updated sketch distinguishes `ERROR heartbeat_timeout` from
`ERROR stop_requested`, adding `x`, `y` emitted step counts and `heartbeat_age_ms`.
The watchdog is still **one second**. These counts are not encoder measurements
or a valid origin after an interruption. Old and new backends already reject both
errors; no success reply or acquisition is manufactured. Uploading this sketch
is an explicit operator step, not part of installing the bridge.

Tests: `task test:source-bridge`, `task test:source:firmware`, and
`task build:source-bridge:windows-tests`. Run the resulting
`source-stage-bridge-tests.exe -test.v -test.timeout 30s` on Windows. The latter
uses a private named pipe, not any COM device, to check simultaneous reads/writes
and cancellation of pending I/O. The firmware harness exercises the complete
20,400-pulse first move with periodic keepalives and both stop failure reasons.

Before physical testing, re-establish the source origin after the previous partial
move, check travel and direction, and use a small safe grid without a radioactive
source. Keep independent motor power cutoff available. Capture the bridge JSONL
and Docker logs together. Never disable the watchdog to make a test pass.

### Comparison with the Windows 2 mm test sketch

On 2026-09-30, `Documents\Arduino\X_Y_move_mm\X_Y_move_mm.ino` on the
Windows host was inspected and copied to `.cache/windows-arduino-reference/`.
It describes a TB6600 with Arduino Nano ESP32. Its numeric pins (2/3 and 5/6),
30 microsecond direction setup, positive-direction HIGH, 0.5 mm/turn,
200 full steps/turn, microsteps=1, and 20/3000 microsecond pulse timing match
our firmware and frontend defaults. Use the same board and pin-numbering selection
when compiling; driver DIP settings and real displacement still require physical
verification. No calibration defaults were changed.

The standalone test emits 800 X pulses for +2 mm and zero Y pulses at boot, then
halts. It pauses 300 ms before X and between axes, and has no keepalive watchdog.
The DAQ firmware is idle at boot and moves only on command. The default scan's
first target is X=-25.5 mm, Y=+25.5 mm: -10200/+10200 pulses from zero, X then Y,
about 61.6 seconds total nominal pulse time, not a 2 mm test. There is no fixed
300 ms inter-axis pause; acquisition settling is applied after both axes arrive.
An 11.7-second interruption is not completion of this default move.

## Metadata and recovery

The source-scan form explains every Start-button blocker in a live validation
summary. Each axis span must be an integer multiple of grid pitch (endpoints are
included): for example, -5.5 to +5.5 mm with 1.5 mm pitch is invalid because
11 / 1.5 is not an integer; pitch 1 mm produces 144 positions for that square.
The form also checks finite bounds, ordering, grid size, motor calibration and
step resolution, pulse durations, acquisition limits, and minimum point timeout
including quantized travel. Connection, Ready-state, and origin-confirmation
requirements are shown separately from input errors. Values are never silently
adjusted; backend validation remains authoritative.

Each normal run retains `source_position` in its `manifest.json` and in its API
`RunSummary`. The normal run-history details display the scan, point, row/column,
calculated mm and steps. Metadata contains:

- scan ID and zero-based point/row/column;
- requested X/Y mm, rounded X/Y steps and calculated X/Y mm;
- `position_basis = open_loop_steps_from_operator_confirmed_origin`;
- `configuration_json`: protobuf JSON of the complete grid, calibration, pulse,
  direction, settling, timeout and point stopping configuration.

The same metadata is included in HDF5 run metadata for direct production writes
and JSON-to-HDF5 conversion. It does not change event payloads. Point termination
reasons are `source_scan_time`, `source_scan_events`, or `source_scan_interrupted`.

New source scans have their own sequential numbers: **Source scan 1**, **Source
scan 2**, etc. Their stored IDs are `xy-1`, `xy-2`, etc., independently of the
ordinary run numbers allocated for each position. Numbers survive backend
restarts; allocation reservations in `source-scans/.allocated/` prevent reuse
even if a scan fails or its record is removed. Keep that directory with backups.
Failed allocations can leave gaps.

The scan record is atomically replaced at
`<runs>/source-scans/<scan-id>.json`, with the selected PET configuration, operator,
timestamps, planned points, run IDs and completion markers. A run's position is
persisted when that run is created, before events, so it is retained even if a
crash occurs before the scan record receives its run ID. Runs remain available
through the normal catalog. Select a row in **Source scan history** to inspect its
positions and run IDs. The list includes start time, operator, status and point
counts, with Refresh and pagination. `ListSourceScans` returns the same history;
`GetSourceScan` still accepts an ID for API clients. Previous UUID-based scans
remain readable and appear as legacy scans, without changing their metadata.
Browsing history does not affect acquisition; **Show active scan** returns to
live progress and cancellation always targets the active scan.
An unfinished record loaded after restart is reported as failed;
scans are never automatically resumed and partial runs retain the DAQ's existing
incomplete-run evidence. The latest scan is remembered automatically during a
server session; after a restart, select it from the history list.

## PETXY/1 serial protocol

All messages are ASCII lines. The firmware is idle at boot. The host waits two
seconds after opening a serial device for USB reset, then performs:

| Host command | Device reply | Meaning |
| --- | --- | --- |
| `HELLO` | `PETXY 1` | Protocol version; no motion |
| `CONFIG high_us low_us x_positive y_positive` | `CONFIGURED` | Pulse times and direction levels (0/1); invalidates origin |
| `ZERO` | `ZEROED` | Operator-confirmed present location becomes logical (0,0); no motion |
| `MOVE x_steps y_steps` | `READY x_steps y_steps` | Absolute sequential X/Y move, then stationary acknowledgement |
| `K` | none | Motion heartbeat, sent every 250 ms |
| `S` | `STOPPED` when idle | Stop/invalidate origin |

While moving, `S` or absence of heartbeat for more than 1 second interrupts pulses,
invalidates the origin and replies `ERROR motion_interrupted`. The firmware counts
each emitted pulse rather than adding a whole requested move on interruption.
Cancellation closes blocked I/O; the watchdog stops motion when heartbeats cease.
It is a software watchdog, not a substitute for a physical emergency stop.
Unknown/malformed replies, wrong READY coordinates, disconnects and timeouts fail
the scan. Pulse timing limits are 2–10,000 µs per high/low phase.

Protocol evidence: PETXY/1 is project-defined and simulator/host-test verified;
it is not JANUS/FERSlib protocol and is not hardware-verified. The original
sketch's fixed raster and `POINT`/`READY`/`M` behavior are source-confirmed from
the supplied `.ino`.

## Verification

```sh
task generate
task lint
task test
task test:source
task test:e2e:ci
```

`test:source` includes race-enabled backend tests, Linux serial PTY tests and a
host C++ test of the actual sketch (requires `g++`). Browser tests start independent
CAEN and source-stage simulator processes and verify the persisted per-point runs.
