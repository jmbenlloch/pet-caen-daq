# 0012 — Per-card event quotas

Status: accepted implementation decision, 2026-10-22.

Issue #79 requires equal saved event counts from cards with unequal trigger rates.
Stopping the entire run at a total count cannot meet this requirement. Stopping
individual hardware cards would introduce additional synchronization and drain
behavior without helping preserve exact output counts.

Add the PET configuration extension `PRESET_COUNTS_PER_BOARD`, using
`PresetCounts` as the positive target for every configured physical chain/node.
Apply the quota in the serial decoded-event worker before storage. Count each
non-service record once and commit progress only after a successful write.
The selection includes earlier records from the same batch, preventing overshoot
both during normal acquisition and shutdown draining. Service records are saved
without advancing the target until that card completes, then omitted too.

Continue reading all cards until every configured card completes, then use the
existing stop/drain lifecycle. A silent card requires an operator stop. Raw
capture and transport journals remain unfiltered; decoded statistics and
histograms describe retained records. Persist the quota in runtime provenance.
Source scans keep their own stopping policy.

This avoids changing the event schema and works with both JSON and HDF5 writers.
The existing global `PRESET_COUNTS` semantics remain unchanged. Verification uses
simulator and storage tests; no new hardware behavior is claimed as verified.
