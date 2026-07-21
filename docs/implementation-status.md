# Implementation status

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

The production planner populates both chip representations with its effective 64-channel gains, fine thresholds, HV adjustments, shaping selections, discriminator thresholds/mask, fast-shaper source, and source-hardcoded common modes. The bundled host implementation delegates automatic stream synthesis to FPGA firmware, so it cannot provide a host-generated 36-word golden image. Layout comparisons use its `WriteCStoFileFormatted` field boundaries and the official Citiroc 1A slow-control table. Manual stream loading remains intentionally unavailable until all power-control values have explicit requested/default provenance; normal hardware configuration continues using FPGA-assisted loading.

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
