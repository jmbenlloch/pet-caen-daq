# Implementation status

## Phase 3 operator frontend

Started on 2026-07-21:

- a Vue 3, TypeScript, Tailwind CSS, and Vite application now consumes generated
  Protobuf-ES service descriptors through the ConnectRPC browser transport;
- the initial operator dashboard presents authoritative acquisition state,
  reconnect/staleness status, discovered boards, pipeline/storage health, and
  backend diagnostics;
- configuration can be pasted or loaded from a local file, statically validated,
  and submitted through guarded start/stop-and-drain controls with optional raw
  and transport-journal evidence; and
- a tested client-state boundary replaces state from complete snapshots, marks
  telemetry stale after five seconds, reconnects after stream failure, validates
  before start, and stops only the exact active run identity.

The initial frontend is served by Vite during development and proxies the API to
the backend on port 8080. Production static serving, run history/artifact views,
and Playwright browser workflows remain subsequent Phase 3 slices.

The run-control client now retains the authoritative completed `RunSummary`
returned by `StopRun` rather than trying to reconstruct completion from the next
telemetry snapshot. The dashboard presents the latest run's termination reason,
event/raw-batch counts, incomplete state, and artifact kind, size, and SHA-256
metadata. Component coverage exercises discovered-board rendering and the
validated ready-to-running operator workflow through an injected API boundary.
Historical run browsing and artifact download use explicit backend API methods;
the frontend does not infer history from transient telemetry.

The backend can now optionally serve a built operator application from an
explicit `-frontend-dir`. Startup rejects a missing index or symbolic links,
ConnectRPC service paths retain precedence, hashed assets receive immutable cache
headers, the application shell is never cached, missing file-like resources stay
404s, and extensionless browser routes receive the SPA shell. Development
continues to use Vite's same-origin proxy; no broad CORS policy was introduced.

Persistent run history is now an explicit `RunService` API rather than a
telemetry reconstruction. The backend reads bounded versioned manifests,
returns the newest runs first, reports corrupt storage instead of hiding it,
and streams downloads only for regular artifact files recorded by a finalized
manifest. The dashboard loads that history across backend restarts and offers
the recorded decoded, raw, and transport-journal artifacts for download.

Pinned Playwright Chromium workflows now exercise the built application through
the embedded backend and native simulator. They verify discovered topology and
live telemetry, structured invalid-configuration feedback, a complete validated
start/stop-and-drain operation, persistent history across page reload, and an
actual decoded-artifact browser download. Local execution uses Docker through
`task test:e2e`; CI installs the matching pinned Chromium and retains traces and
screenshots on failures.

## Phase 2 acquisition service foundations

Started on 2026-07-21:

- a serialized system-level acquisition state machine now implements the documented disconnected, connection, configuration, run, drain, fault, and recovery lifecycle;
- every accepted transition records a monotonic sequence, actor, timestamp, and previous/next state, while invalid or concurrent conflicting transitions are rejected without mutation; and
- an ordered bounded event pipeline now makes backpressure policy explicit, supports blocking-with-cancellation or immediate rejection, copies submitted buffers, persists raw evidence before decoding, dispatches every DT5202 event qualifier, and surfaces decode/storage failures.

These are internal orchestration primitives. They are not yet wired to a long-running coordinator or ConnectRPC service; the next Phase 2 slice will build run control and snapshot telemetry on these boundaries.

The Phase 2 protobuf contract now defines coarse start/stop operations, structured configuration diagnostics, complete system/run/board/pipeline/storage telemetry snapshots, health and diagnostic vocabulary, and ConnectRPC server streaming. Every streamed message carries a complete independently usable snapshot with instance/run identity, sequence, and observation time. The original discovery-slice fields and enum numbers remain wire-compatible and are deprecated in favor of the complete snapshot representation.

The internal telemetry publisher and ConnectRPC system adapter now implement the snapshot contract. Publisher-owned instance identity, monotonic sequence, and observation time prevent callers from forging stream ordering; every subscriber immediately receives the current full snapshot, including after reconnect. Slow subscribers retain only the newest independently usable update, cannot backpressure acquisition, and are removed on cancellation. A shared staleness predicate covers missing, invalid, and over-age observations.

