# ADR 0005: Use Task as the command runner

- Status: accepted
- Date: 2026-07-20

## Context

The monorepo will contain Go, Buf/Protocol Buffer, Vue/TypeScript, Playwright, simulator, Docker, and documentation workflows. Contributors and CI need one discoverable, cross-platform entry point without obscuring the underlying tools.

## Decision

Use Task with a root `Taskfile.yml` as the supported project command runner.

Task will orchestrate:

- tool setup and version checks;
- protobuf generation through Buf;
- formatting, linting, and generated-file checks;
- unit, integration, Playwright, replay, and opt-in hardware tests;
- backend, frontend, simulator, and container builds;
- local development services;
- the reproducible CI-equivalent workflow.

CI and project documentation will invoke public Task targets. Component-native commands remain available for focused debugging, but they are not an independent orchestration interface.

The Task CLI version will be pinned during initial scaffolding. CI will use a pinned official installation mechanism or pinned project tool setup. Local installation may use any official method from the Task installation documentation.

## Alternatives considered

- Make: available widely, but less suitable for readable cross-platform orchestration of this mixed toolchain.
- Shell scripts: rejected as the primary interface because discovery, dependency composition, and cross-platform behavior would be fragmented.
- Package-manager scripts only: rejected because they do not naturally own Go, Buf, Docker, and hardware/simulator workflows.
- Separate command runners per component: rejected because CI and contributors need a coherent repository-level interface.

## Consequences

- A root `Taskfile.yml` becomes a reviewed project interface.
- Public target names should remain stable or be migrated deliberately.
- Tasks must be non-interactive and CI-safe by default.
- Destructive, long-running, privileged, network-dependent, and hardware-in-the-loop tasks must be explicit and cannot run through ordinary `task test` or `task ci` dependencies.
- Task includes may split implementation by component while preserving root entry points.
