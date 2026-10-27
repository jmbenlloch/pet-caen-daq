# TDlink ASIC initialization

Normal board configuration now uses the source-confirmed and capture-verified
JANUS automatic ASIC load/readback sequence. A hard reset and all ASIC load
commands use 1,000,000 TDlink delay ticks (10 ms). The DT5215 client waits for
the scheduled command before releasing its control transaction.

For each chip independently, select automatic loading, issue one load, save
the slow-control selector, select that chip again, issue a second load to shift
out the existing ASIC stream, read all 36 words and restore the selector. Only
then proceed to the other chip. Readback actively reloads the ASIC.

Reference: FERSlib 5.0.1 `FERS_configure_5202.c` automatic configuration and
`ReadSCbsFromChip`, and the 2026-10-26 retained control capture (JANUS command
frames 3878/3884/4034/4040 for one card). The previous normal Go path issued one
load per chip with zero scheduled delay. Each chip handles 32 channels, matching
the observed all-zero block; this motivates the change but is not causal proof.

Transport errors, the observed invalid 0xDEADBEEF marker and out-of-width ASIC
readback bits abort configuration. An invalid selector is never restored.
I/O failures are not retried by reloading or replaying arbitrary writes. Existing
FPGA register final-value verification remains enabled. Generated host ASIC
streams contain FPGA-owned fields and known bit-order modeling limitations, so
they are not a byte-exact readback acceptance oracle. This procedure does not
prove that every ASIC bit matches user intent.

This isolated change leaves analog DAC ordering, microcontroller shutdown,
start synchronization and the independent scan helper's single-load behavior
unchanged. It does not invoke JANUS or FERSlib. Validate on the failing hardware
state with raw ADC data; mocks alone cannot establish the zero-channel fix.

Run `task test:initialization` for sequence, every-step failure, invalid readback,
cancellation and simulator integration checks. Run `task build:backend:hdf5` in
the pinned build container for a backend-only artifact. Preserve the previous
deployment and retain failed and successful hardware evidence separately.

## Hardware evidence, 2026-10-27

The implementation was based on deployed source `8990817` (backend `6514fc9`)
and deployed as commit `e2072b2`, with the running frontend preserved. Tests
included the failing-before regression, every-I/O failure injection, full backend
race tests, simulator/API integration and the HDF5 production build.

Before the update, five complete Go-only runs after a physical power cycle
(75, 76, 77, 79, 80) had channels 0–31 on PID 64884 identically zero in both
gains. PID 64887 was healthy. Normal old-version reconnect/configuration had
not recovered the dead block. The updated Go connection initialized the same
cards on concentrator 172.16.0.11 without JANUS or another physical power cycle.
Run 81 immediately recovered all 64 channels on both cards, with 10,000 events
per card, no zero ADC samples, no constant channels and no descriptor CRC errors.
An independent raw decoder matched all 20,000 stored events exactly and verified
12,877 raw batch checksums. The recovery is present in raw data, not introduced
by event decoding or storage.

Five consecutive runs **81–85** passed with 50,000 events per card (100,000
total), all 64 channels varying in both gains, zero zero-valued ADC samples and
zero descriptor CRC errors. Final state was READY with both HV outputs off.

The evidence establishes recovery using the combined initialization change;
it does not distinguish the individual effects of reset delay, load delay,
second load and readback. A physical power cycle after deployment remains
untested. Invalid concentrator responses and the earlier malformed-event
incident remain separate robustness issues.

Detailed source/capture comparisons, test logs, deployment provenance, channel
statistics and retained data are under the sibling evidence directory
`../asic-init-validation-20261027/` (relative to the repository root).
