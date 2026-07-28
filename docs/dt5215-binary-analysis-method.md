# Reproducing the DT5215 binary protocol analysis

Status: reproducible static-analysis procedure, 2026-07-28.

This document explains how the command, route, and chain-scan findings were
extracted from DT5215 `fers_bridge.elf` binaries without issuing undocumented
commands to live hardware.

## Inputs

The analysis used two copies of the device server:

```text
../firmware/device-172.16.0.11-rootfs.tar.gz
../firmware/dt5215-upgrade_2026.4.1.1-extracted/
```

The first is a captured root filesystem from the real device. The second is the
decrypted and unpacked NIU update.

Paths in the commands below are relative to the `pet-caen-daq` repository.
Temporary files are placed under `/tmp`.

## 1. Extract and identify the binaries

Extract the binary from the captured device root filesystem:

```bash
tar -xOf ../firmware/device-172.16.0.11-rootfs.tar.gz \
  caen/software/fers_bridge.elf \
  > /tmp/dt5215-host-fers_bridge.elf

chmod 600 /tmp/dt5215-host-fers_bridge.elf
```

The NIU binary is already available at:

```bash
NIU_ELF=../firmware/dt5215-upgrade_2026.4.1.1-extracted/caen/software/fers_bridge.elf
HOST_ELF=/tmp/dt5215-host-fers_bridge.elf
```

Confirm architecture, size, hashes, and ELF build IDs:

```bash
file "$HOST_ELF" "$NIU_ELF"
stat -c '%n %s bytes %y' "$HOST_ELF" "$NIU_ELF"
sha256sum "$HOST_ELF" "$NIU_ELF"
readelf -n "$HOST_ELF" | rg 'Build ID'
readelf -n "$NIU_ELF" | rg 'Build ID'
```

Both analyzed files are unstripped 64-bit little-endian AArch64 ELF
executables. Keeping the build ID and SHA-256 with every result prevents facts
from one firmware release being silently attributed to another.

Read the system-version files independently of embedded compilation dates:

```bash
tar -xOf ../firmware/device-172.16.0.11-rootfs.tar.gz caen/version
sed -n '1,20p' \
  ../firmware/dt5215-upgrade_2026.4.1.1-extracted/caen/version
```

An embedded date is useful corroboration but is not a substitute for
`/caen/version`.

## 2. Inventory candidate TCP opcodes

The port-9760 protocol uses four printable ASCII bytes as its operation tag.
Extract likely tags and their neighboring diagnostics:

```bash
strings -a -t x -n 4 "$NIU_ELF" |
  rg 'WREG|RREG|FCMD|DCMD|ACMD|RLNK|ENUM|CCNT|SNT0|CLRS|CINF|CWRG|CRRG|VERS|RBIC|RBTF|RSTR'
```

For a normalized set:

```bash
OPCODE_PATTERN='^(WREG|RREG|FCMD|DCMD|ACMD|RLNK|ENUM|CCNT|SNT0|CLRS|CINF|CWRG|CRRG|VERS|RBIC|RBTF|RSTR)$'

strings -a "$HOST_ELF" | rg "$OPCODE_PATTERN" | sort -u \
  > /tmp/dt5215-host-opcodes.txt
strings -a "$NIU_ELF" | rg "$OPCODE_PATTERN" | sort -u \
  > /tmp/dt5215-niu-opcodes.txt

wc -l /tmp/dt5215-host-opcodes.txt /tmp/dt5215-niu-opcodes.txt
diff -u /tmp/dt5215-host-opcodes.txt /tmp/dt5215-niu-opcodes.txt
```

This finds candidate names but does not by itself prove that every string is a
reachable command. Exhaustiveness requires following the dispatcher.

## 3. Locate the control dispatcher

Because the binaries are unstripped, locate the server functions directly:

```bash
nm -S -C "$NIU_ELF" |
  rg 'servercontrol::(connection_handler|start_server)'
```

For NIU release `2026.4.1.1`, the relevant symbol is:

```text
servercontrol::connection_handler(void*)
```

The function reads four bytes, NUL-terminates them, compares them sequentially
with all 17 opcode strings, and branches to one handler block for each match.
An unrecognized value returns to the receive loop. Following that complete
comparison chain is what establishes that the list is exhaustive for port
9760—not merely the fact that 17 strings exist.

## 4. Disassemble AArch64 when the system objdump cannot

Some hosts have an `objdump` that recognizes the ELF container but lacks an
AArch64 disassembler. Python Capstone provides a small architecture-independent
fallback.

