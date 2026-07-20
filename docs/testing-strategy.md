# Testing strategy

The test system must establish confidence without pretending the simulator proves real-hardware behavior.

## Test layers

### Unit tests

Fast and isolated. Cover:

- command encoding and reply parsing;
- descriptor/event decoding;
- register field calculations and configuration validation;
- acquisition state transitions;
- retry, timeout, cancellation, and error classification;
- frontend stores, composables, validation, and components.

Use table-driven tests, boundary values, fuzz tests for parsers, and deterministic clocks/random sources.

### Protocol conformance tests

Byte-exact tests using documented vectors and later real packet captures. Each fixture must include provenance metadata:

- evidence classification;
- hardware topology;
- DT5215/DT5202/PIC/FPGA/software versions;
- capture date and operation performed;
- expected decoded representation.

Test encoders in both directions when replies are simulated. Fuzz stream parsers and ensure malformed input cannot panic or allocate without bounds.

### Backend integration tests

Run the Go backend against the TCP simulator. Exercise complete open/enumerate/synchronize/configure/start/read/stop/drain workflows, including failures. Use real sockets so partial delivery and cancellation are tested.

Test ConnectRPC handlers over an actual in-process or containerized HTTP server, including generated clients, validation, status mapping, streaming/reconnect behavior, and concurrent commands.

### Frontend tests

- Component tests use generated/client abstractions with controlled responses.
- Playwright is the required framework for frontend integration and browser end-to-end tests.
- Playwright integration tests run the built frontend against controlled backend or simulator behavior; full end-to-end tests run against the backend plus hardware simulator.
- Critical workflow: connect, inspect topology, validate/apply configuration, start a run, observe telemetry, stop, drain, and inspect the completed run.
- Failure workflow: disconnect/stale telemetry, configuration rejection, hardware fault, stop timeout, and browser reconnect.
- Browser tests use accessible locators and event/state-based waits rather than styling selectors or fixed delays.
- CI retains Playwright traces, screenshots, and videos on failure according to the eventual retention policy.

### Storage/replay tests

Write raw data and metadata, simulate interruption, reopen, detect incomplete state, replay, and compare decoded events. Golden replay must be deterministic across supported platforms.

### Hardware-in-the-loop tests

Opt-in and never part of ordinary CI. Begin read-only, then controlled test-pulse acquisition. Record hardware and firmware identity in results. Destructive or HV tests require an explicit environment gate and operator procedure.

## Simulator conformance

The same protocol fixtures should be consumable by parser tests and simulator reply tests. Once real captures exist:

1. add sanitized immutable captures or extracted byte fixtures;
2. compare native decoding with FERSlib/JANUS results;
3. adjust the simulator to reproduce verified behavior;
4. retain old fixtures for firmware compatibility unless explicitly unsupported.

The simulator must support fault scripts rather than relying on timing races. A test should be able to specify, for example, “split the next reply after byte 3,” “delay stream data 500 ms,” or “disconnect after the second descriptor row.”

## CI quality gates

The eventual CI pipeline should require:

- formatting and linting for Go, protobuf, TypeScript, Vue, CSS, Markdown, Dockerfiles, and shell files as applicable;
- generated-code consistency;
- unit and fuzz smoke tests;
- backend/simulator integration tests;
- frontend build and component tests;
- Playwright frontend integration and selected browser end-to-end tests;
- race detection for relevant Go packages;
- vulnerability and dependency checks;
- reproducible container builds.

Long fuzzing, soak, performance, and hardware tests run separately but must have documented invocation and result retention.

## Coverage philosophy

Coverage is a diagnostic, not the goal. Require strong branch coverage in protocol parsers, state machines, validation, and safety-critical run control. Do not add low-value tests solely to increase a percentage.
