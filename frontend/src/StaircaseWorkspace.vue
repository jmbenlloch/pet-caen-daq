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
import { localDateTime } from './presentation'
import StaircasePlot from './StaircasePlot.vue'

const props = defineProps<{
  api: DaqApi
  systemState: SystemState
  theme: 'dark' | 'light'
  live?: DeepReadonly<StaircaseScan>
}>()

const board = ref(0)
const minimum = ref(150)
const maximum = ref(500)
const step = ref(5)
const dwellMilliseconds = ref(500)
const selectedSeries = ref('channel:0')
const history = ref<ScanSummary[]>([])
const historyBoard = ref('any')
const historyPage = ref(1)
const historyPageSize = 8
const historyTotal = ref(0)
const scanBoards = [0, 1, 2, 3]
const finalized = ref<StaircaseScan>()
const busy = ref(false)
const error = ref('')
const expanded = ref(false)

const displayed = computed(() => props.live ?? finalized.value)
const canStart = computed(() => props.systemState === SystemState.READY && !busy.value)
const running = computed(
  () =>
    props.live?.summary?.state === ScanState.PREPARING ||
    props.live?.summary?.state === ScanState.RUNNING ||
    props.live?.summary?.state === ScanState.RESTORING,
)
const historyPageCount = computed(() =>
  Math.max(1, Math.ceil(historyTotal.value / historyPageSize)),
)
let historyRequest = 0

async function refresh() {
  const request = ++historyRequest
  try {
    const boardFilter = historyBoard.value === 'any' ? undefined : Number(historyBoard.value)
    const response = await props.api.listScans(
      historyPageSize,
      (historyPage.value - 1) * historyPageSize,
      boardFilter,
    )
    if (request !== historyRequest) return
    history.value = response.scans
    historyTotal.value = response.totalCount
    const lastPage = Math.max(1, Math.ceil(response.totalCount / historyPageSize))
    if (historyPage.value > lastPage) {
      historyPage.value = lastPage
      await refresh()
    }
  } catch (reason) {
    if (request !== historyRequest) return
    error.value = reason instanceof Error ? reason.message : String(reason)
  }
}

function selectHistoryPage(page: number) {
  historyPage.value = page
  void refresh()
}

function scanRunLabel(scanId: string) {
  return /^\d+$/.test(scanId) ? `Run ${scanId}` : 'Legacy scan'
}

function scanStateLabel(state: ScanState) {
  const label = ScanState[state] ?? 'Unknown'
  return label.charAt(0) + label.slice(1).toLowerCase()
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
    if (previous === SystemState.SCANNING && next !== SystemState.SCANNING) {
      historyPage.value = 1
      void refresh()
    }
  },
)
watch(historyBoard, () => {
  historyPage.value = 1
  void refresh()
})
onMounted(refresh)
</script>

<template>
  <section class="plots panel staircase-workspace" aria-labelledby="staircase-heading">
    <div class="section-title">
      <div>
        <p class="eyebrow">Diagnostic scan</p>
        <h2 id="staircase-heading">Threshold staircase</h2>
      </div>
      <span class="safety"
        >Changes TD and QD coarse thresholds · restores configuration afterward</span
      >
      <button
        type="button"
        class="scan-card-toggle"
        :aria-expanded="expanded"
        aria-controls="staircase-content"
        @click="expanded = !expanded"
      >
        {{ expanded ? 'Collapse' : 'Expand' }}
      </button>
    </div>

    <div v-if="expanded" id="staircase-content" class="scan-card-content">
      <div class="plot-controls staircase-controls">
        <label>Board<input v-model.number="board" type="number" min="0" max="3" /></label>
        <label>Minimum<input v-model.number="minimum" type="number" min="0" max="2047" /></label>
        <label>Maximum<input v-model.number="maximum" type="number" min="0" max="2047" /></label>
        <label>Step<input v-model.number="step" type="number" min="1" /></label>
        <label
          >Dwell (ms)<input v-model.number="dwellMilliseconds" type="number" min="1" max="34000"
        /></label>
        <div class="actions staircase-actions">
          <button
            type="button"
            class="primary"
            :disabled="!canStart"
            :aria-busy="busy"
            @click="start"
          >
            Start scan
          </button>
          <button v-if="running" type="button" class="danger" :disabled="busy" @click="cancel">
            Cancel
          </button>
        </div>
      </div>
      <p class="muted">
        Dwell time is the counting interval at each threshold. A longer dwell collects more events
        for steadier rate measurements, but increases the total scan duration.
      </p>
      <p class="muted">
        For a dark-count staircase, turn the light source off and confirm the intended HV state
        before starting.
      </p>
      <p v-if="error" class="field-error" role="alert">{{ error }}</p>

      <div v-if="displayed" class="staircase-status">
        <strong>{{ scanRunLabel(displayed.summary?.scanId ?? '') }}</strong>
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
      <StaircasePlot
        v-if="displayed?.points.length"
        :points="displayed.points"
        :series-key="selectedSeries"
        :theme="theme"
      />
      <p v-else class="empty">
        Start a scan or select a finalized scan to plot its measured rates.
      </p>

      <div class="section-title scan-history-title">
        <div>
          <p class="eyebrow">Stored scan datasets</p>
          <h3>Finalized staircases</h3>
        </div>
        <div class="scan-history-tools">
          <label>
            Board
            <select v-model="historyBoard" aria-label="Filter scans by board">
              <option value="any">Any board</option>
              <option
                v-for="boardNumber in scanBoards"
                :key="boardNumber"
                :value="String(boardNumber)"
              >
                Board {{ boardNumber }}
              </option>
            </select>
          </label>
          <button type="button" class="link-button" :disabled="busy" @click="refresh">
            Refresh
          </button>
        </div>
      </div>
      <div class="scan-history">
        <div v-if="history.length" class="scan-history-header" aria-hidden="true">
          <span>Run</span>
          <span>Started</span>
          <span>Board</span>
          <span>Points</span>
          <span>Status</span>
        </div>
        <button
          v-for="scan in history"
          :key="scan.scanId"
          type="button"
          class="secondary"
          @click="load(scan.scanId)"
        >
          <strong data-label="Run">{{ scanRunLabel(scan.scanId) }}</strong>
          <span data-label="Started">{{ localDateTime(scan.startedAt) }}</span>
          <span data-label="Board">{{ scan.board }}</span>
          <span data-label="Points">{{ scan.completedPoints }} / {{ scan.totalPoints }}</span>
          <span data-label="Status" class="scan-history-state">{{
            scanStateLabel(scan.state)
          }}</span>
        </button>
      </div>
      <nav v-if="historyPageCount > 1" class="run-pagination" aria-label="Scan history pages">
        <button
          type="button"
          :disabled="historyPage === 1"
          @click="selectHistoryPage(historyPage - 1)"
        >
          Previous
        </button>
        <span>Page {{ historyPage }} of {{ historyPageCount }}</span>
        <button
          type="button"
          :disabled="historyPage === historyPageCount"
          @click="selectHistoryPage(historyPage + 1)"
        >
          Next
        </button>
      </nav>
    </div>
  </section>
</template>
