export {}

type Library = 'echarts' | 'uplot'
type Theme = 'dark' | 'light'
type BenchmarkResult = {
  library: Library
  theme: Theme
  series: number
  bins: number
  points: number
  initMs: number
  updateCallMedianMs: number
  updateMedianMs: number
  updateP95Ms: number
  zoomMedianMs: number
  zoomCallMedianMs: number
  cursorMedianMs: number
  heapDeltaMiB?: number
}

declare global {
  interface Window {
    runPlotBenchmark: (input: {
      library: Library
      theme: Theme
      series: number
      bins: number
    }) => Promise<BenchmarkResult>
  }
  interface Performance {
    memory?: { usedJSHeapSize: number }
  }
}

const chart = document.querySelector<HTMLDivElement>('#chart')!
Object.assign(document.body.style, {
  margin: '0',
  fontFamily: 'system-ui',
  background: '#09111f',
  color: '#e7edf7',
})
Object.assign(chart.style, { width: '1200px', height: '600px' })

function generate(seriesCount: number, bins: number, phase = 0) {
  const x = new Float64Array(bins)
  const series: Float64Array[] = []
  for (let bin = 0; bin < bins; bin++) x[bin] = bin
  for (let channel = 0; channel < seriesCount; channel++) {
    const values = new Float64Array(bins)
    const center1 = bins * (0.22 + (channel % 8) * 0.055)
    const center2 = bins * (0.68 + (channel % 5) * 0.025)
    for (let bin = 0; bin < bins; bin++) {
      const peak1 = 1200 * Math.exp(-((bin - center1) ** 2) / (2 * (bins * 0.012) ** 2))
      const peak2 = 420 * Math.exp(-((bin - center2) ** 2) / (2 * (bins * 0.025) ** 2))
      values[bin] = Math.max(
        0,
        Math.round(peak1 + peak2 + 8 + 5 * Math.sin(bin * 0.013 + channel + phase)),
      )
    }
    series.push(values)
  }
  return { x, series }
}

const palette = [
  '#35d49a',
  '#ef596f',
  '#5b8ff9',
  '#f6bd16',
  '#6dc8ec',
  '#9270ca',
  '#ff9d4d',
  '#269a99',
]
const settle = () =>
  new Promise<void>((resolve) =>
    requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
  )
const percentile = (values: number[], fraction: number) =>
  [...values].sort((a, b) => a - b)[
    Math.min(values.length - 1, Math.floor(values.length * fraction))
  ]
async function timed(action: () => void | Promise<void>) {
  const start = performance.now()
  await action()
  await settle()
  return performance.now() - start
}
async function timedSync(action: () => void) {
  const start = performance.now()
  action()
  const callMs = performance.now() - start
  await settle()
  return { callMs, settledMs: performance.now() - start }
}