The ConnectRPC configuration-validation method now uses the same lossless JANUS parser, semantic-owner catalog, and four-link production-topology rules as backend startup. It reports structured severity, field, source-line, and message diagnostics while retaining the deprecated string error list for existing clients. This endpoint performs static validation only; firmware-, readback-, and pedestal-dependent effective-configuration validation remains part of configuration application against discovered boards.

A long-running acquisition coordinator now serializes start and stop operations over the explicit state machine. It synchronizes and clears the stream before start, continuously routes immutable raw/event batches through the bounded pipeline, cancels the reader before an orderly bounded stop-and-drain, and returns to ready only after pipeline finalization. Start, stream, drain, and pipeline failures retain their primary cause, attempt safe cleanup where possible, and leave the system in fault with a queryable diagnostic.

ConnectRPC run-control handlers now validate required identity and the static JANUS configuration before start, enforce exact active-run identity on stop, invoke the long-running coordinator, map lifecycle rejection to typed RPC status, and publish complete running, ready, or fault snapshots. Successful stop records completion time and termination reason and clears the current run only after coordinator drain/finalization succeeds.

The development run writer now directly implements the bounded pipeline's typed event sink for spectroscopy, timing, counting, waveform, service, and test events. Each JSON Lines envelope retains chain/node, qualifier, trigger ID, timestamp, and the complete project-owned decoded event, with 64-bit values encoded as decimal strings. The original Phase 1 spectroscopy append method and envelope kind remain available for replay compatibility.

A storage-backed run-session factory now creates one development run directory and bounded pipeline per coordinator run. It records the start time, optionally captures complete raw batches, persists every typed event, drains accepted work before closing, and exposes an explicit finalize/abort choice. The coordinator finalizes and removes `incomplete` only after successful stop/drain and pipeline closure; start, stream, decode, storage, or finalization failures abort while retaining the marker and primary error.

Restart inspection now scans run storage read-only for `incomplete` markers, reads manifests through a strict size bound, validates schema and directory/run identity, and deterministically reports valid unfinished runs separately from corrupt recovery metadata. It never repairs or removes evidence automatically. The telemetry adapter marks storage degraded and publishes warning/error diagnostics so operators can explicitly inspect, replay, or recover each artifact set.

The bounded pipeline now exposes race-safe operational counters for capacity/depth, accepted and rejected batches, decoded events, decode failures, and raw/event sink failures. Backpressure rejection counts only actual full-queue rejection, while cancellation and closure remain distinct outcomes. A service adapter copies these values into complete telemetry snapshots and promotes sink failure to storage fault without introducing protobuf dependencies into acquisition.

Storage-backed run sessions now expose race-safe decoded-event and raw-batch counts, artifact bytes currently present on disk, run-directory identity, finalization state, and the last observed pipeline/storage error. A service adapter publishes storage health and updates the current run's persisted counters and incomplete state. File inspection ignores symlinks and remains observational; it does not alter durability or finalization behavior.

A cancellable run-health monitor now samples the active storage-backed pipeline immediately and at a configured cadence, coalescing pipeline and storage measurements into one complete telemetry publication per sample. Its tick source is injectable for deterministic tests, slow telemetry consumers remain isolated by the snapshot publisher, and monitor cancellation exits without manufacturing an acquisition fault.

Run control now owns that health monitor for the complete active lifecycle: it starts after acquisition reaches `running` and stops only after coordinator stop/drain/finalization returns. Decoded service events feed race-safe per-board temperature, HV, acquisition-status, and event-count observations into snapshots; HV over-current or over-voltage marks the board faulty. Asynchronous coordinator failures publish a stable `COORDINATOR_FAULT` diagnostic immediately while cleanup continues.

The backend command now runs a long-lived HTTP ConnectRPC service after parsing/classifying the JANUS configuration, validating and discovering the provisioned topology, creating run storage, and reporting restart evidence. Startup transitions `idle -> configuring -> ready`, builds each board's production plan, loads protected-flash pedestal calibration read-only, applies and reads back FPGA/Citiroc/probe/pedestal registers, and publishes per-stage progress or precise failures. HV peripheral setpoints remain disabled unless the operator explicitly supplies `-authorize-hv-config`; an application failure moves the system to fault because the partial effective hardware state is uncertain. The service mounts system and run services and performs bounded graceful HTTP and hardware shutdown on process cancellation.

`StartRun` now applies the exact submitted JANUS configuration through the same production configurator before acquisition and refuses to start unless application returns the system to `ready`. Each run receives its requested raw-capture and transport-journal choices rather than backend-wide defaults. The development manifest records the requesting actor, byte-exact requested configuration, effective per-board plans, complete configuration audit, and the effective evidence-capture choices.

