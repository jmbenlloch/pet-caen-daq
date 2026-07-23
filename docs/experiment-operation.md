# How the PET CAEN acquisition experiment works

## Purpose and scope

This report describes the conceptual operation of the four-board PET data
acquisition system built from CAEN DT5202 boards behind a DT5215 concentrator.
It focuses on spectroscopy-with-timing acquisition: how detector pulses create
triggers, how energy and timing measurements are produced, and how the fields
in a decoded event should be interpreted.

The retained run `run-go-native-detector-hvon-003` is used as a concrete
example. It acquired 87,989 spectroscopy events from four DT5202 boards. Its
configuration selected `SPECT_TIMING`, both energy gains, a majority trigger,
and a TLOGIC timing reference. Only 26,277 events (29.9%) contained one or more
timing hits; 61,712 events (70.1%) contained no timing hits.

## System overview

Each detector sensor is connected to one DT5202 input channel. A detector pulse
is processed along several parallel paths:

```text
detector pulse
   |
   +-- slow shaping --> peak hold --> ADC --> energy measurement
   |
   +-- charge discriminator (QD) --> per-channel discriminator state
   |
   +-- time discriminator (TD) --> edge measurement --> TDC timing hit

enabled channel trigger signals
   --> trigger-logic network
   --> bunch trigger
   --> validation/veto decision
   --> accepted event
```

These paths are related but not identical. A channel can have an energy value
without crossing a discriminator threshold, and a charge-discriminator result
does not by itself prove that the channel caused the final board trigger. The
time discriminator and TDC also have their own threshold, mask, reference, and
acceptance window.

The four DT5202 boards send event data through TDlinks to the DT5215
concentrator. The DAQ decodes each DT5215 stream descriptor and retains its
chain, node, qualifier, trigger ID, timestamp, CRC indication, and decoded
DT5202 payload. Trigger IDs and timestamps are needed to establish ordering and
to study synchronization across boards.

## From a detector pulse to an accepted event

### Channel threshold crossings

The front-end creates fast discriminator signals from detector pulses. Two
discriminator concepts matter for spectroscopy-with-timing:

- The **charge discriminator (QD)** indicates whether a channel crossed its
  configured charge threshold. Its state is stored in the spectroscopy energy
  record as `discriminator`.
- The **time discriminator (TD)** produces an edge that can be measured by the
  TDC and can participate in channel trigger logic.

The coarse threshold sets a common level and the fine threshold trims
individual channels. Charge- and time-discriminator masks select which
channels participate in their respective paths. These are distinct from the
energy readout channel mask.

### Trigger formation

The board combines enabled channel trigger signals according to the configured
trigger logic. Possible concepts include an OR, grouped AND/OR combinations,
and a majority condition. The resulting signal can be selected as the bunch
trigger source.

The retained run used:

```text
BunchTrgSource  TLOGIC
TriggerLogic    MAJ64
MajorityLevel   4
Tlogic_Width    0
```

Conceptually, the majority network requires sufficient overlap among enabled
channel trigger inputs. `TLOGIC` then supplies the bunch trigger that starts the
spectroscopy event. The exact effect of `Tlogic_Width=0` is hardware-defined;
the configuration describes it as linear trigger-logic output width.

The trigger can be subjected to further decisions:

- `VetoSource` can inhibit it.
- `ValidationMode` can require or reject a signal from `ValidationSource`.
- `TrgIdMode` selects the counter used as the event trigger identifier.

Validation and veto were disabled in the retained run, so no additional
validation requirement was placed after initial trigger formation.

An accepted board event is not necessarily a complete multi-board PET event.
The current run has event building disabled. Correlation between boards must
therefore use synchronized timestamps, trigger identifiers, or a later event
building stage rather than assuming that adjacent file records belong to the
same physical interaction.

## Energy measurement

### Slow shaping and peak capture

In parallel with the fast discriminator paths, the analog pulse is amplified
and passed through slow shapers. The shaped pulse improves energy measurement
but reaches its peak after the original detector pulse. When a bunch trigger is
accepted, the board waits for the configured `HoldDelay`, holds the slow-shaper
outputs, and multiplexes enabled channels to the ADC.

The principal controls are:

- `HG_Gain` and `LG_Gain`: high- and low-gain amplification.
- `HG_ShapingTime` and `LG_ShapingTime`: pulse shaping, affecting noise,
  resolution, pile-up behavior, and peak time.
- `HoldDelay`: delay between the bunch trigger and peak hold.
- `MuxClkPeriod`: speed of multiplexed ADC readout.
- `GainSelect`: high gain, low gain, automatic selection, or both.
- `Pedestal`: requested baseline level.
- `ZS_Threshold_LG` and `ZS_Threshold_HG`: energy zero-suppression thresholds
  where supported by the selected acquisition mode.

