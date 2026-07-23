# HDF5 schema examples

Status: illustrative companion to `hdf5-storage-design.md`

This document shows the logical tables used by the initial HDF5 writer.
Dataset names, event-kind enum numbers, fixed-width field types, child ranges,
and optional-value bit assignments shown here are covered by implementation
tests. Chunk sizing and compression remain subject to corpus benchmarks before
production acceptance.

The first spectroscopy values come from
`run-go-native-detector-hvon-003`. Other event families use the project's
golden decoder fixtures or illustrative rows because the retained run corpus
does not contain those event kinds. Configuration values are either taken from
that run or explicitly illustrative.

Offsets are zero-based. A parent row refers to a contiguous range in a child
dataset with `(offset,count)`.

## Run-wide event index

`/events/index` preserves acquisition order and routes each record to its
kind-specific parent dataset:

| sequence | kind | kind_row | chain | node | qualifier | trigger_id | timestamp | payload_offset_words | payload_size_words | crc_error |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | spectroscopy | 0 | 1 | 0 | `0x33` | 0 | 26865 | 0 | 82 | 0 |
| 2 | timing | 0 | 0 | 0 | `0x02` | 7 | 291 | 0 | 2 | 0 |
| 3 | counting | 0 | 2 | 0 | `0x84` | 11 | 12 | 0 | 4 | 0 |
| 4 | waveform | 0 | 3 | 0 | `0x08` | 1 | 2 | 0 | 3 | 0 |
| 5 | service | 0 | 0 | 0 | `0x2f` | 0 | 55 | 0 | 8 | 0 |
| 6 | test | 0 | 1 | 0 | `0xff` | 8 | 9 | 0 | 2 | 0 |

The event-kind enum can use stable `uint8` values:

| Stored value | Meaning |
| ---: | --- |
| 1 | spectroscopy |
| 2 | timing |
| 3 | counting |
| 4 | waveform |
| 5 | service |
| 6 | test |

`kind` is convenient for queries; `qualifier` is the authoritative packed
variant. The payload offset and size are descriptor evidence and are not HDF5
byte offsets.

## Spectroscopy

### Parent dataset

`/events/spectroscopy/events`:

| trigger_id | timestamp | channel_mask | relative_clock | valid_bits | energy_offset | energy_count | timing_offset | timing_count | time_reference |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 26865 | `0xffffffffffffffff` | 0 | `0b10` | 0 | 64 | 0 | 13 | 428870 |
| 4 | 61124 | `0xffffffffffffffff` | 0 | `0b10` | 64 | 64 | 13 | 23 | 977014 |
| 10 | 105400 | `0x0000000000000089` | 77 | `0b01` | 128 | 3 | 36 | 0 | 0 |

For this example, validity bit 0 means `relative_clock` is present and bit 1
means `time_reference` is present. The exact bit assignments become schema
constants.

The third row illustrates sparse energy readout for channels 0, 3, and 7 and
an empty TDC child range. It is not from the retained real run.

### Energy children

`/events/spectroscopy/energies`:

| parent_row | channel | low_gain | high_gain | has_low_gain | has_high_gain | discriminator |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 0 | 263 | 2225 | 1 | 1 | 1 |
| 0 | 1 | 374 | 3673 | 1 | 1 | 1 |
| 0 | 2 | 354 | 2164 | 1 | 1 | 1 |
| 0 | 3 | 202 | 1688 | 1 | 1 | 0 |
| … | … | … | … | … | … | … |
| 0 | 63 | 371 | 3464 | 1 | 1 | 1 |
| 1 | 0 | 437 | 3425 | 1 | 1 | 0 |
| 1 | 1 | 166 | 140 | 1 | 1 | 0 |

The first parent owns `energies[0:64]`. `discriminator` is the packed
charge-discriminator/QD bit. It is not proof that the channel alone caused the
board trigger.

`parent_row` is redundant with the parent range and may be omitted from the
physical format. Retaining it improves direct inspection and integrity checks
at a storage cost that should be measured.

### TDC children attached to spectroscopy

`/events/spectroscopy/timings`:

| parent_row | channel | toa | tot |
| ---: | ---: | ---: | ---: |
| 0 | 63 | 861 | 0 |
| 0 | 40 | 869 | 0 |
| 0 | 13 | 903 | 0 |
| 0 | 49 | 871 | 0 |
| … | … | … | … |
| 1 | 48 | 913 | 0 |
| 1 | 52 | 918 | 0 |
| 1 | 44 | 939 | 0 |

These rows prove that accepted TDC measurements exist for the listed channels.
They do not provide a general `TD asserted` bit. Analysis may derive
`has_timing_hit`; absence of a row does not prove that the analog TD never
crossed.

## Timing-only events

`/events/timing/events`:

| trigger_id | timestamp | time_reference | hit_offset | hit_count |
| ---: | ---: | ---: | ---: | ---: |
| 7 | 291 | 4666 | 0 | 2 |
| 8 | 350 | 5604 | 2 | 1 |
| 9 | 410 | 6561 | 3 | 0 |

`/events/timing/hits`:

| parent_row | channel | toa | tot |
| ---: | ---: | ---: | ---: |
| 0 | 3 | 13398 | 18 |
| 0 | 9 | 13510 | 22 |
| 1 | 4 | 39030 | 427 |

The common event index retains the qualifier needed to distinguish common
start, common stop, and streaming layouts. ToA allocation and ToT availability
must not be inferred solely from the group name.

## Counting events

`/events/counting/events`:

| trigger_id | timestamp | relative_clock | relative_clock_valid | channel_mask | count_offset | count_count | t_or_count | q_or_count |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 11 | 12 | 99 | 1 | `0x0000000000000004` | 0 | 1 | 456 | 789 |
| 12 | 20 | 105 | 1 | `0x000000000000002c` | 1 | 3 | 502 | 810 |
| 13 | 28 | 0 | 0 | `0x0000000000000000` | 4 | 0 | 530 | 850 |

`/events/counting/counts`:

| parent_row | channel | counter_value |
| ---: | ---: | ---: |
| 0 | 2 | 123 |
| 1 | 2 | 130 |
| 1 | 3 | 95 |
| 1 | 5 | 12 |

T-OR and Q-OR are aggregate time- and charge-discriminator OR counters, using
reserved wire identifiers 64 and 65. They are not counts of asserted channels
in a single event and need not equal the accepted-event count.

## Waveform events

`/events/waveform/events`:

| trigger_id | timestamp | sample_offset | sample_count |
| ---: | ---: | ---: | ---: |
| 1 | 2 | 0 | 4 |
| 2 | 15 | 4 | 3 |

`/events/waveform/samples`:

| parent_row | sample_index | high_gain | low_gain | digital_probes |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 0 | 111 | 222 | `0b1010` |
| 0 | 1 | 118 | 229 | `0b1010` |
| 0 | 2 | 145 | 270 | `0b0010` |
| 0 | 3 | 120 | 225 | `0b0010` |
| 1 | 0 | 95 | 190 | `0b0000` |
| 1 | 1 | 160 | 310 | `0b0100` |
| 1 | 2 | 100 | 200 | `0b0000` |

Each packed word contains a 14-bit HG sample, a 14-bit LG sample, and four
digital-probe bits. Channel and signal interpretation comes from the immutable
per-board waveform/probe configuration, not from each sample. The raw nibble
is authoritative until all four bit mappings are verified.

## Service events

`/events/service/events`:

| timestamp | version | format | valid_bits | fpga_temp_c | board_temp_c | detector_temp_c | hv_temp_c | hv_voltage_v | hv_current_a | hv_on | hv_ramping | hv_over_current | hv_over_voltage | status | counter_offset | counter_count | t_or_count | q_or_count | unknown_offset | unknown_count |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 55 | 1 | 3 | `0x7f` | -21.1625 | 25.0 | 25.6 | 51.2 | 45.4 | 0.001 | 1 | 0 | 1 | 0 | `0x4321` | 0 | 1 | 99 | 111 | 0 | 0 |
| 80 | 1 | 1 | `0x7f` | 31.4 | 27.5 | 24.8 | 29.1 | 49.5 | 0.0003 | 1 | 0 | 0 | 0 | `0x0007` | 1 | 0 | 0 | 0 | 0 | 0 |
| 100 | 2 | 0 | `0x00` | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 1 | 0 | 0 | 0 | 4 |

One possible validity bitmap is:

| Bit | Optional value |
| ---: | --- |
| 0 | FPGA temperature |
| 1 | board temperature |
| 2 | detector temperature |
| 3 | HV temperature |
| 4 | HV voltage |
| 5 | HV current |
| 6 | status |

`/events/service/counters`:

| parent_row | channel | counter_value |
| ---: | ---: | ---: |
| 0 | 7 | 88 |

These are 24-bit per-channel trigger-monitoring values. Their exact
reset/interval behavior is not established for every firmware version, so the
schema does not label them as totals since run start.

`/events/service/unknown_payload`, represented as a flat `uint8` pool:

| Byte offset | Hex value |
| ---: | ---: |
| 0 | `0x78` |
| 1 | `0x56` |
| 2 | `0x34` |
| 3 | `0x12` |

The third service parent points to bytes `[0:4]`. This preserves an unknown
newer-format payload without pretending to decode it.

## Test events

`/events/test/events`:

| trigger_id | timestamp | word_offset | word_count |
| ---: | ---: | ---: | ---: |
| 8 | 9 | 0 | 2 |
| 9 | 15 | 2 | 4 |

