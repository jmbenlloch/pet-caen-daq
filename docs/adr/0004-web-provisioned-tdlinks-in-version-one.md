# ADR 0004: Provision TDlinks through the web interface in version one

- Status: accepted
- Date: 2026-07-20

## Context

The DT5215 manual states that persistent TDlink enablement is performed through its web interface. Although the source register map defines `VR_ENABLED_LINKS`, JANUS/FERSlib does not write it, and there is no evidence that doing so is a supported replacement for the web Apply operation.

The manual also warns that the concentrator can become stuck if an enabled TDlink has no connected FERS unit. Enabled links must be sequential starting from link 0, and unused links must remain disabled.

The production system has four DT5202 boards, one at node 0 on each of chains 0 through 3.

## Decision

Version one assumes that an operator has enabled the required TDlinks through the DT5215 web interface before starting the DAQ.

The required provisioned topology is:

- chains 0, 1, 2, and 3 enabled;
- exactly one DT5202 at node 0 on each enabled chain;
- chains 4 through 7 disabled.

On startup, the native backend queries chain status and enumerates the enabled links. It must reject configuration or acquisition when the observed topology differs and return an actionable message directing the operator to the DT5215 web interface.

Version one must not:

- write `VR_ENABLED_LINKS`;
- invoke or reverse-engineer the private web interface;
- treat `CCNT` as persistent physical-link enablement;
- continue acquisition after a topology mismatch.

The backend still owns runtime `CINF`, `ENUM`, `RLNK`, `SNT0`, and `CCNT` operations on already provisioned links.

## Alternatives considered

- Write `VR_ENABLED_LINKS`: rejected because its supported write behavior and persistence are unverified.
- Automate the web interface: deferred because its private HTTP API is undocumented and unnecessary for the first release.
- Trust provisioning without validation: rejected because incorrect link activation can hang the concentrator or route data from an unexpected topology.

## Consequences

- Installation and topology changes require a documented manual web-provisioning step.
- Normal runs do not require reopening the web interface when provisioning remains correct.
- Startup validation and simulator scenarios must cover disabled, missing, extra, and empty enabled chains.
- Persistent link activation can be revisited in a later ADR if CAEN documents a supported API or operational need justifies validating the web protocol.
