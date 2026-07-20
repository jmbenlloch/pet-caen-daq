# ADR 0006: Use ConnectRPC for version-one telemetry

- Status: accepted
- Date: 2026-07-20

## Context

The first release has one Go DAQ backend and a small number of operator browsers. Telemetry consists of acquisition state, rates, temperatures, HV/status information, link health, buffer occupancy, warnings, and backend pipeline health. Raw detector data is persisted by the backend and is not browser telemetry.

ConnectRPC is already the API contract and supports browser-compatible server-streaming RPCs. Centrifugo has useful pub/sub, fan-out, presence, channel-history, and reconnect-recovery capabilities, but those do not currently justify another deployed service and integration path.

## Decision

Use ConnectRPC unary methods for commands and point-in-time queries, and a ConnectRPC server-streaming method for live telemetry in version one.

Telemetry messages are complete or independently usable snapshots, not a durable event log. They include enough identity and ordering information to detect replacement, restart, gaps, and staleness:

- backend instance identity;
- run identity;
- monotonically increasing sequence;
- observation timestamp;
- current acquisition state;
- aggregated concentrator, board, pipeline, and storage telemetry.

On every initial connection or reconnection, the backend immediately sends a complete snapshot. The frontend replaces its current telemetry state from that snapshot and marks it stale if updates stop arriving within the defined interval. Missing intermediate telemetry samples do not affect acquisition correctness or persisted run metadata.

The backend exposes an internal telemetry publisher abstraction so acquisition and hardware packages do not depend on ConnectRPC. This is an internal boundary, not a second external messaging system.

Centrifugo is deferred. A later ADR may add it if concrete requirements emerge for large fan-out, multiple publishers or consumer applications, recoverable channel history, presence, dynamic subscriptions, or independent connection-tier scaling.

## Alternatives considered

- Centrifugo immediately: deferred because it introduces another service, authentication path, client protocol, configuration, observability, and test surface without a present scaling requirement.
- Polling only: rejected as the primary live-monitoring mechanism, though unary snapshots may remain available for recovery and diagnostics.
- Raw WebSocket implementation: rejected because ConnectRPC already supplies typed browser-compatible server streaming.
- Streaming every detector event: rejected because browser telemetry must be aggregated and bounded; event data belongs in the acquisition/storage pipeline.

## Consequences

- The initial deployment has no separate real-time messaging service.
- Reverse proxies and browser tests must verify unbuffered long-lived ConnectRPC server streams and cancellation.
- Reconnect tests must assert an immediate full snapshot, sequence reset/replacement behavior, and stale-state presentation.
- Telemetry is not authoritative history. Important state transitions and run results must be persisted separately.
- Adding Centrifugo later should replace or supplement only the service transport adapter, not hardware acquisition or telemetry production.
