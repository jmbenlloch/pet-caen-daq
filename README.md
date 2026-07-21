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

```sh
task generate
task test
task ci
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

## Start here

- [Project instructions](AGENTS.md)
- [Architecture](docs/architecture.md)
- [Engineering guidelines](docs/engineering-guidelines.md)
- [Testing strategy](docs/testing-strategy.md)
- [Protocol evidence and hardware notes](docs/daq_protocol_notes.md)
- [Production JANUS fixture provenance](test/fixtures/janus/README.md)
- [Production Run 54 replay fixture](test/fixtures/runs/run54/README.md)
- [Implementation roadmap](docs/roadmap.md)
- [Current implementation status](docs/implementation-status.md)

## Current hardware boundary

The DT5215 USB connection presents an Ethernet network interface. The known JANUS/FERSlib implementation communicates with the concentrator over TCP ports 9760 (slow control) and 9000 (data stream). The existing protocol study is based on the FERSlib source distributed with JANUS 5.0.0 and must be validated against real packet captures before protocol behavior is declared verified.
