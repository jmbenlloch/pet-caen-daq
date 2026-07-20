# Implementation status

## Vertical slice 1: read-only topology discovery

Implemented on 2026-07-20:

- lossless parsing of JANUS assignment syntax, including indexed settings, comments, repeated settings, and the production Windows/CRLF fixture;
- extraction and validation of the four production `Open` connections (TDlinks 0–3, node 0);
- exact little-endian codecs for DT5215 `CINF`, `ENUM`, and `RREG` requests and responses;
- simultaneous connection to TCP 9760 for slow control and TCP 9000 for the data stream;
- read-only validation that links 0–3 are enabled and links 4–7 are disabled, as required by the version-one web-provisioning decision;
- enumeration of exactly one DT5202 on each production link;
- reads of product ID, FPGA firmware revision, and acquisition status registers;
- a typed Go topology returned as JSON by the initial command-line backend;
- a deterministic native-protocol DT5215/DT5202 TCP simulator;
- golden codec and configuration-parser unit tests plus simulator-backed integration tests, including incorrect provisioning and pre-enumeration link states;
- an initial Buf API module and generated Go/ConnectRPC bindings for configuration validation and system snapshots.

The implementation does not use FERSlib or cgo. It does not configure persistent DT5215 link activation: operators must provision links 0–3 enabled and links 4–7 disabled through the concentrator web interface before startup. Discovery sends `ENUM`, but no register writes.

Protocol constants and behavior remain source-derived until checked against a real packet capture and hardware. In particular, DT5215 link-status numeric meanings beyond zero meaning disabled are provisional.

## Run locally

Run all non-hardware tests:

```sh
task test
```

Generate checked-in protobuf and ConnectRPC bindings:

```sh
task generate
```

Build both commands:

```sh
task build
```

Start the simulator in one terminal and run the DAQ command in another. The simulator command defaults to loopback ports 9760 and 9000; integration tests request ephemeral ports so they can run concurrently.

The next protocol work should add the complete configuration/register-write sequence, Citiroc configuration bitstream, acquisition state machine, data-stream framing, and Run 54 processed-event compatibility decoder. Each step should retain captured raw bytes so it can later be compared with the real PCAP.
