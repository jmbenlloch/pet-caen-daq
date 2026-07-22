# JANUS GUI configuration and feature audit

Audit date: 2026-07-21. Reference: bundled JANUS 5202 5.0.0 (`bin/param_defs.txt`,
`gui/tabs.py`, `gui/cfgfile_rw.py`, and the production configuration fixture).

## Scope and status vocabulary

JANUS builds most controls from `param_defs.txt`. A parameter has global (`g`),
board (`b`), or channel (`c`) scope and is rendered as a combo, checkbox, or text
entry. Board and channel values are exceptions: an empty cell inherits the global
value. Board exceptions are serialized as `Name[board]`; channel exceptions as
`Name[board][channel]`. JANUS additionally creates live controls for connection,
HV, register access, statistics, logging, masks, jobs, and date selection.

This report uses four implementation states:

- **Complete**: the web UI exposes the value with the required scope and the
  backend applies its intended effect.
- **Partial**: it is parsed/editable or audited, but an important JANUS behavior,
  specialized control, or runtime effect is absent.
- **Inactive by design**: accepted with an explicit replacement or inactive
  reason; it does not reproduce JANUS output/analysis behavior.
- **Missing**: absent from the accepted schema, UI, or runtime.

The frontend parses the checked-in `param_defs_5.0.0.txt` into a typed catalog
and merges it with the loaded configuration. Catalog scope, choices, ranges,
units, and activation dependencies therefore remain available even when the
sample comments omit them. Masks get a 64-channel editor, board fields get four
effective values and overrides, and channel fields get a 4 x 64 exception
editor. A parameter still needs an assignment in the configuration document to
appear in the editor; catalog-only defaults are not synthesized.

## Summary

All 96 named parameters/monitors in the bundled definition are accounted for
below, along with the controls created directly by the Python GUI. The strongest
coverage is the DT5202 hardware path: acquisition, trigger,
discriminator, gain, shaping, probe, test-pulse, HV, per-board and per-channel
settings are translated to native FPGA/Citiroc/HV operations and verified. The
largest gaps are JANUS job scheduling, event building, advanced/historical
plot families, JANUS output formats/rotation, raw register access, and the
full historical log experience.

High-priority frontend gaps after the current milestones are:

1. Add dependent enable/disable behavior (mode-specific fields, test-pulse and
   probe dependencies, jobs, output-size controls).
2. Decide whether run-control/output/analysis options will be implemented or
   rejected instead of appearing operational merely because they parse.

## Connect tab

| Element | JANUS control and effect | Scope/range | pet-caen-daq status |
|---|---|---|---|
| `Open` | Per-board path entry plus sequential enable checkbox; selects USB/ETH/TDL connection and displays PID, model, FPGA FW, and uC FW after connection. | Board; non-empty paths enable consecutive boards. | **Partial.** Indexed paths are parsed and topology discovery validates four TDL links. Discovered board/model/FW and health appear in telemetry. The web form still exposes raw path text rather than a dedicated topology editor, and it has no sequential enable workflow or uC FW column. |

## HV_bias tab

