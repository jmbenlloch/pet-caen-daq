# HV bias, channel offset, and Janus

Janus and this DAQ program the board HV supply and the individual channel DAC
separately. Neither subtracts 4.5 V from `HV_Vbias` before writing the supply
setpoint. Both interfaces subtract an estimated channel offset when displaying
**Vnom**, and both use **4.2 V** for the range labelled `4.5`.

These findings are **source-confirmed**. They establish software behavior, not a
measurement of the voltage at a SiPM.

## Three different voltages

| Quantity | Meaning |
| --- | --- |
| `HV_Vbias` | Configured board HV supply setpoint, common to its channels. |
| Vmon | Measured board HV supply output. It does not measure each channel after its individual adjustment. |
| Vnom | Calculated estimate of channel bias using the configured setpoint and channel DAC code. It is not a channel voltage measurement. |

The offset is implemented through the channel input DAC. A lower Vnom does not
require the supply setpoint or Vmon to decrease by the same amount. Temperature
feedback and ramping can also make the measured supply output differ from the
configured setpoint; the Vnom calculation below uses the configured value.

## The calculation used by both interfaces

For the effective channel code `D = HV_IndivAdj`, including any channel override:

```text
span = 4.2 V  when HV_Adjust_Range = 4.5
span = 2.5 V  when HV_Adjust_Range = 2.5
span = 0 V    when HV_Adjust_Range = DISABLED

offset = span × (255 − D) / 255
Vnom   = HV_Vbias − offset
```

Code **0 gives the maximum estimated reduction**; code **255 gives zero
estimated reduction**. Selecting the `4.5` range does not itself request a fixed
4.5 V subtraction. The selected range and channel code work together.

For the example configuration `HV_Vbias = 45.4 V`, `HV_Adjust_Range = 4.5`:

| `HV_IndivAdj` | Estimated offset | Estimated Vnom |
| --- | --- | --- |
| 0 | 4.20 V | 41.20 V |
| 128 | 2.09 V | 43.31 V |
| 255 | 0.00 V | 45.40 V |
| Any code with adjustment disabled | 0.00 V | 45.40 V |

In all these examples the configured board supply setpoint remains **45.4 V**.
A Janus HV monitor showing about 45.4 V therefore does not demonstrate that the
channel adjustment is missing. Compare its channel **Vnom** with this DAQ's
**Vnom** in the `HV_IndivAdj` per-channel editor.

## Register-level comparison

| Operation | Janus / FERSlib | This DAQ |
| --- | --- | --- |
| Supply voltage | Writes `HV_Vbias × 10000` to HV peripheral register 2, type 1. | Same scaling and destination; no channel offset is subtracted. |
| Channel adjustment enabled | Writes `0x100 \| HV_IndivAdj` to each channel adjustment register. | Same value. |
| Range selection | Selects the CITIROC input-DAC reference: 0 for `2.5`, 1 for `4.5`. | Same reference selection. |
| Adjustment disabled | Keeps the DAC enabled with code 255 (`0x1ff`) and selects the `4.5` reference. | Same behavior. |
| Display estimate | Uses 4.2 V for the `4.5` range and subtracts the calculated offset. | Same formula. |

The disabled case is implemented as zero trim through the maximum DAC code,
rather than by clearing the DAC enable bit.

## Source evidence

The comparison uses the bundled **Janus 5202 5.0.1 (2026-07-21)** source. The
bundled `janus/windows` GUI also contains the same Vnom formula.

| Source | Relevant implementation |
| --- | --- |
| Janus `gui/tabs.py`, `UpdateVnom` | Sets `dacfs = 4.2` for range `4.5`; computes `vdac = dacfs * (255 - D) / 255` and `vnom = HV_Vbias - vdac`. |
| FERSlib `src/FERSlib.c`, `FERS_HV_Set_Vbias` | Writes the supply voltage directly, scaled by 10000. |
| FERSlib `src/FERS_configure_5202.c` | Selects the DAC reference and writes the channel code, including `0x1ff` for disabled adjustment. |
| This DAQ `frontend/src/ChannelOverrides.vue`, `nominalVoltage` | Implements the matching Vnom estimate. |
| This DAQ `backend/internal/dt5202/config.go` | Builds the reference selection and individual channel register writes. |
| This DAQ `backend/internal/dt5202/hv.go`, `buildHVPlan` / `ReadHVTelemetry` | Writes the unadjusted supply setpoint and decodes board voltage readback. |

Janus paths above are relative to
`janus/Janus_5202_5.0.1_20260721_linux` in the reference-source workspace;
DAQ paths are relative to the application repository. These are source references,
not claims that the vendor files are served by this documentation site.

## What remains unverified

**Unknown:** why CAEN uses 4.2 V in its display estimate while naming the range
`4.5`, and how closely that estimate matches the actual channel voltages on our
boards. The source does not establish whether 4.2 V is a measured span, a typical
value, or another approximation.

Do not infer a universal 0.3 V correction from the two numbers. Verification
requires the board/chip identity, firmware, applied configuration, measurement
point and method, and voltage measurements across channel DAC codes. Until that
evidence exists, treat Vnom as an estimate and retain the distinction between
supply readback and channel bias.

See [temperatures and HV](temperatures-and-hv.md) for operating controls and
[hardware caveats](caveats.md) for evidence labels.
