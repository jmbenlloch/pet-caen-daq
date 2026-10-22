# Hardware caveats

## The 4.5 V setting and the reported 4.2 V value

**Source-confirmed:** `HV_Adjust_Range` accepts `4.5`, `2.5`, or `DISABLED`.
It selects the individual channel adjustment DAC range; it is separate from the
board HV bias setpoint `HV_Vbias` and measured output Vmon. The
[Citiroc configuration audit](../janus-gui-configuration-audit.md)
documents the input-DAC reference selection.

**Unknown:** the relationship between the reported **4.2 V** value and the
**4.5 V** setting has not been established in the checked-in documentation.
Do not apply a 0.3 V correction or assume these values are interchangeable.

To resolve this entry, retain the board/chip identity, firmware, configuration,
measurement point and method, and the source or measurement supporting the 4.2 V
value. State whether it is a measured span, a nominal reference, or another quantity
before prescribing an operating adjustment.

## Evidence labels

- **Source-confirmed:** implemented in the reference source.
- **Capture-verified:** observed and matched in a real packet capture.
- **Hardware-verified:** exercised successfully on the real system.
- **Inferred:** reasoned from behavior, with verification still needed.
- **Unknown:** unresolved.

Keep these labels with each hardware claim. Firmware and topology can affect its
applicability. See [protocol notes](../daq_protocol_notes.md) and
[hardware capture evidence](../real-hardware-capture-evidence.md).

## Telemetry and trigger estimates

A stale value is not a current measurement. Trigger-ID gaps are an estimate of
lost triggers and require interpretation. See [reading telemetry](telemetry.md).
