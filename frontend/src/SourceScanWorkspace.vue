<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { createClient } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import {
  SourceScanService,
  type SourceScan,
  type SourceScanHistoryEntry,
} from './gen/pet/caen/daq/v1/source_scan_pb'
import { SystemState } from './gen/pet/caen/daq/v1/system_pb'
import { validateSourceScan } from './sourceScanValidation'

const props = defineProps<{
  configuration: string
  systemState: SystemState
  captureRaw: boolean
  journalTransport: boolean
  persistHistograms: boolean
  hdf5SegmentSizeMb: number
  hdf5Compression: string
}>()
const emit = defineEmits<{ active: [boolean] }>()
const client = createClient(
  SourceScanService,
  createConnectTransport({ baseUrl: window.location.origin }),
)
const scan = ref<SourceScan>()
const liveScan = ref<SourceScan>()
const history = ref<SourceScanHistoryEntry[]>([])
const historyPage = ref(1)
const historyTotal = ref(0)
const historyBusy = ref(false)
const historyError = ref('')
const historyPageSize = 20
const historyPages = computed(() => Math.max(1, Math.ceil(historyTotal.value / historyPageSize)))
function scanLabel(id: string) {
  return /^xy-[0-9]+$/.test(id) ? `Source scan ${id.slice(3)}` : `Legacy scan ${id.slice(3, 11)}`
}
function startedLabel(value: string) {
  return value ? new Date(value).toLocaleString() : 'Unknown'
}
const deviceConfigured = ref(false)
const error = ref('')
const busy = ref(false)
const expanded = ref(false)
const stale = ref(true)
const originConfirmed = ref(false)
const scanId = ref('')
const xMin = ref(-25.5),
  xMax = ref(25.5),
  yMin = ref(-25.5),
  yMax = ref(25.5)
const pitch = ref(1.5),
  mmPerTurn = ref(0.5),
  fullSteps = ref(200),
  microsteps = ref(1)
const xPositive = ref(true),
  yPositive = ref(true),
  pulseHigh = ref(20),
  pulseLow = ref(3000)
const settle = ref(250),
  timeout = ref(300),
  dwell = ref(1),
  events = ref('1000')
