# CAEN DAQ

Replacement data-acquisition software for a PET system using four CAEN DT5202 front-end boards and one DT5215 concentrator.

The planned system has:

- a Go backend responsible for hardware access, acquisition control, decoding, persistence, and monitoring;
- a Vue.js and Tailwind CSS web frontend;
- a ConnectRPC API defined with Protocol Buffers;
- a deterministic DT5215/DT5202 simulator;
- unit, integration, protocol-conformance, and end-to-end tests.

The first protocol vertical slice is implemented: the backend parses the production JANUS configuration, connects to the DT5215 control and data ports, discovers and validates the provisioned four-link topology, and reads board identity/status registers. A deterministic TCP simulator exercises the same native binary protocol in integration tests.

Project workflows use [Task](https://taskfile.dev/docs/installation) through the root `Taskfile.yml`.
The reproducible Docker build environment, including HDF5 and Blosc, is
documented in [Docker development workflow](docs/docker-development.md).

```sh
task generate
task test
task ci
task test:e2e
```

Install the pinned frontend dependencies once, then start the operator UI. The
development server proxies ConnectRPC requests to the backend on port 8080.

```sh
npm --prefix frontend ci
task dev:frontend
```

For a single-origin deployment, build the repository and point the backend at
the generated application directory:

```sh
task build
./bin/pet-caen-daq -config config.txt -frontend-dir frontend/dist
```

The frontend directory is optional. When enabled, the backend validates it at
startup, serves browser routes through `index.html`, and keeps ConnectRPC on the
same HTTP origin.

To package the compiled server and frontend in a production runtime image:

```sh
task container:build IMAGE=pet-caen-daq:latest
mkdir -p runs
docker run --rm --network host \
  -v "$PWD/config.txt:/etc/pet-caen/config.txt:ro" \
  -v "$PWD/runs:/var/lib/pet-caen/runs" \
  pet-caen-daq:latest
```

After authenticating with `docker login`, build all compiled assets and publish
the complete image to Docker Hub:

```sh
task container:push
task container:push TAG=2026.07.23
```

The first command publishes `nextmgmt/pet-caen-daq:latest`; `TAG` selects an
explicit version tag.

This example uses the host network so the backend can reach the DT5215 directly
and publish the UI on port 8080. A routed bridge network with `-p 8080:8080` is
also suitable when it can reach the hardware subnet. The container runs as the
unprivileged numeric user `65532`; a bind-mounted runs directory must be
writable by that user. Additional backend flags can replace the image's default
command, for example to select different control or stream addresses. The
safety-sensitive `-authorize-hv-config` flag is deliberately not enabled by the
image.

The operator dashboard also lists persisted runs from the configured `-runs`
directory. Artifact downloads are streamed through the generated RunService API
and are limited to files recorded in each run's manifest.

Run configuration uses a searchable, categorized parameter editor initialized
immediately from the checked-in production sample; no file import is required.
Operators can reset to that sample or replace it with the exact JANUS document
loaded by the backend. Documented options are
presented as choices, binary flags as switches, and indexed overrides identify
their board or channel. Operators can still import a JANUS file or open the raw
source editor; all paths produce the same text submitted to backend validation.
Numeric parameters use bounded steppers with visible ranges and increments;
native number inputs support typing and Arrow Up/Down. The three paired 64-bit
channel masks open an accessible 8×8 selector with bulk enable, disable, and
invert operations and Global/Board 0–3 targets. Channel-scoped settings retain
a general value and provide per-board 64-channel exception grids; blank cells
inherit the general value and explicit exceptions use JANUS
`Parameter[board][channel]` syntax.

On Windows, after starting the backend, a bounded evidence-capturing hardware run
can be launched with `scripts\take-data.ps1`. Pass `-PeriodicTestPulse` to submit
an in-memory `TestPulseSource PTRG` override without modifying the configuration
file; omit it for ordinary detector acquisition.

`task test:e2e` builds the commands and frontend, then runs the pinned
Playwright Chromium container against a real simulator-backed backend. It uses
dedicated loopback ports and stores all transient runs and failure artifacts
under the container's `/tmp`.

## Start here

- [Project instructions](AGENTS.md)
- [Architecture](docs/architecture.md)
- [Engineering guidelines](docs/engineering-guidelines.md)
- [Testing strategy](docs/testing-strategy.md)
- [Protocol evidence and hardware notes](docs/daq_protocol_notes.md)
- [Real-hardware captures and patch history](docs/real-hardware-capture-evidence.md)
- [Hardware operating and recovery procedures](docs/hardware-operations.md)
- [Production JANUS fixture provenance](test/fixtures/janus/README.md)
- [Production Run 54 replay fixture](test/fixtures/runs/run54/README.md)
- [Implementation roadmap](docs/roadmap.md)
- [Current implementation status](docs/implementation-status.md)
- [JANUS GUI configuration and feature audit](docs/janus-gui-configuration-audit.md)

## Current hardware boundary

The DT5215 USB connection presents an Ethernet network interface. JANUS/FERSlib and the native backend communicate with the concentrator over TCP ports 9760 (slow control) and 9000 (data stream). The protocol implementation combines source evidence from FERSlib 5.0.0 with the indexed 2026-07-21 real-hardware captures; individual facts retain their explicit evidence classification.
