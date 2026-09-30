# Live histogram architecture

## Boundary

Histograms belong to one active run and are accumulated in its bounded pipeline
session after successful event decoding. They are not embedded in the periodic
telemetry snapshot: a four-board, 64-channel, multi-spectrum payload would make
every health update unnecessarily large. The frontend instead calls the typed
`GetHistograms` run API with an exact run ID, histogram family, and 1–65
channel/board-sum selections. The active run is copied from its synchronized
accumulator; completed runs are read from their finalized histogram artifact.

The implemented families are PHA high gain, PHA low gain, time of arrival
(ToA), and time over threshold (ToT). `EHistoNbin` and `ToAHistoNbin` determine
the server allocation; `DISABLED` prevents accumulation and requests fail
explicitly. ToT uses its native 512-value domain. Arrays are allocated lazily on
the first qualifying hit, so quiet channels cost no bin storage.

## Data and concurrency model

Each accumulator stores fixed minimum/bin-width metadata, unsigned 32-bit bins,
entries, underflow, and overflow. PHA maps the configured ADC domain into the
energy bins: `Range_14bit 0` uses the default 13-bit range (0–8191), while
`Range_14bit 1` uses the full 14-bit range (0–16383). ToA maps the decoded
ticks with the source-confirmed JANUS rule: one tick is 0.5 ns, the configured
`ToAHistoMin` is subtracted, and the result is integer-divided by `ToARebin`.
The ToA bin width is therefore `0.5 * ToARebin` ns. ToT maps the 9-bit domain
exactly. A bin at `2^32-1` saturates instead of wrapping; subsequent hits are
accounted in overflow while total entries remain unsigned 64-bit. Updates share
the session's event accounting lock. Requests copy selected arrays
while holding that lock, so API responses cannot alias or race live acquisition
memory.

The endpoint validates the run identity, histogram kind, hardware coordinates,
and a maximum of 65 datasets covering at most 256 distinct channel spectra.
Missing quiet-channel datasets in a persisted
artifact are returned as zero-filled arrays using the recorded run-time axis
configuration. When the run-control
persistence option is enabled in an HDF5 production build, the drained snapshot
is written once to `run_<run-id>.histograms.h5`, hashed, and cataloged as a run
artifact. It is independent of the 500 MiB decoded-event segments.

## Frontend contract

Each board has a separate **Board sum (all 64 channels)** selection alongside
the 64 individual channels, supported for every histogram family and both live
and historical runs. This is a bin-by-bin spectrum sum, not an event-by-event
sum of channel energies. The selection uses `board_sum=true` and channel=0;
the response identifies it with `board_sum` and carries `board_sum_bins` as
uint64 instead of the individual spectrum's uint32 `bins`. Entries, underflow,
and overflow are also summed; inconsistent axes or uint64 counter overflow fail
explicitly. Existing individual requests and their wire fields are unchanged.

The backend expands sums into all 64 channels, deduplicates overlapping selections,
loads one coherent snapshot, and returns the extra aggregate dataset. Quiet
channels contribute zero. Chain AND node identify a board. The sum is independent
of which channels the operator selected for plotting. Per-channel bin saturation
still applies; excess hits already recorded as overflow are not reconstructed.
The maximum summed bin (64 × (2^32−1)) is exactly representable for browser plotting.

HDF5 files retain their existing per-channel layout: board-sum API datasets are
derived on the server from stored spectra on demand, so older runs work without
migration or rewriting and there is no redundant persisted copy. The browser
only plots the returned dataset; it never adds the channel spectra itself.

The plot workspace selects the live run or any completed run with a histogram
artifact, then a family, board, and channel set. Live data can be requested on
demand or once per second; historical data is read on demand. Stale responses
from older selections/runs are discarded. uPlot renders selected
channels as bar overlays with cursor inspection, horizontal drag-to-zoom,
linear/logarithmic Y scales, responsive sizing, and both application themes.
One-second data updates preserve the operator's current zoom. Metadata reports
the bin width, populated-bin count, and peak count beside the canvas.

## Next extensions

- Add MCS/time-series and waveform data contracts, which are not fixed-bin
  channel histograms.
- Add explicit clear/freeze semantics and multi-trace presentation state.
- Extend the benchmarked uPlot renderer with explicit reset-zoom and trace
  visibility controls as more plot families are introduced.
