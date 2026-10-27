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
