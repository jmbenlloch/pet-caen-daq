# DT5215 webapp chain-scan protocol compared with native Go discovery

Status: static analysis of captured device release `2025.11.24.1`, NIU release
`2026.4.1.1`, and the current native Go implementation, 2026-07-28.

## Executive result

The DT5215 webapp does not open TCP port 9760. It calls the HTTP API in the
already-running `fers_bridge.elf`. The HTTP handler then invokes the same
internal concentrator methods used by the TCP control dispatcher.

At the port-9760 abstraction boundary, the web scan is equivalent to:

```text
RLNK
ENUM <each persistently enabled chain, in ascending/configured order>
SNT0
```

The native Go discovery contains that same reset/enumerate/synchronize core,
but adds identity reads, pre-scan link-state inspection, retry framing,
capture-derived TDlink recovery, post-scan validation, and three identity/status
register reads for every discovered node.

## Evidence

- Captured rootfs webapp: `/caen/www/home.js`, system `2025.11.24.1`.
- Captured rootfs server:
  `/caen/software/fers_bridge.elf`, build ID
  `7b7ae10271580747203ed65157b3cd360acb7b86`.
- NIU webapp:
  `firmware/dt5215-upgrade_2026.4.1.1-extracted/caen/www/assets/index-3aJOtuC_.js`.
- NIU server:
  `firmware/dt5215-upgrade_2026.4.1.1-extracted/caen/software/fers_bridge.elf`,
  build ID `2b23f43d87239c3df052beebd75ccbd4eea4deb8`.
- Native implementation:
  `backend/internal/dt5215/client.go` and `protocol.go`.

The two server binaries are unstripped. Their handler call graphs establish
the internal scan sequence without issuing a scan against live hardware.

## Captured-device webapp (`2025.11.24.1`)

### Browser requests

The Scan button calls `RescanChains()` in `home.js`:

1. Open the scan/log modal.
2. Start polling `GET /api-v1.0/get_enum_log` every 333 ms.
3. Send `GET /api-v1.0/restart_chains`.
4. Wait for that request to complete.
5. Hide the scan modal two seconds after success.

Independently, the home page polls:

```http
GET /api-v1.0/device_info
GET /api-v1.0/chains_status
```

once per second. `chains_status` supplies each link's state, enabled flag,
board count, round-trip time, counters, rates, and buffer usage.

Changing the Enable checkboxes is a separate operation:

```http
POST /api-v1.0/update_chain_enable
Content-Type: application/json

[
  {"name":"chain0_enable","value":true},
  ...
]
```

That persists which links the next scan will use. It is not part of the Scan
button's `restart_chains` request.

### Server-side scan

Static call-graph recovery of `HANDLER_restart_chains` gives:

```text
concentrator::reset_chains()
for each enabled chain:
    concentrator::initialize_chain(chain, &node_count, &word2)
concentrator::syncronize_chains()
return JSON result
```

`initialize_chain` performs enumeration, timing/delay setup, and board-info
collection for that link. The TCP `ENUM` dispatcher calls this same method and
then refreshes the per-chain board information and debug counters.

Therefore the closest externally reproducible port-9760 sequence is:

| Order | Request bytes | Reply | Meaning |
|---:|---|---|---|
| 1 | ASCII `RLNK` | `u32 status` | Reset all enabled TDlink state |
| 2...N | ASCII `ENUM`, `u16 chain` | `u32 status, u32 node_count, u32 word2` | Initialize/enumerate each enabled chain |
| N+1 | ASCII `SNT0` | `u32 status` | Synchronize all initialized chains |

Example for enabled chains 0--3:

```text
52 4c 4e 4b                         RLNK
45 4e 55 4d 00 00                   ENUM chain 0
45 4e 55 4d 01 00                   ENUM chain 1
45 4e 55 4d 02 00                   ENUM chain 2
45 4e 55 4d 03 00                   ENUM chain 3
53 4e 54 30                         SNT0
```

The browser never sees or emits those bytes; it remains inside HTTP while the
device process invokes the corresponding internal methods.

## NIU webapp (`2026.4.1.1`)

The newer React webapp keeps the same scan trigger:

```http
GET /api-v1.0/restart_chains
```

It adds:

```http
GET  /api-v1.0/get_enumeration_status
POST /api-v1.0/abort_restart_chains
GET  /api-v1.0/get_enum_log
```

The status endpoint is polled every second while the scan modal is open. The
returned fields used by the UI are `status` and `attempt`; status includes at
least `idle` and `enumerating`. Abort is cooperative: the binary states that it
takes effect after the current attempt completes.

The NIU `HANDLER_restart_chains` call graph is:

```text
concentrator::reset_chains()
for each enabled chain:
    concentrator::initialize_chain(chain, &node_count, &word2)
on failure, retry the scan attempt
concentrator::syncronize_chains()
concentrator::read_boards_info()
concentrator::reset_debug_counters()
```

