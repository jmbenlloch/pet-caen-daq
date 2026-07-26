# HDF5 service events

This document describes how decoded DT5202 service events are stored in the
production HDF5 run files. Service events are periodic monitoring records, not
detector-hit events. JANUS/FERSlib expects them approximately once per second
and uses them to monitor board temperature, high voltage, acquisition status,
and trigger counters.

The wire decoding and unit conversions described here are **source-confirmed**
by the bundled FERSlib implementation and the native Go decoder. The example
values are **capture-verified** from `pcap/run-54/run_54.0000.h5`.

## Dataset relationships

Service data use one parent dataset and two flat child pools:

```text
/events/service/events
    +-- counter_offset, counter_count --> /events/service/counters
    `-- unknown_offset, unknown_count --> /events/service/unknown_payload
```

The source board is recorded separately in `/events/index`. Select index rows
whose `kind` is `5` (`KindService`); `kind_row` identifies the corresponding
row in `/events/service/events`, while `chain` and `node` identify its source.

In run 54 the datasets contain:

| Dataset | Rows |
| --- | ---: |
| `/events/service/events` | 60 |
| `/events/service/counters` | 2,903 |
| `/events/service/unknown_payload` | 3,920 bytes |

## Parent event schema

`/events/service/events` is an extensible one-dimensional compound dataset:

| Field | HDF5 type | Stored meaning and unit |
| --- | --- | --- |
| `timestamp` | `uint64` | Raw hardware timestamp in 8 ns ticks |
| `version` | `uint8` | Service payload version |
| `format` | `uint8` | Bit 0: HV section; bit 1: counter section |
| `validity` | `uint8` | Bitmap specifying which optional scalar fields are valid |
| `fpga_temperature_c` | `float64` | Converted FPGA temperature in degrees Celsius |
| `board_temperature_c` | `float64` | Converted board temperature in degrees Celsius |
| `detector_temperature_c` | `float64` | Converted detector temperature in degrees Celsius |
| `hv_temperature_c` | `float64` | Converted HV subsystem temperature in degrees Celsius |
| `hv_voltage_v` | `float64` | Converted monitored HV in volts |
| `hv_current_a` | `float64` | Converted monitored HV current in amperes |
| `hv_on` | `uint8` | Boolean, stored as 0 or 1 |
| `hv_ramping` | `uint8` | Boolean, stored as 0 or 1 |
| `hv_over_current` | `uint8` | Boolean, stored as 0 or 1 |
| `hv_over_voltage` | `uint8` | Boolean, stored as 0 or 1 |
| `status` | `uint16` | Raw service status word |
| `counter_offset` | `uint64` | First child row in `/events/service/counters` |
| `counter_count` | `uint32` | Number of child counter rows |
| `t_or_count` | `uint32` | T-OR counter reported as wire channel 64 |
| `q_or_count` | `uint32` | Q-OR counter reported as wire channel 65 |
| `unknown_offset` | `uint64` | First byte in `/events/service/unknown_payload` |
| `unknown_count` | `uint32` | Number of preserved unknown bytes |

An optional numeric column containing zero does not by itself mean that the
measurement was zero. Consumers must first check its `validity` bit.

### Validity bitmap

| Bit | Mask | Optional field |
| ---: | ---: | --- |
| 0 | `0x01` | `fpga_temperature_c` |
| 1 | `0x02` | `board_temperature_c` |
| 2 | `0x04` | `detector_temperature_c` |
| 3 | `0x08` | `hv_temperature_c` |
| 4 | `0x10` | `hv_voltage_v` |
| 5 | `0x20` | `hv_current_a` |
| 6 | `0x40` | `status` |

For example, `validity = 127` (`0x7f`) means that all seven optional values are
valid. `validity = 0` means that none of those columns may be interpreted.

## Stored values and decoder conversions

The HDF5 writer does not store the raw ADC values for recognized service
versions. The native decoder converts them to the engineering units named in
the schema, and the writer stores the resulting `float64` values.

| HDF5 field | Conversion from the unsigned wire value |
| --- | --- |
| `fpga_temperature_c` | `(raw * 503.975 / 4096) - 273.15` |
| `board_temperature_c` | `raw / 4` |
| `detector_temperature_c` | `raw_13bit * 256 / 10000` |
| `hv_temperature_c` | `raw_13bit * 256 / 10000` |
| `hv_voltage_v` | `raw / 10000` |
| `hv_current_a` | `raw / 10000000` |

The HV flags are extracted from bits 26 through 29 of the HV status word. The
counter values are described in detail below.

Tools such as `h5dump` may print fewer decimal digits than the stored `float64`
value. Expressing a stored current such as `0.0001326 A` as `132.6 uA`, or a
stored status value such as decimal `13` as `0x000d`, is display formatting and
does not imply a different stored representation.

## Timestamp units

The HDF5 `timestamp` is the integer timestamp taken directly from the
DT5215/DT5202 event descriptor. It is deliberately stored as ticks rather than
as a floating-point time:

```text
1 tick = 8 ns

