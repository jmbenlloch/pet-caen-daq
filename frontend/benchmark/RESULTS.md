# ECharts versus uPlot benchmark

Date: 2026-07-22. This file and all benchmark code/dependencies are deliberately
uncommitted pending the library decision.

## Environment and method

- ECharts 6.1.0, uPlot 1.6.32, Chromium from Playwright 1.61.1.
- Headless Chromium, 1280 x 720 viewport, 1200 x 600 Canvas chart.
- Deterministic histogram-like arrays with two peaks and baseline noise.
- 1, 8, 32, and 64 aligned channel overlays; 4K, 8K, and 16K bins.
- Seven complete data replacements, eight zoom changes, and twelve cursor moves.
- Animation disabled. ECharts uses modular Canvas line/dataZoom/tooltip/legend;
  uPlot uses aligned typed arrays and its native cursor/scale implementation.
- Every case uses a fresh page. Results were repeated; the table below is the
  refined final pass, which excludes synthetic data generation from chart
  update timing.

`Call` is synchronous time inside the chart update API. `Frame` includes two
animation-frame boundaries so deferred Canvas work can complete. Headless frame
scheduling makes absolute frame numbers approximate; relative differences and
the synchronous call measurements are more informative.

## Dark-theme results

| Channels x bins |    Points | ECharts init | uPlot init | ECharts update call/frame | uPlot update call/frame | ECharts heap delta | uPlot heap delta |
| --------------: | --------: | -----------: | ---------: | ------------------------: | ----------------------: | -----------------: | ---------------: |
|          1 x 4K |     4,096 |     252.9 ms |    47.7 ms |             7.6 / 33.1 ms |           0.0 / 33.0 ms |           23.0 MiB |          2.0 MiB |
|          8 x 4K |    32,768 |     251.7 ms |    47.9 ms |            15.3 / 32.3 ms |           0.0 / 31.7 ms |           28.9 MiB |          1.1 MiB |
|         32 x 8K |   262,144 |     309.4 ms |    74.7 ms |            61.1 / 74.0 ms |           0.0 / 23.2 ms |           57.4 MiB |          5.1 MiB |
|        64 x 16K | 1,048,576 |     400.3 ms |   114.8 ms |          222.1 / 240.2 ms |            0.1 / 8.2 ms |          105.0 MiB |         25.1 MiB |

The million-point ECharts update P95 was 276.9 ms; uPlot was 12.3 ms. Both are
below the planned one-second data-request cadence. Programmatic zoom/cursor
work completed within the next measured frame for both; synchronous worst-case
zoom calls were about 8.5 ms for ECharts and 0.1 ms for uPlot.

The light theme did not materially change the result. At 64 x 16K, ECharts
initialized in 439.4 ms and updated in 236.8 ms median; uPlot initialized in
96.6 ms and updated in 6.8 ms median. Heap deltas are noisy because garbage
collection timing is browser-controlled, but repeated passes consistently put
uPlot far below ECharts.

## Production payload

Independent Vite production entries were built to measure incremental library
cost rather than the existing application bundle.

| Import                                               | Minified |     Gzip |
| ---------------------------------------------------- | -------: | -------: |
| ECharts line + grid/legend/tooltip/dataZoom + Canvas | 548.6 kB | 184.4 kB |
| ECharts above + heatmap/visualMap                    | 593.1 kB | 198.2 kB |
| uPlot JavaScript                                     |  52.0 kB |  22.6 kB |
| uPlot CSS                                            |   1.6 kB |   0.7 kB |

Either library can be lazy-loaded so this payload does not block the main
operator dashboard.

## Interpretation

- uPlot is decisively faster and smaller for the current fixed-bin, aligned
  one-dimensional histogram contract.
- ECharts remains fast enough at a one-second refresh cadence and directly
  supplies heatmaps, visual maps, richer legends/tooltips, annotations, and a
  broader path to JANUS 2-D and analysis plots.
- Choosing uPlot likely means implementing more interaction/presentation code
  and selecting or building a separate renderer for heatmaps and other 2-D
  families later.
- Choosing ECharts spends roughly 175 kB more gzip and substantially more heap
  to obtain one coherent plotting system for the broader planned feature set.

No selection is made by this benchmark.
