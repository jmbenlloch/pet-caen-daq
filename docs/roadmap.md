# Initial roadmap

This roadmap orders discovery and risk reduction. It does not commit to dates.

## Phase 0: repository and decisions

- Establish project guidance and protocol evidence notes.
- Select pinned Go, Node, Vue, Tailwind, ConnectRPC, Buf CLI/plugin, Playwright, and unit-test tool versions.
- Add the Buf module, lint, breaking-change, and generation configuration.
- Decide monorepo build commands and generated-code policy.
- Define the initial protobuf API and configuration units.
- Record ADRs for native protocol ownership, storage format, and telemetry transport.

Exit criterion: skeleton choices are explicit, reproducible, and reviewed.

## Phase 1: protocol core and simulator

- Implement byte-level DT5215 slow-control types with golden tests.
- Implement descriptor-table and DT5202 event decoders with unit/fuzz tests.
- Port the complete DT5202 register/configuration and Citiroc bitstream behavior into project-owned Go code.
- Implement a deterministic TCP simulator for one concentrator and four boards.
- Implement discovery, enumeration, synchronization, and minimal register access against the simulator.
- Add raw capture/replay fixtures.

Exit criterion: a Go test can perform a complete simulated test-pulse run and deterministically reproduce decoded events.

## Phase 2: backend service

- Implement the acquisition state machine and bounded event pipeline.
- Define and generate ConnectRPC contracts.
- Implement configuration validation, run control, health, and telemetry services.
- Persist run metadata and raw data with crash/incomplete-run handling.
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

## Deferred questions

- Which processed storage format best fits downstream analysis?
- What authentication boundary and deployment environment are required?
- Is DT5215 web provisioning a documented manual prerequisite, or should its private HTTP behavior eventually be automated?
- What throughput, run duration, retention, and acceptable event-loss requirements apply?
