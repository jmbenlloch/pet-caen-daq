# Engineering guidelines

## API-first workflow

Define public messages and services in `api/` before implementing cross-component behavior. Use protobuf package names and versioned API namespaces deliberately. Prefer additive API evolution; never reuse removed field numbers. Validate requests in the backend even if the UI already validates them.

Buf is the protobuf toolchain entry point. The repository will use committed Buf configuration for module/dependency management, linting, compatibility checks, and generation of Go and TypeScript ConnectRPC bindings. Generation must be deterministic and exposed through one documented project command.

CI must run `buf lint`, verify generated files are current, and run `buf breaking` against the configured baseline once the first API is published. Exceptions to lint or compatibility policy require an explanation in the change that introduces them.

ConnectRPC unary methods suit commands and snapshots. Server streams suit state/telemetry updates where supported by the deployment path. Design reconnect semantics: a client that reconnects must obtain a complete snapshot before applying incremental updates.

## Configuration

- Every physical value must include a documented unit.
- Separate requested configuration from effective hardware configuration.
- Validate ranges and cross-field constraints before writing hardware.
- Apply configuration transactionally at the coordinator level: record each completed step and return a precise partial-failure report.
- Store the effective configuration with every run.
- Configuration files and API messages require schema versions.

## Go

- Follow standard Go layout and idioms; keep the module count minimal.
- Use `gofmt`, `go vet`, and a pinned linter configuration.
- Wrap errors with operation/device context while retaining `errors.Is`/`errors.As` behavior.
- Interfaces belong near their consumers and should be narrow.
- Avoid goroutine ownership ambiguity. The component that starts a goroutine owns cancellation and joining it.
- Tests should use deterministic clocks and seeded generators where time/randomness affects behavior.

## Vue and Tailwind

- Use Vue 3 Composition API and TypeScript.
- Keep generated Connect clients behind small application-facing composables/services.
- Treat backend state as authoritative.
- Build reusable accessible components; keyboard operation and readable error/status presentation are required.
- Tailwind classes should express the design system consistently. Avoid arbitrary values when a shared token is appropriate.
- Unit-test stores, composables, validation, and complex components; use browser tests for operator workflows.

## Data handling

- Raw acquisition bytes are append-only evidence.
- Use explicit file-format magic, version, byte order, checksums where useful, and self-describing metadata.
- Write through temporary/incomplete artifacts and finalize atomically when feasible.
- A crash must leave a detectable incomplete run rather than a seemingly valid completed run.
- Decoding must be replayable offline with no hardware.

## Observability

At minimum expose:

- system/acquisition state and transition duration;
- connection and reconnect counts;
- bytes and events received per concentrator/board;
- decoded events by qualifier;
- malformed frames, CRC flags, sequence/timestamp anomalies, and dropped events;
- queue occupancy/high-water marks and storage throughput;
- service-event age, temperatures, HV state, and hardware status bits.

Logs complement metrics and must contain actionable context, not duplicate every event.

## Dependencies and generated artifacts

- Pin direct dependency and tool versions, including the Buf CLI and protobuf plugins.
- Prefer maintained, narrowly scoped dependencies.
- Commit generated API code only if the chosen build/consumer workflow requires it; whichever policy is selected must be consistent and checked in CI.
- Docker images should be multi-stage, run as non-root, and contain only runtime requirements.

## Change discipline

Keep commits conceptually focused. Include tests and documentation with the behavior they describe. Do not mix bulk formatting or generated-code churn with unrelated logic changes.
