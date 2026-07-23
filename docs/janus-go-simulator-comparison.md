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