Install Capstone through the normal development environment if it is not
already present, then disassemble the symbol range. The analyzed ET_EXEC files
map file offset `virtual_address - 0x400000`:

```python
from capstone import Cs, CS_ARCH_ARM64, CS_MODE_LITTLE_ENDIAN

path = "../firmware/dt5215-upgrade_2026.4.1.1-extracted/caen/software/fers_bridge.elf"
start = 0x4606B8
size = 0x2038

with open(path, "rb") as stream:
    stream.seek(start - 0x400000)
    code = stream.read(size)

decoder = Cs(CS_ARCH_ARM64, CS_MODE_LITTLE_ENDIAN)
for instruction in decoder.disasm(code, start):
    print(
        f"{instruction.address:08x}: "
        f"{instruction.mnemonic:8} {instruction.op_str}"
    )
```

Do not copy the example addresses to another build. Obtain each build's address
and size from `nm -S -C` first. The `0x400000` relationship should also be
verified from ELF program/section headers:

```bash
readelf -l "$NIU_ELF"
readelf -S "$NIU_ELF"
```

### Recovering request and response sizes

Within each dispatcher branch:

1. Identify the receive call immediately after the opcode match.
2. Record the requested byte count.
3. Follow loads from the receive buffer to determine field widths and offsets.
4. Resolve the called `concentrator::*` symbol.
5. Follow the send call and record its byte count.

For example, the `ACMD` branch:

- receives two bytes;
- loads them as one `u16`;
- calls `concentrator::fers_abort_command(unsigned int)`;
- stores the result; and
- sends four bytes.

This establishes:

```text
ACMD + u16 chain -> u32 status
```

The same approach confirms `RBTF` and `RSTR`: both send the sentinel
`0xADDEADDE` before their delayed restart behavior. `RBTF` then executes:

```text
devmem 0xFF5E0218 32 0x10
```

Static recovery is preferable to trying either operation on a production
device.

## 5. Map call targets back to symbols

Create a symbol table from `nm -S -C`. For every AArch64 `bl #address`
instruction, look up an exact address match. This turns an assembly call graph
into readable evidence such as:

```text
webserver::HANDLER_restart_chains
  -> concentrator::reset_chains
  -> concentrator::initialize_chain
  -> concentrator::syncronize_chains
  -> concentrator::read_boards_info
  -> concentrator::reset_debug_counters
```

A minimal call-target resolver:

```python
import re
import subprocess

from capstone import Cs, CS_ARCH_ARM64, CS_MODE_LITTLE_ENDIAN

path = "../firmware/dt5215-upgrade_2026.4.1.1-extracted/caen/software/fers_bridge.elf"
symbols = {}

output = subprocess.check_output(
    ["nm", "-S", "-C", path],
    text=True,
    errors="replace",
)
for line in output.splitlines():
    match = re.match(r"([0-9a-f]+) ([0-9a-f]+) [TtWw] (.*)", line)
    if match:
        symbols[int(match.group(1), 16)] = (
            int(match.group(2), 16),
            match.group(3),
        )

name = "webserver::HANDLER_restart_chains(mg_connection*, void*)"
start, size = next(
    (address, entry[0])
    for address, entry in symbols.items()
    if entry[1] == name
)

with open(path, "rb") as stream:
    stream.seek(start - 0x400000)
    code = stream.read(size)

decoder = Cs(CS_ARCH_ARM64, CS_MODE_LITTLE_ENDIAN)
for instruction in decoder.disasm(code, start):
    if instruction.mnemonic != "bl" or not instruction.op_str.startswith("#0x"):
        continue
    target = int(instruction.op_str[1:], 16)
    target_name = symbols.get(target, (0, "<external or PLT>"))[1]
    print(f"{instruction.address:08x} -> {target:08x} {target_name}")
```

Calls through the PLT or function pointers will not always have an exact
symbol-address match. Classify those as unresolved unless the relocation table
or surrounding setup proves the target.

## 6. Inventory HTTP routes

Extract explicit `/api-v1.0` route literals:

```bash
strings -a "$NIU_ELF" |
  rg '^/api-v1\.0/[A-Za-z0-9_/]+$' |
  sort -u \
  > /tmp/dt5215-niu-routes.txt

strings -a "$HOST_ELF" |
  rg '^/api-v1\.0/[A-Za-z0-9_/]+$' |
  sort -u \
  > /tmp/dt5215-host-routes.txt

wc -l /tmp/dt5215-host-routes.txt /tmp/dt5215-niu-routes.txt
diff -u /tmp/dt5215-host-routes.txt /tmp/dt5215-niu-routes.txt
```

