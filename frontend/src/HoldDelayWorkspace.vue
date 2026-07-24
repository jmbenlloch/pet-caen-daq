<script setup lang="ts">
import { create } from '@bufbuild/protobuf'
import { computed, ref, type DeepReadonly } from 'vue'
import type { DaqApi } from './api'
import HoldDelayPlot from './HoldDelayPlot.vue'
import {
  ScanState,
  StartHoldDelayScanRequestSchema,
  SystemState,
  type HoldDelayScan,
} from './gen/pet/caen/daq/v1/system_pb'

const props = defineProps<{
  api: DaqApi
  systemState: SystemState
  theme: 'dark' | 'light'
  live?: DeepReadonly<HoldDelayScan>
}>()
const board = ref(0)
const minimum = ref(0)
const maximum = ref(300)
const step = ref(8)
const events = ref(100)
const timeout = ref(30)
const channel = ref(0)
const started = ref<HoldDelayScan>()
const busy = ref(false)
const error = ref('')
const displayed = computed(() => props.live ?? started.value)
const running = computed(() => {
  const state = props.live?.summary?.state
  return state === ScanState.PREPARING || state === ScanState.RUNNING || state === ScanState.RESTORING
})

async function start() {
  busy.value = true
  error.value = ''
  try {
    started.value = await props.api.startHoldDelay(
      create(StartHoldDelayScanRequestSchema, {
        board: board.value,
        minimumDelayNs: minimum.value,
        maximumDelayNs: maximum.value,
        stepNs: step.value,
        eventsPerDelay: events.value,
        pointTimeoutSeconds: timeout.value,
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
  const id = props.live?.summary?.scanId
  if (!id) return
  await props.api.cancelScan(id, 'operator')
}
</script>

<template>
  <section class="plots panel staircase-workspace" aria-labelledby="hold-delay-heading">
    <div class="section-title">
      <div>
        <p class="eyebrow">Spectroscopy calibration</p>
        <h2 id="hold-delay-heading">Hold delay scan</h2>
      </div>
      <span class="safety">Collects high-gain spectra · restores configuration afterward</span>
    </div>
    <div class="plot-controls staircase-controls">
      <label>Board<input v-model.number="board" type="number" min="0" max="3" /></label>
      <label>Minimum (ns)<input v-model.number="minimum" type="number" min="0" step="8" /></label>
      <label>Maximum (ns)<input v-model.number="maximum" type="number" min="0" step="8" /></label>
      <label>Step (ns)<input v-model.number="step" type="number" min="8" step="8" /></label>
      <label>Events / delay<input v-model.number="events" type="number" min="10" /></label>
      <label>Timeout (s)<input v-model.number="timeout" type="number" min="1" /></label>
      <div class="actions staircase-actions">
        <button type="button" class="primary" :disabled="systemState !== SystemState.READY || busy" @click="start">
          Start scan
        </button>
        <button v-if="running" type="button" class="danger" @click="cancel">Cancel</button>
      </div>
    </div>
    <p class="muted">
      JANUS recommends 0–300 ns in 8 ns increments. Color intensity is logarithmic event count.
    </p>
    <p v-if="error" class="field-error" role="alert">{{ error }}</p>
    <div v-if="displayed" class="staircase-status">
      <strong>Run {{ displayed.summary?.scanId }}</strong>
      <span>{{ displayed.summary?.completedPoints }} / {{ displayed.summary?.totalPoints }} points</span>
      <progress :value="displayed.summary?.completedPoints" :max="displayed.summary?.totalPoints || 1" />
    </div>
    <label class="staircase-series">
      Channel
      <select v-model.number="channel">
        <option v-for="number in 64" :key="number - 1" :value="number - 1">Channel {{ number - 1 }}</option>
      </select>
    </label>
    <HoldDelayPlot
      v-if="displayed?.points.length"
      :points="displayed.points"
      :channel="channel"
      :theme="theme"
    />
  </section>
</template>
