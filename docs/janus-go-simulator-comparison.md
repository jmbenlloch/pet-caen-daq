# JANUS versus native Go simulator comparison

## Result

On 2026-07-23, JANUS 5.0.0/FERSlib 2.2.0 and the native Go DAQ both
completed a four-board, 15-second `PRESET_TIME` spectroscopy run against a
fresh instance of the project simulator. Both used the same configuration
bytes after changing only the connection paths, output path, and online-analysis
setting needed for a headless Linux test.

The comparison supports these conclusions:

- Both clients discovered and configured one simulated DT5202 on each of
  TDlinks 0–3.
- Both data streams decoded without error through the production DT5215 and
  DT5202 decoders. Each stream contained equal per-chain event counts, only
  spectroscopy (`0x13`) and service (`0x2f`) events, and service format version
  1.
- The final values of all 1,392 directly comparable board-register locations
  were equal except `TrefDelay` on the four boards. JANUS wrote the host-side
  sign extension `0xffffffc2`; Go wrote the capture-verified effective 20-bit
  field `0x000fffc2`. Real hardware reads both as `0x000fffc2`; this intentional
  difference is documented in `daq_protocol_notes.md`.
- All 28 HV selector/data settings written by both clients had equal values.
  Go additionally wrote selector `0x021e = 1` and `0x2001 = 0` on every board
  as explicit enable/shutdown safety state.
- A focused follow-up on 2026-07-24 applied two matched HV profiles and
  explicitly switched all four simulated outputs on and off from both clients.
  Every functional voltage, current-limit, temperature-sensor, feedback, and
  output-enable selector/data value was equal. Go reinitializes the HV bus
  before every output switch; JANUS relies on FERSlib's process-local
  `HVinit` state after the first initialization.
- Both clients use the firmware-4+ direct monitor registers for FPGA/board/HV
  temperatures, voltage, current, and status. Go reads the combined HV status
  word once per sample; JANUS reads that same word separately for detector
  temperature, HV temperature, and output/fault status.
- JANUS also reads A7585 HV-module firmware register 252 through indirect
  selector `0x000103fc`, interprets its raw IEEE-754 bits as a version number,
  prints it during connection, and records it in JANUS run information. The
  native backend previously recorded only DT5202 FPGA firmware.
- JANUS obtains DT5215 software revision, FPGA revision, and PID from `VERS`.
  Native discovery now decodes the same fixed fields and preserves them in
  startup logs, system telemetry, and every run manifest.
- The complete configuration transcripts are **not byte-identical**. JANUS
  issued 1,900 `WREG` requests in the selected configuration cycle and Go
  issued 2,398. JANUS uses `CitirocSlowControl`; Go performs the
  source-confirmed Citiroc SPI transaction and readback/audit sequence through
  `SPIData`. JANUS also uses delayed broadcast commands (`DCMD`) where Go uses
  immediate or scheduled `FCMD` requests.

This is simulator evidence, not hardware verification. It demonstrates that
both programs can run against the same deterministic protocol peer and that
their application-level data is decoder-compatible. It does not by itself
prove that the different Citiroc and command sequences are equivalent on
physical hardware; the native sequence remains covered by source-derived
encoders, byte-level tests, and the separately indexed real-hardware captures.

## Focused HV configuration and switching follow-up

The 2026-07-24 follow-up used the same JANUS/FERSlib and simulator versions
listed below. Both clients reached `READY`; no acquisition was active while
switching HV. The Go backend was launched with `-authorize-hv-config`, and its
`SystemService/SetHighVoltage` operation was used with an empty board list to
select all four boards. JANUS used its native console HV panel's all-board
control.

| Profile | `HV_Vbias` | `HV_Imax` | `TempSensType` | Feedback |
| --- | ---: | ---: | --- | --- |
| A | 40.0 V | 0.5 mA | TMP37 | disabled, 20 mV/°C |
| B | 60.0 V | 2.0 mA | LM94021_G11 | enabled, 35 mV/°C |

For both profiles and every board, the functional indirect HV transactions
matched:

| Function | Selector | Profile A data | Profile B data |
| --- | ---: | ---: | ---: |
| Bias voltage | `0x0102` | `400000` | `600000` |
| Current limit | `0x0105` | `5000` | `20000` |
| Temperature coefficients | `0x0107`–`0x0109` | `0, 500000, 0` | `0, -736300, 1942500` |
| Feedback coefficient | `0x011c` | `-200000` | `-350000` |
| Feedback enable | `0x0001` | `0` | `2` |
| Output ON | `0x0200` | `1` | `1` |
| Output OFF | `0x0200` | `0` | `0` |

Each capture contains four ON and four OFF transactions, one of each per
board. JANUS sends the selector/data pair directly because its HV bus is
already initialized. Go precedes each per-board switch with `0x2001 = 0`.
Consequently, per profile JANUS sends four bus initializations during
configuration, while Go sends twelve: four during configuration, four before
ON, and four before OFF. The extra Go transactions are defensive
initialization and do not change the requested output state.

JANUS's HV panel also repeatedly reads configured setpoints through indirect
selectors `0x10102` and `0x10105`. Go monitors the firmware-4+ direct registers
instead. Both read direct registers `0x01000340`, `0x01000348`,
`0x01000356`, `0x01000358`, and `0x01000360`; JANUS reads `0x01000360`
three times per monitor update because three public helper calls independently
consume the same combined word.

The ignored evidence directory is
`test-results/janus-go-hv-switch-comparison/`. Capture hashes are:

```text
2ef8cf591451e1d4231777f5f986b0dc60fa00aab27b4e20c73ad6e2261a517d  janus-a.pcap
ebfbdddc3aed0b531cc20f3d7b914ee83c8d4c848f280985f0b65da978047fad  go-a.pcap
9c2ba09e88f10d5da8873583baeb6ca0b805e3d9376550f6646428ed48ef349d  janus-b.pcap
13be3b0aff86e73a5b5c73a911f95f6e9209ce972a0a1911d349a89d40b75ccc  go-b.pcap
355c835667e21e4cd9a3cd6b23b314ea7d32a6a0fcd041709522c6c39fabb18d  analysis.txt
```

The successful captures do not exercise the clients' different partial-failure
policy. Go rolls back already-enabled boards if a later board fails during an
all-board enable. JANUS's console panel loops over boards without an equivalent
rollback transaction.

## Inputs and environment

- Repository branch: `feature/janus-simulator-compat`
- Simulator/Go base commit: `cccb95f`
- JANUS package: `Janus_5202_5.0.0_20260713_linux`
- JANUS/FERSlib versions: 5.0.0/2.2.0
- Topology: four chains, node 0 on each chain
- Simulator event interval: 100 ms
- Capture interface/filter: loopback, `tcp port 9760 or tcp port 9000`

The user-supplied `config_same4_v3_good.txt` was copied separately for each
client. Both resulting inputs had SHA-256:

```text
d883f4b3e89f92d081b3bc3303a1ec86475cde66fcf352251b0fac5e8ec05552
```

The applied environment-only changes were:

```text
Open[0] eth:127.0.0.1:tdl:0:0
Open[1] eth:127.0.0.1:tdl:1:0
Open[2] eth:127.0.0.1:tdl:2:0
Open[3] eth:127.0.0.1:tdl:3:0
DataAnalysis DISABLED
DataFilePath /tmp/janus-comparison-output
```

`DataAnalysis` was disabled because this host has no gnuplot executable.
Disabling presentation-side analysis does not alter device configuration or
wire acquisition.

## Captured results

| Measurement | JANUS | Native Go |
| --- | ---: | ---: |
| Preset | 15 s | 15 s |
| Stream TCP flows | 1 | 1 |
| Stream bytes | 363,584 | 341,024 |
| Decoded batches/events | 648 | 608 |
| Per-chain events | 162 each | 152 each |
| Spectroscopy events | 640 | 600 |
| Service events | 8 | 8 |
| Decoder failures | 0 | 0 |
| Selected-cycle `WREG` requests | 1,900 | 2,398 |
| Selected-cycle directly comparable final registers | 1,392 | 1,392 |
| Effective direct-value mismatches | 0 | 0 |

The event-count difference is expected from independently started wall-clock
runs: the simulator emits one four-board event set every 100 ms, while client
startup and preset-stop boundaries are not synchronized. It is not a payload
format difference. Exact stream hashes are consequently expected to differ
because trigger IDs and timestamps advance independently.

The local, ignored evidence directory was
`test-results/janus-go-config-comparison/`. Important artifact hashes were:

```text
75bd57d7612465b0a207f8954b062a931eddd06d8116107be7ab872ebdbe1c37  janus.pcap
c6e32a7c1bfb77150c807feca333dfa988828907563835e2c1c760e0c82cc12e  go.pcap
f5b38262adcc8a881b09f2805eada82b0d39f0da35e692a3ed3da15f0c075e6b  go-runs/run-1/wire.raw
2e3e746ca33194b452de07807356fdfb579eaf9b9f6f808cc9091d8cabc8de06  go-runs/run-1/transport.journal
```

The PCAPs and generated run products are intentionally not committed. They
remain locally available for byte-level follow-up without inflating Git
history.

## Procedure

Build and run the simulator, then start a privileged loopback capture:

```sh
task build
./bin/caen-simulator
docker run --rm --network host --cap-add NET_ADMIN --cap-add NET_RAW \
  -v "$PWD/test-results/janus-go-config-comparison:/captures" \
  nicolaka/netshoot:latest tcpdump -i lo -U -w /captures/janus.pcap \
  'tcp port 9760 or tcp port 9000'
```

From the JANUS `bin` directory:

```sh
LD_LIBRARY_PATH=../ferslib/local/lib \
  ./JanusC -c/absolute/path/to/janus-config.txt
```

Press `s`, wait for the preset stop and `Ready to start` prompt, then press
`q`. Restart the simulator before the native run so counters and register state
cannot leak between clients.

Start a new capture and launch Go with explicit synthetic-HV authorization:

```sh
./bin/pet-caen-daq \
  -config test-results/janus-go-config-comparison/go-config.txt \
  -control 127.0.0.1:9760 -stream 127.0.0.1:9000 \
  -listen 127.0.0.1:18080 \
  -runs test-results/janus-go-config-comparison/go-runs \
  -catalog test-results/janus-go-config-comparison/go-catalog.sqlite \
  -authorize-hv-config
```

Start the run through `RunService/StartRun` with the exact configuration
contents, `captureRaw=true`, and `journalTransport=true`. `ListRuns` reported
run 1 complete with `terminationReason=preset_time`, 608 events, 608 raw
batches, and all three requested artifacts.

For local simulator captures, the opt-in stream conformance test accepts the
loopback source address:

```sh
JANUS_DATA_TAKING_SOURCE_IP=127.0.0.1 \
JANUS_DATA_TAKING_PCAP=/path/to/capture.pcap \
go test -tags='integration capture' ./backend/integration \
  -run TestJANUSDataTakingCaptureConformance -v
```

An unregistered capture is expected to fail the golden-profile assertion after
printing its complete decoded statistics. That failure does not indicate a
decode failure; new permanent evidence should be reviewed and deliberately
added to the golden table before becoming a conformance fixture.
