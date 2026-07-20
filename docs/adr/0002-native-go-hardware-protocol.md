# ADR 0002: Implement the hardware protocol natively in Go

- Status: accepted
- Date: 2026-07-20

## Context

JANUS 5.0.0 includes the FERSlib source implementing DT5215 TCP slow control and streaming, DT5202 configuration, Citiroc bitstream construction, acquisition sequencing, and event decoding. The project is intended to own its DAQ implementation rather than depend on the FERSlib API.

The protocol is incompletely documented by the public manuals, so source-derived behavior must still be validated against real packet captures and hardware.

## Decision

Implement the required DT5215 and DT5202 binary protocol directly in Go. Production code, normal tests, and deployment artifacts must not link to, dynamically load, wrap, or invoke FERSlib.

The native implementation owns:

- TCP control and streaming connection behavior;
- all required command and reply encoders/decoders;
- concentrator and chain discovery, enumeration, synchronization, and control;
- DT5202 register access and configuration translation;
- Citiroc slow-control bitstream construction;
- run start, stop, draining, and recovery sequencing;
- concentrator stream framing and DT5202 event decoding.

FERSlib/JANUS source and outputs may be used as reference evidence and development-time comparison oracles. They are not runtime dependencies or fallback implementations.

## Alternatives considered

- FERSlib through cgo: rejected because it preserves the vendor API/runtime dependency and makes low-level behavior, concurrency, and error handling harder for the project to own and test.
- Supporting both native and FERSlib production backends: rejected because it would double the operational and conformance surface without serving the project goal.
- Calling JANUS or FERSlib utilities as subprocesses: rejected for the same ownership, observability, and deployment reasons.

## Consequences

- The project must port subtle behavior including exact TCP framing, partial I/O handling, chain synchronization, configuration ordering, and the 1,144-bit Citiroc streams.
- Protocol types and errors will be project-owned and idiomatic Go.
- Simulator, golden-vector, fuzz, replay, packet-capture, and hardware-in-the-loop tests are mandatory risk controls.
- Source-derived behavior remains classified as `source-confirmed` until captures or hardware promote it to stronger evidence.
- Firmware compatibility must be explicit and tested rather than inherited implicitly from an installed FERSlib version.