The retained run used both gains, gain values of 55, shaping times of 87.5 ns,
a 300 ns hold delay, and a 300 ns multiplexer period. Consequently, every
included energy entry carries both low- and high-gain ADC values.

### Energy channel mask

The event-level `channel_mask` identifies channels for which an energy entry is
present in that event. It must not be interpreted as a bitmap of sensors that
caused the trigger.

In the retained run the mask is `0xffffffffffffffff`, so all 64 channels are
read out. A channel's energy may represent a real pulse, a coincident pulse,
baseline, noise, or unrelated activity. Its presence only means that an energy
measurement was included.

The configured `ChEnableMask` controls the enabled/readout channels. Depending
on acquisition mode and supported zero suppression, the event payload can use
a sparser channel mask.

### Charge-discriminator state

Each energy entry contains a `discriminator` boolean. In the DT5202 packed
spectroscopy format this comes from bit 15 of the channel energy word. The
bundled FERSlib reference accumulates these bits into a field named `qdmask`,
which establishes that this is the charge-discriminator state.

The practical interpretation is:

```text
discriminator = true
    the channel's charge discriminator was asserted for the event

discriminator = false
    an energy value was read out, but its charge-discriminator flag was not set
```

This normally means that the channel crossed its configured QD threshold, but
it does not mean that this channel alone generated the final trigger. Trigger
acceptance depends on source selection, enabled masks, trigger logic, timing
overlap, validation, and veto. Several channels may be asserted even though
only a subset was necessary to satisfy the trigger condition.

The relevant configuration includes `QD_CoarseThreshold`, per-channel
`QD_FineThreshold`, and the charge-discriminator masks. The retained run used a
QD coarse threshold of 250 and a fine threshold of zero.

## Timing and the TDC

### Producing a timing hit

The time discriminator operates on the fast timing path. When an enabled
channel crosses its TD threshold, it creates an edge that the time-to-digital
converter can timestamp. The TDC converts the relative time of that edge into a
numeric time-of-arrival value.

A timing hit is represented as:

```json
{"channel": 63, "toa": 861, "tot": 0}
```

The fields mean:

- `channel`: channel whose time-discriminator edge was measured;
- `toa`: encoded time of arrival relative to the hardware timing-reference
  convention;
- `tot`: encoded time over threshold when that measurement is enabled.

The raw integer values are retained because conversion scale and interpretation
depend on the timing mode and packed firmware format. Analysis software should
apply a documented, versioned conversion rather than assuming that a raw ToA
value is already expressed in nanoseconds.

### Timing reference and association window

Timing measurements need a reference so that channel edges can be associated
with the spectroscopy trigger. Three settings establish this association:

- `TrefSource` selects the timing-reference signal.
- `TrefWindow` defines the time interval in which channel edges are accepted.
- `TrefDelay` positions that interval relative to the reference and may be
  negative.

The retained run used:

```text
TrefSource  TLOGIC
TrefWindow  1.0 us
TrefDelay   -500 ns
```

Conceptually, the trigger-logic output acts as the event reference and the
window is positioned around it by the configured delay. Only TD edges accepted
by this timing association are emitted in the event's `timings` collection.

The event carries several distinct notions of time:

- The outer `timestamp` is the DT5215/DT5202 event timestamp used for stream
  ordering and synchronization.
- `time_reference` is the fine timing-reference word included in the combined
  spectroscopy/timing payload.
- Each `toa` is a channel-edge measurement relative to the timing-reference
  convention.

These values are related but are not interchangeable.

### Time over threshold

When `EnableToT` is active, the timing path can measure the duration for which
the discriminator output remains asserted. This time-over-threshold value can
provide pulse-size information and can support time-walk corrections. Enabling
ToT changes the packed allocation and range of the timing fields, so its state
must be stored with every run's configuration.

The retained run used `EnableToT 0`, meaning leading-edge timing only. Its
decoded timing records consequently have `tot=0`.

### Why an event may have no timing hits

An accepted spectroscopy event does not guarantee a TDC hit. In the retained
run, 70.1% of events have an empty timing collection. Conceptually relevant
causes include:

- QD and TD use different coarse or fine thresholds.
- Their enabled-channel masks can differ.
- A TD edge can fall outside the timing-reference window.
- `TrefDelay` can place the window away from a particular edge.
- Hit holdoff or timing-path dead time can suppress repeated edges.
- The charge and timing analog paths can respond differently to the same
  detector pulse.
- The board can accept a bunch trigger while no TD word satisfies the timing
  association conditions.

Therefore the following implications are not valid in general:

```text
accepted event       does not imply one or more timing hits
QD discriminator set does not necessarily imply a TDC hit
TDC hit present      does not necessarily imply the QD flag is set
```