When transport journaling is requested, the coordinator attaches the run writer below DT5215 stream framing before any acquisition read and keeps it attached through orderly stop-and-drain. It detaches the sink before finalization or abort on successful stops, start failures, and asynchronous stream failures, preventing writes to a closed journal while preserving fragments and framing/termination evidence from malformed or truncated transport.

Finalization now calculates exact sizes and SHA-256 digests after closing each stable payload artifact: decoded JSON Lines, optional complete-batch raw capture, and optional transport journal. The manifest persists those records and a successful `StopRun` returns the same artifact metadata in `RunSummary`; the manifest deliberately does not self-hash because embedding its own digest would be circular.

Startup discovery now treats any board carrying the acquisition-running status as interrupted hardware state. Before configuration writes, recovery records `idle -> fault -> recovering`, performs a bounded broadcast stop and drain, attempts a broadcast global reset even when an earlier cleanup step fails, and verifies every discovered board is ready and not running. Success returns to `idle` with a warning diagnostic; failure moves to `disconnected` and joins the original already-running evidence with every stop, drain, reset, verification, and transition error.

A generated ConnectRPC client integration test now exercises the complete simulator-backed service workflow: static validation of the production JANUS document, configuration application on all four boards, run start with raw and journal evidence, live test-pulse telemetry, operator stop/drain, and finalized artifact inspection. It verifies the manifest's requested/effective configuration and audit, JSON Lines event count, raw replay, journal presence, and every returned size/SHA-256 digest against the on-disk bytes.

Failure/backpressure coverage now spans both bounded-queue policies, cancellation while blocked, injected disk/sink and finalization failures, decode failure with complete raw retention, stream disconnect/truncation/malformed framing with transport-journal replay, and missing completion/service or stalled drain. Real-socket coordinator tests prove asynchronous decode/storage completion faults cancel an otherwise blocked stream reader immediately, retain the `incomplete` marker, and preserve either the complete raw batch or lower-level malformed transport evidence. Required artifacts that disappear during finalization now fail the run instead of being silently omitted.

Run-control service errors now use stable bracketed diagnostic codes with consistent ConnectRPC status mapping: invalid identity/configuration uses `InvalidArgument`, existing IDs/directories use `AlreadyExists`, lifecycle/configuration failures use `FailedPrecondition`, overlapping operations use `Aborted`, and storage inspection failures use `Internal`. Run IDs are path-safe and bounded, stop is idempotent for the same completed run, completed IDs cannot restart, existing directories are rejected before hardware mutation, and concurrent start/stop requests are serialized with immediate rejection.

An HTTP integration test now uses the checked-in generated ConnectRPC client against the mounted system handler. It verifies the unary complete snapshot, the stream's immediate initial snapshot, a live sequence/state/run update, cancellation, and a new connection receiving the latest complete snapshot rather than replaying deltas.

## Vertical slice 1: read-only topology discovery

Implemented on 2026-07-20:

- lossless parsing of JANUS assignment syntax, including indexed settings, comments, repeated settings, and the production Windows/CRLF fixture;
- extraction and validation of the four production `Open` connections (TDlinks 0–3, node 0);
- exact little-endian codecs for DT5215 `CINF`, `ENUM`, and `RREG` requests and responses;
- simultaneous connection to TCP 9760 for slow control and TCP 9000 for the data stream;
- read-only validation that links 0–3 are enabled and links 4–7 are disabled, as required by the version-one web-provisioning decision;
- enumeration of exactly one DT5202 on each production link;
- reads of product ID, FPGA firmware revision, and acquisition status registers;
- a typed Go topology returned as JSON by the initial command-line backend;
- a deterministic native-protocol DT5215/DT5202 TCP simulator;
- golden codec and configuration-parser unit tests plus simulator-backed integration tests, including incorrect provisioning and pre-enumeration link states;
- an initial Buf API module and generated Go/ConnectRPC bindings for configuration validation and system snapshots.

The implementation does not use FERSlib or cgo. It does not configure persistent DT5215 link activation: operators must provision links 0–3 enabled and links 4–7 disabled through the concentrator web interface before startup. Discovery sends `ENUM`, but no register writes.

