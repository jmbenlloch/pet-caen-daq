# DT5215 system architecture and command inventory

Status: static analysis, 2026-07-28.

This note describes the DT5215 architecture and the command surfaces implemented
by device software release `2026.4.1.1`. It distinguishes the DT5215 TCP
protocol from the command values forwarded to attached FERS boards. Those are
different protocol layers.

“Complete” below is scoped to the port-9760 dispatcher and explicitly registered
HTTP API routes in this exact executable. It does not claim that every Linux
service, CGI capability, static-file path, FPGA register, or operation in other
firmware releases has been enumerated.

## Evidence and scope

The findings combine:

- `UM8977_DT5215_UserManual_rev2.pdf`, which describes the physical system and
  supported operation;
- the unstripped AArch64 executable
  `firmware/dt5215-upgrade_2026.4.1.1-extracted/caen/software/fers_bridge.elf`,
  which is the authoritative inventory of handlers in this firmware release;
- bundled FERSlib 5.0.0 source, especially `ferslib/src/FERS_LLtdl.c` and
  `ferslib/include/FERS_Registers_520X.h`; and
- the native implementation in `backend/internal/dt5215` and
  `backend/internal/dt5202`.

The browser-to-device chain scan and its comparison with native discovery are
documented separately in
[DT5215 webapp chain-scan protocol compared with native Go discovery](dt5215-webapp-discovery-protocol.md).

Evidence labels follow the repository policy:

- **device-confirmed**: present in the exact DT5215 executable;
- **source-confirmed**: request layout or behavior is constructed by bundled
  FERSlib source;
- **capture-verified**: observed in retained hardware captures;
- **inferred**: meaning follows names or surrounding implementation but has not
  been exercised.

No undocumented state-changing command was sent to hardware during this study.

## System architecture

```text
Host DAQ / JANUS / browser
        |
        | 1 GbE, 10 GbE, or USB 3 Ethernet gadget
        | TCP 9760 control | TCP 9000 stream | HTTP 8080 web API/UI
        v
+------------------------------------------------------------------+
| DT5215: Zynq UltraScale+ MPSoC                                   |
|                                                                  |
| ARM processing system                  FPGA programmable logic    |
| - quad-core AArch64 Linux              - 8 TDlink masters         |
| - fers_bridge.elf                      - clock/sync distribution  |
| - TCP control/stream servers           - command broadcast        |
| - HTTP API and static web UI           - descriptor/data buffers  |
| - configuration and upgrade logic      - DMA to ARM memory        |
| - 4 GB DDR buffering                                             |
+------------------------------+-----------------------------------+
                               |
                   8 x 3.125-Gbit/s optical TDlinks
                               |
             up to 16 FERS-5200 nodes per daisy-chain ring
                               |
                  DT5202 front ends (64 channels each)
```

The manual specifies eight TDlink masters, at most 16 nodes per link, hence at
most 128 front ends or 8192 channels. The present PET system uses chains 0--3
with one DT5202 at node 0 on each chain.

### Control path

The host opens TCP port 9760 and sends a four-byte ASCII operation followed by
packed little-endian fields. `fers_bridge.elf` translates board operations into
TDlink transactions handled by the FPGA. Concentrator virtual registers instead
control local FPGA functions such as link state, synchronization, clocks, GPS,
and front/rear-panel I/O.

### Data path

DT5202 events travel around their TDlink rings into the FPGA. The FPGA maintains
an event-descriptor table and payload buffer. DMA transfers both into ARM-visible
memory; `fers_bridge.elf` packages them into the port-9000 stream. The host
reconstructs each event by combining its 32-byte descriptor with the referenced
payload words. Control and streaming use independent TCP connections.

### Synchronization path

The DT5215 distributes its 156.25/156.26 MHz-class clock through each TDlink.
Enumeration measures link/ring propagation, and synchronization aligns the
front-end absolute-time counters. Run start, stop, timestamp reset, periodic
trigger reset, test pulse, and other board commands can be broadcast with a
scheduled delay. The normal native start sequence uses a 10 ms scheduled delay:
time reset, periodic-trigger reset, then acquisition start.