| Parameter/element | JANUS control and effect | Scope/range | pet-caen-daq status |
|---|---|---|---|
| `HV_Vbias` | Base detector bias; global default with a value per board. | Board, 20–85 V. | **Complete for configuration.** Range stepper, four effective board values, inheritance/editor, native HV plan. |
| `HV_Imax` | HV current trip limit. | Board, current with units. | **Complete for configuration.** Four-board editor and native HV plan; frontend has a lower bound but JANUS provides no explicit upper bound. |
| `HV_Adjust_Range` | Selects the per-channel DAC full scale. | Global combo: 4.5, 2.5, DISABLED. | **Complete.** Select and Citiroc/HV translation. |
| `HV_IndivAdj` | Per-channel DAC trim. | Channel, integer 0–255. | **Complete for configuration.** 4 x 64 editor and exact Citiroc channel setting; main row reports non-zero exception counts. |
| `Vnom` | Read-only estimated channel voltage, calculated from board bias, trim range, and `HV_IndivAdj`. | Channel monitor. | **Complete.** The 4 x 64 adjustment editor shows each effective nominal voltage and updates it immediately as bias/range/trim values change. |
| `TempSensType` | Chooses detector temperature conversion. | Global combo: TMP37, LM94021_G11, LM94021_G00 (JANUS also describes generic coefficients). | **Complete.** Known sensors are suggested and a custom `c0 c1 c2` polynomial is accepted and translated to the HV plan. |
| `TempFeedbackCoeff` | Temperature compensation coefficient for bias. | Global float, mV/°C. | **Complete.** Native HV plan applies it. |
| `EnableTempFeedback` | Enables temperature feedback. | Global boolean. | **Complete.** |
| Global/board HV switches and LEDs | Commands all boards or one board on/off; grey/yellow/green/red represents off/ramping/on/fault. Disabled while acquiring. | Live action, board/global. | **Complete.** Guarded native controls support each board or all boards, require backend HV authorization for ON, roll back a partial all-board enable, and are locked outside Ready. Off/ramping/on/fault state is continuously displayed. |
| `Vmon`, `Imon`, detector/HV/FPGA/board temperatures | Live per-board service values. | Read-only monitors. | **Complete for firmware 4+.** Direct monitor registers are polled while idle or running and all six values are streamed in complete telemetry snapshots. |

## RunCtrl tab

