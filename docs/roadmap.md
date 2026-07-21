# Initial roadmap

This roadmap orders discovery and risk reduction. It does not commit to dates.

## Phase 0: repository and decisions

- Establish project guidance and protocol evidence notes.
- Select pinned Go, Node, Vue, Tailwind, ConnectRPC, Buf CLI/plugin, Playwright, Task, and unit-test tool versions.
- Add the root `Taskfile.yml` and stable setup, generation, check, build, development, simulator, and CI targets.
- Add the Buf module, lint, breaking-change, and generation configuration.
- Decide monorepo build commands and generated-code policy.
- Define the initial protobuf API and configuration units.
- Specify and test the lightweight JSON/JSON Lines development format; HDF5 is the first production storage target.

Exit criterion: skeleton choices are explicit, reproducible, and reviewed.

## Phase 1: protocol core and simulator

Progress as of 2026-07-21: read-only discovery/topology validation, core slow-control including register writes and synchronization/start/stop commands, the production configuration parser and FPGA/Citiroc/probe/HV/pedestal configuration planning/application paths, explicit inactive-setting reporting, incremental descriptor-table stream decoding, spectroscopy/timing event decoding, Run 54 format-3.4 compatibility decoding, simulator-backed synchronized control and command-triggered test pulses, raw batch capture/replay, lightweight JSON/JSON Lines run storage, and a complete persisted one-pulse run are implemented. Native protected-flash pedestal loading, the remaining event qualifiers, and simulator drain/fault behavior remain.

- Implement byte-level DT5215 slow-control types with golden tests.
- Implement a JANUS-compatible configuration parser and parse the complete production fixture without ignored or silently defaulted fields.
- Implement descriptor-table and DT5202 event decoders with unit/fuzz tests.
- Port the complete DT5202 register/configuration and Citiroc bitstream behavior into project-owned Go code.
- Implement a deterministic TCP simulator for one concentrator and four boards.
- Implement discovery, enumeration, synchronization, and minimal register access against the simulator.
- Validate the provisioned four-link/one-node topology and return actionable errors without attempting persistent link activation.
- Add raw capture/replay fixtures.
- Implement and test a JANUS processed-list format 3.4 reader against the production Run 54 prefix for compatibility and cross-checking.
- Implement the JSON manifest, JSON Lines event writer/reader, incomplete-run marker, and deterministic offline replay without HDF5.

Exit criterion: a Go test can perform a complete simulated test-pulse run and deterministically reproduce decoded events.

## Phase 2: backend service

- Implement the acquisition state machine and bounded event pipeline.
- Define and generate ConnectRPC contracts.
- Implement snapshot-based ConnectRPC telemetry streaming, sequence/staleness handling, and browser reconnection tests.
- Implement configuration validation, run control, health, and telemetry services.
- Persist run metadata and raw data with crash/incomplete-run handling.
- Keep storage behind an interface and add failure/backpressure integration tests using the lightweight writer.
- Add integration, cancellation, disconnect, and recovery tests.

Exit criterion: generated clients can operate complete simulated runs and inspect artifacts.

## Phase 3: operator frontend

- Build topology/status, configuration, run control, monitoring, fault, and run-history views.
- Implement reconnect and stale-state behavior.
- Add unit/component tests and Playwright integration/end-to-end tests against the backend plus simulator.

Exit criterion: an operator can safely complete and inspect simulated runs from a browser.

## Phase 4: real capture validation

- Capture known-good JANUS control and data traffic for the real topology.
- Record all firmware/software/configuration metadata.
- Create sanitized golden fixtures and compare with the current protocol implementation.
- Cross-check event counts, timestamps, energy/timing fields, service events, and stop/drain behavior against JANUS/FERSlib.
- Resolve or document every mismatch.

Exit criterion: protocol paths used by the first production milestone are capture-verified.

## Phase 5: hardware integration

- Run read-only discovery/status tests.
- Run controlled configuration and test-pulse acquisition.
- Validate four-board synchronization, throughput, backpressure, recovery, and long-run stability.
- Produce operating, recovery, and provisioning procedures.

Exit criterion: acceptance criteria on the real system pass with retained evidence.

## Phase 6: production HDF5 storage

- Define the HDF5 schema, datasets, chunking, compression, metadata, and compatibility/version policy from measured run characteristics and analysis needs.
- Implement the HDF5 writer behind the established storage interface.
- Convert/replay golden and hardware runs into HDF5 and cross-check counts, values, metadata, interruption handling, and performance.
- Package the native HDF5 dependency in reproducible production and test containers without making it a dependency of ordinary protocol/unit workflows.

Exit criterion: production acceptance runs produce validated HDF5 artifacts suitable for downstream analysis.

## Deferred questions

- Which HDF5 dataset organization, chunking, compression, and analysis compatibility requirements apply?
- What authentication boundary and deployment environment are required?
- What measured scale or pub/sub requirement would justify introducing Centrifugo after version one?
- After version one, is there sufficient need and a supported interface to automate DT5215 web provisioning?
- What throughput, run duration, retention, and acceptable event-loss requirements apply?