Cross-check literals against named handler functions:

```bash
nm -C "$NIU_ELF" |
  rg 'webserver::HANDLER_' |
  sed 's/.*webserver::HANDLER_//' |
  sed 's/(.*//' |
  sort -u
```

For the NIU, 60 explicit route literals map to 60 API handlers. A separate
`spa_fallback` symbol accounts for static webapp routing and must not be counted
as another device-command API.

Literal extraction can miss dynamically constructed routes. Inspecting
`webserver::InitServer()` and confirming its request-handler registrations is
the stronger check.

## 7. Correlate server handlers with the webapp

Extract the real-device JavaScript without unpacking the complete rootfs:

```bash
tar -xOf ../firmware/device-172.16.0.11-rootfs.tar.gz \
  caen/www/home.js \
  > /tmp/dt5215-host-home.js

rg -n -i \
  'restart_chains|get_enum_log|chains_status|update_chain_enable' \
  /tmp/dt5215-host-home.js
```

Search the minified NIU application:

```bash
rg -n -o \
  '.{0,300}(restart_chains|get_enum_log|get_enumeration_status|abort_restart_chains).{0,800}' \
  ../firmware/dt5215-upgrade_2026.4.1.1-extracted/caen/www/assets/index-3aJOtuC_.js
```

This establishes browser-visible HTTP methods, request bodies, response fields,
polling intervals, authentication headers, and how status values are consumed.
The server call graph establishes what those HTTP requests actually do.

For example:

- frontend: `GET /restart_chains`;
- server handler: reset, initialize every enabled chain, synchronize;
- TCP dispatcher equivalence: `RLNK`, `ENUM` per enabled chain, `SNT0`.

No single evidence layer is sufficient on its own.

## 8. Recover endpoint schemas

Use three sources together:

1. frontend property access, such as `response.status` and
   `response.attempt`;
2. JSON key/status strings in the binary; and
3. the handler's JSON construction and HTTP response calls.

Useful string search:

```bash
strings -a -t x "$NIU_ELF" |
  rg -i \
  'get_enumeration_status|abort_restart_chains|enumerating|aborting|idle|attempt|no enumeration'
```

This recovered:

```json
{"status":"enumerating","attempt":1}
```

and the device-confirmed states `idle`, `enumerating`, and `aborting`.

The abort handler's response construction shows HTTP 200 with an empty body
both for “abort scheduled” and “no enumeration in progress.” Its call graph
contains no `fers_abort_command`, proving that the HTTP endpoint sets a
cooperative software flag rather than sending TCP opcode `ACMD`.

## 9. Compare with the Go implementation

Locate every discovery operation:

```bash
rg -n \
  'DiscoverEnabledTopologyWithObserver|enumerateChainsWithRetry|recoverTDL|ConcentratorInfo|ChainInfo|ReadRegister' \
  backend/internal/dt5215/client.go
```

Read the encoder beside each call in:

```text
backend/internal/dt5215/protocol.go
```

Tests provide byte-exact confirmation:

```text
backend/internal/dt5215/protocol_test.go
backend/internal/dt5215/client_test.go
backend/integration/discovery_test.go
```

The resulting comparison should distinguish:

- commands essential to the web-equivalent scan:
  `RLNK`, `ENUM`, `SNT0`;
- read-only identity/topology operations:
  `VERS`, `CINF`, `RREG`; and
- capture-derived recovery:
  `CWRG`, `CCNT`, and delayed broadcast `FCMD`.

## 10. Run the real ARM executable under QEMU

The 2026 bridge can be executed with QEMU user-mode. This is not a complete
DT5215 emulator: QEMU executes the genuine AArch64 Linux process, while sparse
files and a small `ioctl` interposer stand in for the FPGA MMIO, DMA, UIO, and
I2C devices. It is useful for exercising process initialization and handlers
whose result does not depend on realistic FPGA state.

Run this only on an analysis workstation. All writable state is placed in a
temporary root filesystem; never point the commands at a mounted production
device filesystem.

### 10.1 Prerequisites

The host needs:

- `qemu-aarch64-static`;
- `bwrap` (Bubblewrap);
- `docker`, used below only to build a temporary AArch64 shared object; and
- the copied rootfs and extracted NIU under the sibling `firmware` directory.

On Debian-derived systems, the first two packages are normally
`qemu-user-static` and `bubblewrap`.

Run the following commands from the `pet-caen-daq` repository root.

### 10.2 Prepare an isolated ARM root filesystem

