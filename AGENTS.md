# Project instructions

These instructions apply to the entire repository. They are intended for both human contributors and coding agents in future sessions.

## Mission

Build a reliable, observable, testable DAQ for four CAEN DT5202 boards behind a DT5215 concentrator. Preserve raw evidence, make acquisition behavior deterministic, and never hide uncertainty about undocumented hardware behavior.

## Required architecture

- Backend: Go.
- API: Protocol Buffers and ConnectRPC, managed with Buf. The `.proto` definitions are the contract and must not be duplicated manually in Go or TypeScript.
- Frontend: Vue.js with TypeScript and Tailwind CSS.
- Frontend integration and browser end-to-end tests: Playwright.
- Hardware development: a simulator must support development and CI without physical CAEN hardware.
- Deployment and reproducible tooling may use Docker and Docker Compose.
- Command runner: Task. Normal developer and CI workflows must be exposed through the root `Taskfile.yml`.
- Hardware access: implement the DT5215/DT5202 binary protocol natively in Go. Production code must not link to or call FERSlib.
- Version-one provisioning: operators enable DT5215 TDlinks through its web interface before starting the DAQ. The DAQ validates that provisioning but does not change persistent link enablement.

Do not replace these technologies without an explicit architecture decision approved by the user.

## Repository layout target

```text
api/                  protobuf contracts
backend/              Go modules and commands
  cmd/                executable entry points
  internal/           private backend packages
frontend/             Vue application
simulator/            hardware simulator, if separate from backend internals
test/                  cross-component fixtures and integration/e2e tests
docs/                  architecture, protocol, operations, and decisions
deploy/                Docker and deployment assets
```

Create directories only when the relevant implementation begins; do not add empty scaffolding merely to match the diagram.

## Design boundaries

Keep these responsibilities separate:

1. Transport: TCP connection lifecycle, exact reads/writes, deadlines, and reconnect policy.
2. Protocol: DT5215 commands/replies and stream framing.
3. Device model: concentrator, chains, nodes, DT5202 registers, configuration, and status.
4. Acquisition: state machine, synchronization, start/stop/drain, event routing, and backpressure.
5. Decoding: immutable raw frames to typed events.
6. Storage: raw capture, run metadata, decoded output, and retention.
7. Service/API: authorization, validation, orchestration, and streaming updates.
8. UI: operator workflows and presentation; never hardware logic.

The production backend and simulator must share protocol types or conformance fixtures, but the simulator must not be called from production hardware code.

FERSlib and JANUS are reference implementations and comparison oracles only. Do not introduce cgo bindings, dynamic loading, subprocess wrappers, or runtime fallbacks that call FERSlib.

## Hardware safety

- Default new tools to read-only behavior.
- Treat reset, firmware update, HV changes, acquisition start, and register writes as state-changing operations.
- Require explicit configuration and clear logging for HV or firmware operations.
- Do not experiment with undocumented writes on real hardware without user approval and a recovery plan.
- Do not write `VR_ENABLED_LINKS` or automate the DT5215 private web interface in version one. Report provisioning mismatches with instructions to use the web interface.
- On shutdown or error, attempt an orderly acquisition stop and drain while preserving the original error.
- Never discard raw bytes solely because decoding failed. Record the failure and retain evidence when configured to capture raw data.

## Protocol evidence policy

Every protocol fact must be classified as one of:

- `source-confirmed`: directly implemented in the bundled JANUS/FERSlib source;
- `capture-verified`: observed in a real packet capture and matched to expected behavior;
- `hardware-verified`: exercised successfully against the real system;
- `inferred`: reasoned from implementation behavior but not guaranteed;
- `unknown`: unresolved.

Tests and documentation must not silently upgrade an inference to a verified fact. Keep golden packet fixtures byte-exact and record their origin, firmware versions, topology, and capture procedure.

## Coding expectations

- Prefer small packages with explicit interfaces at I/O boundaries.
- Use dependency injection for clocks, transports, storage, and hardware sessions where it improves deterministic testing.
- Avoid global mutable state.
- Carry `context.Context` through blocking Go operations and define deadlines intentionally.
- TCP is a byte stream: implement full-write and exact-read behavior; never assume one `Read` or `Write` maps to one protocol message.
- Define byte order explicitly and test it with golden bytes.
- Keep wire encoders/decoders independent of FERSlib data structures. Translate protocol bytes into project-owned immutable types.
- Model acquisition as an explicit state machine. Reject invalid transitions.
- Use bounded queues and state the backpressure/overflow policy.
- Use structured logging with run ID, device, chain, node, operation, and error fields where applicable.
- Never log credentials or unrestricted event payloads by default.
- Frontend code must be TypeScript, accessible, and resilient to reconnects and stale state.
- Use Playwright for frontend integration tests that exercise browser behavior or cross the frontend/backend boundary. Keep lower-level component and composable tests in the frontend unit-test runner.
- Generated protobuf/Connect files must be reproducible and must not be hand-edited.
- Use Buf for protobuf linting, breaking-change detection, dependency management, and code generation. Do not invoke language-specific protobuf generators through parallel ad hoc scripts.
- Use `task <name>` as the documented entry point for generation, formatting, linting, tests, builds, simulator workflows, and local development. Underlying native commands may remain directly usable for focused debugging, but CI and project documentation must call the Task targets.
- Keep Task targets non-interactive by default, composable, and consistent between local development and CI. Destructive, hardware, or long-running targets must be explicit and clearly named.

## Test requirements

Every behavior change requires proportionate tests. At minimum:

- pure parsing, encoding, state transitions, and validation: unit tests;
- TCP behavior, simulator interaction, API handlers, storage boundaries: integration tests;
- critical operator workflows: browser end-to-end tests;
- protocol changes: byte-level golden or conformance tests;
- bug fixes: a regression test that fails before the fix.

Tests must cover success, timeout, cancellation, partial I/O, malformed data, disconnect, and invalid-state paths where relevant. CI must not require real hardware. Hardware-in-the-loop tests must be separately tagged and opt-in.

Do not weaken, skip, or delete a failing test merely to make CI pass. Explain and isolate genuinely flaky external tests.

## Documentation requirements

Update documentation in the same change when modifying:

- API contracts or compatibility;
- acquisition states or run semantics;
- protocol layouts or evidence status;
- configuration schema or units;
- deployment, storage, safety, or recovery behavior.

Record significant design choices as short ADRs under `docs/adr/` once implementation decisions are made. Architecture documentation describes current decisions, not aspirational features disguised as implemented behavior.

## Definition of done

A change is done when it is implemented, formatted, linted, tested at the appropriate layers, documented, and leaves no unexplained generated or temporary files. Report which checks were run and any checks that could not be run.