| Parameter/element | JANUS control and effect | Scope/options | pet-caen-daq status |
|---|---|---|---|
| `StartRunMode` | Selects asynchronous, TDL, external-run, GPS, or chain start synchronization. | Global combo: ASYNC, TDL, TDL_EXTRUN, TDL_GPS, CHAIN_T0, CHAIN_T1. | **Partial.** The sample subset is selectable and audited, but the coordinator uses its fixed synchronized native start sequence rather than dispatching every JANUS mode. |
| `ExtRunSource` | Selects the DT5215 external-run input. | Global combo: SYNC-IN, LEMO_RA/RB/FA/FB. | **Missing.** Not in the sample or accepted catalog. |
| `GPSTimeUTC` + Calendar | UTC timestamp for GPS start; Calendar opens a date/time picker. | Global ISO-8601 string. | **Missing.** |
| `StopRunMode` | Manual, elapsed-time, or event-count stop policy. | Global combo. | **Complete.** A dedicated run-control selector configures the policy; the backend monitors authoritative elapsed time/event totals and invokes normal stop/drain/finalization automatically. Manual stop remains available in every mode. |
| `EventBuildingMode` | Disables building or sorts/groups by trigger timestamp or trigger ID. | Global combo. | **Partial.** Parsed/audited; no JANUS online event builder. Events are persisted in received order. |
| `TstampCoincWindow` | Coincidence window for timestamp event building. | Global float with time unit. | **Partial.** Range input exists; inactive when building is disabled and no builder consumes it. |
| `PresetTime` | Duration used by PRESET_TIME. | Global positive time. | **Complete.** Dedicated seconds input plus unit-aware source parsing (`s`, `ms`, `us`, `ns`); zero/invalid values are rejected before hardware start. Completion is recorded as `preset_time`. |
| `PresetCounts` | Count used by PRESET_COUNTS. | Global positive integer. | **Complete.** Dedicated bounded integer input; the backend stops when persisted decoded-event count reaches the target and records `preset_counts`. |
| `JobFirstRun` | First run number in a job. | Global integer. | **Partial.** Editable/audited; no scheduler. |
| `JobLastRun` | Last run number in a job. | Global integer. | **Partial.** Editable/audited; no scheduler. |
| `RunSleep` | Delay between job runs. | Global time. | **Partial.** Editable/audited; no scheduler. |
| `EnableJobs` | Enables multi-run jobs and changes related GUI options. | Global boolean. | **Partial.** Checkbox only; no job execution or dependent UI behavior. |
| `RunNumber_AutoIncr` | Automatically increments JANUS run number. | Global boolean. | **Partial.** Runs use explicit string IDs supplied by the web form. |
| Reset Job button | Sends JANUS command `j` and is active when jobs are enabled. | Live action. | **Missing.** |
| `DataAnalysis` | Enables all online analysis, counters only, or disables analysis. | Global combo: ALL, CNT_ONLY, DISABLED. | **Inactive by design.** Decoded events are persisted without JANUS's online analysis pipeline. |
| `DataFilePath` + Browse | Chooses JANUS output directory. | Global path. | **Inactive by design.** The service/runstore owns its root; the imported Windows path is explicitly ignored. No browser picker is appropriate for a server path. |
| `OF_OutFileUnit` | Chooses LSB or ns for JANUS text/list timing values. | Global combo. | **Inactive by design.** Current JSON output has defined typed units and no enabled JANUS text consumer. |
| `OF_EnMaxSize` | Enables maximum list-file size/rotation. | Global boolean. | **Inactive by design.** No JANUS-style rotation. |
| `OF_MaxSize` | Maximum list-file size, minimum 1 MB. | Global byte quantity. | **Inactive by design.** No JANUS-style rotation; frontend lacks a byte-unit range constraint. |
| `OF_RawData` | Writes JANUS raw event list. | Global boolean. | **Partial/replaced.** The run request has an independent `captureRaw` switch and writes the project `wire.raw` format; the config checkbox does not control it. |
| `OF_ListBin` | Writes JANUS binary list. | Global boolean. | **Inactive by design.** Replaced by JSON Lines decoded events. |
| `OF_ListAscii` | Writes ASCII list. | Global boolean. | **Inactive by design** when off; no ASCII writer when on. |
| `OF_ListCSV` | Writes CSV list. | Global boolean. | **Inactive by design** when off; no CSV writer when on. |
| `OF_Sync` | Writes board/timestamp/trigger-ID synchronization list. | Global boolean. | **Inactive by design** when off; no separate sync file when on. |
| `OF_ServiceInfo` | Writes temperature, Vmon, Imon, and HV status events. | Global boolean. | **Inactive by design** when off; no JANUS service-info file when on. |
| `OF_RunInfo` | Writes run metadata. | Global boolean. | **Partial/replaced.** A richer manifest is always finalized; this flag does not gate it. |
| `OF_SpectHisto` | Writes PHA spectrum. | Global boolean. | **Inactive by design.** No online histogram pipeline. |
| `OF_ToAHisto` | Writes ToA histogram. | Global boolean. | **Inactive by design.** |
| `OF_ToTHisto` | Writes ToT histogram. | Global boolean. | **Inactive by design.** |
| `OF_MCS` | Writes MCS spectrum. | Global boolean. | **Inactive by design.** |
| `OF_Staircase` | Writes discriminator staircase. | Global boolean. | **Inactive by design.** |

## AcqMode tab

