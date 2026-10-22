# Temperatures and HV

## Reading board cards

| Reading | Interpretation |
| --- | --- |
| Board temperature | Board temperature measurement |
| FPGA temperature | FPGA temperature measurement |
| Detector temperature | Detector sensor reading using the configured conversion |
| HV temperature | HV module temperature |
| Vmon (V) | Measured HV output voltage |
| Imon (mA) | Measured current, converted from backend amperes |
| Telemetry updated | Observation time of service telemetry |

Temperatures are displayed in degrees Celsius. Detector readings depend on
`TempSensType` and its conversion coefficients. A plausible displayed value alone
does not verify that the sensor configuration is correct. This guide does not
establish hardware temperature limits; use the approved procedure for the actual
board, sensor, and detector assembly.

## Setpoints and readback

`HV_Vbias` is configuration; Vmon is a measurement. The HV on/off buttons use the
setpoints last applied during hardware configuration. After editing settings,
apply the configuration before switching HV on.

HV controls require the Ready state. HV-on also requires backend authorization
through `-authorize-hv-config`. Watch ramp state, voltage, current, and temperatures;
an accepted command does not prove that the requested voltage has been reached.

Read the [full HV procedure](../operator-user-manual.md) and
[hardware operations](../hardware-operations.md) before changing settings.
The [voltage-range caveat](caveats.md) concerns a separate configuration concept.
