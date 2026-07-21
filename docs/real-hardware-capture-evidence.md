# Real-hardware capture evidence

This document inventories the DT5215/DT5202 packet captures collected on 2026-07-21 and records the implementation corrections derived from them. Protocol claims use the evidence classes defined in `AGENTS.md`. All timestamps are Europe/Madrid (`UTC+02:00`).

## Evidence location and integrity

The PCAP files currently live in the workspace-level `pcap/` directory, adjacent to this repository (`../pcap/` from the repository root). They are evidence inputs and are not currently committed inside this Git repository. Preserve the original bytes; use the hashes below to detect accidental changes.

| Capture | Bytes | Packets | Duration | First packet | SHA-256 |
|---|---:|---:|---:|---|---|
| `scan_devices_go.pcap` | 536,952 | 2,813 | 22.570 s | 2026-07-21 18:29:21 | `8a5220cdc367aa2e5c51a4d655bb7bc359504306e503286365fa78b5bc3d4b17` |
| `janus_data_taking.pcap` | 11,032,189 | 42,411 | 217.779 s | 2026-07-21 17:32:18 | `bce2824b96031a0c6b64d4498ced5fd2fd9e4c36563b5b6849652c8d17110ec0` |
| `go_concentrator_no_janus_web.pcap` | 1,479 | 19 | 6.892 s | 2026-07-21 18:39:30 | `97f42dc5d1092217ec421a377e5161ae3d179c37a705ba2978c9905b8de0b1da` |
| `janus_connection.pcap` | 5,300,523 | 18,026 | 121.538 s | 2026-07-21 18:40:07 | `4fa08f367940251660f2b177a57b395ac5d23f1c8299cbbee0acb001f0997af8` |
| `go_connect_patch1.pcap` | 339,452 | 2,946 | 12.313 s | 2026-07-21 18:51:35 | `53303888b47a6351093c257aa799c66a51d16be28b4f4749c00ea69dc62868fb` |
| `go_connect_patch2.pcap` | 1,022,937 | 10,734 | 19.505 s | 2026-07-21 18:55:18 | `0febb27b12b1458a1a39ea9a240162f99cf6cedd55fa2143ed99f0936048832e` |
| `go_connect_patch3.pcap` | 1,386,713 | 11,718 | 53.378 s | 2026-07-21 18:58:40 | `8155a3a9f0af6634cebf105a0ecd70975fc705aa6c4b46f12ce72a6dbb40d1c2` |
| `go_connect_patch4.pcap` | 1,056,362 | 10,828 | 21.603 s | 2026-07-21 19:01:53 | `86b6e4b5682bf57d8e98d07c1cb8bc50123d28faa7f3bb47673245682b8328a3` |
| `janus_data_taking_2.pcap` | 71,225,850 | 97,775 | 464.057 s | 2026-07-21 19:08:52 | `6745e5bae69d41f8070275c6f101c4e5745cffedc422599dd768d98264f8a9fc` |
| `go_data_taking.pcap` | 1,633,664 | 20,427 | 431.805 s | 2026-07-21 19:25:17 | `ccb12fb356ef8dfd8533cc0e2fd5cd14b2a3b4011a6347e890c345ff6fe56729` |
| `go_data_taking_2.pcap` | 3,254,575 | 40,684 | 457.025 s | 2026-07-21 19:37:04 | `0597efecb6d6e23a75a6d3290e411bfa2d1b20b3975bb375024c2a44a7692f97` |
| `go_data_taking_4.pcap` | 4,287,206 | 34,634 | 108.413 s | 2026-07-21 19:54:08 | `5d98ae93d119b4c867dd0d5efd426ca2af15bb8ec6dfd99758ad84c0fd5ec2bb` |
| `go_data_taking_5.pcap` | 41,809,419 | 30,773 | 62.380 s | 2026-07-21 20:02:41 | `a00acb7c35249a8621f26e71beccbb38a430004d164bfa358269d0c0b843f139` |

PCAP timestamps have microsecond resolution. File modification times are not provenance and should not replace the captured timestamps.

## System represented by the captures

- Host-side DT5215 USB-Ethernet address: `172.16.0.1`.
- Concentrator address: `172.16.0.11`.
- Slow control: TCP port 9760.
- Event stream: TCP port 9000.
- Concentrator web interface: TCP port 80.
- Expected topology: chains 0, 1, 2, and 3 enabled, with one DT5202 at node 0 on each; chains 4--7 disabled.
- Configuration fixture: `test/fixtures/janus/config_same4_v3_good.txt`, SHA-256 `c472a36aefb2d6956a10bbcaab97515258a7779f96d62caa0e6e39e01e49675d`.