| Parameter | JANUS control and effect | Scope/range/options | pet-caen-daq status |
|---|---|---|---|
| `AcquisitionMode` | Selects payload/acquisition behavior. | Global: SPECTROSCOPY, SPECT_TIMING, TIMING_CSTART, TIMING_CSTOP, COUNTING, WAVEFORM. | **Complete for hardware configuration and native decoding paths covered by tests.** The web form is a select. Some less-used qualifier/mode combinations still have less real-hardware evidence than spectroscopy/timing. |
| `EnableToT` | Chooses 16-bit ToA + 9-bit ToT versus 25-bit ToA in timing mode. | Global boolean. | **Complete.** |
| `EnableListZeroSuppr` | Suppresses zero-hit timing events in JANUS output lists. | Global boolean. | **Partial.** Parsed as analysis policy; current persistence does not implement this list filter. |
| `BunchTrgSource` | Selects bunch trigger source. | Global combo: T0-IN, T1-IN, Q-OR, T-OR, TLOGIC, PTRG. | **Complete.** Native register mask. |
| `VetoSource` | Selects active-high trigger veto source. | Global combo. | **Complete.** |
| `ValidationSource` | Selects validation input. | Global combo: SW_CMD, T0-IN, T1-IN. | **Complete.** |
| `ValidationMode` | Disabled, accept-on-validation, or reject-on-validation. | Global combo. | **Complete.** |
| `CountingMode` | Independent singles or paired-channel coincidence. | Global: SINGLES, PAIRED_AND. | **Complete.** |
| `ChTrg_Width` | Paired-AND coincidence width. | Global time, 8–2032 ns in 8 ns steps. | **Complete.** Range/step keyboard control and FPGA ticks. |
| `EnableCntZeroSuppr` | Omits zero-count channels in counting output. | Global boolean. | **Complete at hardware-output configuration.** |
| `TrgIdMode` | Trigger ID counts all triggers or validation signals. | Global combo. | **Complete.** |
| `TriggerLogic` | Selects 64-channel combinatorial trigger topology. | Global combo: OR64, AND2_OR32, OR32_AND2, OR16_AND4, MAJ64, MAJ32_AND2 (production also uses OR_QUAD). | **Complete for backend choices.** The current sample exposes its option subset; definition/sample drift should be eliminated. |
| `Tlogic_Width` | Trigger-logic output width; zero is linear. | Global time, 8 ns hardware ticks. | **Complete.** |
| `MajorityLevel` | Multiplicity for majority modes. | Global integer 1–64. | **Complete.** |
| `PtrgPeriod` | Internal periodic trigger period. | Global time. | **Complete.** |
| `TrefSource` | Reference source for common-start/stop timing. | Global combo. | **Complete.** |
| `TrefWindow` | Timing reference gate. | Global time, sampled in 8 ns steps. | **Complete in backend.** Frontend does not yet encode a dedicated range/step constraint. |
| `TrefDelay` | Signed timing-reference delay. | Global signed time, 8 ns steps; hardware signed 20-bit ticks. | **Complete in backend**, including capture-verified signed packing. Frontend lacks its exact signed range/step constraint. |
| `T0_Out` | Routes a selected internal/external signal to T0 output. | Global combo. | **Complete.** |
| `T1_Out` | Routes a selected internal/external signal to T1 output. | Global combo. | **Complete.** |
| `ChEnableMask0`, `ChEnableMask1` | The first setting enables acquisition channels 0–31 and the second enables 32–63. JANUS edits the pair in one popup with Global/board selection, 64 toggles, enable/disable all, and optional pixel-map layout. | Board, two 32-bit halves. | **Complete except pixel-map layout.** Four effective board masks, inheritance, 64 toggles, enable/disable all, invert, and low/high serialization are present. |

## Discr tab

| Parameter | JANUS control and effect | Scope/range/options | pet-caen-daq status |
|---|---|---|---|
| `FastShaperInput` | Chooses HG or LG preamplifier for the fast shaper. | Global: HG-PA, LG-PA. | **Complete.** |
| `TD_CoarseThreshold` | Common timing discriminator threshold for a board; global default plus board exceptions. | Board integer 0–2047. | **Complete.** Four values are visible and editable with inheritance/range enforcement. |
| `TD_FineThreshold` | Individual timing discriminator trim. | Channel integer 0–15. | **Complete.** 4 x 64 editor, non-zero counts, exact channel programming. |
| `Hit_HoldOff` | Imposes discriminator dead time. | Global time. | **Complete in backend.** Frontend lacks a specific maximum/step derived from hardware. |
| `Tlogic_Mask0`, `Tlogic_Mask1` | Select channels 0–31 and 32–63 respectively for timing trigger logic; JANUS edits them as one mask. | Board, two 32-bit halves. | **Complete except JANUS pixel-map layout.** |
| `QD_CoarseThreshold` | Common charge discriminator threshold for all boards/channels. | Global integer 0–2047. | **Complete.** JANUS 5.0 defines this as global, unlike TD coarse. |
| `QD_FineThreshold` | Individual charge discriminator trim. | Channel integer 0–15. | **Complete.** |
| `Q_DiscrMask0`, `Q_DiscrMask1` | Select channels 0–31 and 32–63 respectively for Q-OR; JANUS edits them as one mask. | Board, two 32-bit halves. | **Complete except JANUS pixel-map layout.** |

