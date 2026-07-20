# System architecture

Status: initial design constraints, not yet an implementation specification.

## Goals

- Reliably configure and acquire from four DT5202 boards through one DT5215.
- Make every run reproducible from saved configuration, topology, software version, and firmware metadata.
- Support normal development and CI without hardware.
- Preserve raw data for later replay and protocol verification.
- Give operators clear state, diagnostics, and safe controls through a browser.
- Keep native protocol details isolated so acquisition policy and the UI API do not depend on wire-format implementation details.

## Non-goals for the first milestone

- Firmware upgrading.
- Automating the DT5215 private web interface.
- Multiple concentrators.
- Distributed event building across hosts.
- Long-term physics-analysis tooling.

These may be added later without placing their assumptions into the initial core.

## Context

```text
Browser
   |
   | ConnectRPC (HTTP)
   v
Go DAQ service ---- run metadata / raw and decoded files
   |
   | hardware interface
   +---- real DT5215: TCP 9760 control + TCP 9000 stream
   |
   `---- simulator: the same externally observable protocol
              |
              `---- simulated TD5202 chains, registers, and events
```

The physical USB cable to the DT5215 creates an IP network interface. The backend therefore treats the concentrator as a network device.

## Backend components

### Hardware transport

Owns TCP connections, deadlines, complete writes, exact reads, cancellation, connection health, and byte counters. It knows no detector semantics.

### DT5215 protocol

Encodes slow-control requests and parses replies, chain information, descriptor tables, and stream payload placement. Protocol evidence status should be referenced in tests and documentation.

### DT5202 device and decoder

Owns project-maintained register names/fields, command codes, configuration translation, Citiroc configuration, and typed event decoding. It is implemented natively in Go and must be checked against source-derived vectors, simulator conformance tests, real packet captures, and hardware results.

### Acquisition coordinator

Owns one system-level state machine. Proposed states:

```text
disconnected -> connecting -> idle -> configuring -> ready
ready -> starting -> running -> stopping -> draining -> ready
any active state -> fault
fault -> recovering -> idle/disconnected
```

Transitions are serialized. Each transition records who requested it, timestamps, result, and diagnostic context. `Stop` must be idempotent where practical. A process restart must recognize that hardware may still be running and explicitly recover it.

### Event pipeline

Separates stream ingestion, framing, decoding, monitoring, and storage with bounded buffers. Raw capture is upstream of decoding so malformed or newly introduced event formats can be replayed. Queue sizes and overflow behavior must be configuration, metrics, and test concerns rather than implicit channel behavior.

### Run repository

Stores immutable run identity and snapshot metadata: requested/effective configuration, topology, board IDs, firmware versions, software revision, start/stop times, termination reason, counters, warnings, and output artifacts. Storage technology remains undecided.

### ConnectRPC service

Exposes coarse operations rather than register-level UI coupling. Initial service areas are expected to include:

- system discovery and health;
- configuration validation/application;
- run start/stop and current state;
- live rates, temperatures, buffer occupancy, warnings, and errors;
- run history and artifact metadata.

Raw register access should be a separately controlled diagnostic API, not part of the normal operator workflow.

## Frontend components

The Vue application is a stateless client of the backend except for ephemeral presentation state. It should provide:

- connection/topology and firmware overview;
- editable configuration with units, ranges, and validation;
- explicit ready/start/stop/drain/fault state presentation;
- live per-board/channel rates and hardware health;
- run metadata/history and diagnostic download links;
- prominent warnings for stale telemetry, reconnects, incomplete drain, and unsafe settings.

Do not reproduce backend state transitions or hardware calculations in the frontend. Generated Connect clients and shared protobuf enums are the contract.

## Simulator

The simulator should listen on the same DT5215 TCP ports and reproduce observable command and stream behavior. It needs deterministic modes for:

- configurable chain/node topology and firmware identity;
- register reads/writes and command effects;
- enumeration/synchronization/start/stop;
- spectroscopy, timing, counting, waveform, service, and test events;
- deterministic seeded event generation;
- partial TCP delivery and batching;
- delays, timeouts, disconnects, malformed descriptors, CRC flags, missing service events, and stalled drain.

The simulator is a test double, not the protocol authority. Golden captures from real hardware supersede simulator assumptions.

## Native hardware implementation

The production backend implements the DT5215/DT5202 protocol directly in Go. It does not call or link FERSlib. This includes:

- DT5215 TCP control and streaming transports;
- slow-control command/reply encoding;
- chain discovery, enumeration, reset, and synchronization;
- DT5202 register access and complete configuration translation;
- Citiroc slow-control bitstream construction;
- acquisition sequencing and stream framing;
- all required DT5202 event decoders.

The bundled FERSlib/JANUS source remains valuable evidence and an offline comparison oracle. Tests may compare native results with recorded FERSlib/JANUS outputs, but builds, deployment images, and normal runtime behavior must not depend on the library.

## Cross-cutting requirements

- Structured logs and Prometheus-compatible metrics.
- Traceable run and request IDs.
- Explicit configuration schema/version and units.
- Authentication/authorization before exposure beyond a trusted control network.
- Graceful shutdown with stop/drain deadlines.
- Reproducible containers and generated code.
- No CI dependency on physical hardware.