The captures prove that one USB-Ethernet interface carries web, control, and stream traffic. The four `Open[]` entries describe logical TDlink paths, not four host network interfaces.

## Capture-by-capture findings

### `scan_devices_go.pcap`

This capture was made while JANUS and the concentrator web page were active. JANUS already owned control connection `172.16.0.1:61470 -> 172.16.0.11:9760` and successfully completed 730 `RREG` exchanges. The first Go attempt opened `55787 -> 9760`, sent `CINF` for chain 0, received no application reply, and timed out. This is evidence of concurrent-client contention, not a missing capture interface or a dead concentrator. Do not use this capture to establish normal single-client response timing.

### `janus_data_taking.pcap`

This is the reference configuration and short acquisition run. It contains both slow-control traffic and 4,605,560 bytes from `172.16.0.11:9000`. It verifies:

- one board on each of chains 0--3 and no boards on chains 4--7;
- identical complete configuration traffic for all four boards, with the configured per-board timing-threshold overrides;
- HV voltage `45.4 V`, current limit `1.0 mA`, temperature configuration, and explicit HV-on transactions on all four boards;
- broadcast acquisition start command `0x12`, event-stream traffic, and broadcast acquisition stop command `0x13`;
- continuous TCP sequence coverage in both control and stream directions.

This capture includes heavy web-interface polling, but that traffic uses port 80 and is separate from the DAQ sockets.

The opt-in capture-conformance test reconstructs the complete port-9000 TCP stream with retransmission de-duplication and continuous-sequence validation. Its stream SHA-256 is `98ab7980e6d689d86b7f260bf7e978bbde6a734b0b07823fd7ddfa36d1383b44`. Production framing and event decoders successfully consume all 4,605,560 bytes: 18,590 batches, 18,784 descriptors/events, 3,781,392 event-payload bytes, and exactly 4,696 events per enabled chain. Qualifiers comprise 18,668 spectroscopy-plus-timing leading-edge events (`0x23`) and 116 service events (`0x2f`).

`janus_data_taking_2.pcap` contains two independent port-9000 TCP flows. Their ordered reconstructed streams have SHA-256 `cf75d42e3003d1547cde5be7e814b74ec5acd0612de9c238712433dfad3dfe1e`. Production decoders consume all 59,541,516 bytes: 54,081 batches, 158,417 descriptors/events, and 53,823,200 event-payload bytes. The chain totals are 33,484, 45,114, 59,342, and 20,477. It contains 158,233 both-gain spectroscopy/timing leading-edge events (`0x33`) and 184 service events (`0x2f`). Of the service events, 182 are version 1 and one each reports forward version `0xfe` and `0xff`; their unknown payloads are retained byte-for-byte.

### Native acquisition progression

The `go_data_taking*.pcap` series records successive native run-control corrections. The first capture exposed an invalid three-second timeout on an otherwise valid idle port-9000 connection. The second established that native start/stop used zero delay and that the simulator-only ready-service-event drain condition is not produced by real hardware. The fourth verified delayed reset/start/stop and idle drain, but exposed the missing runtime `CCNT` token-train sequence.

`go_data_taking_5.pcap` is the successful native reference. It contains the complete capture-matched sequence—disable chains 0--3, delayed reset time, delayed reset periodic trigger, delayed acquisition start, enable chains with token interval `0x100`, event traffic, and delayed acquisition stop. The corresponding external `run-go-native-detector-hvon-003` artifacts record 36,255 raw batches and 87,989 events. All raw records pass CRC validation and production decoding; all events are qualifier `0x33`. Artifact hashes are:

- `events.jsonl` (675,772,115 bytes): `f133a9999cd710e862806cc89f292ff6e12384011dc812eb97cb7ffcdc595316`;
- `wire.raw` (38,796,980 bytes): `721e280831636d6d00679267f428ce146ff20a51e3dd3b934a764db4988c9376`;
- `transport.journal` (46,926,672 bytes): `9858234f2b0ce1296eb9a45c1aa46ec20ef0b2a7f4e2c52ddae4223dafa2f093`.

That reference run predates native service-event enablement and therefore contains no qualifier-`0x2f` monitoring records. The production planner now matches the JANUS/FERSlib default by setting acquisition-control bits 18--19 to `3` (HV monitoring plus counters) when `EnableServiceEvents` is absent. Explicit values `DISABLED`, `ENABLED`, or `0`--`3` are supported; counting mode is restricted to the HV service section as in FERSlib. Service-event current is converted from the CAEN wire unit (10,000 counts per mA) to amperes for the public `hv_current_a` field.

