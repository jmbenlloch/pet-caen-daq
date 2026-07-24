# ADR 0009: Runtime hardware connection lifecycle

## Status

Accepted.

## Decision

The backend HTTP and ConnectRPC services start independently of the DT5215.
At startup the backend begins one hardware connection attempt in the
background. A failed attempt leaves the process and operator UI available in
`DISCONNECTED` state.

`SystemService.ConnectHardware` and `SystemService.DisconnectHardware` own
operator-triggered connection changes. Connecting performs the same discovery,
startup recovery, configuration, and monitor setup as the initial attempt.
Disconnecting closes the DT5215 control and stream transports and removes
hardware topology from telemetry.

Disconnect is rejected while a run is active. Run control, configuration, and
HV operations use a synchronized runtime delegate, so they fail with a clear
precondition error while disconnected and cannot race transport teardown.

## Consequences

- Run history, configuration editing/validation, and backend telemetry remain
  available when the hardware or its network is unavailable.
- The frontend reports backend telemetry connectivity separately from the
  hardware lifecycle represented by `TelemetrySnapshot.state`.
- Operators must stop and drain an acquisition before disconnecting hardware.
- Every reconnect performs full topology validation and startup configuration;
  an old hardware session is never reused.