`/events/test/words`, a flat `uint32` pool:

| Word offset | Hex value | Decimal value |
| ---: | ---: | ---: |
| 0 | `0x11223344` | 287454020 |
| 1 | `0xaabbccdd` | 2864434397 |
| 2 | `0x00000001` | 1 |
| 3 | `0x00000002` | 2 |
| 4 | `0x00000003` | 3 |
| 5 | `0x00000004` | 4 |

The words are opaque evidence and receive no invented interpretation.

## Effective configuration

The exact requested JANUS document, audit, packed Citiroc streams, and ordered
FPGA writes remain authoritative. The following tables are query-friendly
effective views.

### Board and physical source

`/configuration/effective/boards`:

| board | chain | node | firmware_revision |
| ---: | ---: | ---: | ---: |
| 0 | 0 | 0 | `0x05000000` |
| 1 | 1 | 0 | `0x05000000` |
| 2 | 2 | 0 | `0x05000000` |
| 3 | 3 | 0 | `0x05000000` |

The firmware values are illustrative. Schema version 1 does not require a
second logical-board identity beyond the configured board index and physical
`(chain,node)` mapping.

### Effective per-channel configuration

`/configuration/effective/channels`:

| board | channel | chip | chip_channel | readout_enabled | qd_enabled | td_enabled | qd_fine | td_fine | hg_gain | lg_gain | hv_adjustment | calibrate_hg | calibrate_lg | preamp_disabled |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 0 | 0 | 0 | 1 | 1 | 1 | 0 | 0 | 55 | 55 | 256 | 0 | 0 | 0 |
| 0 | 31 | 0 | 31 | 1 | 1 | 1 | 0 | 0 | 55 | 55 | 256 | 0 | 0 | 0 |
| 0 | 32 | 1 | 0 | 1 | 1 | 1 | 0 | 0 | 55 | 55 | 256 | 0 | 0 | 0 |
| 1 | 24 | 0 | 24 | 1 | 1 | 1 | 0 | 7 | 55 | 55 | 256 | 0 | 0 | 0 |

Fine TD/QD values, gains, individual HV adjustment, and the calibration/preamp
flags are channel-level. `readout_enabled`, `qd_enabled`, and `td_enabled` are
expanded from the effective 64-bit masks.

A `trigger_enabled` convenience column is deliberately absent until the exact
relationship between TD masks and TLOGIC inputs is verified. Underlying masks,
register writes, and packed streams are authoritative.

### Board-level front-end settings

`/configuration/effective/frontend_boards`:

| board | qd_coarse | td_coarse | fast_shaper_input | hold_delay_ns | mux_period_ns | hit_holdoff_ns |
| ---: | ---: | ---: | --- | ---: | ---: | ---: |
| 0 | 250 | 220 | `HG-PA` | 300 | 300 | 0 |
| 1 | 250 | 123 | `HG-PA` | 300 | 300 | 0 |
| 2 | 250 | 220 | `HG-PA` | 300 | 300 | 0 |
| 3 | 250 | 220 | `HG-PA` | 300 | 300 | 0 |

This illustrates the JANUS override hierarchy: global TD coarse value 220,
with board 1 overridden to 123. Value 123 applies to all 64 channels and both
chips on board 1; each channel still has its separate TD fine DAC. Coarse and
fine are separate hardware fields, not values analysis should simply add.

### Board-level trigger settings

`/configuration/effective/board_trigger`:

| board | bunch_source | logic | majority | logic_width_ns | validation_mode | validation_source | veto_source | holdoff_ns |
| ---: | --- | --- | ---: | ---: | --- | --- | --- | ---: |
| 0 | `TLOGIC` | `MAJ64` | 4 | 0 | `DISABLED` | `T0-IN` | `DISABLED` | 0 |
| 1 | `TLOGIC` | `MAJ64` | 4 | 0 | `DISABLED` | `T0-IN` | `DISABLED` | 0 |

The board trigger algorithm is stored once per board rather than duplicated in
64 channel rows.

### Timing-reference settings

`/configuration/effective/timing_reference`:

| board | source | window_ns | delay_ns | tot_enabled |
| ---: | --- | ---: | ---: | ---: |
| 0 | `TLOGIC` | 1000 | -500 | 0 |
| 1 | `TLOGIC` | 1000 | -500 | 0 |

### Citiroc chip-common settings

`/configuration/effective/citiroc_chips` contains every versioned
`CitirocCommon` field. A shortened view is:

| board | chip | qd_mask | qd_coarse | td_coarse | hg_shaping_ns | lg_shaping_ns | fast_shaper_input | channel_triggers_enabled |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: |
| 0 | 0 | `0xffffffff` | 250 | 220 | 87.5 | 87.5 | `HG-PA` | 1 |
| 0 | 1 | `0xffffffff` | 250 | 220 | 87.5 | 87.5 | `HG-PA` | 1 |
| 1 | 0 | `0xffffffff` | 250 | 123 | 87.5 | 87.5 | `HG-PA` | 1 |
| 1 | 1 | `0xffffffff` | 250 | 123 | 87.5 | 87.5 | `HG-PA` | 1 |

The public planner currently applies the same coarse thresholds to both chips
on a board. Repeating them here records the actual chip-common packed values.
The complete table must include all power, bias, shaping, threshold, and
trigger-output fields, not only this shortened analysis view.

### Packed Citiroc streams

`/configuration/effective/citiroc_streams`, with a fixed `words[36]` field:

| board | chip | bit_count | words[0] | words[1] | … | words[35] |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 0 | 1144 | `0x00000000` | `0x00000000` | … | `0x0012ab34` |
| 0 | 1 | 1144 | `0x00000000` | `0x00000000` | … | `0x0012ab34` |
| 1 | 0 | 1144 | `0x00000000` | `0x00000000` | … | `0x0012ab34` |

The word values are illustrative. The complete 1,144-bit stream is
authoritative hardware-facing evidence.

### Ordered FPGA writes

`/configuration/effective/fpga_writes`:

| board | ordinal | address | value |
| ---: | ---: | ---: | ---: |
| 0 | 0 | `0x01000100` | `0xffffffff` |
| 0 | 1 | `0x01000104` | `0xffffffff` |
| 0 | 2 | `0x01000000` | `0x40003003` |
| 0 | 3 | `0x01000238` | `0x00000000` |
| 1 | 0 | `0x01000100` | `0xffffffff` |

`ordinal` preserves order and repeated writes to the same address.

### Waveform and probe configuration

`/configuration/effective/waveform_configuration`:

| board | waveform_length | waveform_source | analog_probe_0 | probe_channel_0 | analog_probe_1 | probe_channel_1 | digital_probe_0 | digital_probe_1 | packed_bits_mapping_verified |
| ---: | ---: | --- | --- | ---: | --- | ---: | --- | --- | ---: |
| 0 | 800 | `UNKNOWN` | `FAST` | 12 | `PREAMP_LG` | 33 | `Q_OR` | `PEAK_HG` | 0 |

The raw waveform nibble remains authoritative while `WaveformSource` behavior
and all four packed digital-bit mappings are unverified. Configuration is
immutable during a run, so waveform event rows do not need a configuration ID.

### HV plans

`/configuration/effective/hv_plans`:

| board | voltage_v | current_limit_ma | temperature_feedback | feedback_mv_per_c | coefficient_0 | coefficient_1 | coefficient_2 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 45.4 | 1.0 | 0 | 35.0 | — | — | — |
| 1 | 45.4 | 1.0 | 0 | 35.0 | — | — | — |
| 2 | 45.4 | 1.0 | 0 | 35.0 | — | — | — |
| 3 | 45.4 | 1.0 | 0 | 35.0 | — | — | — |

The coefficients must contain the actual effective sensor model. They are
shown absent here rather than filled with invented values.

### Pedestal calibration

`/configuration/effective/pedestal_calibration`:

| board | channel | low_gain_pedestal | high_gain_pedestal | source |
| ---: | ---: | ---: | ---: | --- |
| 0 | 0 | 51 | 49 | protected flash |
| 0 | 1 | 48 | 50 | protected flash |
| 0 | 2 | 52 | 51 | protected flash |
| 1 | 0 | 50 | 48 | protected flash |

The numeric rows are illustrative. Production rows include exact protected
flash provenance and any source digest/version.

## Requested configuration bytes

`/configuration/requested_janus` is a one-dimensional `uint8` dataset, not a
table. It preserves comments, whitespace, CRLF/LF choice, ordering, units,
overrides, and final-newline state exactly. Conceptually, it decodes to text
such as:

```text
Open[0] usb:172.16.0.11:tdl:0:0
Open[1] usb:172.16.0.11:tdl:1:0
Open[2] usb:172.16.0.11:tdl:2:0
Open[3] usb:172.16.0.11:tdl:3:0

AcquisitionMode SPECT_TIMING
TriggerLogic MAJ64
MajorityLevel 4
GainSelect BOTH
TrefSource TLOGIC
TrefWindow 1.0 us
TrefDelay -500 ns
EnableToT 0
```

Suggested dataset attributes are:

| Attribute | Example |
| --- | --- |
| `content_type` | `text/plain` |
| `encoding` | `UTF-8` |
| `length_bytes` | 12,483 |
| `sha256` | exact source-byte digest |

The separate audit and effective tables explain how this requested source was
interpreted and what was actually planned/applied.