window.runPlotBenchmark = async ({ library, theme, series: seriesCount, bins }) => {
  chart.replaceChildren()
  document.body.style.background = theme === 'dark' ? '#09111f' : '#eef3f8'
  document.body.style.color = theme === 'dark' ? '#e7edf7' : '#17243a'
  const initial = generate(seriesCount, bins)
  const heapBefore = performance.memory?.usedJSHeapSize
  let update: (data: ReturnType<typeof generate>) => void
  let zoom: (iteration: number) => void
  let cursor: (iteration: number) => void
  let dispose: () => void = () => undefined

  const initMs = await timed(async () => {
    if (library === 'echarts') {
      const echarts = await import('echarts/core')
      const { LineChart } = await import('echarts/charts')
      const { GridComponent, LegendComponent, TooltipComponent, DataZoomComponent } =
        await import('echarts/components')
      const { CanvasRenderer } = await import('echarts/renderers')
      echarts.use([
        LineChart,
        GridComponent,
        LegendComponent,
        TooltipComponent,
        DataZoomComponent,
        CanvasRenderer,
      ])
      const instance = echarts.init(chart, theme, { renderer: 'canvas', width: 1200, height: 600 })
      const makeSeries = (data: ReturnType<typeof generate>) =>
        data.series.map((values, index) => ({
          name: `CH ${index}`,
          type: 'line' as const,
          dimensions: ['value'],
          data: values,
          showSymbol: false,
          animation: false,
          progressive: 0,
          lineStyle: { width: 1, color: palette[index % palette.length] },
        }))
      instance.setOption(
        {
          animation: false,
          grid: { left: 65, right: 25, top: 35, bottom: 65 },
          legend: { show: seriesCount <= 8 },
          tooltip: { trigger: 'axis', animation: false },
          dataZoom: [
            { type: 'inside', xAxisIndex: 0 },
            { type: 'slider', xAxisIndex: 0 },
          ],
          xAxis: {
            type: 'category',
            data: Array.from(initial.x),
            axisLabel: { formatter: (value: string) => Number(value).toLocaleString() },
          },
          yAxis: { type: 'value', min: 0 },
          series: makeSeries(initial),
        },
        { notMerge: true },
      )
      update = (data) =>
        instance.setOption(
          { series: makeSeries(data) },
          { replaceMerge: ['series'], lazyUpdate: false },
        )
      zoom = (iteration) =>
        instance.dispatchAction({ type: 'dataZoom', start: 5 + iteration, end: 95 - iteration })
      cursor = (iteration) =>
        instance.dispatchAction({
          type: 'showTip',
          seriesIndex: 0,
          dataIndex: Math.floor((bins * iteration) / 20),
        })
      dispose = () => instance.dispose()
    } else {
      await import('uplot/dist/uPlot.min.css')
      const { default: uPlot } = await import('uplot')
      const options = {
        width: 1200,
        height: 600,
        cursor: { drag: { x: true, y: false, setScale: true } },
        scales: {
          x: { time: false },
          y: {
            range: (_: unknown, min: number, max: number) =>
              [Math.min(0, min), max] as [number, number],
          },
        },
        axes: [{}, {}],
        series: [
          { label: 'Bin' },
          ...Array.from({ length: seriesCount }, (_, index) => ({
            label: `CH ${index}`,
            stroke: palette[index % palette.length],
            width: 1,
            points: { show: false },
          })),
        ],
      }
      const instance = new uPlot(options, [initial.x, ...initial.series], chart)
      update = (data) => instance.setData([data.x, ...data.series], false)
      zoom = (iteration) =>
        instance.setScale('x', {
          min: bins * ((5 + iteration) / 100),
          max: bins * ((95 - iteration) / 100),
        })
      cursor = (iteration) =>
        instance.setCursor({ left: (instance.width * iteration) / 20, top: instance.height / 2 })
      dispose = () => instance.destroy()
    }
  })

  const updates: Array<{ callMs: number; settledMs: number }> = []
  for (let iteration = 0; iteration < 7; iteration++) {
    const next = generate(seriesCount, bins, iteration * 0.1)
    updates.push(await timedSync(() => update(next)))
  }
  const zooms: Array<{ callMs: number; settledMs: number }> = []
  for (let iteration = 0; iteration < 8; iteration++)
    zooms.push(await timedSync(() => zoom(iteration)))
  const cursors: Array<{ callMs: number; settledMs: number }> = []
  for (let iteration = 1; iteration <= 12; iteration++)
    cursors.push(await timedSync(() => cursor(iteration)))
  const heapAfter = performance.memory?.usedJSHeapSize
  const result: BenchmarkResult = {
    library,
    theme,
    series: seriesCount,
    bins,
    points: seriesCount * bins,
    initMs,
    updateCallMedianMs: percentile(
      updates.map((value) => value.callMs),
      0.5,
    ),
    updateMedianMs: percentile(
      updates.map((value) => value.settledMs),
      0.5,
    ),
    updateP95Ms: percentile(
      updates.map((value) => value.settledMs),
      0.95,
    ),
    zoomCallMedianMs: percentile(
      zooms.map((value) => value.callMs),
      0.5,
    ),
    zoomMedianMs: percentile(
      zooms.map((value) => value.settledMs),
      0.5,
    ),
    cursorMedianMs: percentile(
      cursors.map((value) => value.settledMs),
      0.5,
    ),
    heapDeltaMiB:
      heapBefore !== undefined && heapAfter !== undefined
        ? (heapAfter - heapBefore) / 1024 / 1024
        : undefined,
  }
  dispose()
  return result
}