SiPM HV was enabled externally with JANUS before this run and JANUS was then disconnected. The native backend reported `hv_authorized=false`, so this sample proves acquisition with preserved externally established HV state, not native HV peripheral configuration or authorization.

### `go_concentrator_no_janus_web.pcap`

This minimal capture isolates the original Go enumeration failure. `CINF` returned its 40-byte reply immediately. `ENUM` was sent at capture offset 0.002286 s; the Go client closed at 3.002848 s; the concentrator returned a 12-byte successful reply at 6.892067 s and the closed client reset it.

The reply was:

```text
00000000 01000000 3c000000
```

The words are status `0`, node count `1`, and a third word `60` whose semantics remain unknown. This capture verifies both the 12-byte framing and an approximately 6.89-second enumeration latency.

### `janus_connection.pcap`

This is the reference JANUS connect/disconnect sequence with the web page open. Initial `CINF` showed chain 0 ready, chains 1--3 enabled but not initialized, and chains 4--7 disabled. JANUS then performed:

```text
CINF 0..7 -> RLNK -> ENUM 0 -> ENUM 1 -> ENUM 2 -> ENUM 3 -> SNT0
```

Observed timings were 3.007 s for `RLNK`, 6.795--6.890 s per `ENUM`, and 5.027 s for `SNT0`. Every `ENUM` reply was 12 bytes with status zero and one node. Final `CINF` reported chains 0--3 ready with one board each. The capture then contains board initialization/configuration and an orderly disconnect.

### `go_connect_patch1.pcap`

This validates the corrected enumeration framing, deadlines, and initialization sequence far enough to reach real board configuration. Configuration stopped on board 0 because Go expected sign-extended `TrefDelay` readback `0xffffffc2`, while hardware returned `0x000fffc2` for register `0x01000048`.

The capture establishes that the FPGA stores this value as a signed 20-bit two's-complement field. It was the only ordinary configuration-register mismatch found before the application rejected startup.

### `go_connect_patch2.pcap`

This validates the 20-bit `TrefDelay` correction. All four boards completed startup configuration: each chain had 636 `WREG` requests, 627 `RREG` requests, one global reset, and two Citiroc configuration commands. Hardware startup succeeded, but the process subsequently failed to bind local API address `127.0.0.1:8080` on Windows.

That failure was unrelated to the PCAP network interface or CAEN hardware. It motivated binding the local HTTP listener before any hardware mutation and improving the `-listen` diagnostic.

### `go_connect_patch3.pcap`

This is the first successful Go startup using local API address `127.0.0.1:8081`. The backend reached `state=ready`. It validates the alternate listen-address workaround and the fail-fast listener ordering. This sample predates the added per-device console inventory.

### `go_connect_patch4.pcap`

This is the current successful startup reference and corresponds to the printed four-device inventory. It verifies:

- `CINF` status 4 and one board on each chain 0--3;
- status 0 and zero boards on chains 4--7;
- product IDs 64883, 64138, 64885, and 64884 on chains 0--3 respectively;
- raw FPGA firmware word `0xa1230708` on every board; FERSlib interprets the low bytes as major 7, minor 8, while `0xa123` is retained as build/revision information;
- acquisition status `0x00000009` on every board: ready and TDlink-synchronized, not running;
- 636 writes and 627 reads per board, one global reset and two Citiroc loads per board;
- zero nonzero protocol status replies and continuous control TCP byte coverage.

No port-9000 event data is present because no acquisition was requested. `hv_authorized=false` means the native backend did not apply HV peripheral setpoints or turn on SiPM power during this startup.

## Corrections derived from the captures

The 2026-07-21 patch series made these production changes:

1. **ENUM framing:** changed the reply size from 8 to 12 bytes and retained the third word without inventing semantics.
2. **Operation deadlines:** retained a 3-second default for short control operations, but assigned 5 seconds to `RLNK` and 10 seconds each to `ENUM` and `SNT0`.
3. **Discovery budget:** increased command startup discovery from 10 to 60 seconds to accommodate reset, four sequential enumerations, synchronization, and identity reads.
4. **State-dependent discovery:** read all `CINF` states first; when any expected link is in state 1 or 2, reset all enabled links, enumerate chains 0--3, synchronize, refresh `CINF`, and require one ready board per chain. Already-ready topologies are preserved without an unnecessary reset.
5. **Simulator parity:** changed simulated `ENUM` replies to 12 bytes, retained a deterministic third word, and modeled reset/enumeration link-state transitions.
6. **Time-reference delay:** encoded `TrefDelay` as the effective signed 20-bit two's-complement register field. `-500 ns / 8 ns` is `-62` ticks and therefore `0x000fffc2`. Added signed-range validation.
7. **Listener ordering:** bound the ConnectRPC/HTTP address before connecting to or configuring hardware. Bind failures now recommend selecting another address with `-listen`.
8. **Device inventory:** printed the discovered count and one line per board with chain, node, product ID, raw FPGA firmware word, and acquisition status.
9. **Real-stream conformance:** added opt-in PCAP TCP reconstruction and full production-decoder replay. This exposed and corrected support for capture-verified qualifier `0x23`, whose low nibble is spectroscopy plus timing and whose `0x20` flag identifies the leading-edge format.
10. **Idle acquisition streams:** `go_data_taking.pcap` and the preserved `run-go-native-001/transport.journal` show a successful four-board configuration and run start followed by no port-9000 payload. A three-second socket deadline incorrectly converted this legitimate idle period into `COORDINATOR_FAULT`. Long-lived stream reads now wait for data or caller cancellation; stop-and-drain remains bounded by its explicit context deadline. Asynchronous acquisition faults are also printed to the server console.
11. **TDL start command timing:** `go_data_taking_2.pcap` contains two native starts encoded as broadcast `FCMD ACQ_START` with delay zero and no returned stream data. The working JANUS capture uses the sequence `RESET_TIME (0x11)`, `RESET_PTRG (0x17)`, `ACQ_START (0x12)`, each broadcast with delay `1,000,000` (10 ms). Native run control now reproduces that command sequence and delay; stop and command-triggered test pulses use the same capture-verified TDL delay.
12. **Real drain completion:** the two native runs remained correctly active for ten seconds but stop produced no service event or stream byte, disproving the simulator's ready-service-event completion contract. Native drain now follows FERSlib's `NODATA_TIMEOUT`: deliver pending complete batches and finish after 100 ms of stream silence, while retaining the operator drain deadline as the upper bound.
13. **Runtime readout trains:** `go_data_taking_4.pcap` proves the corrected delayed reset/start/stop sequence and clean idle drain, but still contains no event-stream bytes. Unlike JANUS, the native sequence omitted `CCNT`. Run control now reproduces the capture/source-confirmed token sequence for every expected chain: disable the readout train before reset/start, then enable it with token interval `0x100` after `ACQ_START`.
14. **Complete native acquisition:** `go_data_taking_5.pcap` and `run-go-native-detector-hvon-003` prove the complete native lifecycle after adding runtime train control. The server configured four boards, acquired for ten seconds, stopped and drained, finalized all artifacts, and returned to ready. The raw capture contains 36,255 batches and 87,989 qualifier-`0x33` spectroscopy/timing events; chain totals are 16,833, 16,422, 46,787, and 7,947. Offline production-decoder replay consumes every record successfully, and SHA-256 verification matches all three manifest artifacts.
15. **Pedestal SPI recovery:** a canceled startup pedestal transaction left flash chip select asserted; the immediately following process read stale byte `0xb3` instead of the page tag. Pedestal reads now explicitly deassert chip select before each transaction and perform bounded cancellation-independent cleanup afterward.

Regression coverage includes the captured ENUM bytes, signed 20-bit delay boundaries, pre-enumeration discovery transitions, refreshed chain state, alternate-listener diagnostics, console inventory formatting, runtime chain control, delayed reset/start/stop, idle drain, SPI cancellation cleanup, final run-summary counts, JANUS PCAP replay, and native raw-run replay. The full `task test` and `task lint` workflows passed after the fixes.

## Other retained samples

- `test/fixtures/janus/config_same4_v3_good.txt` is the byte-preserved production configuration used for these comparisons.
- `test/fixtures/runs/run54/Run54_Info.txt` and `Run54.first256_list.dat` are JANUS processed-output fixtures. They validate list-file compatibility but are not raw DT5215 stream captures.
- The complete Run 54 binary is intentionally external to normal Git and is identified in `test/fixtures/runs/run54/README.md` by size and hash.

## Remaining evidence gaps

- Capture a Go startup with `-authorize-hv-config` and verify HV setpoint/on behavior independently from the JANUS reference.
- Preserve DT5215, DT5202 FPGA, and PIC firmware reports alongside future captures.
- Obtain vendor confirmation for the third ENUM word and protocol stability.
- Exercise cable loss, concentrator restart, partial replies, CRC errors, and board disappearance during a run.