time_seconds      = timestamp * 8e-9
time_microseconds = timestamp * 0.008
```

Examples:

| Stored timestamp | Time in microseconds | Time in seconds |
| ---: | ---: | ---: |
| 32,770 | 262.160 us | 0.000262160 s |
| 34,137 | 273.096 us | 0.000273096 s |
| 134,250,498 | 1,074,003.984 us | 1.074003984 s |
| 134,326,952 | 1,074,615.616 us | 1.074615616 s |

This is a hardware-relative timestamp, not Unix time or a UTC wall-clock
timestamp. Comparisons should normally remain within the same synchronized
hardware clock domain. Keeping integer ticks in the file avoids floating-point
rounding during acquisition.

For example:

```python
timestamp_s = timestamp_ticks * 8e-9
```

## Trigger-monitoring counters

The counters carried by a service event measure trigger activity. They are not
the number of service events, HDF5 rows, or necessarily the number of
spectroscopy events accepted and stored.

Each counter is an unsigned 24-bit value on the wire:

| Wire channel | HDF5 destination | Meaning |
| ---: | --- | --- |
| 0 through 63 | A row in `/events/service/counters` | Number of self-triggers produced by that physical channel's fast discriminator |
| 64 | Parent field `t_or_count` | Number of T-OR trigger-logic pulses |
| 65 | Parent field `q_or_count` | Number of Q-OR trigger-logic pulses |

FERSlib calls the physical-channel values `ch_trg_cnt` and explicitly labels
them "Channel trigger counts." JANUS processes every service event by adding
the reported values to its accumulated statistics:

```c
Stats.ChTrgCnt[b][ch].cnt += sEvt[b].ch_trg_cnt[ch];
Stats.T_OR_Cnt[b].cnt += sEvt[b].t_or_cnt;
Stats.Q_OR_Cnt[b].cnt += sEvt[b].q_or_cnt;
```

This is strong source evidence that each service packet contains an increment
for its latest monitoring interval, normally approximately one second, rather
than a monotonically increasing total since acquisition start. The exact
counter reset and integration interval have not been established for every
firmware version, however. Analysis code should therefore treat each stored
value as a reported interval count and should not label it a run total without
additional firmware-specific evidence.

To estimate a rate, join service rows to `/events/index`, group them by
`chain` and `node`, and use the actual timestamp difference between consecutive
service events from the same source:

```text
interval_seconds = (current_timestamp - previous_timestamp) * 8e-9
rate_hz          = counter_value / interval_seconds
```

Do not calculate an interval across different boards, and do not blindly
divide by one second. Service packets are expected approximately once per
second, but their arrival interval can vary.

The service counters are distinct from:

- `trigger_id`, which is an event sequencing or validation identifier;
- the number of spectroscopy events written to HDF5;
- lost-trigger estimates derived from gaps in trigger identifiers;
- counters carried by counting-mode events; and
- the number of rows in any HDF5 dataset.

Discriminator and trigger-logic activity can include pulses that do not
ultimately become accepted, transmitted, and stored spectroscopy events.
Consequently, a channel trigger count should not be expected to equal the
number of energy hits or event rows.

Only channel words present in the packet appear in the sparse
`/events/service/counters` child pool. FERSlib clears all 64 channel values
before decoding a service event, so its decoded convention treats an omitted
channel as zero. At the HDF5 level it is safest to say that an omitted channel
has no reported counter word; whether omission always means measured zero is a
firmware behavior rather than an HDF5 guarantee. Absence of the entire counter
section is different and is indicated by format bit 1 being clear.

## Decoded run-54 example

Row 1 of `/events/service/events` in run 54 contains:

| Field | Stored value | Interpretation |
| --- | ---: | --- |
| `timestamp` | 32,770 | 262.160 us in 8 ns ticks |
| `version` | 1 | Recognized service version |
| `format` | 3 | HV and counter sections are present |
| `validity` | 127 (`0x7f`) | All optional telemetry is valid |
| `fpga_temperature_c` | 58.5679 | 58.5679 degrees Celsius |
| `board_temperature_c` | 48.5 | 48.5 degrees Celsius |
| `detector_temperature_c` | 26.4192 | 26.4192 degrees Celsius |
| `hv_temperature_c` | 47.4624 | 47.4624 degrees Celsius |
| `hv_voltage_v` | 45.3985 | 45.3985 V |
| `hv_current_a` | 0.0001326 | 132.6 uA |
| `hv_on` | 1 | HV enabled |
| `hv_ramping` | 0 | HV not ramping |
| `hv_over_current` | 0 | No over-current flag |
| `hv_over_voltage` | 0 | No over-voltage flag |
| `status` | 13 (`0x000d`) | Raw status word |
| `counter_offset` | 0 | Counters begin at child row 0 |
| `counter_count` | 23 | 23 reported physical-channel counters |
| `t_or_count` | 4 | T-OR count |
| `q_or_count` | 0 | Q-OR count |
| `unknown_offset` | 280 | Current end of the raw-byte pool |
| `unknown_count` | 0 | No unknown bytes belong to this row |

The first child counters referenced by this event are:

| Counter row | `parent_row` | `channel` | `counter_value` |
| ---: | ---: | ---: | ---: |
| 0 | 1 | 2 | 1 |
| 1 | 1 | 4 | 1 |
| 2 | 1 | 6 | 1 |
| 3 | 1 | 7 | 1 |
| 4 | 1 | 11 | 2 |
| 5 | 1 | 13 | 1 |
| 6 | 1 | 21 | 1 |
| 7 | 1 | 23 | 1 |
| 8 | 1 | 30 | 1 |
| 9 | 1 | 31 | 2 |
| 10 | 1 | 32 | 1 |
| 11 | 1 | 33 | 3 |
| 12 | 1 | 34 | 4 |

In plain language, this service interval reports one self-trigger on channel
2, two on channel 11, three on channel 33, four on channel 34, and the other
values shown above. The parent event additionally reports four T-OR pulses and
zero Q-OR pulses. These are activity counts for the monitoring interval, not
four stored detector events.

`parent_row` is redundant with the parent's offset/count slice, but makes a
child row independently attributable and permits consistency validation.

Another recognized packet, row 4, reports:

| Measurement | Stored value |
| --- | ---: |
| Timestamp | 134,250,498 ticks = 1.074003984 s |
| FPGA temperature | 49.4629 degrees Celsius |
| Board temperature | 38.5 degrees Celsius |
| Detector temperature | 26.8288 degrees Celsius |
| HV temperature | 37.8112 degrees Celsius |
| HV voltage | 45.3978 V |
| HV current | 0.0001208 A = 120.8 uA |
| HV enabled/ramping | 1 / 0 |
| Over-current/over-voltage | 0 / 0 |
| Status | 13 (`0x000d`) |
| Physical-channel counters | 64 |
| T-OR/Q-OR | 111,691 / 14 |

## Unknown and forward service versions

The decoder recognizes source-confirmed service versions 0 and 1. If
`version > 1`, it does not guess the layout. Instead it:

1. retains the version and format fields;
2. leaves decoded telemetry invalid;
3. copies every byte after the service header into
   `/events/service/unknown_payload`; and
4. records that byte slice through `unknown_offset` and `unknown_count`.

For example, run-54 parent row 0 contains:

| Field | Stored value |
| --- | ---: |
| `timestamp` | 34,137 ticks |
| `version` | 64 (`0x40`) |
| `format` | 0 |
| `validity` | 0 |
| `unknown_offset` | 0 |
| `unknown_count` | 280 |

Its uninterpreted bytes are therefore:

```text
/events/service/unknown_payload[0:280]
```

Other sampled run-54 rows contain versions `0xc0` and `0xfb`, also with
280-byte raw payloads. Preserving these bytes is intentional forward
compatibility: unsupported packets remain available for later analysis
without presenting speculative decoded values.

## Inspecting a file

Display the compound datatype and current number of parent rows:

```sh
h5dump -H -d /events/service/events pcap/run-54/run_54.0000.h5
```

Display the first eight parent rows:

```sh
h5dump -d /events/service/events -s 0 -c 8 \
  pcap/run-54/run_54.0000.h5
```

Display the first 20 child counters:

```sh
h5dump -d /events/service/counters -s 0 -c 20 \
  pcap/run-54/run_54.0000.h5
```

Display the first 80 preserved bytes:

```sh
h5dump -d /events/service/unknown_payload -s 0 -c 80 \
  pcap/run-54/run_54.0000.h5
```

When analyzing parent rows programmatically, apply the validity bitmap before
using optional telemetry, use offset/count pairs as half-open slices, and join
through `/events/index` when chain or node identity is required.
