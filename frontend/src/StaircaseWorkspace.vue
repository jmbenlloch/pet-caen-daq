<script setup lang="ts">
import { create } from '@bufbuild/protobuf'
import { computed, onMounted, ref, watch, type DeepReadonly } from 'vue'
import type { DaqApi } from './api'
import {
  ScanState,
  StartStaircaseRequestSchema,
  SystemState,
  type ScanSummary,
  type StaircaseScan,
} from './gen/pet/caen/daq/v1/system_pb'

const props = defineProps<{
  api: DaqApi
  systemState: SystemState
  live?: DeepReadonly<StaircaseScan>
}>()

const board = ref(0)
const minimum = ref(150)
const maximum = ref(500)
const step = ref(5)
const dwellMilliseconds = ref(500)
const selectedSeries = ref('channel:0')
const history = ref<ScanSummary[]>([])
const finalized = ref<StaircaseScan>()
const busy = ref(false)
const error = ref('')

const displayed = computed(() => props.live ?? finalized.value)
const canStart = computed(() => props.systemState === SystemState.READY && !busy.value)
const running = computed(
  () =>
    props.live?.summary?.state === ScanState.PREPARING ||
    props.live?.summary?.state === ScanState.RUNNING ||
    props.live?.summary?.state === ScanState.RESTORING,
)

async function refresh() {
  try {
    history.value = await props.api.listScans(50)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : String(reason)
  }
}

async function start() {
  busy.value = true
  error.value = ''
  finalized.value = undefined
  try {
    await props.api.startStaircase(
      create(StartStaircaseRequestSchema, {
        board: board.value,
        minimumThreshold: minimum.value,
        maximumThreshold: maximum.value,
        step: step.value,
        dwellMilliseconds: dwellMilliseconds.value,
        requestedBy: 'operator',
      }),
    )
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    busy.value = false
  }
}

async function cancel() {
  const scanId = props.live?.summary?.scanId
  if (!scanId) return
  busy.value = true
  try {
    await props.api.cancelScan(scanId, 'operator')
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    busy.value = false
  }
}

async function load(scanId: string) {
  busy.value = true
  error.value = ''
  try {
    finalized.value = await props.api.staircase(scanId)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    busy.value = false
  }
}

watch(
  () => props.systemState,
  (next, previous) => {
    if (previous === SystemState.SCANNING && next !== SystemState.SCANNING) void refresh()
  },
)
onMounted(refresh)

const plot = computed(() => {
  const points = [...(displayed.value?.points ?? [])].sort((a, b) => a.threshold - b.threshold)
  const values = points.map((point) => {
    if (selectedSeries.value === 'tor') return point.tOrRateCps
    if (selectedSeries.value === 'qor') return point.qOrRateCps
    const channel = Number(selectedSeries.value.split(':')[1])
    return point.channelRatesCps[channel] ?? 0
  })
  if (!points.length)
    return { polyline: '', labels: [] as { x: number; text: string }[], maximum: 1 }
  const minX = points[0].threshold
  const maxX = points.at(-1)?.threshold ?? minX
  const maximumValue = Math.max(1, ...values)
  const x = (value: number) => 45 + ((value - minX) / Math.max(1, maxX - minX)) * 710
  const y = (value: number) => 270 - (value / maximumValue) * 235
  return {
    polyline: points.map((point, index) => `${x(point.threshold)},${y(values[index])}`).join(' '),
    labels: [
      { x: 45, text: String(minX) },
      { x: 400, text: String(Math.round((minX + maxX) / 2)) },
      { x: 755, text: String(maxX) },
    ],
    maximum: maximumValue,
  }
})
</script>

<template>
  <section class="panel staircase-workspace" aria-labelledby="staircase-heading">
    <div class="section-title">
      <div>
        <p class="eyebrow">Diagnostic scan</p>
        <h2 id="staircase-heading">Threshold staircase</h2>
      </div>
      <span class="safety"
        >Changes TD and QD coarse thresholds · restores configuration afterward</span
      >
    </div>

    <div class="plot-controls staircase-controls">
      <label>Board<input v-model.number="board" type="number" min="0" max="3" /></label>
      <label>Minimum<input v-model.number="minimum" type="number" min="0" max="2047" /></label>
      <label>Maximum<input v-model.number="maximum" type="number" min="0" max="2047" /></label>
      <label>Step<input v-model.number="step" type="number" min="1" /></label>
      <label
        >Dwell (ms)<input v-model.number="dwellMilliseconds" type="number" min="1" max="34000"
      /></label>
      <button type="button" :disabled="!canStart" :aria-busy="busy" @click="start">
        Start scan
      </button>
      <button v-if="running" type="button" class="danger" :disabled="busy" @click="cancel">
        Cancel
      </button>
    </div>
    <p class="muted">
      For a dark-count staircase, turn the light source off and confirm the intended HV state before
      starting.
    </p>
    <p v-if="error" class="field-error" role="alert">{{ error }}</p>

    <div v-if="displayed" class="staircase-status">
      <strong>{{ displayed.summary?.scanId }}</strong>
      <span>
        {{ displayed.summary?.completedPoints }} / {{ displayed.summary?.totalPoints }} points ·
        {{ ScanState[displayed.summary?.state ?? ScanState.UNSPECIFIED] }}
      </span>
      <progress
        :value="displayed.summary?.completedPoints"
        :max="displayed.summary?.totalPoints || 1"
      />
    </div>

    <label class="staircase-series">
      Curve
      <select v-model="selectedSeries">
        <option v-for="channel in 64" :key="channel - 1" :value="`channel:${channel - 1}`">
          Channel {{ channel - 1 }} TD
        </option>
        <option value="tor">T-OR</option>
        <option value="qor">Q-OR</option>
      </select>
    </label>
    <svg
      v-if="displayed?.points.length"
      class="staircase-plot"
      viewBox="0 0 800 320"
      role="img"
      aria-label="Trigger rate by coarse threshold"
    >
      <line x1="45" y1="270" x2="755" y2="270" />
      <line x1="45" y1="35" x2="45" y2="270" />
      <polyline :points="plot.polyline" />
      <text x="10" y="40">{{ plot.maximum.toPrecision(4) }} cps</text>
      <text x="10" y="274">0</text>
      <text v-for="label in plot.labels" :key="label.x" :x="label.x" y="295" text-anchor="middle">
        {{ label.text }}
      </text>
      <text x="400" y="315" text-anchor="middle">Coarse threshold (DAC)</text>
    </svg>
    <p v-else class="empty">Start a scan or select a finalized scan to plot its measured rates.</p>

    <div class="section-title scan-history-title">
      <div>
        <p class="eyebrow">Stored scan datasets</p>
        <h3>Finalized staircases</h3>
      </div>
      <button type="button" class="secondary" @click="refresh">Refresh</button>
    </div>
    <div class="scan-history">
      <button
        v-for="scan in history"
        :key="scan.scanId"
        type="button"
        class="secondary"
        @click="load(scan.scanId)"
      >
        <strong>{{ scan.scanId }}</strong>
        <span
          >Board {{ scan.board }} · {{ scan.completedPoints }}/{{ scan.totalPoints }} points</span
        >
      </button>
    </div>
  </section>
</template>