### Software and storage

The ARM side runs Linux and the unstripped AArch64 `fers_bridge.elf`. The same
process exposes:

- port 9760: binary slow control;
- port 9000: binary acquisition streaming;
- port 8080: HTTP API plus the static web application.

The upgrade archive installs ARM software, web assets, `BOOT.BIN`, `image.ub`,
and an FPGA `bitstream.bin`. The device has active and fallback boot/root
partitions; the manual describes soft reset and factory fallback behavior.

## DT5215 control-server operations (TCP 9760)

The exact 2026.4.1.1 binary dispatches all 17 opcodes below. Multi-byte fields
are little-endian. Unless noted, status is a `u32`, with zero meaning success.

| Opcode | Request after opcode | Reply | Purpose | Evidence | Native Go |
|---|---|---|---|---|---|
| `WREG` | `u16 chain, u16 node, u32 address, u32 data` | status | Write a FERS-board register through TDlink | device/source/capture | yes |
| `RREG` | `u16 chain, u16 node, u32 address` | status, `u32 data` | Read a FERS-board register | device/source/capture | yes |
| `FCMD` | `u16 chain, u16 node, u32 command, u32 zero, u32 delay` | status | Submit/execute a FERS command | device/source/capture | yes |
| `DCMD` | same as `FCMD` | status | Arm a delayed/synchronous FERS command | device/source/capture | yes |
| `ACMD` | `u16 chain` | status | Abort the pending command on one TDlink chain | device-confirmed by disassembly and `fers_abort_command` | no |
| `RLNK` | none | status | Reset TDlink chains before re-enumeration | device/source/capture | yes |
| `ENUM` | `u16 chain` | status, `u32 node_count`, `u32 unknown` | Enumerate one TDlink ring | device/source/capture | yes |
| `CCNT` | `u16 chain, u16 enable, u32 token_interval` | status | Enable/disable that chain's readout train and set token interval | device/source/capture | yes |
| `SNT0` | none | status | Synchronize the enabled chains | device/source/capture | yes |
| `CLRS` | none | status | Clear/flush concentrator hardware stream buffers | device/source/capture | yes |
| `CINF` | `u16 chain` | fixed 40 bytes | Read link status, board count, RTT, event/byte counters and rates | device/source/capture | yes |
| `CWRG` | `u32 virtual_address, u32 data` | status | Write a DT5215 virtual register | device/source/capture | yes |
| `CRRG` | `u32 virtual_address` | status, `u32 data` | Read a DT5215 virtual register | device/source/capture | no |
| `VERS` | none | `u32 length` + 64-byte payload | Read ARM software revision, FPGA revision, and product ID | device/source/capture | yes |
| `RBIC` | none | `u32 length` + semicolon-delimited board-information string | Read model, PCB, link-count, and MAC metadata | device/source | no |
| `RBTF` | none | `0xADDEADDE`, then connection/process disappears | Wait two seconds, then execute `devmem 0xFF5E0218 32 0x10`, forcing a Zynq system restart | device-confirmed by disassembly | intentionally no |
| `RSTR` | none | `0xADDEADDE`, then connection/process disappears | Wait one second, then terminate/restart the bridge application through an internal runtime call with value `0xABBA` | device-confirmed sequence; final call semantics inferred | intentionally no |

Counting the dispatcher strings gives 17 distinct TCP operations:
`WREG RREG FCMD DCMD ACMD RLNK ENUM CCNT SNT0 CLRS CINF CWRG CRRG VERS RBIC
RBTF RSTR`.

Broadcast board addressing is chain `0x00ff`, node `0x00ff`. A command delay
unit is 10 ns. The capture-verified normal delay `1,000,000` is therefore 10 ms.

`ACMD`, `RBTF`, and `RSTR` exist in the device dispatcher but not in the bundled
FERSlib client version. Static AArch64 disassembly establishes their framing and
the sequences above without live probing. The exact internal runtime call used
at the end of `RSTR` remains unresolved. `RBTF` and `RSTR` must not be tested
against production hardware merely to refine that detail.

