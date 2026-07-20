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
