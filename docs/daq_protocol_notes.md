# CAEN DT5202 + DT5215 DAQ protocol notes

Status: repository study, 2026-07-20. This note records what is directly supported by the material in this repository. It is not yet a product architecture.

## Executive result

The information missing from the public manuals is present in the source distribution bundled with JANUS 5.0.0. In particular, `janus/Janus_5202_5.0.0_20260713_linux/ferslib/src/FERS_LLtdl.c` constructs every DT5215 slow-control request byte by byte, `FERS_readout.c` parses the concentrator stream and DT5202 events, and `FERS_configure_5202.c` translates acquisition settings into register writes and the Citiroc configuration bitstream.

The project decision is to implement the transport, protocol, configuration, acquisition sequencing, and decoding natively in Go. FERSlib will not be a runtime dependency. Its included source is reference evidence and may be used as a comparison oracle while developing tests.

The DT5215's USB-C connection is a USB Ethernet gadget, not the direct-DT5202 USB bulk protocol implemented in `FERS_LLusb.cpp`. On Linux the manual gives the concentrator gadget address as `172.16.1.11` (Windows: `172.16.0.11`). FERSlib opens ordinary TCP sockets to that address.

The production Windows JANUS configuration `test/fixtures/janus/config_same4_v3_good.txt` uses `172.16.0.11`, consistent with the manual's Windows default. A native DAQ must make the concentrator address configurable rather than infer it solely from the host operating system.

## Repository sources and authority

The most useful sources, in descending implementation relevance, are:

- `janus/Janus_5202_5.0.0_20260713_linux/ferslib/src/FERS_LLtdl.c`: DT5215 TCP slow control, chain enumeration/synchronization, and stream reception.
- `ferslib/src/FERS_readout.c`: start/stop behavior, DT5215 descriptor-table parsing, reconstructed event framing, and DT5202 event decoding.
- `ferslib/src/FERS_configure_5202.c`: complete DT5202 register programming and Citiroc configuration.
- `ferslib/src/FERSlib.c`: connection-path parsing, handles, public register/command abstraction, board information, flash, HV, I2C, and firmware helpers.
- `ferslib/include/FERS_Registers_520X.h`: DT5202 addresses, bit fields, commands, Citiroc fields, I/O masks, flash constants, and firmware-upload registers.
- `ferslib/include/FERS_Registers_5215.h`: DT5215 virtual registers and I/O values.
- `ferslib/include/FERSlib.h`: public API, configuration and event structures, constants, qualifiers, errors, and handle encoding.
- `ferslib/src/FERS_paramparser.c` and `include/FERS_config.h`: mapping from JANUS text parameters to the configuration model.
- `src/JanusC.c`: application lifecycle and run-control policy.
- `src/outputfiles.c` and `macros/BinToCsv/BinaryData_5202.cpp`: JANUS output-file formats (distinct from the hardware stream).
- `UM8977_DT5215_UserManual_rev2.pdf`, `WEB_UM7945_A5202-DT5202_rev4.pdf`, and `STMP_UM7946_Janus_UserManual_rev3.pdf`: hardware behavior and user-level configuration.

Paths below beginning with `ferslib/` are relative to the Linux JANUS directory.

## Actual topology seen by software

```text
DAQ process
  |
  | USB 3 cable, presenting an Ethernet network interface
  | IP/TCP to 172.16.1.11 by default on Linux
  v
DT5215 concentrator
  |-- TCP 9760: slow control, register access, commands
  |-- TCP 9000: acquisition stream
  |
  `-- TDlink optical chain(s) --> four DT5202 boards