## FERS command values carried by `FCMD` / `DCMD`

These values are commands for a target DT5202/FERS node, not top-level DT5215
TCP opcodes.

| Value | Vendor name | Meaning | Native Go constant |
|---:|---|---|---|
| `0x11` | `CMD_TIME_RESET` | Reset absolute time | `CommandTimeReset` / `CommandResetTime` |
| `0x12` | `CMD_ACQ_START` | Start acquisition | `CommandAcquisitionStart` |
| `0x13` | `CMD_ACQ_STOP` | Stop acquisition | `CommandAcquisitionStop` |
| `0x14` | `CMD_TRG` | Software trigger | `CommandSoftwareTrigger` |
| `0x15` | `CMD_RESET` | Global reset; clear state and restore register defaults | `CommandGlobalReset` |
| `0x16` | `CMD_TEST_PULSE` | Generate a test pulse | `CommandTestPulse` |
| `0x17` | `CMD_RES_PTRG` | Reset/rearm periodic-trigger counter | `CommandResetPeriodicTrigger` / `CommandResetPeriodic` |
| `0x18` | `CMD_CLEAR` | Clear front-end data | `CommandClearData` |
| `0x19` | `CMD_VALIDATION` | Software trigger-validation signal | `CommandValidation` |
| `0x1a` | `CMD_SET_VETO` | Assert software veto | `CommandSetVeto` |
| `0x1b` | `CMD_CLEAR_VETO` | Clear software veto | `CommandClearVeto` |
| `0x1c` | `CMD_TDL_SYNC` | TDlink synchronization signal | `CommandTDLinkSync` / `CommandSync` |
| `0x1e` | `CMD_USE_ICLK` | Select internal FPGA clock; may reset board | `CommandUseInternalClock` |
| `0x1f` | `CMD_USE_ECLK` | Select external FPGA clock; may reset board | `CommandUseExternalClock` |
| `0x20` | `CMD_CFG_ASIC` | Load/configure selected Citiroc ASIC | `CommandConfigureASIC` |

The complete value set is source-confirmed by the vendor register header and is
already represented in `backend/internal/dt5202/registers.go`. The DT5215 Go
package repeats only the subset needed for concentrator-level acquisition
orchestration.

## DT5215 web API implemented by 2026.4.1.1

The binary contains these 60 explicitly registered `/api-v1.0` routes:

```text
DDMTD_clear_histograms       DDMTD_get_delay_source
DDMTD_get_histograms         DDMTD_set_delay_source
DDMTD_set_phase_delay        abort_restart_chains
alive                        chains_status
chains_status/reset          clear_log
device_info                  fers_read_register
fers_write_register          firmware_upgrade
get_board_sync               get_calibration_status
get_client_status            get_config
get_connectivity             get_debug_counters
get_device_leds              get_device_leds_new
get_enum_log                 get_enumeration_status
get_error_flags              get_gps_info
get_hardware_monitor         get_heatmap
get_hv_status                get_io_config
get_links                    get_log
get_netstats                 get_netstats_history
get_pll                      get_timesyncd
getconfiguration             login
logout                       pushconfiguration
quad_delay_compensation      reboot
reset_debug_counters         restart_app
restart_chains               rotate_link_phase
send_sw_sync                 set_board_sync
set_hostname                 set_io_config
set_password                 set_system_time
set_system_time_gps          set_timesyncd
start_calibration            switch_off_all_hv
switch_off_hv                update_chain_enable
update_config                update_eth_interfaces
```

These routes are **device-confirmed**, but their HTTP verbs, authentication
requirements, JSON schemas, and side effects are not fully documented here.
Handler symbols show that login/token authorization is implemented. The
firmware upgrade, reboot, restart, link-enable, raw register-write, time/network
configuration, and calibration routes are state-changing and must not be used
for DAQ discovery.

The executable also registers a static web-root/SPA fallback handler. That is a
file-serving route, not an additional device-command API. The 60 route literals
map one-for-one to 60 named API handler functions; a separate `spa_fallback`
handler accounts for the non-API web application route.

## Coverage in `pet-caen-daq`

