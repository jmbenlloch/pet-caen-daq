# ADR 0001: Use Buf for Protocol Buffers

- Status: accepted
- Date: 2026-07-20

## Context

The Go backend and Vue/TypeScript frontend communicate through ConnectRPC. Both sides require generated code from the same Protocol Buffer definitions, and the project needs consistent schema quality and compatibility checks.

## Decision

Use Buf as the repository's protobuf toolchain entry point for:

- module and dependency management;
- protobuf linting;
- breaking-change detection;
- reproducible generation of Go and TypeScript protobuf/ConnectRPC bindings.

The repository will commit its Buf configuration and pin the CLI and plugin versions. A single documented project command will perform generation. Contributors must not hand-edit generated bindings or maintain an independent generator script outside Buf.

## Alternatives considered

- Direct `protoc` commands: workable, but versioning and generator parity are easier to centralize with Buf.
- Manually maintained Go and TypeScript API types: rejected because they would duplicate the protobuf contract and drift over time.

## Consequences

- Local development and CI require the pinned Buf toolchain or its project container.
- CI will lint schemas and check that generated code is current.
- After the first API baseline is published, CI will check breaking changes against that baseline.
- Buf configuration becomes part of the public API review surface.
