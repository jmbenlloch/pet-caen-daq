# JANUS versus native Go run comparison

## Result

The 2026-07-25 capture `pcap/janus_go_comparison.pcap` contains two complete
run starts against the same DT5215 at `172.16.0.11`. The first connection is
JANUS and the second is the native Go backend, as specified when the capture
was supplied. Both control and event-stream TCP SYN handshakes are present.

Go reached acquisition start **7.365 s sooner** than JANUS: 15.504 s instead
of 22.869 s from the control TCP SYN. Most of the difference is connection
initialization, where Go was 5.508 s faster. Go configuration was 1.230 s
faster, and its run-control setup was 627 ms faster.

The connection/configuration subdivision is useful for locating wire traffic,
but it is not a like-for-like lifecycle comparison. JANUS reads identity and
pedestal flash for all four boards inside `FERS_OpenDevice`, before its
configuration loop. Go performs minimal topology discovery first and loads a
board's pedestal immediately before configuring that board. Consequently the
first-`GLOBAL_RESET` boundary attributes all four JANUS flash loads, but only
Go board 0's flash load, to "connection"; Go boards 1--3 appear under
"configuration". The combined SYN-to-run-start result is directly comparable.

| Measured interval | JANUS | Native Go | Go difference |
| --- | ---: | ---: | ---: |
| Connection | 7.111 s | 1.603 s | 5.508 s faster (77.4%) |
| Configuration | 15.071 s | 13.840 s | 1.230 s faster (8.2%) |
| Run setup | 0.687 s | 0.060 s | 0.627 s faster (91.2%) |
| Active acquisition | 16.034 s | 15.014 s | 1.020 s shorter |
| Data taking, setup through stop | 16.721 s | 15.075 s | 1.646 s shorter |
| SYN through acquisition start | 22.869 s | 15.504 s | 7.365 s faster (32.2%) |

The active interval is operator-selected run duration, not configuration
performance. In this sample Go is within 14 ms of 15 seconds; JANUS runs about
one second longer. The capture alone cannot determine whether that extra
second came from JANUS polling/UI stop timing or the operator.

## Measurement boundaries

The PCAP does not label application phases, so this comparison uses observable
wire boundaries:

- **Connection:** control TCP SYN through the first per-board
  `GLOBAL_RESET`. This includes device identity, topology, flash/pedestal, and
  other startup access performed before configuration begins.
- **Configuration:** first per-board `GLOBAL_RESET` through `CLRS`, the stream
  clear immediately before run-control setup.
- **Run setup:** `CLRS` through broadcast `ACQ_START`.
- **Active acquisition:** broadcast `ACQ_START` through broadcast `ACQ_STOP`.
- **Data taking through stop:** `CLRS` through `ACQ_STOP`, combining setup and
  active acquisition.

The first `GLOBAL_RESET` is an inferred configuration boundary supported by
the subsequent per-board configuration pattern. The command timestamps and
durations are capture-verified; the semantic phase label is inferred.

| Milestone from control SYN | JANUS | Native Go |
| --- | ---: | ---: |
| First per-board `GLOBAL_RESET` | 7.111 s | 1.603 s |
| `CLRS` | 22.182 s | 15.444 s |
| Broadcast `ACQ_START` | 22.869 s | 15.504 s |
| Broadcast `ACQ_STOP` | 38.903 s | 30.518 s |

JANUS opened control/data connections from ports `52423/52424` at
11:39:42.540696/11:39:42.566688 and closed both at about 11:40:34.458. Go
opened ports `52719/52720` at 11:40:44.230860/11:40:44.234912. The capture
ends with the persistent Go backend connections still open, so connection
shutdown is intentionally not compared.

## Commands used

### Connection

| Request or command | JANUS | Native Go |
| --- | ---: | ---: |
| `VERS` | 1 | 1 |
| `RBIC` | 1 | 0 |
| `CINF` | 32 | 8 |
| `CWRG` | 4 | 0 |
| `RREG` | 1,320 | 312 |
| `WREG` | 1,372 | 302 |
| `CCNT` | 8 | 0 |
| Broadcast `SYNC` | 1 | 0 |
| Broadcast `RESET_TIME` / `RESET_PTRG` | 1 / 1 | 0 / 0 |
| Per-board `ACQ_STOP` | 4 | 0 |