## Spectroscopy tab

| Parameter | JANUS control and effect | Scope/range/options | pet-caen-daq status |
|---|---|---|---|
| `GainSelect` | Selects HG, LG, automatic, or both in output data. | Global combo. | **Complete.** |
| `HG_Gain` | High-gain preamplifier setting. | Channel integer 1–63. | **Complete.** |
| `LG_Gain` | Low-gain preamplifier setting. | Channel integer 1–63. | **Complete.** |
| `Pedestal` | Common pedestal and basis for calibration-dependent zero suppression. | Global integer 0–16383 in this implementation. | **Complete.** Applied through the pedestal calibration stage before audit finalization. |
| `ZS_Threshold_LG` | Per-channel LG energy zero-suppression threshold. | Channel integer 0–65535. | **Complete when spectroscopy is active; explicitly inactive otherwise.** |
| `ZS_Threshold_HG` | Per-channel HG energy zero-suppression threshold. | Channel integer 0–65535. | **Complete when spectroscopy is active; explicitly inactive otherwise.** |
| `HG_ShapingTime` | High-gain slow-shaper peaking time. | Global select: 12.5–87.5 ns. | **Complete.** |
| `LG_ShapingTime` | Low-gain slow-shaper peaking time. | Global select: 12.5–87.5 ns. | **Complete.** |
| `HoldDelay` | Delay from bunch trigger to peak-detector hold. | Global time. | **Complete in backend.** Frontend lacks a specific range/8 ns step. |
| `MuxClkPeriod` | Analog multiplexer readout period. | Global time; JANUS recommends 300 ns. | **Complete in backend.** Frontend lacks a dedicated legal-range constraint. |
| `EHistoNbin` | PHA histogram bin count. | Global combo: disabled, 256–8K. | **Complete for live accumulation.** The active run lazily stores bounded HG/LG arrays per board/channel and selected sets are requestable. Durable histogram artifacts remain missing. |
| `ToAHistoNbin` | ToA histogram bin count. | Global combo: disabled, 256–16K. | **Complete for the initial live domain.** Per-channel ToA arrays are accumulated and requested independently of telemetry. |
| `ToARebin` | ToA histogram rebin factor. | Global integer. | **Partial.** Editable/audited, but the initial accumulator maps the full decoded 25-bit domain and does not yet apply JANUS rebinning. |
| `ToAHistoMin` | ToA histogram lower edge. | Global time. | **Partial.** Editable/audited but not yet applied to the initial live domain. |
| `MCSHistoNbin` | Counting-mode MCS bin count. | Global combo: disabled, 256–16K. | **Inactive by design.** |

## Test-Probe tab