```bash
EMU_ROOT=$(mktemp -d /tmp/dt5215-qemu-rootfs.XXXXXX)

tar -xzf ../firmware/device-172.16.0.11-rootfs.tar.gz \
  -C "$EMU_ROOT"

cp -a \
  ../firmware/dt5215-upgrade_2026.4.1.1-extracted/caen/. \
  "$EMU_ROOT/caen/"

cp /usr/bin/qemu-aarch64-static \
  "$EMU_ROOT/usr/bin/qemu-aarch64-static"
```

The copied rootfs supplies the AArch64 dynamic loader and libraries. Copying
the extracted `caen` directory over it selects the 2026.4.1.1 application,
configuration, and web assets. The original archives are never modified.

### 10.3 Create synthetic MMIO, DMA, and UIO resources

The bridge maps physical addresses as file offsets in `/dev/mem`. A sparse file
therefore provides the required address space without consuming 4.3 GB of
physical storage:

```bash
truncate -s 4300000000 /tmp/dt5215-devmem.bin

# FPGA firmware magic at physical 0x81000000 + 15 * 4.
printf '\x22\x22\xba\xab' |
  dd of=/tmp/dt5215-devmem.bin \
    bs=1 seek=2164260924 conv=notrunc status=none

truncate -s 1048576 /tmp/dt5215-udmabuf0.bin
truncate -s 16384   /tmp/dt5215-udmabuf1.bin
truncate -s 4096    /tmp/dt5215-uio4.bin
truncate -s 4096    /tmp/dt5215-i2c.bin
```

Create the sysfs metadata read by the application's DMA wrappers:

```bash
mkdir -p \
  /tmp/dt5215-udma-sys/udmabuf0 \
  /tmp/dt5215-udma-sys/udmabuf1 \
  /tmp/dt5215-uio-sys/uio4/maps/map0

printf '%s\n' 1048576   > /tmp/dt5215-udma-sys/udmabuf0/size
printf '%s\n' 0x10000000 > /tmp/dt5215-udma-sys/udmabuf0/phys_addr
printf '%s\n' 0          > /tmp/dt5215-udma-sys/udmabuf0/sync_for_device
printf '%s\n' 0          > /tmp/dt5215-udma-sys/udmabuf0/sync_for_cpu

printf '%s\n' 16384      > /tmp/dt5215-udma-sys/udmabuf1/size
printf '%s\n' 0x10100000 > /tmp/dt5215-udma-sys/udmabuf1/phys_addr
printf '%s\n' 0          > /tmp/dt5215-udma-sys/udmabuf1/sync_for_device
printf '%s\n' 0          > /tmp/dt5215-udma-sys/udmabuf1/sync_for_cpu

printf '%s\n' 0x1000     > /tmp/dt5215-uio-sys/uio4/maps/map0/size
printf '%s\n' 0x80050000 > /tmp/dt5215-uio-sys/uio4/maps/map0/addr
printf '%s\n' 0x0        > /tmp/dt5215-uio-sys/uio4/maps/map0/offset
```

The firmware magic is directly established by the `io_driver` constructor:
it maps `0x81000000`, reads word 15, and compares it with `0xABBA2222`.

### 10.4 Build the temporary I2C `ioctl` interposer

The application expects hardware-specific I2C ioctls. For handler analysis,
an interposer can make those initialization calls return success:

```bash
cat >/tmp/dt5215-ioctl-shim.c <<'EOF'
#include <sys/ioctl.h>

int ioctl(int fd, unsigned long request, ...)
{
    (void)fd;
    (void)request;
    return 0;
}
EOF

docker run --rm \
  -v /tmp:/work \
  debian:bookworm-slim \
  sh -lc '
    apt-get update >/dev/null &&
    apt-get install -y --no-install-recommends \
      gcc-aarch64-linux-gnu libc6-dev-arm64-cross >/dev/null &&
    aarch64-linux-gnu-gcc -shared -fPIC -O2 \
      -o /work/dt5215-ioctl-shim.so \
      /work/dt5215-ioctl-shim.c
  '

cp /tmp/dt5215-ioctl-shim.so \
  "$EMU_ROOT/caen/software/libdt5215-ioctl-shim.so"
```

This shim intentionally does not model I2C transactions. Any value derived
from an I2C device is synthetic and must not be classified as hardware
evidence.

### 10.5 Launch the bridge

Run this in a dedicated terminal:

```bash
timeout 300s bwrap \
  --bind "$EMU_ROOT" / \
  --dev /dev \
  --bind /tmp/dt5215-devmem.bin /dev/mem \
  --bind /tmp/dt5215-udmabuf0.bin /dev/udmabuf0 \
  --bind /tmp/dt5215-udmabuf1.bin /dev/udmabuf1 \
  --bind /tmp/dt5215-uio4.bin /dev/uio4 \
  --bind /tmp/dt5215-i2c.bin /dev/i2c-0 \
  --bind /tmp/dt5215-i2c.bin /dev/i2c-2 \
  --bind /tmp/dt5215-i2c.bin /dev/i2c-3 \
  --bind /tmp/dt5215-i2c.bin /dev/i2c-4 \
  --proc /proc \
  --bind /tmp/dt5215-udma-sys /sys/class/u-dma-buf \
  --bind /tmp/dt5215-uio-sys /sys/class/uio \
  --setenv LD_PRELOAD /caen/software/libdt5215-ioctl-shim.so \
  -- /usr/bin/qemu-aarch64-static \
     /caen/software/fers_bridge.elf
```

Successful startup includes:

```text
Application Started
FW_VERSION: 2026.4.1.1
Starting data and control server...
```

If `Socket creation failed` appears, the execution environment is blocking
local socket creation or one of ports 9000, 9760, and 8080 is already occupied.

Add `--setenv QEMU_STRACE 1` before `--` to trace ARM Linux system calls.

### 10.6 Send read-only protocol probes

Use a second terminal. This example sends only `VERS`, `CINF`, and `ENUM`;
it does not send register writes, commands, resets, or acquisition control:

```bash
python3 - <<'PY'
import socket

probes = (
    (b"VERS", 68),
    (b"CINF\x00\x00", 40),
    (b"ENUM\x06\x00", 12),  # disabled chain in the supplied configuration
)

for request, expected in probes:
    with socket.create_connection(("127.0.0.1", 9760), timeout=2) as sock:
        sock.sendall(request)
        reply = bytearray()
        while len(reply) < expected:
            chunk = sock.recv(expected - len(reply))
            if not chunk:
                break
            reply.extend(chunk)
    print(request[:4].decode(), len(reply), bytes(reply).hex())
PY
```

Observed read-only results from the 2026 executable included:

```text
VERS 68 ...
CINF 40 010000000000000000000000000000000000000000000000000000000000000000803b4500000000
ENUM 12 190000000000000000000000
```

The CINF capacity field `00 80 3b 45` is little-endian float32 `3000.0`.
ENUM status `0x19` is decimal 25, `StatusChainDisabled`.

Do not invoke `RBTF`, `RSTR`, firmware upgrade, raw writes, or other destructive
handlers in this environment. The shims do not make those operations
meaningful, and a command may escape into host-visible process or network
behavior even though MMIO is synthetic.

### 10.7 Evidence limits

This setup proves that code paths in the genuine ARM executable are reachable
and can confirm framing, parsing, fixed metadata, and software-only behavior.
It does not reproduce FPGA state transitions, TDlink traffic, interrupt timing,
DMA semantics, boot-slot selection, physical I2C devices, or real board
responses.

Classify results accordingly:

- handler framing directly observed from the real executable: `inferred`,
  strengthened by static data flow;
- bytes also present in retained physical captures: `capture-verified`;
- values produced only by sparse MMIO or the ioctl shim: synthetic emulator
  output, not protocol evidence.

Stop the bridge with Ctrl-C or let `timeout` terminate it. The temporary root
can be discarded after recording the executable hash and results.

## 11. Validation checklist

Before publishing results:

- record SHA-256, build ID, size, and `/caen/version`;
- keep findings separated by firmware release;
- prove dispatcher reachability, not just string presence;
- cross-check every HTTP literal against a registered handler;
- cross-check frontend methods and consumed response fields;
- classify unresolved indirect calls as unknown;
- compare recovered layouts with FERSlib source and existing captures;
- never infer that identical opcode names imply identical implementations;
- do not test reset, factory fallback, firmware update, raw writes, or
  undocumented commands on production hardware merely to confirm a guess; and
- run `git diff --check` after documenting results.

## Limitations

This method establishes the command surfaces of the analyzed
`fers_bridge.elf` builds. It does not enumerate:

- commands in other firmware releases;
- every possible FPGA or FERS register value;
- unrelated Linux services available over SSH;
- dynamically loaded code absent from the executable;
- unreachable diagnostic code merely retained in the binary; or
- semantics hidden behind an unresolved indirect call.

Those boundaries should remain explicit in downstream documentation.
