# ADR 0007: Start with JSON storage and deliver HDF5 for production

- Status: accepted
- Date: 2026-07-20

## Context

The first production release must store acquisition data in HDF5 for downstream analysis. HDF5 introduces a comparatively heavy native dependency, packaging work, schema/performance choices, and a larger integration-test surface. Requiring it before implementing the binary protocol, simulator, decoders, acquisition state machine, and backend would delay the highest-risk hardware work.

Development requires an appendable, inspectable, replayable event representation plus human-readable run metadata. Keeping this initial representation simple is more important than optimizing storage density before the protocol and event model stabilize.

## Decision

Use phased storage behind project-owned interfaces.

### Development storage

Each development run uses:

- `manifest.json`: bounded run metadata, configuration snapshot, topology, firmware/software identity, lifecycle timestamps, termination state, counters, warnings, artifact hashes, and format versions;
- `events.jsonl`: one independently parseable, versioned JSON event envelope per line;
- `wire.raw`: optional byte-exact DT5215 stream/control evidence with sufficient side metadata for replay;
- an incomplete marker or equivalent atomic-finalization mechanism.

The exact envelope schema, 64-bit integer representation, flush/finalization, and durability rules will be specified and golden-tested during implementation. Readers must stream, enforce line-size bounds, detect a truncated final record, identify malformed records precisely, and reject unsupported versions clearly.

YAML and monolithic JSON event arrays are not acquisition formats. A monolithic array would require whole-document finalization and would leave awkward invalid output after a crash; JSON Lines remains appendable and record-oriented.

### Production storage

The first production release adds HDF5 as the primary decoded analysis output. Its writer consumes the same project-owned event and run types through the storage interface. HDF5 types and handles remain confined to the adapter.

Protocol, decoder, simulator, API, and normal unit-test packages must build and run without HDF5. HDF5 integration and performance tests may use pinned containers.

## Alternatives considered

- HDF5 from the first implementation commit: rejected because native dependency and schema decisions would block protocol and backend development.
- Monolithic JSON event arrays: rejected because they require invalid/unbounded whole-document handling during acquisition and leave awkward invalid output after interruption.
- Framed Protobuf: compact, typed, and streamable, but deferred because human inspection and minimal initial implementation are the current priority. It can be introduced later if JSON Lines becomes a measured development bottleneck.
- YAML event storage: rejected because it is not a good large append-only numeric format and adds parsing ambiguity/complexity without an advantage over JSON metadata.
- A custom binary event schema: rejected because it would add format design and tooling before performance requirements are measured.

## Consequences

- Protocol, decoder, simulator, and backend work can begin without native HDF5 tooling.
- The lightweight format is explicitly a project development/replay format and still requires versioning, bounds, numeric-fidelity rules, truncation tests, and documentation.
- Storage is replaceable at the writer boundary, not by changing decoded event structures.
- Offline replay/conversion becomes a first-class workflow and the route for producing HDF5 from retained development/raw runs.
- HDF5 schema, chunking, compression, flush/durability, and analysis compatibility require a later focused decision before production acceptance.
