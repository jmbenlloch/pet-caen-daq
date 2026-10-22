# Hardware caveats

## The 4.5 V setting and the reported 4.2 V value

**Source-confirmed:** Janus and this DAQ both use **4.2 V** when estimating
channel voltage for the range labelled `4.5`. Neither subtracts that offset from
the board HV supply setpoint. The channel DAC is programmed separately, and Vmon
reports the supply output rather than the individual channel voltage.

See [HV bias, channel offset, and Janus](hv-bias-and-offset.md) for the formula,
a worked example, register comparison, and source references.

**Unknown:** the physical DAC span on our hardware and CAEN's reason for using
4.2 V in the estimate have not been established. The display formula alone does
not justify a 0.3 V operating correction.

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