Disassembly shows the outer attempt counter compared with `2`, consistent with
up to three attempts. `initialize_chain` also contains its own lower-level
enumeration retry behavior. Abort state is checked between attempts.

The core TDlink protocol is therefore unchanged; the NIU adds asynchronous
progress, cooperative abort, outer retries, a complete board-info refresh, and
debug-counter reset.

### Enumeration companion endpoint contract

These endpoints exist in NIU release `2026.4.1.1`. Of the three, only abort
changes server state, and even abort does not directly issue a hardware
command. None starts a scan; that still requires `GET /restart_chains`.

All three require the NIU bearer token:

```http
Authorization: Bearer <token>
```

The token is obtained with:

```http
POST /api-v1.0/login
Content-Type: application/json

{"username":"<user>","password":"<password>"}
```

The successful login response contains:

```json
{"token":"..."}
```

#### `GET /api-v1.0/get_enum_log`

Purpose:

- retrieve the accumulated server-side enumeration log;
- show per-link progress and retry/failure diagnostics;
- show the final success, permanent failure, or user-abort message.

Request body: none.

The frontend consumes this JSON shape:

```json
{
  "log": "line 1\nline 2\n..."
}
```

The UI recognizes the literal markers `{{SUCCESS}}` and `{{FAILED}}` in log
lines and converts them into green/red indicators. The log is a snapshot, not
a streaming response: poll again to obtain newer content. The NIU UI polls it
once per second while the scan dialog is open and every five minutes in the
background.

This endpoint is read-only. It sends no `RLNK`, `ENUM`, `SNT0`, `ACMD`, or
other TDlink command.

Example:

```bash
curl --fail --silent \
  -H "Authorization: Bearer $DT5215_TOKEN" \
  http://172.16.0.11/api-v1.0/get_enum_log
```

The older captured-device release `2025.11.24.1` also implements
`get_enum_log`, but uses HTTP Digest authentication and its legacy UI polls the
endpoint every 333 ms.

#### `GET /api-v1.0/get_enumeration_status`

Purpose:

- determine whether enumeration is idle, running, or stopping;
- display the current outer retry-attempt number;
- decide whether continuing to poll the log is useful.

Request body: none.

The frontend relies on these response fields:

```json
{
  "status": "enumerating",
  "attempt": 1
}
```

Device-confirmed status strings are:

- `idle`: no scan is executing;
- `enumerating`: a scan attempt is executing;
- `aborting`: abort has been requested and will take effect after the current
  attempt.

`attempt` is the current outer scan-attempt number. The NIU scan permits up to
three outer attempts; lower-level `initialize_chain` can perform additional
transient retries within one attempt.

This endpoint is read-only and sends no hardware command.

Example:

```bash
curl --fail --silent \
  -H "Authorization: Bearer $DT5215_TOKEN" \
  http://172.16.0.11/api-v1.0/get_enumeration_status
```

This endpoint is absent from captured-device release `2025.11.24.1`.

#### `POST /api-v1.0/abort_restart_chains`

Purpose:

- request cooperative cancellation of a scan started by
  `GET /restart_chains`.

Request body:

```json
{}
```

The handler returns HTTP 200 with an empty body both when it schedules an abort
and when no enumeration is active. Server log messages distinguish:

```text
abort scheduled, will take effect after current attempt completes
no enumeration in progress
```

The operation sets an internal abort-request flag. It does **not** send the
port-9760 `ACMD` operation, reset a link, terminate an in-flight
`initialize_chain`, or roll back work already completed. The scan checks the
flag after the current attempt and stops before starting another. While waiting,
`get_enumeration_status` reports `aborting`.

Example:

```bash
curl --fail --silent \
  -X POST \
  -H "Authorization: Bearer $DT5215_TOKEN" \
  -H "Content-Type: application/json" \
  --data '{}' \
  http://172.16.0.11/api-v1.0/abort_restart_chains
```

This endpoint is absent from captured-device release `2025.11.24.1`.

### Commands required for a complete API-driven scan

The minimum NIU HTTP workflow is:

```text
POST /login                       obtain bearer token, unless already logged in
GET  /restart_chains              start and execute the scan
GET  /get_enumeration_status      optional polling
GET  /get_enum_log                optional polling/diagnostics
POST /abort_restart_chains        optional cooperative cancellation
GET  /chains_status               retrieve final per-link counts and state
```

The `restart_chains` request can remain outstanding while another HTTP
connection polls status/log or requests abort; the embedded HTTP server handles
connections concurrently.

Example shell workflow:

```bash
# In one terminal or background job:
curl --fail --silent \
  -H "Authorization: Bearer $DT5215_TOKEN" \
  http://172.16.0.11/api-v1.0/restart_chains

# From another connection while it runs:
curl --fail --silent \
  -H "Authorization: Bearer $DT5215_TOKEN" \
  http://172.16.0.11/api-v1.0/get_enumeration_status

curl --fail --silent \
  -H "Authorization: Bearer $DT5215_TOKEN" \
  http://172.16.0.11/api-v1.0/get_enum_log
```

