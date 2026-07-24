<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch, type DeepReadonly } from 'vue'
import uPlot, { type AlignedData, type Options } from 'uplot'
import 'uplot/dist/uPlot.min.css'
import type { HoldDelayPoint } from './gen/pet/caen/daq/v1/system_pb'

const props = defineProps<{
  points: readonly DeepReadonly<HoldDelayPoint>[]
  channel: number
  theme: 'dark' | 'light'
}>()

const host = ref<HTMLElement>()
let plot: uPlot | undefined
let resizeObserver: ResizeObserver | undefined

function orderedPoints() {
  return [...props.points].sort((a, b) => a.effectiveDelayNs - b.effectiveDelayNs)
}

function data(): AlignedData {
  const ordered = orderedPoints()
  return [ordered.map((point) => point.effectiveDelayNs), ordered.map(() => 0)]
}

function drawHeatmap(u: uPlot) {
  const ordered = orderedPoints()
  if (!ordered.length) return
  const histograms = ordered.map(
    (point) => point.channels.find((item) => item.channel === props.channel)?.highGainBins ?? [],
  )
  const maximum = Math.max(1, ...histograms.flat())
  const step = ordered.length > 1 ? ordered[1].effectiveDelayNs - ordered[0].effectiveDelayNs : 8
  const { ctx } = u
  ctx.save()
  for (let x = 0; x < ordered.length; x++) {
    const left = u.valToPos(ordered[x].effectiveDelayNs - step / 2, 'x', true)
    const right = u.valToPos(ordered[x].effectiveDelayNs + step / 2, 'x', true)
    for (let bin = 0; bin < histograms[x].length; bin++) {
      const count = histograms[x][bin]
      if (!count) continue
      const top = u.valToPos(bin + 1, 'y', true)
      const bottom = u.valToPos(bin, 'y', true)
      const intensity = Math.log1p(count) / Math.log1p(maximum)
      ctx.fillStyle = `rgba(20, 184, 255, ${0.12 + intensity * 0.88})`
      ctx.fillRect(left, top, Math.max(1, right - left), Math.max(1, bottom - top))
    }
  }
  ctx.restore()
}

function options(): Options {
  const light = props.theme === 'light'
  const axis = light ? '#60748d' : '#8194ac'
  const grid = light ? '#d8e1eb' : '#24364c'
  return {
    width: Math.max(host.value?.clientWidth ?? 0, 320),
    height: 420,
    title: `Channel ${props.channel} high-gain spectrum`,
    series: [{}, { show: false }],
    scales: { x: { time: false }, y: { range: [0, 512] } },
    axes: [
      { label: 'Effective hold delay (ns)', stroke: axis, grid: { stroke: grid } },
      { label: 'Pulse-height bin', stroke: axis, grid: { stroke: grid } },
    ],
    cursor: { drag: { x: true, y: true, setScale: true } },
    hooks: { draw: [drawHeatmap] },
    legend: { show: false },
  }
}

function rebuild() {
  if (!host.value || !props.points.length) return
  plot?.destroy()
  host.value.replaceChildren()
  plot = new uPlot(options(), data(), host.value)
  plot.setScale('y', { min: 0, max: 512 })
}

watch(() => props.points, rebuild, { deep: true })
watch([() => props.channel, () => props.theme], rebuild)
onMounted(() => {
  rebuild()
  resizeObserver = new ResizeObserver((entries) => {
    const width = Math.floor(entries[0]?.contentRect.width ?? 0)
    if (plot && width > 0) plot.setSize({ width: Math.max(width, 320), height: 420 })
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
    <div ref="host" role="img" :aria-label="`Hold delay heatmap for channel ${channel}`"></div>
  </div>
</template>
