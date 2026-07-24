# JANUS versus native Go scan comparison

## Result

On 2026-07-24, JANUS 5.0.0/FERSlib 2.2.0 and the native Go DAQ each
completed matched threshold-staircase and hold-delay scans against fresh
instances of the deterministic simulator. All DT5215 control and stream
traffic was captured. The final native verification runs used the timing
correction described below.

The functional scan sequences agree:

- A staircase writes the same value to QD and TD, loads both Citiroc chips,
  resets the periodic trigger, waits the dwell plus margin, and reads T-OR,
  Q-OR, and all 64 channel counters in descending threshold order.
- A hold-delay scan writes `delay_ns / 8`, starts acquisition, collects ten
  spectroscopy events, stops and flushes before the next delay, and restores
  production configuration.
- Both programs completed all three requested points. Native artifacts record
  validated restoration and exactly ten events at every hold-delay point.
- Both use the FERSlib TDL command delay of 1,000,000 units (10 ms) for
  scan-specific Citiroc, periodic-reset, acquisition-start, and
  acquisition-stop commands after the correction in this study.

The study found one hardware-relevant native timing mismatch. Native scan
commands originally carried zero delay, and consecutive Citiroc loads had no
host settling interval. JANUS uses a 10 ms TDL delay and sleeps 20 ms after
each load. The simulator accepted both, but that did not establish safe
physical-board timing. Native scans now use the source-confirmed command delay,
20 ms Citiroc settling, 100 ms staircase setup settling, and 100 ms
post-stop hold-delay settling. Unit and simulator-backed integration tests
pass with those changes, and follow-up captures verify the corrected wire
values.

This is **source-confirmed plus simulator-observed**, not hardware-verified.
The corrected sequence should still be exercised on one physical board with a
small range before promoting its evidence classification.

## Matched inputs

| Parameter | Value |
| --- | ---: |
| Target | board 0, chain 0, node 0 |
| Staircase | 200–204, step 2, descending |
| Staircase dwell | 10 ms |
| Hold delay | 0–16 ns, step 8 ns |
| Hold samples | 10 events per delay |
| Simulator event interval | 10 ms |
| Topology | one board on each of chains 0–3 |
| Capture filter | localhost TCP 9760 or 9000 |

JANUS used the production-shaped configuration with only connection paths,
data path, and headless analysis changed. Native Go used the same document.
Fresh simulator processes prevented register, counter, or stream state from
leaking between clients.

## Staircase comparison

| Behavior | JANUS | Native Go after correction |
| --- | --- | --- |
| Point order | 204, 202, 200 | 204, 202, 200 |
| Threshold coupling | QD and TD | QD and TD |
| Citiroc loads per point | chip 0, chip 1 | chip 0, chip 1 |
| Command delay | 1,000,000 | 1,000,000 |
| Host settling per Citiroc load | 20 ms | 20 ms |
| Counter wait | dwell + 200 ms | dwell + 200 ms |
| Counters per recorded point | T-OR, Q-OR, channels 0–63 | same |
| Recorded points | 3 | 3 |
| Full selected-board restore | yes | yes, with readback |

The corrected native point records report elapsed counter windows of
210.41, 210.32, and 211.07 ms. JANUS's reset-to-counter-read intervals were
about 220 ms because its FERSlib command path contributes roughly another
10 ms before the host sleep. Both divide raw counters by the requested 10 ms
dwell, which is JANUS-compatible; native storage also retains actual elapsed
time so the distinction is auditable.

JANUS performs an additional unrecorded point at threshold 206. It reads T-OR
and Q-OR but deliberately skips all 64 channel counters and output. Native Go
does not reproduce this undocumented `max + step` warm-up. That is an
intentional, already documented difference, not a range error: both canonical
outputs contain exactly 204, 202, and 200. A physical-board A/B run is needed
to determine whether the warm-up materially affects the first recorded point.

Native restoration uses immediate Citiroc commands, as does the existing
production configuration path. This is outside the scan engine and was
already identified by the broader configuration comparison; the scan-specific
commands are now aligned.

## Hold-delay comparison

| Behavior | JANUS | Native Go after correction |
| --- | --- | --- |
| Delay register values | 0, 1, 2 | 0, 1, 2 |
| Delay readback | no | yes, must match |
| Acquisition command delay | 1,000,000 | 1,000,000 |
| Events per point | 10 | 10 |
| Completed points | 3 | 3 |
| Stop/flush boundary | stop, 100 ms sleep, FERSlib flush | stop, 100 ms settle, clear stream before next point |
| Histogram | 64 × 512 bins per delay | 64 × 512 bins per delay |
| Restore | JANUS reconfiguration paths | one full selected-board restore with readback |

Observed acquisition windows were approximately 82–93 ms for JANUS and
91–100 ms for native Go. This spread is expected from the phase of a 10 ms
simulator event ticker; both loops terminate on the tenth successfully decoded
event, not on wall-clock duration. Native artifact point elapsed values were
97.79, 91.37, and 99.44 ms.

Native Go has two useful safety differences: it reads the programmed hold
delay back before starting, and it rejects CRC or spectroscopy decode errors
instead of silently treating them as samples. JANUS has a bounded polling loop
that can leave a point early after repeated no-data polls; native Go uses an
explicit per-point context timeout and fails the scan if the requested event
count is not reached.

JANUS performs more restoration traffic after a hold scan: the selected board
is configured by overlapping scan/restart paths and the main loop then
reconfigures all boards. Native Go restores only the selected board because
only that board was modified, and validates the effective registers. This is
a meaningful efficiency difference, but the final selected-board state is
equivalent in the simulator.

## Evidence

Generated evidence is intentionally ignored by Git under
`test-results/janus-go-scan-comparison/`. The four final PCAP hashes are:

```text
2eceb9d47d241ed8506497253564dca633608017fe581be980fc2eaa85a21fb5  janus-staircase/traffic.pcap
83d638ffb90214447dc1ed90c0eebb1bd9e8671e0b080e9148ae258c9fe7464a  janus-hold/traffic.pcap
9227d43dd7d100c2bb98291fc5fa727eda76a66c7606ee0d6e9e9b087a45991a  go-staircase-fixed/traffic.pcap
fc90f4cd1b10ac98c0aa3bf3294db1e9de8e09303ee76a7fb4d9edc1b3f12bad  go-hold-fixed/traffic.pcap
```

Each evidence directory also contains a tshark control export and the output
of `scripts/analyze-scan-control.py`. Native directories contain manifests and
point JSON Lines artifacts. The capture analyzer decodes relevant WREG, RREG,
FCMD, and DCMD fields and prints a timestamped scan transcript.

## Conclusion

The native algorithms are scanning the requested values correctly in the
simulator, collect the intended counters/events, and restore configuration.
The command-delay mismatch was meaningful enough to correct. Remaining
differences are deliberate or favorable: no undocumented staircase warm-up,
hold-delay readback, explicit decode failure, bounded timeout, durable
per-point evidence, and targeted validated restoration.

The only unresolved correctness question that the simulator cannot answer is
whether the JANUS warm-up changes the first physical staircase point. A
conservative hardware comparison should run 200–204 twice—once after an
explicit 206 warm-up and once without—while preserving both captures and raw
counters.
