# Hardware operations

These procedures cover the version-one four-board topology. They do not replace
site electrical-safety rules or the DT5215/DT5202 manuals.

## Provisioning

In the DT5215 web interface, enable TDlinks 0 through 3 and disable links 4
through 7. Connect exactly one DT5202 at node 0 on each enabled link. The DAQ
validates this state and never changes persistent link enablement.

## Read-only acceptance

Disconnect JANUS and other control clients, preserve the configuration used for
the check, and run:

```sh
task hardware:inspect CONFIG=path/to/config.txt
```

The inspection opens the DT5215 control and stream TCP connections, sends only
chain-information (`CINF`) and register-read (`RREG`) requests, and exits. It
does not bind the HTTP API, create run storage, reset or enumerate links,
synchronize boards, issue commands, or write registers. A successful result
ends with `inspection complete mode=read-only hardware_writes=0` and lists four
boards whose acquisition status is ready and not running.

If an expected link is in a pre-enumeration state, inspection fails rather than
initializing it. Use the web interface to confirm physical provisioning before
allowing the normal backend startup to perform runtime link initialization.

Retain the console output together with the configuration, date, operator,
DT5215 firmware report, DT5202 FPGA/PIC firmware reports, and any packet capture.
Record hashes for every retained input.

## Controlled acquisition

Start the backend without `-authorize-hv-config` unless applying the configured
HV setpoints has been explicitly approved. This default leaves HV peripheral
setpoints untouched. On Windows, `scripts\take-data.ps1` performs a bounded run,
requests raw and transport-journal evidence, monitors state, and attempts an
orderly stop after interruption.

Before a detector run, confirm that the backend is ready, all four expected
boards are listed, storage has enough free space, the SQLite catalog can allocate
a unique monotonically increasing numeric run ID, and the
submitted configuration is the intended byte-exact document. Afterward, retain
`manifest.json`, all production `run_<run-id>.0000.h5`, `run_<run-id>.0001.h5`, … segments,
and any requested `wire.raw` and `transport.journal` artifacts and verify the
manifest sizes and SHA-256 values.

Starting a run reuses the configuration that was successfully applied when the
hardware connected, or by the most recent successful configuration request,
when the newly submitted parsed assignments are identical. Comments, blank
lines, and source line numbers do not force reconfiguration; assignment order,
scope, or value changes do. Changed configurations still use the complete hard
apply and readback validation.

Protected-flash pedestal calibration is read and validated once per board when
the current hardware connection first needs it. Later configuration changes on
that connection reuse the validated calibration; disconnecting or restarting
creates a new hardware session and forces fresh pedestal reads.

During a hard configuration, the operator UI reports the current board and
stage: planning, pedestal loading, register writes, Citiroc programming,
register readback, and high-voltage setup. Register stages include completed
and total operation counts, while a separate bar reports progress across all
boards. Pedestal reuse is identified explicitly. This information is part of
the latest `TelemetrySnapshot`, rather than a transient log stream, so a
browser that reconnects during configuration immediately receives the current
operation, stage, counters, and original start time. Intermediate register
updates are published every 50 operations and at stage boundaries to keep
telemetry bounded without hiding exact completion totals. A failed operation
remains visible with its last stage and error message.

DT5215 `SNT0` synchronization is session-scoped. Run start skips it only when
discovery verified the TDlink-synchronized acquisition-status bit on every
board, or after `SNT0` has succeeded once on the current connection.
Reconnecting creates a new session with no inherited synchronization evidence.

## Fault and recovery

If acquisition faults, do not delete or edit the run directory. Preserve its
`incomplete` marker and transport evidence. Stop sending commands from other
clients, record the backend diagnostic and visible hardware state, and restart
the backend only after the cause is understood. Startup detects boards left
running and attempts bounded stop, drain, global reset, and ready-state
verification while preserving the original failure.

For cable loss, concentrator restart, persistent topology mismatch, or failed
startup recovery, power or reconnect hardware only under the site's approved
procedure. Repeat read-only acceptance before another configured startup. Never
write `VR_ENABLED_LINKS`; correct persistent TDlink provisioning in the DT5215
web interface.
## Runtime connection control

The backend starts its API before hardware discovery and remains available if
the DT5215 cannot be reached. It attempts one connection at startup. The
operator dashboard can retry with **Connect hardware** after restoring the
network or concentrator, and can close both DT5215 transports with
**Disconnect hardware**.

Connecting repeats topology discovery, startup recovery, configuration, and HV
monitor initialization. Disconnecting is allowed only when no run is active;
stop and drain the run first. A disconnected backend continues to serve run
history and configuration validation, but rejects acquisition and HV commands.
