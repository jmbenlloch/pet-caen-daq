<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch, type DeepReadonly } from 'vue'
import uPlot, { type AlignedData, type Options, type Series } from 'uplot'
import 'uplot/dist/uPlot.min.css'
import type { HistogramDataset } from './gen/pet/caen/daq/v1/system_pb'

const props = defineProps<{
  datasets: readonly DeepReadonly<HistogramDataset>[]
  theme: 'dark' | 'light'
  logarithmic: boolean
}>()

const host = ref<HTMLElement>()
let plot: uPlot | undefined
let resizeObserver: ResizeObserver | undefined
let structure = ''

const colors = [
  '#3acb9f',
  '#5da9ff',
  '#f7b955',
  '#df73ff',
  '#ff6f87',
  '#65d8e8',
  '#b6df60',
  '#ff9565',
]

function alignedData(): AlignedData {
  const first = props.datasets[0]
  if (!first) return [[]]
  const x = first.bins.map((_, index) => first.minimum + (index + 0.5) * first.binWidth)
  return [
    x,
    ...props.datasets.map((dataset) =>
      dataset.bins.map((count) => {
        const value = Number(count)
        return props.logarithmic && value === 0 ? null : value
      }),
    ),
  ] as AlignedData
}

function datasetStructure() {
  return props.datasets
    .map(
      (dataset) =>
        `${dataset.chain}:${dataset.node}:${dataset.channel}:${dataset.minimum}:${dataset.binWidth}:${dataset.bins.length}`,
    )
    .join('|')
}

function series(): Series[] {
  return [
    {},
    ...props.datasets.map((dataset, index) => ({
      label: `B${dataset.chain} · CH ${dataset.channel}`,
      stroke: colors[index % colors.length],
      width: 1.5,
      paths: uPlot.paths.stepped!({ align: 1 }),
      points: { show: false },
      value: (_plot: uPlot, value: number | null) => (value == null ? '0' : value.toLocaleString()),
    })),
  ]
}

function options(): Options {
  const light = props.theme === 'light'
  const axis = light ? '#60748d' : '#8194ac'
  const grid = light ? '#d8e1eb' : '#24364c'
  return {
    width: Math.max(host.value?.clientWidth ?? 0, 320),
    height: 380,
    title: 'Accumulated channel histograms',
    series: series(),
    cursor: { drag: { x: true, y: false, setScale: true } },
    scales: {
      x: { time: false },
      y: props.logarithmic ? { distr: 3, range: (_plot, min, max) => [Math.max(1, min), max] } : {},
    },
    axes: [
      { label: 'Bin value', stroke: axis, grid: { stroke: grid, width: 1 } },
      { label: 'Counts', stroke: axis, grid: { stroke: grid, width: 1 } },
    ],
    legend: { show: true },
  }
}

function rebuild() {
  if (!host.value || !props.datasets.length) return
  plot?.destroy()
  host.value.replaceChildren()
  plot = new uPlot(options(), alignedData(), host.value)
  structure = `${props.theme}:${props.logarithmic}:${datasetStructure()}`
}

function render() {
  if (!host.value) return
  const nextStructure = `${props.theme}:${props.logarithmic}:${datasetStructure()}`
  if (!plot || structure !== nextStructure) rebuild()
  else plot.setData(alignedData(), false)
}

function resetZoom() {
  plot?.setData(alignedData(), true)
}

watch(() => props.datasets, render, { deep: true })
watch([() => props.theme, () => props.logarithmic], rebuild)

onMounted(() => {
  rebuild()
  resizeObserver = new ResizeObserver((entries) => {
    const width = Math.floor(entries[0]?.contentRect.width ?? 0)
    if (plot && width > 0) plot.setSize({ width: Math.max(width, 320), height: 380 })
  })
  if (host.value) resizeObserver.observe(host.value)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  plot?.destroy()
})
</script>

<template>
  <div class="histogram-plot-shell">
    <div class="histogram-plot-actions">
      <button type="button" class="secondary" @click="resetZoom">Reset zoom</button>
    </div>
    <div
      ref="host"
      class="histogram-plot"
      role="img"
      aria-label="Live selected-channel histogram plot"
    ></div>
  </div>
</template>
