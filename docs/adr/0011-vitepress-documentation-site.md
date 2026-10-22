# ADR 0011: Publish documentation with VitePress

Status: accepted

Date: 2026-10-22

## Context

Collaborators need searchable web documentation for DAQ operation, telemetry,
configuration, and hardware caveats, protected by individual credentials.
The repository already contains Markdown manuals and uses Vue and Node tooling.

## Decision

Build the existing `docs/` tree with VitePress, pinned independently of frontend
dependencies. Add focused operator entry pages while retaining the full manuals.
Use local search so protected content is not sent to an external search provider.
Expose development and build commands through Task and check builds in CI.

Provide a separate static Caddy deployment with HTTPS and server authentication
for all routes and assets. Keep passwords and domain configuration on the server.
Automatic SSH publication is opt-in after hosting and secrets are configured.

## Alternatives

MkDocs adds Python tooling to the existing Node/Vue workflow. Docusaurus offers
built-in versioning but introduces React tooling that this project does not need.

## Consequences

Documentation can change with the software and needs no production Node server.
Editors use Markdown and Git; a rich-text editor is not provided. Initial hosting
publishes one current version, with previous content retained in Git. Server
provisioning, domain setup, and collaborator credential management remain required.