const criterion = ref('time')
const active = computed(
  () => !!liveScan.value && !['completed', 'cancelled', 'failed'].includes(liveScan.value.state),
)
const point = computed(() => scan.value?.points[scan.value.currentPoint])
const validation = computed(() =>
  validateSourceScan({
    xMin: xMin.value,
    xMax: xMax.value,
    yMin: yMin.value,
    yMax: yMax.value,
    pitch: pitch.value,
    mmPerTurn: mmPerTurn.value,
    fullSteps: fullSteps.value,
    microsteps: microsteps.value,
    pulseHigh: pulseHigh.value,
    pulseLow: pulseLow.value,
    settle: settle.value,
    timeout: timeout.value,
    dwell: dwell.value,
    events: events.value,
    criterion: criterion.value,
  }),
)
const startReasons = computed(() => {
  const reasons = [...validation.value.errors]
  if (busy.value) reasons.push('A scan request is being processed.')
  if (active.value)
    reasons.push('A source scan is already active. Wait for completion or cancel it.')
  if (stale.value)
    reasons.push('Waiting for a current backend status. Check the backend connection.')
  else if (!deviceConfigured.value)
    reasons.push('The source-stage endpoint is not configured on the backend.')
  if (!props.configuration.trim()) reasons.push('Select a PET configuration in Acquisition first.')
  if (props.systemState !== SystemState.READY)
    reasons.push('The DAQ must be connected and Ready before starting a source scan.')
  if (!originConfirmed.value)
    reasons.push(
      'Confirm that the source is physically at logical (0, 0) and the grid fits the stage travel.',
    )
  return reasons
})
let timer: ReturnType<typeof setTimeout> | undefined
let disposed = false
let revision = 0
async function refresh() {
  const requestedRevision = revision
  try {
    const response = await client.getSourceScan({}, { timeoutMs: 5000 })
    if (disposed || requestedRevision !== revision) return
    liveScan.value = response.scan
    if (!scanId.value || scanId.value === response.scan?.scanId) scan.value = response.scan
    deviceConfigured.value = response.deviceConfigured
    stale.value = false
    emit('active', active.value)
  } catch (e) {
    stale.value = true
    error.value = String(e)
  } finally {
    if (!disposed) timer = setTimeout(refresh, 1000)
  }
}
async function refreshHistory(page = historyPage.value) {
  if (historyBusy.value) return
  historyBusy.value = true
  historyError.value = ''
  try {
    const response = await client.listSourceScans(
      { limit: historyPageSize, offset: (page - 1) * historyPageSize },
      { timeoutMs: 5000 },
    )
    if (disposed) return
    history.value = response.scans
    historyTotal.value = response.totalCount
    historyPage.value = page
  } catch (e) {
    historyError.value = String(e)
  } finally {
    historyBusy.value = false
  }
}
async function loadScan(id: string) {
  historyError.value = ''
  const previous = scanId.value
  scanId.value = id
  try {
    const response = await client.getSourceScan({ scanId: id }, { timeoutMs: 5000 })
    if (!disposed && scanId.value === id) scan.value = response.scan
  } catch (e) {
    if (scanId.value === id) {
      scanId.value = previous
      historyError.value = String(e)
    }
  }
}
function showCurrent() {
  scanId.value = ''
  scan.value = liveScan.value
}
watch(expanded, (value) => {
  if (value) void refreshHistory()
})
watch(
  () => liveScan.value?.state,
  () => {
    if (expanded.value) void refreshHistory()
  },
)
onMounted(refresh)
onUnmounted(() => {
  disposed = true
  clearTimeout(timer)
})
async function start() {
  if (startReasons.value.length) return
  busy.value = true
  error.value = ''
  try {
    const response = await client.startSourceScan({
      originConfirmed: originConfirmed.value,
      configuration: {
        xMinMm: xMin.value,
        xMaxMm: xMax.value,
        yMinMm: yMin.value,
        yMaxMm: yMax.value,
        pitchMm: pitch.value,
        mmPerTurn: mmPerTurn.value,
        fullStepsPerTurn: fullSteps.value,
        microsteps: microsteps.value,
        xPositiveDirectionHigh: xPositive.value,
        yPositiveDirectionHigh: yPositive.value,
        pulseHighUs: pulseHigh.value,
        pulseLowUs: pulseLow.value,
        settleMilliseconds: settle.value,
        dwellMilliseconds: criterion.value === 'time' ? BigInt(Math.round(dwell.value * 1000)) : 0n,
        eventCount: criterion.value === 'events' ? BigInt(events.value) : 0n,
        pointTimeoutSeconds: timeout.value,
      },
      run: {
        janusConfiguration: props.configuration,
        requestedBy: 'web-operator',
        captureRaw: props.captureRaw,
        journalTransport: props.journalTransport,
        persistHistograms: props.persistHistograms,
        hdf5SegmentSizeMb: props.hdf5SegmentSizeMb,
        hdf5Compression: props.hdf5Compression,
      },
    })
    revision++
    scan.value = response.scan
    liveScan.value = response.scan
    scanId.value = ''
    originConfirmed.value = false
    emit('active', active.value)
  } catch (e) {
    error.value = String(e)
  } finally {
    busy.value = false
  }
}
async function cancel() {
  busy.value = true
  error.value = ''
  try {
    await client.cancelSourceScan({ scanId: liveScan.value?.scanId, requestedBy: 'web-operator' })
  } catch (e) {
    error.value = String(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section
    class="plots panel staircase-workspace source-scan"
    aria-labelledby="source-scan-heading"
  >
    <div class="section-title">
      <div>
        <p class="eyebrow">Crystal response scan</p>
        <h2 id="source-scan-heading">Source XY scan</h2>
      </div>
      <span class="safety">Moves the source in XY · acquires one PET run per position</span>
      <button
        type="button"
        class="scan-card-toggle"
        :aria-expanded="expanded"
        aria-controls="source-scan-content"
        @click="expanded = !expanded"
      >
        {{ expanded ? 'Collapse' : 'Expand' }}
      </button>
    </div>
    <div v-if="expanded" id="source-scan-content" class="scan-card-content">
      <p class="muted">
        Uses the PET configuration and storage options selected in Acquisition. Each stationary
        position produces a separate run.
      </p>
      <p v-if="!deviceConfigured">
        Source stage unavailable. Configure the stage connection on the DAQ server.
      </p>
      <p v-if="stale" role="status">Scan status unavailable or stale.</p>
      <form @submit.prevent="start">
        <fieldset :disabled="active || busy">
          <legend>Grid and motor calibration</legend>
          <div class="source-fields">
            <label
              >X minimum (mm)<input v-model.number="xMin" type="number" step="0.01" required
            /></label>
            <label
              >X maximum (mm)<input v-model.number="xMax" type="number" step="0.01" required
            /></label>
            <label
              >Y minimum (mm)<input v-model.number="yMin" type="number" step="0.01" required
            /></label>
            <label
              >Y maximum (mm)<input v-model.number="yMax" type="number" step="0.01" required
            /></label>
            <label
              >Grid pitch (mm)<input
                v-model.number="pitch"
                type="number"
                min="0.01"
                step="0.01"
                required
            /></label>
            <label
              >Travel per turn (mm)<input
                v-model.number="mmPerTurn"
                type="number"
                min="0.01"
                step="0.01"
                required
            /></label>
            <label
              >Full steps per turn<input
                v-model.number="fullSteps"
                type="number"
                min="1"
                max="100000"
                required
            /></label>
            <label
              >Microsteps (driver DIP setting)<input
                v-model.number="microsteps"
                type="number"
                min="1"
                max="256"
                required
            /></label>
            <label
              >Pulse high (µs)<input
                v-model.number="pulseHigh"
                type="number"
                min="2"
                max="10000"
                required
            /></label>
            <label
              >Pulse low (µs)<input
                v-model.number="pulseLow"
                type="number"
                min="2"
                max="10000"
                required
            /></label>
          </div>
          <div class="source-directions">
            <label class="switch source-checkbox"
              ><input v-model="xPositive" type="checkbox" /><span
                >X positive direction HIGH</span
              ></label
            >
            <label class="switch source-checkbox"
              ><input v-model="yPositive" type="checkbox" /><span
                >Y positive direction HIGH</span
              ></label
            >
          </div>
          <p v-if="validation.points !== undefined">
            {{ validation.points }} positions, serpentine from (X minimum, Y maximum). Maximum
            10,000 positions.
          </p>
          <p v-else class="field-error">
            Grid cannot be calculated. Check the validation messages below.
          </p>
          <p>
            Positions are calculated from emitted steps, without encoder feedback. The origin must
            be re-established before each scan; no automatic return or homing is performed.
          </p>
          <div class="source-fields">
            <label
              >Stop each point after<select v-model="criterion">
                <option value="time">Time</option>
                <option value="events">Events</option>
              </select></label
            >
            <label v-if="criterion === 'time'"
              >Acquisition time (s)<input
                v-model.number="dwell"
                type="number"
                min="0.001"
                step="0.001"
                required
            /></label>
            <label v-else
              >Events per point<input
                v-model="events"
                type="text"
                inputmode="numeric"
                pattern="[1-9][0-9]*"
                required
            /></label>
            <label
              >Settling time (ms)<input
                v-model.number="settle"
                type="number"
                min="0"
                max="60000"
                required
            /></label>
            <label
              >Point timeout (s)<input
                v-model.number="timeout"
                type="number"
                min="1"
                max="86400"
                required
            /></label>
          </div>
          <p>
            Point timeout includes travel, settling, configuration and acquisition. Event limits may
            overshoot while the DAQ stops and drains.
          </p>
          <label class="switch source-checkbox"
            ><input v-model="originConfirmed" type="checkbox" /> I placed the source at logical (0,
            0) and checked the grid fits the stage travel.</label
          >
        </fieldset>
        <div v-if="startReasons.length" id="source-start-reasons" aria-live="polite">
          <p><strong>Cannot start source scan:</strong></p>
          <ul>
            <li
              v-for="reason in startReasons"
              :key="reason"
              :class="{ 'field-error': validation.errors.includes(reason) }"
            >
              {{ reason }}
            </li>
          </ul>
        </div>
        <div class="actions source-actions">
          <button
            type="submit"
            class="primary"
            :aria-busy="busy"
            :disabled="startReasons.length > 0"
            :aria-describedby="startReasons.length ? 'source-start-reasons' : undefined"
          >
            Start source scan
          </button>
          <button type="button" class="danger" :disabled="busy || !active" @click="cancel">
            Cancel source scan
          </button>
        </div>
      </form>
      <p v-if="error" class="field-error" role="alert">{{ error }}</p>
      <div v-if="scan" aria-live="polite">
        <p>
          {{ scanLabel(scan.scanId) }} — <strong>{{ scan.state }}</strong> —
          {{ scan.completedPoints }}/{{ scan.points.length }}
          completed
        </p>
        <p v-if="point">
          Position {{ point.index + 1 }}: row {{ point.row }}, column {{ point.column }}; X
          {{ point.calculatedXMm }} mm, Y {{ point.calculatedYMm }} mm ({{ point.xSteps }},
          {{ point.ySteps }} steps). Run {{ point.runId || 'pending' }}.
        </p>
        <p v-if="scan.error" role="alert">{{ scan.error }}</p>
        <details>
          <summary>Position and run metadata</summary>
          <table>
            <thead>
              <tr>
                <th>Point</th>
                <th>Row / column</th>
                <th>X / Y (mm)</th>
                <th>X / Y (steps)</th>
                <th>Run</th>
                <th>Completed</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in scan.points" :key="p.index">
                <td>{{ p.index + 1 }}</td>
                <td>{{ p.row }} / {{ p.column }}</td>
                <td>{{ p.calculatedXMm }} / {{ p.calculatedYMm }}</td>
                <td>{{ p.xSteps }} / {{ p.ySteps }}</td>
                <td>{{ p.runId }}</td>
                <td>{{ p.completed ? 'Yes' : 'No' }}</td>
              </tr>
            </tbody>
          </table>
        </details>
      </div>
      <div class="section-title scan-history-title">
        <div>
          <p class="eyebrow">Stored scan datasets</p>
          <h3>Source scan history</h3>
        </div>
        <div class="scan-history-tools">
          <button v-if="scanId && liveScan" type="button" class="link-button" @click="showCurrent">
            Show {{ active ? 'active' : 'latest' }} scan
          </button>
          <button
            type="button"
            class="link-button"
            :disabled="historyBusy"
            @click="refreshHistory()"
          >
            Refresh
          </button>
        </div>
      </div>
      <p v-if="historyError" class="field-error" role="alert">{{ historyError }}</p>
      <div class="scan-history" aria-label="Source scan history" :aria-busy="historyBusy">
        <div v-if="history.length" class="scan-history-header" aria-hidden="true">
          <span>Scan</span><span>Started</span><span>Operator</span><span>Points</span
          ><span>Status</span>
        </div>
        <button
          v-for="entry in history"
          :key="entry.scanId"
          type="button"
          class="secondary"
          :title="entry.scanId"
          :aria-pressed="scan?.scanId === entry.scanId"
          @click="loadScan(entry.scanId)"
        >
          <strong data-label="Scan">{{ scanLabel(entry.scanId) }}</strong>
          <span data-label="Started">{{ startedLabel(entry.startedAt) }}</span>
          <span data-label="Operator">{{ entry.requestedBy || '—' }}</span>
          <span data-label="Points">{{ entry.completedPoints }} / {{ entry.totalPoints }}</span>
          <span data-label="Status" class="scan-history-state">{{ entry.state }}</span>
        </button>
        <p v-if="!history.length" class="empty">
          {{ historyBusy ? 'Loading source scans…' : 'No stored source scans yet.' }}
        </p>
      </div>
      <nav v-if="historyPages > 1" class="run-pagination" aria-label="Source scan history pages">
        <button
          type="button"
          :disabled="historyBusy || historyPage === 1"
          @click="refreshHistory(historyPage - 1)"
        >
          Previous
        </button>
        <span>Page {{ historyPage }} of {{ historyPages }}</span>
        <button
          type="button"
          :disabled="historyBusy || historyPage === historyPages"
          @click="refreshHistory(historyPage + 1)"
        >
          Next
        </button>
      </nav>
    </div>
  </section>
</template>

<style scoped>
.source-scan > .section-title {
  gap: 1rem;
  flex-wrap: wrap;
}
.source-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr));
  align-items: end;
  gap: 1rem;
  margin: 1rem 0;
}
.source-fields label {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  min-width: 0;
}
.source-fields input,
.source-fields select {
  margin-top: 0;
  min-height: 46px;
}
.source-directions {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem 2rem;
  margin-bottom: 1rem;
}
.source-checkbox {
  justify-content: flex-start;
  text-align: left;
}
fieldset {
  min-width: 0;
  border: 0;
  padding: 0;
}
legend {
  font-weight: 700;
}
fieldset p {
  margin-bottom: 0.5rem;
}
.source-actions {
  flex-wrap: wrap;
}
.source-actions .danger {
  margin-left: 0;
}
details {
  overflow-x: auto;
}
table {
  width: 100%;
  text-align: left;
}
</style>