The native implementation intentionally covers the DAQ-safe and operationally
required subset:

- both TCP connections and exact-read/full-write transport;
- `VERS`, `CINF`, `RLNK`, `ENUM`, `SNT0`, `CCNT`;
- board `RREG`/`WREG`;
- `FCMD` and `DCMD`;
- concentrator `CWRG`;
- `CLRS`;
- all DT5202 command values in the device model; and
- port-9000 descriptor, framing, event decoding, capture journaling, drain, and
  recovery behavior.

Not implemented by design are factory/restart commands, firmware upgrade,
unrestricted operator register/command endpoints, and automatic persistent
TDlink provisioning. The web interface remains the supported provisioning
surface for enabled links.

## Remaining unknowns

1. Resolve the final internal call in `RSTR`; its framing and one-second delayed
   bridge-process restart sequence are already established.
2. Determine how boot-slot/factory-button state affects the full-system reset
   initiated by `RBTF`; the command itself directly writes the Zynq reset
   register.
3. Document HTTP verbs and JSON schemas by statically following every registered
   handler. Do not learn destructive routes by probing the live device.
4. Resolve the third `ENUM` response word. FERSlib receives but does not name it.
5. Resolve the unused final eight bytes of the fixed 40-byte `CINF` reply.

## Comparison with the captured device root filesystem

The copied device root filesystem contains software release `2025.11.24.1`,
not the NIU's `2026.4.1.1`:

| Property | Captured device rootfs | Extracted NIU |
|---|---|---|
| System version | `2025.11.24.1` | `2026.4.1.1` |
| ELF size | 1,022,512 bytes | 1,313,192 bytes |
| SHA-256 | `418b26b8cb80735698658469a25540273068f5e08c1af5d34fb9467d6da17d16` | `bef2755b8d9faec11cadf27a57c25cd1ae49c9dea08c6d61ab3664df2289d7b0` |
| ELF build ID | `7b7ae10271580747203ed65157b3cd360acb7b86` | `2b23f43d87239c3df052beebd75ccbd4eea4deb8` |
| Embedded build date | 2025-11-24 | 2026-04-01 |
| TCP 9760 opcodes | 17 | 17 |
| Control-handler code size | 7,832 bytes (`0x1e98`) | 8,248 bytes (`0x2038`) |
| Stream-handler code size | 2,756 bytes (`0x0ac4`) | 8,020 bytes (`0x1f54`) |
| Explicit `/api-v1.0/...` routes | 25 | 60 |

The TCP opcode sets are identical:

```text
WREG RREG FCMD DCMD ACMD RLNK ENUM CCNT SNT0 CLRS CINF CWRG CRRG VERS RBIC
RBTF RSTR
```

Identical opcode names do not imply byte-identical implementations: both the
control handler and, especially, the port-9000 stream handler changed size.
Wire compatibility for the operations used by `pet-caen-daq` is separately
supported by FERSlib source and retained captures; unexercised internal behavior
may still differ between these releases.

All 25 explicit API routes from the captured device remain present in the NIU.
The NIU adds these 35:

```text
DDMTD_clear_histograms       DDMTD_get_delay_source
DDMTD_get_histograms         DDMTD_set_delay_source
DDMTD_set_phase_delay        abort_restart_chains
alive                        fers_read_register
fers_write_register          get_board_sync
get_calibration_status       get_client_status
get_connectivity             get_device_leds
get_device_leds_new          get_enumeration_status
get_error_flags              get_hardware_monitor
get_heatmap                  get_hv_status
get_links                    get_netstats
get_netstats_history         get_timesyncd
getconfiguration             login
logout                       pushconfiguration
quad_delay_compensation      rotate_link_phase
set_board_sync               set_timesyncd
start_calibration            switch_off_all_hv
switch_off_hv
```

The older web server also exposes `/login.html` and installs Digest
authentication at the `/api-v1.0` prefix through `AuthStartHandler` and
`HANDLER_CheckAuthStatus`; those are not additional device-command endpoints.
The NIU replaces that design with explicit `login`/`logout` handlers,
persisted bearer tokens, and an SPA fallback.
