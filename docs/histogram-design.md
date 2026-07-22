# Live histogram architecture

## Boundary

Histograms belong to one active run and are accumulated in its bounded pipeline
session after successful event decoding. They are not embedded in the periodic
telemetry snapshot: a four-board, 64-channel, multi-spectrum payload would make
every health update unnecessarily large. The frontend instead calls the typed
`GetHistograms` run API with an exact active run ID, histogram family, and 1–64
board/node/channel selections.

The first implemented families are PHA high gain, PHA low gain, time of arrival
(ToA), and time over threshold (ToT). `EHistoNbin` and `ToAHistoNbin` determine
the server allocation; `DISABLED` prevents accumulation and requests fail
explicitly. ToT uses its native 512-value domain. Arrays are allocated lazily on
the first qualifying hit, so quiet channels cost no bin storage.

## Data and concurrency model

Each accumulator stores fixed minimum/bin-width metadata, unsigned 64-bit bins,
entries, underflow, and overflow. PHA maps the 14-bit ADC domain into the
configured energy bins. ToA maps the decoded 25-bit domain into its configured
bins, and ToT maps the 9-bit domain exactly. Updates share the session's event
accounting lock. Requests copy selected arrays while holding that lock, so API
responses cannot alias or race live acquisition memory.

The endpoint accepts only the active run identity and validates histogram kind,
hardware coordinates, and a maximum of 64 datasets. A completed run's histogram
memory is released with its session; durable histogram artifacts and historical
queries are a later milestone.

## Frontend contract

The plot workspace selects a family, board, and comma/range channel expression
(for example `0, 2, 8-15`). It can request on demand or once per second. Stale
responses from older selections/runs are discarded. uPlot renders selected
channels as stepped overlays with cursor inspection, horizontal drag-to-zoom,
linear/logarithmic Y scales, responsive sizing, and both application themes.
One-second data updates preserve the operator's current zoom. Metadata and a
short populated-bin preview remain available beside the canvas.

## Next extensions

- Apply the exact JANUS `ToARebin` and `ToAHistoMin` calibration semantics.
- Add MCS/time-series and waveform data contracts, which are not fixed-bin
  channel histograms.
- Add explicit clear/freeze semantics and multi-trace presentation state.
- Persist finalized histograms and expose bounded historical-run queries.
- Extend the benchmarked uPlot renderer with explicit reset-zoom and trace
  visibility controls as more plot families are introduced.
