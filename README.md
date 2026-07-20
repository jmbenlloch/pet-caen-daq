# CAEN DAQ

Replacement data-acquisition software for a PET system using four CAEN DT5202 front-end boards and one DT5215 concentrator.

The planned system has:

- a Go backend responsible for hardware access, acquisition control, decoding, persistence, and monitoring;
- a Vue.js and Tailwind CSS web frontend;
- a ConnectRPC API defined with Protocol Buffers;
- a deterministic DT5215/DT5202 simulator;
- unit, integration, protocol-conformance, and end-to-end tests.

This repository is currently in its documentation and architecture-definition phase. No production implementation has been selected or scaffolded yet.

## Start here

- [Project instructions](AGENTS.md)
- [Architecture](docs/architecture.md)
- [Engineering guidelines](docs/engineering-guidelines.md)
- [Testing strategy](docs/testing-strategy.md)
- [Protocol evidence and hardware notes](docs/daq_protocol_notes.md)
- [Production JANUS fixture provenance](test/fixtures/janus/README.md)
- [Production Run 54 replay fixture](test/fixtures/runs/run54/README.md)
- [Implementation roadmap](docs/roadmap.md)

## Current hardware boundary

The DT5215 USB connection presents an Ethernet network interface. The known JANUS/FERSlib implementation communicates with the concentrator over TCP ports 9760 (slow control) and 9000 (data stream). The existing protocol study is based on the FERSlib source distributed with JANUS 5.0.0 and must be validated against real packet captures before protocol behavior is declared verified.