```

JANUS connection paths described by the DT5215 manual are `usb:IP:tdl:x:y` or `eth:IP:tdl:x:y`, where `x` is chain and `y` is node. In FERSlib, both ultimately use the IP address with `LLtdl_OpenDevice`; the `usb`/`eth` prefix describes the host-to-concentrator physical interface. The production configuration establishes the intended topology as four separate links—chains 0, 1, 2, and 3—with one board at node 0 on each. Runtime enumeration must still verify that topology rather than assume it.

DT5215 web configuration remains relevant before DAQ startup. Version one explicitly requires operators to enable persistent TDlinks through that interface. The DAQ validates but does not modify link enablement and must not write `VR_ENABLED_LINKS`. The manual warns that an enabled but physically unconnected TDlink can hang the concentrator, that only connected links should be enabled, and that enabled links must be sequential starting at link 0.

## Byte order and transport assumptions

All multi-byte fields below are emitted by casting into a C byte buffer without endian conversion. On the supported x86 hosts this means little-endian. The protocol is therefore documented here as little-endian, but that is an inference from the implementation rather than a stated wire-protocol guarantee.

Both DT5215 sockets are TCP streams. Correct new code must use `send_all`/`recv_exact` loops. FERSlib often assumes one `recv()` returns the entire small response; copying that assumption would be fragile.

Slow control uses TCP port 9760. Streaming uses TCP port 9000. FERSlib applies a 3 s connection timeout and configures receive/send socket timeouts during opening.

## DT5215 slow-control protocol (TCP 9760)

The four ASCII opcode bytes are followed by packed little-endian binary fields. Offsets are byte offsets.

| Operation | Request | Reply |
|---|---|---|
| Board register write | `WREG`, u16 chain @4, u16 node @6, u32 address @8, u32 data @12 (16 B) | u32 status (0 success) |
| Board register read | `RREG`, u16 chain @4, u16 node @6, u32 address @8 (12 B) | u32 status, u32 data (8 B) |
| Immediate board command | `FCMD`, u16 chain @4, u16 node @6, u32 command @8, u32 zero @12, u32 delay @16 (20 B) | u32 status |
| Arm delayed command | `DCMD`, same 20-byte shape as `FCMD` | u32 status |
| Concentrator virtual-register write | `CWRG`, u32 address @4, u32 data @8 (12 B) | u32 status |
| Concentrator virtual-register read | `CRRG`, u32 address @4 (8 B) | u32 status, u32 data |
| Chain information | `CINF`, u16 chain @4 (6 B) | 40 B requested; fields consumed are status u16, board count u16, RTT float32, event count u64, byte count u64, event rate float32, Mbit/s float32 |
| Chain control | `CCNT`, u16 chain @4, u16 enable @6, u32 token interval @8 (12 B) | u32 status |
| Enumerate one chain | `ENUM`, u16 chain @4 (6 B) | u32 status, u32 node count |
| Synchronize chains | `SNT0` (4 B) | u32 status, then eight float32 values (one per chain) |
| Reset links | `RLNK` (4 B) | u32 status |
| Clear concentrator stream | `CLRS` (4 B) | u32 status |
| Version information | `VERS` (4 B) | u32 byte count, then a variable binary block parsed by `LLtdl_GetCncInfo` |
| Board-information block | `RBIC` plus fields used in the source | variable; parsed by `LLtdl_GetCncInfo` |

Broadcast addressing is chain `0x00ff`, node `0x00ff`. `FERS_SendCommand` uses `FCMD` for a board reached through TDlink. The default `TDL_COMMAND_DELAY` is 1,000,000 units, with the header documenting one unit as 10 ns. Delayed/broadcast synchronization uses `DCMD` and additional concentrator synchronization logic; use the source implementation as authoritative because this area has firmware-dependent paths and comments marking experimental behavior.

The full enumeration, reset, synchronization, master/slave, external-run, GPS, and multi-concentrator procedures are in `FERS_LLtdl.c` and `FERS_readout.c`. They should initially be reused rather than rewritten.

## DT5202 commands and register access

For TDlink boards, registers are reached through `WREG`/`RREG`. Commands use `FCMD`/`DCMD`. For a directly connected Ethernet or USB DT5202, a command is instead a write of the command value to register `0x01008000` (`a_commands`).

Important command values from `FERS_Registers_520X.h`:

| Value | Meaning |
|---:|---|
| `0x11` | absolute time reset |
| `0x12` | acquisition start |
| `0x13` | acquisition stop |
| `0x14` | software trigger |
| `0x15` | global reset; clears data and restores register defaults |
| `0x16` | test pulse |
| `0x17` | reset/rearm periodic-trigger counter |
| `0x18` | clear data |
| `0x19` | trigger validation |
| `0x1a` / `0x1b` | set / clear veto |
| `0x1c` | TDlink sync |
| `0x1e` / `0x1f` | select internal / external clock |
| `0x20` | load/configure Citiroc ASIC |

The project-owned DT5202 register and command map is implemented in `backend/internal/dt5202/registers.go`, source-confirmed against `FERS_Registers_520X.h`. It includes all common and DT5202-specific FPGA addresses used by the bundled implementation, acquisition-status flags, command values, and the per-channel/broadcast address conversion. Firmware-family-specific DT5203, DT5204, picoTDC, flash-layout, and I2C peripheral sub-registers are deliberately outside the DT5202 map.

Per-channel address conversion is:

```text
individual(addr, channel) = 0x02000000 | (addr & 0xffff) | (channel << 16)
broadcast(addr)           = 0x03000000 | (addr & 0xffff)
```

Per-channel bases cover zero-suppression thresholds, fine Q/T thresholds, low/high gain, HV adjustment, and hit counters. Do not create a second hand-maintained register list: import or translate the vendor header so firmware updates remain diffable.

## DT5215 virtual registers

The complete map is in `FERS_Registers_5215.h`. It defines virtual addresses 0 through 58 for I/O electrical standard and directions, front/rear I/O functions, input/output masks, test pulse, clock source, synchronization, master/slave role, enabled links, delayed-command source, GPS state/data, and link K/CRC error counters.

These are accessed with `CWRG`/`CRRG`, not board `WREG`/`RREG`. Particularly relevant addresses are:

- 16: clock source
- 20: master/slave role (the source contains the misspelled symbol `VR_IO_MASTER_SALVE`)
- 22: send sync pulse
- 23: synchronization delay
- 25: PPS source
- 26: PLL status
- 31: enabled-link mask
- 33: external input to delayed-command trigger mapping
- 34: target epoch
- 35--42: FPGA/GPS time and navigation state
- 43--50: K errors per link
- 51--58: register-transaction CRC errors per link

## Acquisition setup translated by FERSlib

`Configure5202()` is the definitive translation from high-level configuration to hardware. In broad order it:

1. Optionally sends global reset for a hard configuration.
2. Programs channel enables and connection mode; with TDlink and FPGA firmware >=4 it shuts down the unused PIC using `a_uC_shutdown = 0xDEAD`.
3. Programs acquisition mode, zero suppression, service events, timing/ToT, validation, counting mode, widths, trigger-ID source, gain selection, ADC range, pedestal behavior, I/O, waveform length, periodic trigger, trigger source, run mode, time-reference window/delay, trigger logic, veto, and validation.
4. Programs test pulse and probes.
5. Programs Citiroc common settings, masks, shaping, thresholds, hold/mux timing, and all per-channel gains/fine thresholds/HV adjustment.
6. Constructs two 1,144-bit Citiroc slow-control streams, writes them through the FPGA slow-control registers, and sends `CMD_CFG_ASIC`.
7. Programs HV and remaining run-dependent values and verifies/report errors.

This is important: setting gains is not just one register write. Values participate in construction of the Citiroc bitstream. A replacement that bypasses FERSlib must port the `SetCitiroc`/slow-control bit placement exactly.

The project-owned Citiroc representation is now implemented in `backend/internal/dt5202/citiroc.go`. It uses the same least-significant-bit-first word convention as `ReadSCbsFromFile`, covers all 1,144 positions documented by the bundled `WriteCStoFileFormatted`, and maps board channels 0--31 to chip 0 and 32--63 to chip 1. The 15-bit channel preamplifier field is source-confirmed against the official Citiroc 1A datasheet: six HG-gain bits, six LG-gain bits, HG/LG calibration enables, and preamplifier disable.

Normal JANUS configuration does not construct or upload these words on the host. FPGA firmware builds the stream from the DT5202 configuration registers. The source-confirmed production command sequence is therefore a write of `a_scbs_ctrl=0`, `CMD_CFG_ASIC`, a write of `a_scbs_ctrl=0x200`, and a second `CMD_CFG_ASIC`. The Go implementation follows that sequence. Explicit stream construction is retained for golden comparison and future manual loading, but must not be presented as a production manual-load image until every power-control value has been requested or deliberately defaulted.

## HV peripheral configuration

The DT5202 HV module is accessed indirectly through `a_hv_regaddr` and `a_hv_regdata`. A write selector is `(data_type << 8) | peripheral_register`; data type 0 is signed integer, 1 is fixed point scaled by 10,000, 2 is unsigned integer, and 3 is float. The bus initialization selector is `0x2001`. Each selector and data write is followed by polling acquisition-status bit 17 (I2C busy), while bit 18 reports I2C failure.

The source-confirmed hard-configuration sequence initializes the bus, selects PID precision through peripheral register 30, writes voltage register 2 twice, current-limit register 5 twice, temperature coefficients 7/8/9 twice, and feedback coefficient/enable registers 28/1 twice. Repeated writes reproduce FERSlib's workaround for unreliable first accesses. Applying this plan is a separate explicit operation because changing HV setpoints is safety-relevant; ordinary FPGA configuration does not implicitly perform it.

## Pedestal calibration semantics

`Pedestal` is not a DT5202 register. FERSlib loads 64 low-gain and 64 high-gain calibration values from a protected flash page during board connection, then corrects decoded energy as `raw + common_pedestal - channel_calibration`, clamped to the energy range. The Go decoder preserves raw values and exposes this correction as a separate pure step requiring calibration provenance.

In spectroscopy mode only, FPGA zero-suppression thresholds are derived per channel as `requested_threshold - common_pedestal + channel_calibration` and assigned to an unsigned 16-bit register value. Spectroscopy-plus-timing intentionally does not program these thresholds because suppressing only energy would produce partial events. The planner requires a provenance-tagged calibration before completing pedestal and spectroscopy zero-suppression semantics; it never writes protected flash.

The user configuration vocabulary and units are completely represented by `Config_t` in `FERS_config.h`, parsing in `FERS_paramparser.c`, and the example/definition files under `bin/`. `FERS_LoadConfigFile`, `FERS_SetParam`, `FERS_GetParam`, and `FERS_configure` are suitable first-version APIs even if JANUS itself is not used.

## Run-control sequence for this topology

The implemented safe sequence is more than start/stop commands:

1. Open every connection path. Opening reads board information/calibration flash, restores DC offsets, establishes the concentrator connection, and sends acquisition stop to recover from an earlier run.
2. Enumerate the enabled TDlink chains and map `(concentrator, chain, node)` to board handles.
3. Synchronize TDlink chains. TDL start is rejected unless synchronization completed.
4. Load configuration, apply hard configuration, and initialize readout for every board.
5. Before a run, flush each board/concentrator and clear CRC counters.
6. Start receiver thread(s) before issuing start. One stream receiver services all boards behind one concentrator.
7. In TDL mode, disable readout trains, issue delayed broadcast periodic-trigger reset, then delayed broadcast `CMD_ACQ_START`. The concentrator resumes trains as the start is received.
8. Repeatedly call the event reader/decoder and dispatch by board index and data qualifier.
9. Stop with delayed broadcast `CMD_ACQ_STOP`; keep draining until the library reports the buffers empty. Only then end receiver processing and close raw files/readout/device handles.

FERSlib exposes this as `FERS_OpenDevice`, `FERS_LoadConfigFile` or `FERS_SetParam`, `FERS_configure`, `FERS_InitReadout`, `FERS_SyncTDLchains`, `FERS_StartAcquisition`, `FERS_GetEvent`, `FERS_StopAcquisition`, `FERS_CloseReadout`, and `FERS_CloseDevice`. Exact declarations and error values are in `FERSlib.h`.

## DT5215 stream framing (TCP 9000)

The stream is not a simple sequence of DT5202 packets. The concentrator sends batches per chain:

1. Three 32-bit little-endian header words.
   - word 0 = `0xffffffff`
   - word 1 = `0xffffffff`
   - word 2 bits 7:0 = chain ID
   - word 2 bits 31:8 = descriptor-row count
2. A descriptor table of `row_count * 32` bytes; each row is eight 32-bit words.
3. An event-payload region. Descriptor pointers are word offsets into this region; gaps/fillers can occur.

Descriptor row decoding performed by FERSlib:

```text
payload_pointer_words = (w0 >> 24) | ((w1 & 0x00ffffff) << 8)
payload_size_words    = w0 & 0x00ffffff
timestamp_56          = (w1 >> 24) | (w2 << 8) | ((w3 & 0xffff) << 40)
trigger_id_56         = (w3 >> 16) | (w4 << 16) | ((w5 & 0xff) << 48)
node                  = w7 & 0xff
data_qualifier        = (w7 >> 8) & 0xff
crc_error             = (w7 >> 16) & 1
```

FERSlib consumes the payload using its pointer/size, then synthesizes a uniform five-word event header before decoding:

```text
word 0 = (data_qualifier << 24) | (payload_size_words + 5)
word 1 = trigger_id low 32
word 2 = trigger_id high 32
word 3 = timestamp low 32
word 4 = timestamp high 32
word 5... = original DT5202 payload
```

The 56-bit descriptor values therefore become zero-extended 64-bit values in this internal format. Maximum internal event size is 64 KiB. The implementation validates the two sentinel words, nonzero/bounded table size, event size, missing payload timeouts, and the descriptor CRC-error bit.

## DT5202 event formats

All decoded events begin with the five-word normalized header above. Word 0 low 16 bits is total size in 32-bit words and bits 31:24 are the data qualifier. Clock period for DT5202 timestamp conversion is 8 ns.

Qualifier constants and public decoded structures are defined in `FERSlib.h`; decoder truth is `FERS_DecodeEvent_5202()`.

### Spectroscopy / spectroscopy plus timing

After the common header, words 5--6 are a 64-bit channel mask. Energy entries follow only for set channels.

- If qualifier bit 4 says both gains are present, each channel uses one word: HG in bits 13:0 and LG in bits 29:16; discriminator information uses bit 15.
- Otherwise two 16-bit energy entries are packed per word. Entry bit 14 identifies low versus high gain, bit 15 carries discriminator state, and bits 13:0 carry energy.
- Optional timing words follow. A word with bit 31 set is a time reference (`bits 30:0`). Otherwise bits 31:25 identify channel, bits 24:16 contain 9-bit ToT, and bits 15:0 contain timestamp. The decoder retains the first hit per channel.
- FERSlib optionally applies flash-derived pedestal correction and clamps to the configured 13/14-bit energy range.

### Counting

Words 1--4 contain trigger ID and timestamp. Payload entries use bits 31:24 as channel and bits 23:0 as count. Channels 0--63 are physical channels, 64 is T-OR, and 65 is Q-OR. Qualifier bit 7 adds a relative-timestamp word before counts.

### Timing list

Word 5 contains the fine part of the time reference; FERSlib forms `Tref = (coarse_timestamp << 4) | (word5 & 0xf)`. Hits start at word 6. Bits 31:25 are channel. In leading-edge-only format (qualifier high nibble `0x2`), bits 24:0 are timestamp. Otherwise bits 15:0 are timestamp and bits 24:16 are 9-bit ToT.

### Waveform

Each sample word contains HG bits 13:0, LG bits 27:14, and four digital-probe bits at 31:28.

### Service and test events

The decoder supports test payloads and multiple service-event versions. Service data include FPGA/board/detector/HV temperatures, HV voltage/current/status, acquisition status, and optional per-channel/T-OR/Q-OR counters. This format has version-dependent branches; port the decoder rather than relying on a short prose summary. JANUS expects service events roughly once per second and warns after about two seconds without one.

## Raw capture and JANUS files are different layers

Do not confuse:

- TCP 9000 concentrator framing (descriptor table + payload);
- FERSlib `.frd` raw capture, which prepends a versioned file header containing concentrator/board information and calibration data and then stores low-level stream bytes;
- JANUS processed binary/list/count/spectrum files written by `outputfiles.c`;
- CSV conversion formats in `macros/BinToCsv`.

FERSlib already supports opening raw output, splitting subruns, and replaying raw files offline. Retaining this facility in an early replacement DAQ would be valuable for regression tests.

The production Run 54 artifacts add a concrete processed-list example: Windows JANUS 4.3.0, output format 3.4, A5202 spectroscopy plus timing, four boards, and a 0.5 ns ToA LSB. A 256-event record-aligned prefix and the complete run log are committed under `test/fixtures/runs/run54/`; the 129,926,126-byte full binary is identified by hash but kept outside normal Git. This fixture validates JANUS list compatibility, not DT5215 TCP framing.

## Direct DT5202 USB protocol (not DT5215 USB gadget)

For completeness, `FERS_LLusb.cpp` exposes the direct front-end USB protocol (Microchip VID `0x04d8`, PID `0x0053`): bulk OUT endpoint `0x01`, command reply endpoint `0x81`, stream endpoint `0x82`; opcode `0x80` writes memory, `0x81` reads memory, `0x10` writes a service register, and `0xfa {0|1}` disables/enables streaming. Multi-byte fields are little-endian. This is not the host transport used by a DT5215 connected over its USB Ethernet gadget.

## What is known versus what still needs hardware validation

Known from source:

- complete host-side TCP command bytes and replies used by JANUS/FERSlib;
- complete DT5202 and DT5215 register symbols and command values;
- complete configuration algorithm, including Citiroc bitstream construction;
- connection lifecycle, enumeration, synchronization, run control, and readout threading;
- concentrator batch framing and DT5202 event decoding;
- public decoded-event structures, error codes, raw replay, and output conversion.

Still requiring validation or a vendor clarification:

- whether the protocol is contractually stable/public or only an implementation detail of this FERSlib release;
- explicit byte-order and TCP message-framing guarantees (the code assumes little-endian/x86 and sometimes assumes complete `recv()` calls);
- meanings of all nonzero DT5215 status codes;
- precise unused/reserved descriptor bits and the packet-ID field not consumed by the current decoder;
- exact `VERS`/`RBIC` response layout across concentrator firmware versions;
- compatibility matrix for FERSlib 5.0.0, DT5215 firmware `2026.4.1.1`, DT5202 FPGA `8.0_A707`, and PIC firmware `2026.5.28.3`;
- whether CAEN exposes a supported authenticated API for web-interface provisioning; this is explicitly outside version one;
- behavior after cable loss, concentrator reboot, partial TCP writes/reads, CRC errors, and a board disappearing mid-run.

## Recommended evidence-gathering experiments

These are the next study steps, before architecture selection:

1. Record exact installed firmware, board IDs, chain/node enumeration, and the JANUS configuration used for a known-good run.
2. Capture traffic on the DT5215 USB network interface with `tcpdump`/Wireshark while JANUS opens, configures, starts, triggers, stops, and closes. This validates byte order and provides golden request/reply/stream fixtures.
3. Enable FERSlib debug/raw capture and save the same run as `.frd`; replay it using offline mode and compare event counts and decoded fields.
4. Build a small read-only probe using FERSlib: open concentrator, enumerate, read board identity/firmware/status/temperatures, and close. Avoid writes initially.
5. Add controlled writes to a harmless register, then configuration and a test-pulse run. Compare register dumps using the supplied `Dump_ParamAndConfig.txt`/`Dump_RawData.txt` macros.
6. Exercise one board, four boards on one chain, and (if applicable) boards split across links; confirm board-index mapping and time alignment.
7. Fault-inject unplug/replug, disabled/enabled empty links, process kill during run, stop with queued data, and CRC corruption/offline truncated captures.
8. Ask CAEN support for a protocol-stability statement, DT5215 command/status documentation, and redistribution/licensing guidance for FERSlib.

## Practical conclusion for later architecture work

No disassembly-based binary reverse engineering is necessary to begin because the relevant FERSlib source is available. The first milestone will implement a native Go transport/parser against the deterministic simulator and source-derived golden vectors. Real packet captures and hardware tests will then validate and correct the implementation before production use.

The included FERSlib carries LGPL notices while JANUS carries GPL/LGPL files; exact obligations depend on how the eventual product links, modifies, and distributes these components. Treat licensing as a design input and obtain appropriate legal review rather than assuming the source can simply be copied into a proprietary product.
