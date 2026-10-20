# 0010 — Host-controlled XY source scans

Status: accepted implementation decision.

The supplied Arduino sketch starts a fixed raster at boot. Its M/READY handshake
cannot configure a grid, restart a scan or reliably preserve position after an
interrupted move. PET source scans also require exclusive DAQ ownership between
positions and an auditable association between acquisition data and source position.

The backend plans a serpentine grid in absolute integer motor steps. A dedicated
ConnectRPC service owns the hardware for the scan lifetime and runs the existing
PET start/stop/drain workflow once at each stationary point. Every point is an
ordinary cataloged run, with source metadata added through the storage interfaces.
A separate atomic scan record maps points to run IDs. Source scans use a separate
persistent sequential identity (`xy-1`, `xy-2`, ...), with exclusive filesystem
reservations preventing reuse or concurrent allocation collisions. A paginated
history API includes both numbered scans and previous UUID-based records.
No event schema changes or
new production storage dependency are needed.

Use replacement, idle-at-boot PETXY/1 firmware with configuration, explicit origin,
absolute movement acknowledgements and a motion heartbeat watchdog. Preserve the
original sketch as the reference. Do not infer a physical position from an
unacknowledged move, automatically resume after restart, or label open-loop step
coordinates as measured positions. An independent TCP device simulator supports
development and fault injection; Linux serial uses the same protocol driver.

For a Linux Docker backend on Windows, a separate Windows executable owns the
USB COM port and forwards the existing TCP byte stream. It accepts one controller
at a time, closes serial on disconnect and never generates keepalives or replays
commands. This avoids requiring Docker USB passthrough and leaves protocol and
scan ownership in the backend. The unauthenticated listener defaults to loopback;
non-loopback deployment requires restricted firewall access. Cross-compilation
and portable forwarding tests are not Windows hardware verification.

The current contract uses one calibration for both axes, matching the supplied
mechanics. Direct non-Linux backend serial transport, limit switches, encoder feedback,
homing, automatic return and source-scan catalog search are future additions.
