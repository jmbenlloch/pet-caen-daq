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
`manifest.json`, `events.jsonl`, and any requested `wire.raw` and
`transport.journal` artifacts and verify the manifest sizes and SHA-256 values.

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
