# Trigger configuration

Review the complete signal path when changing triggers. A discriminator firing,
a board trigger, an accepted event, and a persisted event are different observations.

| Configuration | What to check |
| --- | --- |
| Trigger source | Which source initiates acquisition; check `BunchTrgSource` |
| Channel thresholds | Which discriminator signals can fire |
| `Tlogic_Mask0/1` | Which channels contribute to trigger logic |
| `TriggerLogic` | How contributing inputs are combined |
| `MajorityLevel` | Required multiplicity for majority modes |
| `VetoSource` | Whether a veto can inhibit triggers |
| `ValidationSource`, `ValidationMode` | Whether validation accepts or rejects triggers |
| `TrgIdMode` | Whether trigger IDs count triggers or validation signals |
| `PtrgPeriod` | Period of the internal periodic trigger |

The supported logic choices include `OR64`, `AND2_OR32`, `OR32_AND2`,
`OR16_AND4`, `MAJ64`, and `MAJ32_AND2`. Consult the
[configuration reference in the operator manual](../operator-user-manual.md)
for scope, units, and allowed values; do not infer logic equivalence from names.

## Reviewing a change

1. Save the existing configuration and identify the intended trigger source.
2. Check channel masks, thresholds, logic, veto, and validation together.
3. Apply the configuration through the normal operator workflow.
4. Take a bounded run using the approved hardware procedure.
5. Compare discriminator rates, board event counts, and stored totals, and retain
   the run configuration with the result.

A periodic test pulse checks a different signal path from detector acquisition.
Record test-pulse overrides explicitly when comparing runs.