| Parameter | JANUS control and effect | Scope/range/options | pet-caen-daq status |
|---|---|---|---|
| `AnalogProbe0` | Routes OFF/FAST/SLOW_LG/SLOW_HG/PREAMP_LG/PREAMP_HG to probe 0. | Global combo. | **Complete.** |
| `DigitalProbe0` | Routes an internal digital signal to probe 0. | Global combo. | **Complete with firmware-aware packing/validation.** |
| `ProbeChannel0` | Selects probe channel in first half. | Global integer 0–31. | **Complete.** Inactive reason recorded when analog probe 0 is off. |
| `AnalogProbe1` | Analog routing for probe 1. | Global combo. | **Complete.** |
| `DigitalProbe1` | Digital routing for probe 1. | Global combo. | **Complete with firmware-aware packing/validation.** |
| `ProbeChannel1` | Selects probe channel in second half. | Global integer 32–63. | **Complete in backend.** The sample's value `0` conflicts with JANUS 5.0's documented 32–63 range and should be corrected; the frontend currently advertises 0–63. |
| `TestPulseSource` | Chooses off, external, T0, T1, periodic, or software command source. | Global combo. | **Complete.** |
| `TestPulseAmplitude` | Internal pulser DAC amplitude. | Global integer 0–4095. | **Complete.** Explicitly inactive/effective zero when source is OFF. |
| `TestPulseDestination` | Connects none/all/even/odd or one channel to test pulse. | Global combo. | **Complete.** |
| `TestPulsePreamp` | Feeds LG, HG, or both preamps. | Global combo. | **Complete.** |

## Regs tab (live expert tool, not configuration-file parameters)

| Element | JANUS effect | pet-caen-daq status |
|---|---|---|
| COMM / INDIV / BCAST address class | Builds common `0x01`, individual-channel `0x02`, or broadcast `0x03` register addresses. | **Missing from web UI.** Backend has typed native register operations internally, intentionally not an unrestricted operator endpoint. |
| Channel 0–63 and Board selector | Selects individual register channel and target board. | **Missing as an expert UI.** |
| Offset / full address / data | Accepts raw register address and 32-bit data. | **Missing intentionally pending a safety/authorization design.** |
| Read / Write | Sends raw register read/write commands and logs results. | **Missing.** Read-only hardware inspection exists through a constrained backend path, not an arbitrary register console. |
| Command + Send Cmd | Writes a raw DT5202 command register. | **Missing intentionally; unrestricted commands can mutate acquisition/HV state.** |
| Register log | Shows read/write/command history. | **Missing.** Configuration/run audit and diagnostics are the safer partial replacement. |

## Statistics tab (live runtime view)

| Element | JANUS effect | pet-caen-daq status |
|---|---|---|
| Board selector + 64 channel cells | Displays per-channel live rate/count/statistic for one board. | **Complete.** Each active board has a 64-cell view. Trigger statistics accumulate discriminator/timing hits or hardware counter values; timestamp statistics accumulate timestamp-bearing channel hits; PHA statistics accumulate decoded channel energies. |
| All Boards Statistics | Switches to a board table containing timestamp, trigger ID, trigger rate, lost trigger %, event-build %, and data rate. | **Complete.** Cumulative acquisition evidence supplies every table column and updates with the telemetry cadence. |
| Integral | Switches statistics between interval and integrated mode and commands JANUS (`I0/I1`). | **Complete in equivalent form.** The switch selects cumulative counts or deltas/rates from consecutive snapshots without resetting acquisition counters. |
| Dynamic global statistics | JANUS creates labels/values announced by the DAQ process. | **Complete for project runtime metrics.** Decoded events, accepted/rejected batches, persisted size, and elapsed run time are shown from typed telemetry. |

## Log tab

| Element | JANUS effect | pet-caen-daq status |
|---|---|---|
| Colored scrolling log | Displays normal, warning, error, empty, and verbose DAQ messages. | **Partial.** The web UI exposes structured warnings/errors and command failures, but no complete scrollable historical log or verbosity filter. Backend process logs remain external. |

## Main control bar and auxiliary dialogs

These controls are constructed by `gui/ctrl.py`, not `param_defs.txt`, but are
part of the operator-facing JANUS GUI and affect acquisition or analysis.

