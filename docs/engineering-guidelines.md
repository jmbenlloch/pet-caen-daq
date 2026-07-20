# Engineering guidelines

## API-first workflow

Define public messages and services in `api/` before implementing cross-component behavior. Use protobuf package names and versioned API namespaces deliberately. Prefer additive API evolution; never reuse removed field numbers. Validate requests in the backend even if the UI already validates them.

Buf is the protobuf toolchain entry point. The repository will use committed Buf configuration for module/dependency management, linting, compatibility checks, and generation of Go and TypeScript ConnectRPC bindings. Generation must be deterministic and exposed through one documented project command.

CI must run `buf lint`, verify generated files are current, and run `buf breaking` against the configured baseline once the first API is published. Exceptions to lint or compatibility policy require an explanation in the change that introduces them.

ConnectRPC unary methods suit commands and snapshots. Server streams suit state/telemetry updates where supported by the deployment path. Design reconnect semantics: a client that reconnects must obtain a complete snapshot before applying incremental updates.

For version one, use the core Connect-Web streaming API for telemetry. Do not assume a unary query abstraction provides streaming support. Keep telemetry payload frequency and size bounded, and aggregate high-rate detector activity before it reaches the API.

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
- Use Playwright for browser integration and end-to-end tests. Prefer user-visible roles, labels, and behavior over CSS selectors or implementation details.
- Run Playwright against controlled backend/simulator states. Tests must not depend on arbitrary sleeps; wait for observable UI or API conditions.

## Data handling

- Raw acquisition bytes are append-only evidence.
- Use explicit file-format magic, version, byte order, checksums where useful, and self-describing metadata.
- Write through temporary/incomplete artifacts and finalize atomically when feasible.
- A crash must leave a detectable incomplete run rather than a seemingly valid completed run.
- Decoding must be replayable offline with no hardware.
- Use JSON for bounded run metadata, never for an indefinitely growing in-memory event array.
- Use JSON Lines as the lightweight development event stream: one versioned event envelope per line, written incrementally.
- Enforce maximum line/record sizes and define fixed rules for representing 64-bit integers without browser/JavaScript precision loss.
- Do not use YAML for machine-produced event data. Its richer parsing surface and weak fit for large append-only numeric streams provide no benefit here.
- The JSON Lines and HDF5 adapters must consume the same internal event model and must not leak file-format types into acquisition/decoder packages.

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

## Command runner

Task is the single project-level command runner. A root `Taskfile.yml` will provide stable commands for contributors and CI while delegating to Go, Buf, frontend package, Playwright, Docker, and other native tools.

Target naming should be predictable. The initial public surface is expected to include:

- `task setup` or `task tools` for pinned project tooling;
- `task generate` for Buf and other generated artifacts;
- `task format`, `task lint`, and `task test` for repository-wide checks;
- `task test:unit`, `task test:integration`, `task test:e2e`, and `task test:hardware` for explicit layers;
- `task build` for all deliverables;
- `task dev` for the normal backend/frontend/simulator development environment;
- `task ci` for the exact required local equivalent of CI.

Leaf Taskfiles may be included from component directories as the repository grows, but the root targets remain the supported interface. Tasks must propagate failures, avoid hidden host mutations, and use dependency/status mechanisms only when their correctness is clear. Hardware-in-the-loop and destructive maintenance tasks must never be dependencies of ordinary `test` or `ci` targets.

The Task CLI version and any CI installer/action reference must be pinned. Contributors may install Task using an official method described by the Task project; repository automation must not silently install an unpinned latest version.

## Change discipline

Keep commits conceptually focused. Include tests and documentation with the behavior they describe. Do not mix bulk formatting or generated-code churn with unrelated logic changes.