Protocol constants and behavior remain source-derived until checked against a real packet capture and hardware. In particular, DT5215 link-status numeric meanings beyond zero meaning disabled are provisional.

## Run locally

Run all non-hardware tests:

```sh
task test
```

Generate checked-in protobuf and ConnectRPC bindings:

```sh
task generate
```

Build both commands:

```sh
task build
```

Start the simulator in one terminal and run the DAQ command in another. The simulator command defaults to loopback ports 9760 and 9000; integration tests request ephemeral ports so they can run concurrently.

The next protocol work should add the complete configuration/register-write sequence, Citiroc configuration bitstream, acquisition state machine, data-stream framing, and Run 54 processed-event compatibility decoder. Each step should retain captured raw bytes so it can later be compared with the real PCAP.

## Phase 1 offline decoding and development storage

Implemented on 2026-07-20:

- bounded decoding of complete DT5215 descriptor-table batches into immutable payload events, including sentinel, chain, row, node, payload range, size, qualifier, timestamp, trigger ID, and CRC-flag validation;
- a streaming JANUS processed-list format 3.4 reader for DT5202 spectroscopy-plus-timing data;
- a golden compatibility test over all 256 records and 12,988 channel hits in the committed production Run 54 prefix, including its complete 25-byte header and exact end-of-file boundary;
- malformed-header, invalid-size, truncated-record, stream-framing, and fuzz coverage;
- a lightweight development run repository with atomic JSON manifests, append-only JSON Lines event envelopes, string-encoded 64-bit counters, bounded replay, precise line/offset errors, and an incomplete-run marker that is removed only after successful synchronization and finalization.

The JANUS list reader is an offline compatibility component: its fixture is processed output rather than raw TCP evidence. The development storage is not yet connected to acquisition orchestration.

## Phase 1 synchronized control and stream slice

Implemented on 2026-07-20:

- byte-exact native codecs and client operations for board register writes, immediate and delayed board commands, chain synchronization/reset, and concentrator stream clearing;
- an incremental descriptor-batch TCP reader that uses exact reads, validates allocation bounds before reading payloads, and handles arbitrary TCP fragmentation;
- simulator-backed mutable registers, synchronization state, broadcast commands, acquisition start/stop status, global reset, and queued deterministic stream chunks;
- native DT5202 spectroscopy and spectroscopy-plus-timing payload decoding for single- and both-gain layouts, time references, first-hit timing behavior, and malformed input, with fuzz coverage;
- an integration workflow that round-trips a register, proves acquisition start is rejected before synchronization, synchronizes and starts four boards, receives a deliberately fragmented descriptor batch, decodes its synthetic test-pulse energy/timing event, and stops all boards.

At this point simulator batches could be explicitly queued by tests, but command-triggered generation and stop/drain behavior remained. Complete production configuration translation and the 1,144-bit Citiroc streams were also still outstanding.

## Phase 1 raw capture and command-triggered test pulses

Implemented on 2026-07-20:

- a versioned `wire.raw` format with an eight-byte `PETRAW` magic/version header and independently length-delimited, CRC32-protected DT5215 batches;
- bounded streaming capture/replay with byte-exact deterministic output and record/offset diagnostics for invalid versions, truncation, invalid sizes, and checksum failures;
- optional raw capture integrated into the development run writer, synchronized and closed before the incomplete-run marker can be removed;
- byte-exact raw batch access alongside typed stream events, ensuring capture occurs without reconstructing wire evidence from decoded values;
- command-triggered deterministic simulator test pulses for every running production board;
- a real-socket integration path that starts four boards, issues one broadcast test pulse, captures four raw batches, decodes their energy/timing events live, and deterministically replays the captured batches offline.

The simulator still needs explicit drain completion/fault behavior and broader event qualifiers. The raw format stores complete validated DT5215 batches; retaining malformed or connection-truncated byte fragments will require a lower-level transport journal if hardware validation shows that evidence is needed.

## Phase 1 persisted test-pulse run

Implemented on 2026-07-20:

- a storage-independent test-pulse acquisition coordinator with the explicit sequence synchronize, clear stream, start, pulse, bounded drain, and stop;
- unconditional stop after successful start, with stop failures joined to rather than replacing the primary acquisition failure;
- raw capture before descriptor CRC validation or event decoding, followed by duplicate/unexpected-chain checks and typed spectroscopy/timing decoding;
- JSONL decoded-event persistence with stable snake-case fields and all 64-bit identifiers, timestamps, masks, and sequences represented as decimal strings;
- a complete simulator-backed persisted run integration test that produces four decoded events plus four raw batches, finalizes the manifest, removes the incomplete marker, and deterministically replays every raw event.