| Element | JANUS effect | pet-caen-daq status |
|---|---|---|
| Connect/plug | Connects the Python GUI to JanusC and the configured FERS boards. | **Replaced.** The web client continuously connects to the service; backend discovery owns hardware connection. Live/stale state is visible. |
| Start | Saves/applies configuration as needed and starts the selected run. | **Complete for the project's run model.** Start validates, configures, authorizes HV as required, synchronizes, starts, and persists a run. |
| Stop | Sends the JANUS stop command. | **Complete.** Stop/drain/finalization is idempotent and fault-aware. |
| Freeze | Freezes plot refresh without pausing acquisition. | **Complete in equivalent form.** Live refresh can be disabled while server accumulation continues. |
| Refresh plot | Requests a single plot refresh. | **Complete for histogram data.** The workspace requests the selected datasets on demand. |
| Clear | Clears histograms/statistics and restarts the JANUS run. | **Missing.** No online histogram/statistics accumulator exists. |
| Trace selector | Opens an 8-trace assignment window: trace slot, board, online/offline/browse source, run/file, channel, 8-channel octet, X calibration, and optional pixel-map layout. | **Partial.** The workspace selects an online board and up to 64 explicit/ranged channels. Offline files, calibration, trace slots, and pixel maps remain missing. |
| Staircase | Opens threshold-scan settings (board, min/max threshold, step, dwell), starts command `y`, and shows progress. | **Missing.** Configuration accepts staircase output policy but cannot execute a scan. |
| Hold-delay scan | Opens hold-delay sweep settings (board, min/max delay, 8 ns step, averaging points), starts command `Y`, and shows progress. | **Missing.** |
| Save configuration as | Writes current defaults and sparse exceptions to a selected JANUS file. | **Partial.** Configuration is preserved/submitted and source is editable, but there is no dedicated browser download/export action. |
| Save configuration for run | Associates a configuration file with a numeric job run. | **Missing.** There is no JANUS job-file scheduler. The finalized manifest always embeds requested/effective configuration evidence instead. |
| Load configuration | Loads a JANUS text file into all controls. | **Complete.** Browser file load plus reset-to-sample and backend-template load are present. |
| Binary-to-CSV converter | Selects JANUS binary files, optionally converts ToA/ToT to ns, optionally emits a file-name list, and invokes the converter. | **Missing.** Project artifacts use JSON Lines and have no JANUS binary writer/converter UI. |
| Run number spinbox | Selects numeric run 0–10000 and participates in jobs/file naming. | **Replaced.** Web runs use a required path-safe string run ID plus requester identity. |
| Plot Type | Selects PHA LG/HG, ToA, ToT, channel trigger rate, MCS, waveform, 2-D trigger/charge, staircase, or hold-delay plots. | **Partial.** Typed live datasets exist for PHA LG/HG, ToA, and ToT. The frontend intentionally has no plotting library yet; other families remain missing. |
| Statistics Type | Selects channel trigger rate/count, timestamp rate/count, or PHA rate/count. | **Complete.** Type selects the counter family and Integral selects count versus live interval rate. |
| Apply | Saves changed controls and applies configuration when state permits. | **Replaced.** Validate and Start are explicit; Start applies the validated configuration transactionally. There is no separate mutate-hardware-only action in the web UI. |
| Status/run LEDs and text | Shows JanusC connection/acquisition status and run activity. | **Complete in equivalent form.** The dashboard shows live/stale connection, system state, active run, sequence, and diagnostics. |
| External configuration macros | Adds/removes multiple macro config files, enables selected files, orders them, and appends their overrides. | **Missing.** Only one resolved configuration document is loaded. |
| Basic/advanced GUI selection | Persists a GUI mode and changes visible tabs/parameters/options. | **Missing.** Section/search filtering is available but is not semantic mode selection. |
| File menu | Duplicates load/save-as/macro actions and quits JANUS. | **Partial.** Load is present; export and macros are missing; closing a browser tab replaces application Quit. |
| Upgrade FPGA | Selects connected boards and a firmware file, performs upgrade, and reports per-board status/new revision. | **Missing intentionally.** Firmware mutation is not exposed by the service and needs a separate authenticated maintenance workflow. |
| Restore IP 192.168.50.3 | Sends the recovery command only when one board is connected over USB. | **Missing intentionally.** No firmware/network recovery endpoint. |
| Show warning pop-up | Enables JANUS modal warnings. | **Replaced.** Structured diagnostics and inline alerts do not block the operator with modal popups. |
| Verbose service event / socket messages | Selects one verbose diagnostic stream for the Log tab. | **Missing.** Backend log verbosity is deployment-controlled; service diagnostics are structured but not switchable from the web UI. |
| About | Displays JANUS release information. | **Partial.** Backend instance/status is visible, but the frontend has no dedicated build/version dialog. |

