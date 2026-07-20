# ADR 0003: Use Playwright for frontend integration tests

- Status: accepted
- Date: 2026-07-20

## Context

The Vue frontend controls and monitors acquisition through the ConnectRPC backend. Critical confidence requires testing real browser behavior, generated-client integration, reconnect handling, and complete operator workflows against deterministic backend and simulator states.

## Decision

Use Playwright for frontend browser integration and end-to-end tests.

Playwright tests will cover:

- the built Vue application in a real browser;
- frontend-to-ConnectRPC integration;
- complete workflows against the Go backend and hardware simulator;
- reconnects, stale telemetry, failures, and recovery presentation;
- accessibility-oriented interaction using roles, names, and labels.

Pure stores, composables, validation, and isolated components remain the responsibility of the frontend unit/component test runner. Playwright is not a replacement for fast unit tests.

## Alternatives considered

- Cypress: capable, but not selected for this project.
- Component tests alone: rejected because they do not exercise browser, networking, deployment, or cross-component behavior.
- Manual browser acceptance testing only: rejected because critical run-control regressions must be reproducible in CI.

## Consequences

- Playwright and browser versions must be pinned or provided by a pinned container.
- CI must retain useful failure artifacts such as traces and screenshots.
- Tests run against deterministic simulator scenarios and must avoid arbitrary sleeps.
- Page abstractions may be introduced where they improve clarity, but they must not conceal meaningful operator assertions.