This completes the initial Phase 1 test-pulse vertical slice for the currently implemented spectroscopy/timing path. It is intentionally a bounded one-pulse coordinator, not the Phase 2 long-running acquisition state machine. Remaining Phase 1 protocol breadth includes production configuration/Citiroc translation, other DT5202 event qualifiers, and simulator drain/fault modes.

## Phase 1 DT5202 register vocabulary

Implemented on 2026-07-20:

- a project-owned typed map of every common and DT5202-specific FPGA register used by the bundled JANUS/FERSlib implementation;
- typed definitions for all DT5202 commands and acquisition-status flags; and
- source-confirmed individual-channel and broadcast-address conversion with byte-exact tests.

This map intentionally excludes registers belonging only to other FERS board families and peripheral-internal sub-register spaces. Production configuration translation and Citiroc stream construction build on this vocabulary and remain incomplete.

The production configuration's complete 103-assignment document is also covered by an explicit semantic-owner catalog. Unknown or misspelled settings now fail classification with their source line instead of being silently ignored. Ownership does not yet imply implementation: hardware translation, run-control, storage, and analysis consumers must each prove requested-versus-effective behavior before configuration coverage is complete.

The production DT5202 FPGA subset now has a pure configuration planner. It applies global values followed by per-board overrides, performs strict option/unit/range parsing, and emits ordered effective register writes using the 8 ns DT5202 clock. The production fixture yields 349 writes per board, including 64-channel gains, fine discriminator thresholds, and HV adjustments, with distinct effective timing thresholds of 181, 183, 179, and 178. Plans can compare their final requested values with a register readback snapshot and diagnose exact missing or mismatched addresses. Settings requiring pedestal calibration reads, probe sequencing, or HV peripheral commands remain explicitly deferred in each plan.

## Phase 1 Citiroc layout and automatic loading

Implemented on 2026-07-20:

- a bounded 1,144-bit/36-word Citiroc stream type with the bundled source's exact bit and word ordering;
- placement for both chips' fine time/charge thresholds, nine-bit HV adjustment, six-bit HG/LG gains, calibration flags, preamplifier disable, discriminator mask, shaping, coarse thresholds, and every documented common control bit;
- explicit mapping of board channels 0--31 to Citiroc 0 and 32--63 to Citiroc 1; and
- the source-confirmed normal FPGA-assisted ASIC configuration sequence for both chips, including fail-fast error propagation.

The production planner populates both chip representations with its effective 64-channel gains, fine thresholds, HV adjustments, shaping selections, discriminator thresholds/mask, fast-shaper source, and source-hardcoded common modes. Layout comparisons use the JANUS `WriteCStoFileFormatted` field boundaries and the official Citiroc 1A slow-control table.

The verification path now reproduces JANUS `ReadSCbsFromChip`, captures both complete FPGA-generated 36-word streams without enabling manual loading, restores the slow-control selector, and compares all 1,144 bits with exact word/bit diagnostics. An exhaustive 52-entry provenance catalog covers every ASIC enable and power-mode bit. The Citiroc 1A V2.53 datasheet establishes their position and semantics; JANUS establishes the forced-on OTA policy, while all other automatic-load power values are explicitly FPGA-owned and require real-board readback evidence. Manual stream loading remains unavailable until those authoritative captures are committed; normal hardware configuration continues using FPGA-assisted loading.

## Phase 1 configuration application and readback

Implemented on 2026-07-20:

- an explicit configuration-application boundary that can perform a JANUS-compatible hard reset, execute every ordered write, load both Citiroc ASICs, and read back every unique requested register;
- fail-fast diagnostics carrying board, chain, node, write index, and register address, with no ASIC load after a failed register write;
- requested-versus-effective validation over actual DT5215 register reads rather than only an in-memory snapshot;
- simulator modeling of per-chip `CMD_CFG_ASIC` effects and reset behavior; and
- a real-socket integration test that plans, applies, reads back, and validates the complete production FPGA configuration on all four simulated DT5202 boards.

The apply path currently covers the 354-write FPGA/Citiroc-register subset per board. HV-module peripheral commands, pedestal calibration, and spectroscopy-mode zero-suppression values that depend on measured pedestals remain explicit configuration gaps.

