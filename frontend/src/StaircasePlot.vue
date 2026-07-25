<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch, type DeepReadonly } from 'vue'
import uPlot, { type AlignedData, type Options } from 'uplot'
import 'uplot/dist/uPlot.min.css'
import type { StaircasePoint } from './gen/pet/caen/daq/v1/system_pb'

const props = defineProps<{
  points: readonly DeepReadonly<StaircasePoint>[]
  seriesKey: string
  theme: 'dark' | 'light'
  logarithmic: boolean
}>()

const host = ref<HTMLElement>()
let plot: uPlot | undefined
let resizeObserver: ResizeObserver | undefined

function seriesLabel() {
  if (props.seriesKey === 'tor') return 'T-OR'
  if (props.seriesKey === 'qor') return 'Q-OR'
  return `Channel ${Number(props.seriesKey.split(':')[1])} TD`
}

function alignedData(): AlignedData {
  const points = [...props.points].sort((a, b) => a.threshold - b.threshold)
  return [
    points.map((point) => point.threshold),
    points.map((point) => {
      let value: number
      if (props.seriesKey === 'tor') value = point.tOrRateCps
      else if (props.seriesKey === 'qor') value = point.qOrRateCps
      else {
        const channel = Number(props.seriesKey.split(':')[1])
        value = point.channelRatesCps[channel] ?? 0
      }
      return props.logarithmic && value === 0 ? null : value
    }),
  ]
}

function options(): Options {
  const light = props.theme === 'light'
  const axis = light ? '#60748d' : '#8194ac'
  const grid = light ? '#d8e1eb' : '#24364c'
  const line = light ? '#087bbf' : '#5da9ff'
  return {
    width: Math.max(host.value?.clientWidth ?? 0, 320),
    height: 380,
    title: `${seriesLabel()} threshold staircase`,
    series: [
      {},
      {
        label: seriesLabel(),
        stroke: line,
        width: 2,
        points: { show: true, size: 5, width: 1 },
        value: (_plot: uPlot, value: number | null) =>
          value == null ? '—' : `${value.toLocaleString()} cps`,
      },
    ],
    cursor: { drag: { x: true, y: false, setScale: true } },
    scales: {
      x: { time: false },
      y: props.logarithmic
        ? { distr: 3, range: (_plot, min, max) => [Math.max(1, min), max] }
        : { range: (_plot, _min, max) => [0, Math.max(1, max * 1.05)] },
    },
    axes: [
      { label: 'Coarse threshold (DAC)', stroke: axis, grid: { stroke: grid, width: 1 } },
      { label: 'Rate (cps)', stroke: axis, grid: { stroke: grid, width: 1 } },
    ],
    legend: { show: true },
  }
}

function rebuild() {
  if (!host.value || !props.points.length) return
  plot?.destroy()
  host.value.replaceChildren()
  plot = new uPlot(options(), alignedData(), host.value)
}

function render() {
  if (!plot) rebuild()
  else plot.setData(alignedData(), true)
}

function resetZoom() {
  plot?.setData(alignedData(), true)
}

watch(() => props.points, render, { deep: true })
watch([() => props.seriesKey, () => props.theme, () => props.logarithmic], rebuild)

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
  <div class="staircase-plot-shell">
    <div class="histogram-plot-actions">
      <button type="button" class="secondary" @click="resetZoom">Reset zoom</button>
    </div>
    <div
      ref="host"
      class="staircase-plot"
      role="img"
      :aria-label="`${seriesLabel()} trigger rate by coarse threshold`"
    ></div>
  </div>
</template>