Internally, only `restart_chains` performs the hardware workflow:

```text
reset_chains()
initialize_chain() for each persistently enabled link
syncronize_chains()
read_boards_info()
reset_debug_counters()
```

At the external port-9760 protocol boundary, the essential equivalent remains:

```text
RLNK
ENUM <each enabled chain>
SNT0
```

The three companion endpoints add no port-9760 commands.

## Native Go enabled-topology discovery

`DiscoverEnabledTopologyWithObserver` emits this exact high-level sequence:

```text
VERS
CINF chain 0
CINF chain 1
...
CINF chain 7

attempt 1..5:
    RLNK
    ENUM each link whose initial CINF status was nonzero
    if an ENUM returns a complete nonzero-status reply:
        restart the whole attempt

SNT0

for each enabled link:
    CWRG VR_SYNC_DELAY, (chain << 24) | 0x00000016
    CWRG VR_SYNC_DELAY, (chain << 24) | 0x00010000
for each enabled link:
    CCNT chain, disable, token_interval=0

FCMD broadcast CMD_TDL_SYNC, delay=1,000,000
FCMD broadcast CMD_TIME_RESET, delay=1,000,000
FCMD broadcast CMD_RES_PTRG, delay=1,000,000

for each enabled link:
    CCNT chain, enable, token_interval=256

CINF each enabled link

for every node 0..board_count-1:
    RREG 0x01000400    product ID
    RREG 0x01000300    FPGA firmware revision
    RREG 0x01000304    acquisition/TDlink status
```

The three broadcast targets use chain `0x00ff`, node `0x00ff`. A delay unit is
10 ns, so `1,000,000` schedules execution 10 ms later.

### Byte layouts used by Go

| Operation | Request |
|---|---|
| `VERS` | `56 45 52 53` |
| `CINF c` | `43 49 4e 46`, `u16 c` |
| `RLNK` | `52 4c 4e 4b` |
| `ENUM c` | `45 4e 55 4d`, `u16 c` |
| `SNT0` | `53 4e 54 30` |
| `CWRG a,v` | `43 57 52 47`, `u32 a`, `u32 v` |
| `CCNT c,e,t` | `43 43 4e 54`, `u16 c`, `u16 e`, `u32 t` |
| `FCMD c,n,cmd,d` | `46 43 4d 44`, `u16 c`, `u16 n`, `u32 cmd`, `u32 zero`, `u32 d` |
| `RREG c,n,a` | `52 52 45 47`, `u16 c`, `u16 n`, `u32 a` |

All integers are little-endian.

## Direct comparison

| Behavior | Device web scan | Native Go discovery |
|---|---|---|
| Read concentrator identity | UI does separately through `device_info`; not part of scan handler | `VERS` before scan |
| Determine enabled links | Device reads its persistent enabled-link configuration internally | `CINF` all 8; nonzero status means enabled |
| Change persistent enabled links | Separate web `update_chain_enable` action | Never |
| Reset links | Internal `reset_chains`, equivalent to `RLNK` | `RLNK` |
| Enumerate | Internal `initialize_chain` per enabled link, same method behind `ENUM` | `ENUM` per enabled link |
| Retry entire enumeration | Old webapp: no handler-level retry established; NIU: up to 3 attempts | Up to 5 attempts on complete nonzero-status `ENUM` replies |
| Synchronize after enumeration | Internal `syncronize_chains`, equivalent to `SNT0` | `SNT0` |
| Program sync-delay recovery values | May occur inside device initialization; not separately visible to browser | Explicit `CWRG` values from captured JANUS sequence |
| Stop/restart readout trains | Internal implementation detail | Explicit `CCNT` disable/enable |
| Broadcast sync/time/periodic reset | Not a separate browser-visible step | Three explicit delayed broadcast `FCMD`s |
| Refresh board metadata | Device internal state; NIU explicitly calls `read_boards_info` | Three `RREG`s per node |
| Validate enumerated vs reported count | Display-oriented device result | Required equality; otherwise error |
| Identify product/firmware/status per node | Returned indirectly by web status/info APIs | Returned in typed `Topology.Boards` |
| Progress/abort | Old: log only; NIU: status, attempt, cooperative abort | Observer/context cancellation |
| Debug-counter reset | NIU does it after scan | No |

## Conclusion

The Go implementation is protocol-compatible with the web scan at its core:

```text
RLNK -> ENUM(enabled links) -> SNT0
```

It is intentionally not a byte-for-byte copy of the browser workflow. Go uses
the public port-9760 protocol rather than the private HTTP API and adds the
recovery operations required by retained real-hardware captures before direct
board register access. This is why Go sends more commands than the web scan.

The main behavioral discrepancy is retry policy: the NIU web scan makes up to
three outer attempts, while Go makes up to five. The older captured-device
handler directly performs one outer scan invocation, although lower-level
`initialize_chain` itself retries transient enumeration conditions.