## Phase 1 production probe and inactive-setting semantics

Implemented on 2026-07-21:

- source-confirmed analog-probe selection for both Citirocs, including channel selection and chip switching;
- firmware-5-or-later digital-probe byte packing for both probe outputs;
- strict validation of test-pulse destination/preamp and zero-suppression values even when their controlling mode makes them inactive; and
- explicit requested-versus-effective reasons for production settings made inactive by `TestPulseSource OFF`, `AcquisitionMode SPECT_TIMING`, or disabled analog probes.

The production fixture now distinguishes inactive settings from unsupported/deferred settings. Probe settings are applied and read back; energy zero-suppression remains deferred only for spectroscopy mode, where its effective values require pedestal calibration data.

## Phase 1 production semantic audit

Implemented on 2026-07-21:

- an ordered audit record for every topology, hardware, run-control, storage, and analysis assignment in the imported JANUS configuration;
- explicit applied, inactive-with-reason, and rejected dispositions, including per-board effective hardware targets;
- rejection of unresolved calibration-dependent settings and firmware revisions older than the firmware-5 digital-probe representation;
- preservation of board firmware evidence and the complete requested/effective audit in `manifest.json`; and
- production-fixture coverage proving that all 103 assignments have a disposition and that every applied assignment has an effective value.

The Phase 1 JSON Lines runstore and service-supplied run directory are recorded as the effective storage behavior. JANUS binary/text products, online histograms, and disabled job/coincidence features remain explicitly inactive rather than being silently ignored.

## Phase 1 HV peripheral configuration

Implemented on 2026-07-21:

- strict voltage/current unit parsing and production-range validation;
- named and three-coefficient temperature-sensor translation;
- exact signed/fixed-point ×10,000 peripheral encoding;
- source-ordered bus initialization, PID, duplicated voltage/current, duplicated sensor-coefficient, and duplicated temperature-feedback transactions;
- bounded I2C busy polling with failure and cancellation handling; and
- simulator-backed application of the production 45.4 V, 1.0 mA, TMP37, feedback-disabled plan on all four boards.

HV application is deliberately separate from ordinary FPGA configuration so callers must explicitly authorize the safety-relevant setpoint operation. This completes production fixture translation except for pedestal calibration; spectroscopy-only zero suppression remains dependent on those measured pedestal values.

## Phase 1 pedestal and zero-suppression semantics

Implemented on 2026-07-21:

- an immutable, provenance-tagged 64-channel LG/HG pedestal calibration model;
- source-compatible host-side energy correction with zero/14-bit saturation;
- explicit completion of the production `Pedestal` request from supplied board calibration evidence;
- per-channel spectroscopy-mode LG/HG zero-suppression translation, including disabled thresholds and source-compatible unsigned wrapping; and
- confirmation that spectroscopy-plus-timing leaves energy-only zero suppression inactive while still applying host-side pedestal correction.

With deterministic calibration evidence supplied, the complete production fixture has no deferred hardware-owned settings in the four-board simulator integration path. Reading the protected pedestal flash page natively remains necessary before real-hardware configuration; no code writes or modifies calibration flash.

## Phase 1 complete DT5202 event decoding

Implemented on 2026-07-21:

- qualifier-dispatched, project-owned decoding for timing common-start, timing common-stop, timing streaming, counting, waveform, service-version 0/1, forward-version service headers, and test events;
- spectroscopy and counting relative-timestamp qualifier handling plus all source-confirmed spectroscopy single/both-gain and optional-timing combinations;
- source-compatible timing-reference, leading-edge-only, ToT, T-OR/Q-OR, waveform-probe, temperature, HV-monitor/status, board-status, and service-counter field translations; and
- byte-exact golden vectors, explicit unsupported-qualifier diagnostics, bounded timing/test data, malformed/truncated/oversized-event tests, and qualifier-dispatch fuzz coverage.

These layouts are `source-confirmed` against the bundled JANUS/FERSlib decoder; real DT5215 capture compatibility remains scheduled for Phase 4. The Go decoder deliberately rejects out-of-range and duplicate channel entries that FERSlib silently ignores or overwrites, and bounds fixed-size vendor arrays before decoding.

## Phase 1 expanded simulator event behavior

Implemented on 2026-07-21:

- deterministic command-triggered generation for spectroscopy, spectroscopy-plus-timing, timing common-start/common-stop, counting, and waveform acquisition modes;
- deterministic service-version-1 events at acquisition start when service events are enabled;
- register-backed channel-enable and charge/time discriminator masks, per-channel gains and fine thresholds, coarse timing thresholds, counting zero suppression, and waveform length; and
- simulator pedestal calibration and raw-energy generation whose host-side correction recovers the deterministic signal, plus spectroscopy zero-suppression filtering from effective per-channel thresholds.

Pure conformance tests decode every generated event through the project-owned DT5215 and DT5202 decoders. A real-socket integration test changes acquisition modes on a running simulated board and verifies the resulting service, timing, counting, and waveform events. These behaviors are deterministic development models, not claims about the exact physical detector response.

## Phase 1 stop and drain

Implemented on 2026-07-21:

- an explicit stop-and-drain operation that broadcasts stop, continues delivering complete pending batches, and completes only after every expected chain reports ready status in a service event;
- deterministic simulator completion events after stop, idempotent repeated stop commands, and FIFO delivery of data queued before completion;
- bounded drain through caller cancellation/deadline with distinct incomplete-chain diagnostics for missing completion, missing service events, stalled streams, disconnects, and timeouts; and
- joined error handling that preserves the original acquisition failure when stop, drain, raw capture, decoding, or pending-event storage also fails.

The persisted test-pulse coordinator now performs this orderly stop-and-drain cleanup even when its acquisition context was canceled, using a separate bounded cleanup context. The ready-status service completion is a deterministic simulator/application contract inferred for testability; Phase 4 captures must establish the real DT5215 end-of-data signal or replace this inference with a capture-verified no-data/status mechanism.

## Phase 1 deterministic simulator faults

Implemented on 2026-07-21:

- a validated FIFO one-shot fault script whose entries can target a specific control opcode or the next generated/queued stream batch;
- control and command delays, explicit timeouts, control disconnects, and replies truncated after an exact byte count;
- stream delays, disconnects, exact-byte truncation, malformed descriptor nodes, impossible payload sizes/offsets, and descriptor CRC flags; and
- missing start service events, missing drain completion, and stalled-drain scenarios.

Every fault is deterministic and consumed only by its matching operation class, so unrelated control and stream activity cannot reorder a scenario. Real-socket integration tests assert the corresponding timeout, cancellation, EOF, framing, CRC, and incomplete-drain diagnostics. Fault behavior remains a test facility and does not change the source-confirmed normal protocol path.

## Phase 1 native pedestal flash loading

Implemented on 2026-07-21:

- a strictly read-only AT45DB321 main-memory page-read sequence for protected DT5202 pedestal page 4, using only opcode `0xD2` and SPI dummy clocks;
- exact validation of the 272-byte format-0 page, including `P` tag, Gregorian calibration date, four valid 12-bit DC offsets, and 64 little-endian LG plus 64 HG values within the 14-bit energy range;
- an immutable result carrying page, format, date, offsets, and provenance-tagged calibration without automatically applying DC offsets or exposing any flash program/erase operation; and
- a byte-exact synthetic source-confirmed fixture with metadata, malformed-page and I/O tests, parser fuzz coverage, and a four-board real-socket simulator integration test.

The simulator models the read transaction and rejects the page-program opcode. Protected flash is never modified. Fixture contents are synthetic from the bundled source layout rather than hardware-captured evidence; authoritative page captures remain scheduled for Phase 4.

## Phase 1 raw transport evidence journal

Implemented on 2026-07-21:

- a versioned, length-delimited, CRC32-protected `transport.journal` alongside the existing complete-batch `wire.raw` format;
- journaling of every successful socket-read fragment before DT5215 header, descriptor, extent, CRC, or payload validation, with connection identity, continuous byte offset, timestamp, and framing stage;
- explicit framing-failure and connection-termination records carrying the observed offset and reason, including cancellation/deadline classification; and
- deterministic per-connection replay that reconstructs the exact observed byte stream and ordered failures, rejects offset gaps/corruption, and reproduces truncated-header and malformed-descriptor failures offline.

The development run writer can create, synchronize, and close the transport journal with the other run artifacts. Complete validated batches continue to be stored in `wire.raw`; the two formats intentionally preserve different evidence boundaries. Real DT5215 capture fixtures remain scheduled for Phase 4.