The relevant timing configuration includes `TD_CoarseThreshold`, per-channel
`TD_FineThreshold`, TD masks, `Hit_HoldOff`, `TrefSource`, `TrefWindow`,
`TrefDelay`, and `EnableToT`. In the retained run the board-specific TD coarse
thresholds are approximately 178--183 and the fine thresholds are zero.

## A decoded spectroscopy event

An abbreviated event from the retained run is:

```json
{
  "sequence": "1",
  "payload": {
    "chain": 1,
    "node": 0,
    "qualifier": 51,
    "trigger_id": "0",
    "timestamp": "26865",
    "event": {
      "kind": "spectroscopy",
      "spectroscopy": {
        "channel_mask": "18446744073709551615",
        "energies": [
          {
            "channel": 0,
            "low_gain": 263,
            "high_gain": 2225,
            "has_low_gain": true,
            "has_high_gain": true,
            "discriminator": true
          }
        ],
        "timings": [
          {"channel": 63, "toa": 861, "tot": 0}
        ],
        "time_reference": 428870
      }
    }
  }
}
```

The complete event has 64 energy entries and 13 timing hits. Its qualifier is
51 (`0x33`), the captured combined spectroscopy/timing format with both gain
values. The event means:

1. Board `chain=1`, `node=0` emitted an accepted event.
2. The descriptor assigned trigger ID 0 and timestamp 26865.
3. All 64 channels have energy measurements.
4. Channel 0 has low-gain value 263 and high-gain value 2225.
5. Channel 0's charge-discriminator flag is asserted.
6. Thirteen channels, including channel 63, have accepted TDC measurements.
7. The timing payload includes reference value 428870.

It does not establish that channel 0 was solely responsible for the trigger,
that every energy entry is a physical hit, or that all four boards emitted a
corresponding event.

## Configuration groups that determine spectroscopy behavior

The following groups should be considered together when interpreting data.

### Trigger selection

- acquisition mode;
- bunch-trigger source;
- trigger-logic function, majority, masks, and output width;
- validation and veto source/mode;
- trigger-ID mode;
- start and synchronization mode.

### Energy response

- channel-enable masks;
- high- and low-gain settings;
- gain selection;
- high- and low-gain shaping times;
- hold delay and multiplexer period;
- pedestal calibration and zero-suppression thresholds.

### Discriminator response

- charge-discriminator coarse and per-channel fine thresholds;
- time-discriminator coarse and per-channel fine thresholds;
- separate QD, TD, and trigger-logic masks;
- fast-shaper input selection and channel-trigger width/holdoff.

### Timing association

- timing-reference source;
- reference window and delay;
- time-over-threshold enablement;
- timing mode and firmware format.

### Detector operating point

- common HV bias and current limit;
- per-channel HV adjustment;
- temperature feedback and calibration;
- board/ASIC calibration and firmware revision.

Changing the detector bias or analog gain changes pulse amplitudes and therefore
also changes the probability of crossing QD and TD thresholds. Thresholds
cannot be interpreted independently of the detector operating point.

## Recommended analysis model

Analysis should treat each accepted record as four related but distinct views:

```text
accepted event
|
+-- acceptance evidence
|     trigger ID, timestamp, source, trigger configuration
|
+-- amplitude measurements
|     channel mask, HG/LG energies, pedestal and calibration
|
+-- charge-threshold observations
|     one QD discriminator flag per included energy channel
|
+-- time-threshold observations
      zero or more TDC ToA/ToT measurements and a timing reference
```

Useful quality-control plots include per-channel energy spectra, QD assertion
rate, TDC-hit rate, ToA distribution, number of QD assertions per event, number
of timing hits per event, QD-versus-TD coincidence, inter-board trigger-ID and
timestamp agreement, pedestal stability, and rate versus temperature/HV.

The run configuration, effective hardware writes, calibration, topology,
firmware, and software version must accompany those measurements. Without that
context, changes in spectra or timing efficiency cannot be reliably attributed
to the detector rather than to a configuration or firmware change.

## Evidence and current limitations

The packed-field interpretation in this report follows the project decoder and
the bundled JANUS/FERSlib source. In particular, FERSlib identifies packed
energy bit 15 as the charge-discriminator mask, and the project decoder maps it
to `Energy.Discriminator`. The event counts and examples come directly from
the retained native run.

Some higher-level causal descriptions, such as the precise analog timing of
the trigger-logic majority relative to QD and TD signals, remain dependent on
firmware and hardware documentation. They should be validated with controlled
test pulses, digital-probe captures, and real-board timing scans before being
treated as hardware-verified. The DAQ must preserve the raw evidence and exact
configuration so those conclusions can be revisited.