JANUS performs substantially more work while connecting: it queries `CINF`
four times per chain, reads the additional `RBIC` identity block, manipulates
concentrator virtual registers and readout trains, issues synchronization and
reset commands, stops each board, and performs roughly four times as many
board register accesses before the chosen boundary. Most of those JANUS
accesses are four complete board-information and pedestal-flash reads. Go
reads `VERS`, queries every chain once, and reads three identity/status
registers per board during discovery. Its pedestal reads belong to the
configuration service and straddle the chosen boundary, one board before it
and three afterward. The already-ready topology means neither client sends
`RLNK`, `ENUM`, or `SNT0` in this capture.

### Configuration

| Request or command | JANUS | Native Go |
| --- | ---: | ---: |
| `WREG` | 1,900 | 2,398 |
| `RREG` | 789 | 2,432 |
| `CWRG` | 12 | 0 |
| Per-board `GLOBAL_RESET` | 4 | 4 |
| Per-board `CONFIGURE_ASIC` | 16 | 8 |
| `CLEAR_DATA` before `CLRS` | 1 | 0 |

JANUS sends four `CONFIGURE_ASIC` commands per board because each of the two
chips is first loaded and then clocked again by `ReadSCbsFromChip` before its
36-word shift-register stream is read. Thus the four commands are two loads
plus two readback clocks, not four distinct configurations. Go sends two
immediate (`delay=0`) commands—one load per chip—and validates ordinary FPGA
registers afterward. Although Go contains `VerifyCitirocReadback`, the normal
`ApplyConfiguration` path does not call it, so this capture has no equivalent
ASIC shift-register verification. Go completes the wire-defined
configuration interval faster despite 2,141 more `RREG` requests and 498 more
`WREG` requests in this phase. Some of that difference is the three Go
pedestal loads assigned to this phase by the boundary. JANUS also repeats a
full four-board configuration after stopping; Go does not. That post-run
JANUS work is outside the timing table above.

### Data taking

Both clients use the same essential acquisition sequence:

```text
CLRS
CCNT disable
FCMD RESET_TIME  delay=1,000,000
FCMD RESET_PTRG  delay=1,000,000
FCMD ACQ_START   delay=1,000,000
CCNT enable      token_interval=0x100
...
FCMD ACQ_STOP    delay=1,000,000
```

The differences are:

- JANUS sends `CLEAR_DATA` to all four boards before disabling trains. One
  occurs immediately before `CLRS` and three immediately after it. Go sends
  none.
- JANUS sends disable/enable `CCNT` requests to all eight chain indices. Go
  sends them only to the four provisioned chains.
- During active acquisition JANUS sends 160 monitoring `RREG` requests; Go
  sends 300.
- Event data begins 7.6 ms after JANUS `ACQ_START` and 10.3 ms after Go
  `ACQ_START`.
- JANUS's last captured event-stream payload is 10.1 ms after `ACQ_STOP`.
  Go continues draining captured stream payload until 1.341 s after
  `ACQ_STOP`; this is drain behavior after the active interval, not extra
  acquisition time.

JANUS sends the first board's `CLEAR_DATA` 16.9 ms before `CLRS`, which puts it
at the end of the configuration interval under the common boundary above.
Counting all four clear commands as run setup would increase JANUS setup from
0.687 s to 0.704 s but would not change the conclusion.

## Reproduction

The capture is external evidence and is not copied into Git:

```text
size:   151,878,660 bytes
sha256: e0f6f5d75f934bd1988ca81b9c55981ef2625f7f643e21d4fdb819288e1ff31b
```

Run the repository analyzer from the worktree root:

```bash
python3 scripts/analyze-run-control.py \
  ../pcap/janus_go_comparison.pcap --json > /tmp/janus-go.json
```

The analyzer reads classic little-endian Ethernet PCAP directly, reconstructs
each client-to-DT5215 TCP stream with retransmission de-duplication, rejects
sequence gaps and unknown opcodes, decodes command arguments, and emits the
phase definitions, timestamps, durations, request counts, and complete
timestamped command transcript.

## Evidence classification

- Session ordering and program identity: supplied capture context.
- TCP endpoints, timestamps, request bytes, counts, and durations:
  **capture-verified**.
- Phase names at the first `GLOBAL_RESET`: **inferred**, with the boundary
  stated explicitly above.
- Command meanings and delay units: **source-confirmed** by bundled
  FERSlib/JANUS and matched by the native protocol implementation.
