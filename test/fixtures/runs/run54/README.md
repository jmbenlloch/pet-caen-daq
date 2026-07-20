# Production Run 54 fixture

This directory contains a byte-preserved run-information file and a record-aligned prefix of its JANUS processed binary list output.

## Provenance

- Evidence classification: production-generated, Windows hardware/software output.
- Run: 54.
- Acquisition: 2026-07-17 11:06:37 to 11:06:52 as recorded by JANUS; elapsed time 13.697 s.
- JANUS version: 4.3.0.
- JANUS output format: 3.4.
- Board family in binary header: 5202.
- Acquisition mode: spectroscopy plus timing (`0x03`).
- Output time unit: ns; ToA LSB recorded in the header as 0.5 ns.
- Energy histogram bins: 4096.
- Topology: one DT5215 and four A5202 boards.

Hardware identities from `Run54_Info.txt`:

| Device | PID | Firmware |
|---|---:|---|
| DT5215 concentrator 0 | 66643 | FPGA `25.11.24.01-2-2`; software `2025.11.24.1` |
| A5202 board 0 | 64883 | FPGA `7.8`, build `A123`; uC reported N/A |
| A5202 board 1 | 64138 | FPGA `7.8`, build `A123`; uC reported N/A |
| A5202 board 2 | 64885 | FPGA `7.8`, build `A123`; uC reported N/A |
| A5202 board 3 | 64884 | FPGA `7.8`, build `A123`; uC reported N/A |

The run-info file embeds the configuration used for the run. Its effective parameter content matches the separately supplied production configuration, including four boards on chains 0 through 3, node 0, via `usb:172.16.0.11`.

## Files and hashes

### Committed files

- `Run54_Info.txt`: original 15,987-byte CRLF run log. SHA-256 `1c340d599e7e645e731554b092506926e55457f6979296fd4272c7026cf5a781`.
- `Run54.first256_list.dat`: first 113,825 bytes of the original binary, ending exactly after event 256. SHA-256 `0b3c5f7e49127693006ce206ffe9d74404cba7b1668801742bf467399900e8c1`.

### Full binary retained outside Git

- Source name: `Run54.0_list.dat`.
- Size: 129,926,126 bytes.
- Records: 310,315 events after the 25-byte file header.
- SHA-256: `0a895a9829244f85ee7bd9046f79fbcd1a64c49458d269fb7c206ebf3a9fb865`.

The full file exceeds GitHub's normal 100 MiB object limit and is intentionally not committed. Do not replace the sample with a byte-count cut that ends inside an event.

## Intended tests

- Decode and validate every field of the 25-byte format-3.4 header.
- Iterate exactly 256 length-prefixed records and finish at byte 113,825.
- Decode spectroscopy/timing channel entries and compare aggregate/golden expectations added during implementation.
- Reject truncation, invalid record sizes, and trailing bytes.
- Confirm offline replay is deterministic.

This is JANUS's processed list-file format, not raw TCP port 9000 traffic and not a FERSlib `.frd` raw capture. It validates processed-event compatibility but cannot by itself validate DT5215 wire framing.