## Cross-cutting JANUS GUI behavior

| Behavior | JANUS effect | pet-caen-daq status |
|---|---|---|
| Global/board/channel inheritance | Empty scoped cells inherit the global default; config writer emits only exceptions. | **Complete for masks, numeric board settings, and seven numeric channel settings.** Generic board/channel scope is not yet definition-driven. |
| Basic/advanced mode | `gui_mode.txt` hides parameters/tabs and changes some option lists according to acquisition/job mode. | **Missing.** Search and section filters exist, but no curated basic/advanced dependency model. |
| Acquisition-state locking | JANUS enables/disables paths, HV switches, and controls according to disconnected/ready/ramping/running state. | **Partial.** Run actions have state safety, but parameter controls and all equivalent live widgets are not comprehensively locked. |
| Unit validation/conversion | JANUS accepts compatible time/voltage/current units and normalizes entry format. | **Partial.** Backend performs strict conversion. Frontend has good unit/range handling for a selected numeric catalog, but not every unit-bearing field. |
| Combo option source | JANUS uses `param_defs.txt`, sometimes dynamically narrowed by GUI mode. | **Partial.** The web UI extracts options from comments in the loaded configuration. |
| Pixel map | Reorders mask buttons using a channel-to-pixel mapping file. | **Missing.** The web mask always uses channel order. |
| Configuration load/save | Reads defaults and sparse board/channel exceptions, refreshes GUI, and writes a JANUS text file. | **Complete for load/edit/submit semantics.** Raw source editing remains available; browser-side file export is not currently a dedicated action. |

## Recommended implementation order

1. **Definition catalog:** translate the full JANUS 5.0 `param_defs.txt` into a
   checked-in typed catalog (scope, widget, options, units, min/max/step,
   dependencies) and test it against the upstream file.
2. **Truthful runtime semantics:** mark automatic stop, event building, jobs,
   output formats, and analysis as unsupported during validation until each has
   an actual consumer; do not report them as applied solely because they parse.
3. **Dependent controls:** mode-aware visibility and enabled state for counting,
   timing, spectroscopy, probes, test pulse, jobs, and output rotation.
4. **Expert diagnostics:** if required, implement an explicitly authorized,
   read-first register console with typed addresses and an immutable audit log;
   do not expose JANUS's unrestricted write/command interface by default.

## Known definition drift

- The bundled JANUS 5.0 definition includes `ExtRunSource`, `GPSTimeUTC`,
  `TDL_EXTRUN`, and `TDL_GPS`, while the committed production sample predates or
  omits them. They therefore do not appear in the current frontend.
- The project catalog includes `EnableServiceEvents`, which is used by the
  production hardware planner but is not present in this `param_defs.txt`
  snapshot; this is a compatibility extension and should be labeled as such.
- The production sample exposes `OR_QUAD`, while this JANUS definition lists
  `OR16_AND4`; the backend knows `OR_QUAD`. A versioned catalog must resolve this
  instead of silently taking options from whichever config comment was loaded.
- `ProbeChannel1` is documented as 32–63 by JANUS 5.0, but the production sample
  contains `0`. Current backend validation rejects that value only when probe 1
  is active; the sample and frontend constraint should be corrected.
