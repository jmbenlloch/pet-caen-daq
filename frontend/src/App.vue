<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import defaultConfiguration from '../../test/fixtures/janus/config_same4_v3_good.txt?raw'
import { createDaqApi, type DaqApi } from './api'
import BoardOverrides from './BoardOverrides.vue'
import ChannelOverrides from './ChannelOverrides.vue'
import MaskEditor from './MaskEditor.vue'
import NumericField from './NumericField.vue'
import PlotWorkspace from './PlotWorkspace.vue'
import StatisticsTab from './StatisticsTab.vue'
import {
  isBooleanField,
  isMaskField,
  numericConstraint,
  numericError,
  parameterActive,
  parameterScope,
  parseConfiguration,
  setConfigurationValue,
  updateConfiguration,
  type ConfigurationField,
} from './configuration'
import { DiagnosticSeverity, HealthStatus, SystemState } from './gen/pet/caen/daq/v1/system_pb'
import { bytes, compact, healthLabel, stateLabel } from './presentation'
import { useDaq } from './useDaq'

const props = defineProps<{ api?: DaqApi }>()
const daq = useDaq(props.api ?? createDaqApi())
type Theme = 'dark' | 'light'
const theme = ref<Theme>(
  window.localStorage.getItem('pet-caen-theme') === 'light' ? 'light' : 'dark',
)
document.documentElement.dataset.theme = theme.value
function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark'
  document.documentElement.dataset.theme = theme.value
  window.localStorage.setItem('pet-caen-theme', theme.value)
}
const runId = ref('')
const requestedBy = ref('operator')
const configurationDocument = ref(parseConfiguration(defaultConfiguration))
const configuration = computed({
  get: () => configurationDocument.value.source,
  set: (source: string) => (configurationDocument.value = parseConfiguration(source)),
})
const captureRaw = ref(true)
const journalTransport = ref(true)
const configFile = ref<HTMLInputElement>()
const selectedSection = ref('All')
const parameterSearch = ref('')
const showRawConfiguration = ref(false)
const activeMask = ref<{ low: ConfigurationField; high: ConfigurationField }>()
const activeBoardField = ref<ConfigurationField>()
const activeChannelField = ref<ConfigurationField>()

const sections = computed(() => [
  'All',
  ...new Set(configurationDocument.value.fields.map((field) => field.section)),
])
const visibleFields = computed(() => {
  const query = parameterSearch.value.trim().toLowerCase()
  return configurationDocument.value.fields.filter(
    (field) =>
      !(isMaskField(field) && (field.name.endsWith('1') || field.index !== undefined)) &&
      !(parameterScope(field) === 'board' && field.index !== undefined) &&
      field.channel === undefined &&
      (selectedSection.value === 'All' || field.section === selectedSection.value) &&
      (!query ||
        field.name.toLowerCase().includes(query) ||
        field.help.toLowerCase().includes(query) ||
        field.value.toLowerCase().includes(query)),
  )
})
const configurationErrors = computed(() =>
  configurationDocument.value.fields
    .filter((field) => parameterActive(configurationDocument.value, field))
    .map((field) => ({ field, error: numericError(field) }))
    .filter((item) => item.error),
)
const stopMode = computed(() => globalValue('StopRunMode') ?? 'MANUAL')
const presetTime = computed(() => globalValue('PresetTime') ?? '0')
const presetCounts = computed(() => globalValue('PresetCounts') ?? '0')
const stopPolicyError = computed(() => {
  if (stopMode.value === 'PRESET_TIME' && !(Number.parseFloat(presetTime.value) > 0))
    return 'Preset time must be greater than zero.'
  if (stopMode.value === 'PRESET_COUNTS' && !(Number(presetCounts.value) > 0))
    return 'Preset event count must be a positive integer.'
  return ''
})
const configuredStopPolicy = computed(() => {
  const mode = globalValue('StopRunMode') ?? 'MANUAL'
  if (mode === 'PRESET_TIME') return `Automatic stop after ${globalValue('PresetTime') ?? '0'} s`
  if (mode === 'PRESET_COUNTS')
    return `Automatic stop after ${globalValue('PresetCounts') ?? '0'} events`
  return 'Manual stop'
})

const state = computed(() => stateLabel[daq.snapshot.value?.state ?? 0])
const boards = computed(() =>
  (daq.snapshot.value?.chains ?? []).flatMap((chain) =>
    chain.boards.map((board) => ({ chain: chain.index, ...board })),
  ),
)
const severeDiagnostics = computed(() =>
  (daq.snapshot.value?.diagnostics ?? []).filter(
    (item) => item.severity >= DiagnosticSeverity.WARNING,
  ),
)

async function loadConfiguration(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (file) configuration.value = await file.text()
}

function setField(field: ConfigurationField, value: string) {
  configurationDocument.value = updateConfiguration(configurationDocument.value, field, value)
}

function setGlobalField(name: string, value: string) {
  const field = configurationDocument.value.fields.find(
    (candidate) => candidate.name === name && candidate.index === undefined,
  )
  if (field) setField(field, value)
}

function openMask(field: ConfigurationField) {
  const highName = field.name.replace(/0$/, '1')
  const high = configurationDocument.value.fields.find(
    (candidate) => candidate.name === highName && candidate.index === field.index,
  )
  if (high) activeMask.value = { low: field, high }
}

function maskBoardSummaries(field: ConfigurationField) {
  const highName = field.name.replace(/0$/, '1')
  const globalHigh = configurationDocument.value.fields.find(
    (candidate) => candidate.name === highName && candidate.index === undefined,
  )
  return Array.from({ length: 4 }, (_, board) => {
    const index = String(board)
    const low = configurationDocument.value.fields.find(
      (candidate) => candidate.name === field.name && candidate.index === index,
    )
    const high = configurationDocument.value.fields.find(
      (candidate) => candidate.name === highName && candidate.index === index,
    )
    return {
      board,
      low: low?.value ?? field.value,
      high: high?.value ?? globalHigh?.value ?? '0x00000000',
      inherited: !low && !high,
    }
  })
}

function maskVariants() {
  if (!activeMask.value) return []
  const variants = []
  for (let target = -1; target < 4; target++) {
    const index = target < 0 ? undefined : String(target)
    const low = configurationDocument.value.fields.find(
      (field) =>
        field.name === activeMask.value?.low.name &&
        field.index === index &&
        field.channel === undefined,
    )
    const high = configurationDocument.value.fields.find(
      (field) =>
        field.name === activeMask.value?.high.name &&
        field.index === index &&
        field.channel === undefined,
    )
    variants.push({
      target: target < 0 ? 'global' : String(target),
      label: target < 0 ? 'Global' : `Board ${target}`,
      low: low?.value ?? activeMask.value.low.value,
      high: high?.value ?? activeMask.value.high.value,
      inherited: target >= 0 && (!low || !high),
    })
  }
  return variants
}

function applyMask(target: string, low: string, high: string) {
  if (!activeMask.value) return
  const index = target === 'global' ? undefined : Number(target)
  configurationDocument.value = setConfigurationValue(
    configurationDocument.value,
    activeMask.value.low.name,
    index,
    undefined,
    low,
  )
  configurationDocument.value = setConfigurationValue(
    configurationDocument.value,
    activeMask.value.high.name,
    index,
    undefined,
    high,
  )
  activeMask.value = undefined
}

function channelOverrides(field: ConfigurationField) {
  const result: Record<number, Record<number, string>> = {}
  for (const candidate of configurationDocument.value.fields) {
    if (
      candidate.name !== field.name ||
      candidate.index === undefined ||
      candidate.channel === undefined
    )
      continue
    const board = Number(candidate.index)
    result[board] ??= {}
    result[board][Number(candidate.channel)] = candidate.value
  }
  return result
}

function nonZeroChannelOverrides(field: ConfigurationField) {
  const counts = [0, 0, 0, 0]
  for (const candidate of configurationDocument.value.fields) {
    if (
      candidate.name !== field.name ||
      candidate.index === undefined ||
      candidate.channel === undefined ||
      Number(candidate.value) === 0
    )
      continue
    const board = Number(candidate.index)
    if (board >= 0 && board < counts.length) counts[board]++
  }
  return counts
}

function boardValues(field: ConfigurationField) {
  return Array.from({ length: 4 }, (_, board) => {
    const override = configurationDocument.value.fields.find(
      (candidate) =>
        candidate.name === field.name &&
        candidate.index === String(board) &&
        candidate.channel === undefined,
    )
    return { board, value: override?.value ?? field.value, inherited: !override }
  })
}

function boardOverrides(field: ConfigurationField) {
  const result: Record<number, string> = {}
  for (const candidate of configurationDocument.value.fields) {
    if (candidate.name !== field.name || candidate.index === undefined) continue
    result[Number(candidate.index)] = candidate.value
  }
  return result
}

function globalValue(name: string) {
  return configurationDocument.value.fields.find(
    (field) => field.name === name && field.index === undefined && field.channel === undefined,
  )?.value
}

function effectiveBoardNumericValues(name: string) {
  const general = Number.parseFloat(globalValue(name) ?? '0')
  const result: Record<number, number> = {}
  for (let board = 0; board < 4; board++) {
    const override = configurationDocument.value.fields.find(
      (field) =>
        field.name === name && field.index === String(board) && field.channel === undefined,
    )
    result[board] = Number.parseFloat(override?.value ?? String(general))
  }
  return result
}

function activeStopPolicy() {
  const run = daq.snapshot.value?.currentRun
  if (!run || run.stopMode === 'MANUAL' || !run.stopMode) return 'Manual stop enabled'
  if (run.stopMode === 'PRESET_COUNTS') {
    const remaining =
      run.presetEventCount > run.eventCount ? run.presetEventCount - run.eventCount : 0n
    return `Stops at ${compact(run.presetEventCount)} events · ${compact(remaining)} remaining · manual stop enabled`
  }
  const started = run.startedAt ? Number(run.startedAt.seconds) * 1000 : Date.now()
  const remaining = Math.max(Number(run.presetTimeMilliseconds) - (Date.now() - started), 0)
  return `Stops after ${(Number(run.presetTimeMilliseconds) / 1000).toFixed(1)} s · ${(remaining / 1000).toFixed(1)} s remaining · manual stop enabled`
}

function applyBoardOverrides(values: Record<number, string>) {
  if (!activeBoardField.value) return
  for (let board = 0; board < 4; board++) {
    configurationDocument.value = setConfigurationValue(
      configurationDocument.value,
      activeBoardField.value.name,
      board,
      undefined,
      values[board],
    )
  }
  activeBoardField.value = undefined
}

function applyChannelOverrides(board: number, values: Record<number, string>) {
  if (!activeChannelField.value) return
  for (let channel = 0; channel < 64; channel++) {
    configurationDocument.value = setConfigurationValue(
      configurationDocument.value,
      activeChannelField.value.name,
      board,
      channel,
      values[channel],
    )
  }
  activeChannelField.value = undefined
}

function loadDefaultConfiguration() {
  configuration.value = defaultConfiguration
}

function loadBackendConfiguration() {
  if (daq.configurationTemplate.value) configuration.value = daq.configurationTemplate.value
}

onMounted(() => daq.connect())
</script>

<template>
  <div class="shell">
    <header class="masthead">
      <div>
        <p class="eyebrow">PET detector control</p>
        <h1>CAEN acquisition</h1>
      </div>
      <div class="masthead-actions">
        <div class="connection" role="status" aria-live="polite">
          <span
            class="status-dot"
            :class="{ live: daq.connected.value && !daq.stale.value }"
            aria-hidden="true"
          />
          <span>{{ daq.stale.value ? 'Telemetry stale' : 'Live telemetry' }}</span>
          <small>{{ daq.snapshot.value?.instanceId || 'No backend' }}</small>
        </div>
        <button
          type="button"
          class="theme-toggle"
          :aria-label="`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`"
          :title="`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`"
          @click="toggleTheme"
        >
          <span aria-hidden="true">{{ theme === 'dark' ? '☀' : '☾' }}</span>
          {{ theme === 'dark' ? 'Light' : 'Dark' }}
        </button>
      </div>
    </header>

    <main>
      <section class="hero panel" aria-labelledby="system-heading">
        <div>
          <p class="eyebrow">System state</p>
          <h2 id="system-heading">{{ state }}</h2>
          <p class="muted">
            Sequence {{ compact(daq.snapshot.value?.sequence) }} ·
            {{ daq.snapshot.value?.chains.length ?? 0 }} provisioned links
          </p>
        </div>
        <div v-if="daq.snapshot.value?.currentRun" class="run-now">
          <span>Active run</span>
          <strong>{{ daq.snapshot.value.currentRun.runId }}</strong>
          <span>{{ compact(daq.snapshot.value.currentRun.eventCount) }} events</span>
          <small>{{ activeStopPolicy() }}</small>
        </div>
        <div v-else class="run-now quiet"><span>No active run</span></div>
      </section>

      <div v-if="daq.error.value" class="alert error" role="alert">
        <strong>Connection or command failed</strong>
        <span>{{ daq.error.value }}</span>
      </div>

      <div v-if="severeDiagnostics.length" class="alert warning" role="alert">
        <strong>System diagnostics</strong>
        <span v-for="diagnostic in severeDiagnostics" :key="diagnostic.code">
          {{ diagnostic.code }} — {{ diagnostic.message }}
        </span>
      </div>

      <section class="workspace">
        <div class="panel control-panel">
          <div class="section-title">
            <div>
              <p class="eyebrow">Run control</p>
              <h2>New acquisition</h2>
            </div>
            <span class="safety">Configuration is validated before start</span>
          </div>

          <div class="fields">
            <label>
              Run ID
              <input v-model.trim="runId" autocomplete="off" placeholder="run-0055" />
            </label>
            <label>
              Requested by
              <input v-model.trim="requestedBy" autocomplete="name" />
            </label>
            <label>
              Run stop
              <select
                :value="stopMode"
                @change="setGlobalField('StopRunMode', ($event.target as HTMLSelectElement).value)"
              >
                <option value="MANUAL">Manual only</option>
                <option value="PRESET_TIME">After elapsed time</option>
                <option value="PRESET_COUNTS">After event count</option>
              </select>
            </label>
            <label v-if="stopMode === 'PRESET_TIME'">
              Preset time (seconds)
              <input
                :value="Number.parseFloat(presetTime)"
                type="number"
                min="0.001"
                step="1"
                @change="setGlobalField('PresetTime', ($event.target as HTMLInputElement).value)"
              />
            </label>
            <label v-if="stopMode === 'PRESET_COUNTS'">
              Preset event count
              <input
                :value="presetCounts"
                type="number"
                min="1"
                step="1"
                @change="setGlobalField('PresetCounts', ($event.target as HTMLInputElement).value)"
              />
            </label>
          </div>
          <p v-if="stopPolicyError" class="field-error" role="alert">{{ stopPolicyError }}</p>

          <div class="config-heading">
            <div>
              <label>Acquisition parameters</label>
              <p class="muted">
                {{ configurationDocument.fields.length }} settings from the backend template
              </p>
            </div>
            <div class="config-tools">
              <button class="link-button" type="button" @click="loadDefaultConfiguration">
                Reset sample
              </button>
              <button
                class="link-button"
                type="button"
                :disabled="!daq.configurationTemplate.value"
                @click="loadBackendConfiguration"
              >
                Use backend config
              </button>
              <button
                class="link-button"
                type="button"
                @click="showRawConfiguration = !showRawConfiguration"
              >
                {{ showRawConfiguration ? 'Use parameter editor' : 'Edit source' }}
              </button>
              <button class="link-button" type="button" @click="configFile?.click()">
                Import file
              </button>
            </div>
            <input
              ref="configFile"
              class="visually-hidden"
              type="file"
              accept=".txt,.cfg,text/plain"
              @change="loadConfiguration"
            />
          </div>
          <div
            v-if="!showRawConfiguration"
            class="parameter-editor"
            aria-label="Configuration parameters"
          >
            <div class="parameter-toolbar">
              <label>
                Find a parameter
                <input
                  v-model="parameterSearch"
                  type="search"
                  placeholder="Threshold, trigger, gain…"
                />
              </label>
              <span>{{ visibleFields.length }} shown</span>
            </div>
            <div class="section-tabs" role="tablist" aria-label="Parameter categories">
              <button
                v-for="section in sections"
                :key="section"
                type="button"
                role="tab"
                :aria-selected="selectedSection === section"
                :class="{ active: selectedSection === section }"
                @click="selectedSection = section"
              >
                {{ section }}
              </button>
            </div>
            <div class="parameter-list">
              <article
                v-for="field in visibleFields"
                :key="field.id"
                class="parameter-row"
                :class="{ 'mask-parameter-row': isMaskField(field) }"
              >
                <div class="parameter-copy">
                  <label :for="field.id">
                    {{ field.name }}
                    <span v-if="field.index !== undefined" class="override"
                      >Override {{ field.index }}</span
                    >
                  </label>
                  <p>{{ field.help || `JANUS configuration line ${field.line}` }}</p>
                </div>
                <label v-if="isBooleanField(field)" class="switch">
                  <input
                    :id="field.id"
                    type="checkbox"
                    :checked="field.value === '1'"
                    @change="
                      setField(field, ($event.target as HTMLInputElement).checked ? '1' : '0')
                    "
                  />
                  <span>{{ field.value === '1' ? 'Enabled' : 'Disabled' }}</span>
                </label>
                <div v-else-if="isMaskField(field)" class="mask-summary">
                  <div class="mask-board-values" :aria-label="`${field.name} values by board`">
                    <div
                      v-for="summary in maskBoardSummaries(field)"
                      :key="summary.board"
                      class="mask-board-value"
                    >
                      <strong>B{{ summary.board }}</strong>
                      <code>{{ summary.low }} · {{ summary.high }}</code>
                      <span v-if="summary.inherited">global</span>
                    </div>
                  </div>
                  <button type="button" class="secondary" @click="openMask(field)">
                    Configure channels
                  </button>
                </div>
                <select
                  v-else-if="field.options.length && field.name !== 'TempSensType'"
                  :id="field.id"
                  :value="field.value"
                  @change="setField(field, ($event.target as HTMLSelectElement).value)"
                >
                  <option v-if="!field.options.includes(field.value)" :value="field.value">
                    {{ field.value }}
                  </option>
                  <option v-for="option in field.options" :key="option" :value="option">
                    {{ option }}
                  </option>
                </select>
                <div v-else-if="field.name === 'TempSensType'" class="sensor-input">
                  <input
                    :id="field.id"
                    :value="field.value"
                    list="temperature-sensor-types"
                    placeholder="Sensor name or c0 c1 c2"
                    @change="setField(field, ($event.target as HTMLInputElement).value)"
                  />
                  <datalist id="temperature-sensor-types">
                    <option v-for="option in field.options" :key="option" :value="option" />
                  </datalist>
                  <small>Choose a known sensor or enter coefficients: c0 c1 c2</small>
                </div>
                <NumericField
                  v-else-if="numericConstraint(field)"
                  :field="field"
                  :constraint="numericConstraint(field)!"
                  @change="setField(field, $event)"
                />
                <input
                  v-else
                  :id="field.id"
                  :value="field.value"
                  @change="setField(field, ($event.target as HTMLInputElement).value)"
                />
                <div
                  v-if="parameterScope(field) === 'channel'"
                  class="channel-override-summary"
                  :aria-label="`${field.name} non-zero individual values`"
                >
                  <div class="channel-override-counts">
                    <template v-if="nonZeroChannelOverrides(field).some(Boolean)">
                      <span
                        v-for="(count, board) in nonZeroChannelOverrides(field)"
                        v-show="count"
                        :key="board"
                      >
                        B{{ board }}: {{ count }} non-zero
                      </span>
                    </template>
                    <span v-else>None non-zero</span>
                  </div>
                  <button
                    type="button"
                    class="channel-overrides-button secondary"
                    @click="activeChannelField = field"
                  >
                    Per-channel overrides
                  </button>
                </div>
                <div
                  v-if="
                    parameterScope(field) === 'board' &&
                    !isMaskField(field) &&
                    field.index === undefined
                  "
                  class="board-value-summary"
                  :aria-label="`${field.name} values by board`"
                >
                  <span v-for="item in boardValues(field)" :key="item.board">
                    <strong>B{{ item.board }}</strong> {{ item.value }}
                    <small v-if="item.inherited">global</small>
                  </span>
                  <button
                    type="button"
                    class="board-overrides-button secondary"
                    @click="activeBoardField = field"
                  >
                    Per-board overrides
                  </button>
                </div>
              </article>
              <p v-if="!visibleFields.length" class="empty">No parameters match this filter.</p>
            </div>
          </div>
          <div v-if="configurationErrors.length" class="configuration-errors" role="alert">
            <strong
              >{{ configurationErrors.length }} parameter value{{
                configurationErrors.length === 1 ? '' : 's'
              }}
              outside the allowed range</strong
            >
            <span v-for="item in configurationErrors.slice(0, 3)" :key="item.field.id"
              >{{ item.field.name }}: {{ item.error }}</span
            >
          </div>
          <textarea
            v-else
            id="configuration"
            v-model="configuration"
            aria-label="JANUS configuration source"
            spellcheck="false"
            placeholder="Paste or load the production JANUS configuration"
          />

          <ul
            v-if="daq.validationIssues.value.length"
            class="issues"
            aria-label="Validation issues"
          >
            <li
              v-for="issue in daq.validationIssues.value"
              :key="`${issue.sourceLine}-${issue.field}`"
            >
              <strong
                >Line {{ issue.sourceLine || '—' }} · {{ issue.field || 'configuration' }}</strong
              >
              {{ issue.message }}
            </li>
          </ul>

          <div class="options">
            <label
              ><input v-model="captureRaw" type="checkbox" /> Preserve complete raw batches</label
            >
            <label
              ><input v-model="journalTransport" type="checkbox" /> Journal socket evidence</label
            >
          </div>

          <p class="stop-policy-summary" role="status">{{ configuredStopPolicy }}</p>

          <div class="actions">
            <button
              class="secondary"
              type="button"
              :disabled="
                daq.busy.value ||
                !configuration ||
                configurationErrors.length > 0 ||
                !!stopPolicyError
              "
              @click="daq.validate(configuration)"
            >
              Validate
            </button>
            <button
              class="primary"
              type="button"
              :disabled="
                !daq.canStart.value ||
                !runId ||
                !requestedBy ||
                !configuration ||
                configurationErrors.length > 0 ||
                !!stopPolicyError
              "
              @click="
                daq.startRun({ runId, requestedBy, configuration, captureRaw, journalTransport })
              "
            >
              Start run
            </button>
            <button
              class="danger"
              type="button"
              :disabled="!daq.canStop.value || !requestedBy"
              @click="daq.stopRun(requestedBy)"
            >
              Stop and drain
            </button>
          </div>
        </div>

        <aside class="side-column">
          <section class="panel" aria-labelledby="pipeline-heading">
            <p class="eyebrow">Data path</p>
            <h2 id="pipeline-heading">Pipeline</h2>
            <dl class="metrics">
              <div>
                <dt>Decoded events</dt>
                <dd>{{ compact(daq.snapshot.value?.pipeline?.decodedEvents) }}</dd>
              </div>
              <div>
                <dt>Queue depth</dt>
                <dd>
                  {{ compact(daq.snapshot.value?.pipeline?.queueDepth) }} /
                  {{ compact(daq.snapshot.value?.pipeline?.queueCapacity) }}
                </dd>
              </div>
              <div>
                <dt>Rejected</dt>
                <dd>{{ compact(daq.snapshot.value?.pipeline?.rejectedBatches) }}</dd>
              </div>
              <div>
                <dt>Decode failures</dt>
                <dd>{{ compact(daq.snapshot.value?.pipeline?.decodeFailures) }}</dd>
              </div>
            </dl>
          </section>

          <section class="panel" aria-labelledby="storage-heading">
            <p class="eyebrow">Persistence</p>
            <h2 id="storage-heading">Storage</h2>
            <div class="health-line">
              <span
                class="health-dot"
                :class="{ healthy: daq.snapshot.value?.storage?.health === HealthStatus.OK }"
              />
              {{ healthLabel[daq.snapshot.value?.storage?.health ?? 0] }}
            </div>
            <p class="path">
              {{ daq.snapshot.value?.storage?.runDirectory || 'No run directory' }}
            </p>
            <p class="muted">
              {{ compact(daq.snapshot.value?.storage?.bytesWritten) }} bytes written
            </p>
          </section>
        </aside>
      </section>

      <section
        v-if="daq.latestCompletedRun.value"
        class="completed panel"
        aria-labelledby="completed-heading"
      >
        <div class="completed-summary">
          <div>
            <p class="eyebrow">Latest completed run</p>
            <h2 id="completed-heading">{{ daq.latestCompletedRun.value.runId }}</h2>
          </div>
          <dl class="completed-counts">
            <div>
              <dt>Termination</dt>
              <dd>{{ daq.latestCompletedRun.value.terminationReason || 'Completed normally' }}</dd>
            </div>
            <div>
              <dt>Events</dt>
              <dd>{{ compact(daq.latestCompletedRun.value.eventCount) }}</dd>
            </div>
            <div>
              <dt>Raw batches</dt>
              <dd>{{ compact(daq.latestCompletedRun.value.rawBatchCount) }}</dd>
            </div>
          </dl>
        </div>
        <div v-if="daq.latestCompletedRun.value.incomplete" class="incomplete" role="alert">
          This run is incomplete. Preserve and inspect its evidence before recovery.
        </div>
        <div class="artifacts" aria-label="Run artifacts">
          <article
            v-for="artifact in daq.latestCompletedRun.value.artifacts"
            :key="artifact.name"
            class="artifact"
          >
            <div>
              <strong>{{ artifact.name }}</strong>
              <span>{{ artifact.kind }} · {{ bytes(artifact.sizeBytes) }}</span>
            </div>
            <code :title="artifact.sha256">{{ artifact.sha256 || 'Digest unavailable' }}</code>
          </article>
          <p v-if="!daq.latestCompletedRun.value.artifacts.length" class="empty">
            No artifact metadata was returned.
          </p>
        </div>
      </section>

      <section class="completed panel" aria-labelledby="history-heading">
        <div class="section-title">
          <div>
            <p class="eyebrow">Persistent storage</p>
            <h2 id="history-heading">Run history</h2>
          </div>
          <button
            class="link-button"
            type="button"
            :disabled="daq.busy.value"
            @click="daq.refreshHistory()"
          >
            Refresh
          </button>
        </div>
        <div class="artifacts" aria-label="Stored runs">
          <article
            v-for="run in daq.runHistory.value"
            :key="run.runId"
            class="artifact history-run"
          >
            <div>
              <strong>{{ run.runId }}</strong>
              <span
                >{{ compact(run.eventCount) }} events ·
                {{ run.terminationReason || (run.incomplete ? 'Incomplete' : 'Completed') }}</span
              >
            </div>
            <div class="history-actions">
              <button
                v-for="artifact in run.artifacts"
                :key="artifact.name"
                class="link-button"
                type="button"
                :disabled="daq.busy.value"
                @click="daq.downloadArtifact(run.runId, artifact.name)"
              >
                {{ artifact.name }} · {{ bytes(artifact.sizeBytes) }}
              </button>
            </div>
          </article>
          <p v-if="!daq.runHistory.value.length" class="empty">No stored runs found.</p>
        </div>
      </section>

      <StatisticsTab
        :statistics="daq.snapshot.value?.statistics"
        :pipeline="daq.snapshot.value?.pipeline"
        :storage="daq.snapshot.value?.storage"
      />

      <PlotWorkspace
        :boards="boards"
        :running="daq.snapshot.value?.state === SystemState.RUNNING"
        :loading="daq.histogramsLoading.value"
        :datasets="daq.histogramDatasets.value"
        :theme="theme"
        @request="daq.loadHistograms"
      />

      <section class="boards-section" aria-labelledby="boards-heading">
        <div class="section-title">
          <div>
            <p class="eyebrow">Four-link topology</p>
            <h2 id="boards-heading">Detector boards</h2>
          </div>
          <div class="hv-global-actions">
            <button
              type="button"
              class="secondary"
              :disabled="!daq.canSwitchHV.value || !requestedBy"
              @click="daq.setHighVoltage([], true, requestedBy)"
            >
              All HV on
            </button>
            <button
              type="button"
              class="danger"
              :disabled="!daq.canSwitchHV.value || !requestedBy"
              @click="daq.setHighVoltage([], false, requestedBy)"
            >
              All HV off
            </button>
          </div>
        </div>
        <div class="board-grid">
          <article v-for="board in boards" :key="`${board.chain}-${board.node}`" class="board-card">
            <div class="board-title">
              <div>
                <span>Chain {{ board.chain }}</span
                ><strong>DT5202 · node {{ board.node }}</strong>
              </div>
              <span class="health-pill" :class="healthLabel[board.health].toLowerCase()">{{
                healthLabel[board.health]
              }}</span>
            </div>
            <div
              class="hv-state"
              :class="{
                on: board.hvOn,
                ramping: board.hvRamping,
                fault: board.hvOverCurrent || board.hvOverVoltage,
              }"
            >
              <span class="status-dot" />
              {{
                board.hvOverCurrent || board.hvOverVoltage
                  ? 'HV fault'
                  : board.hvRamping
                    ? 'Ramping'
                    : board.hvOn
                      ? 'HV on'
                      : 'HV off'
              }}
            </div>
            <dl class="metrics board-metrics">
              <div>
                <dt>FPGA</dt>
                <dd>0x{{ board.fpgaFirmware.toString(16).toUpperCase() }}</dd>
              </div>
              <div>
                <dt>Board temp.</dt>
                <dd>{{ board.boardTemperatureC.toFixed(1) }} °C</dd>
              </div>
              <div>
                <dt>Detector temp.</dt>
                <dd>{{ board.detectorTemperatureC.toFixed(1) }} °C</dd>
              </div>
              <div>
                <dt>FPGA temp.</dt>
                <dd>{{ board.fpgaTemperatureC.toFixed(1) }} °C</dd>
              </div>
              <div>
                <dt>HV temp.</dt>
                <dd>{{ board.hvTemperatureC.toFixed(1) }} °C</dd>
              </div>
              <div>
                <dt>Vmon</dt>
                <dd>{{ board.hvVoltageV.toFixed(2) }} V</dd>
              </div>
              <div>
                <dt>Imon</dt>
                <dd>{{ (board.hvCurrentA * 1000).toFixed(3) }} mA</dd>
              </div>
              <div>
                <dt>Events</dt>
                <dd>{{ compact(board.eventCount) }}</dd>
              </div>
            </dl>
            <button
              type="button"
              :class="board.hvOn ? 'danger' : 'secondary'"
              :disabled="!daq.canSwitchHV.value || !requestedBy"
              @click="daq.setHighVoltage([board.chain], !board.hvOn, requestedBy)"
            >
              Turn board {{ board.chain }} HV {{ board.hvOn ? 'off' : 'on' }}
            </button>
          </article>
          <p v-if="!boards.length" class="empty">Waiting for discovered boards…</p>
        </div>
      </section>
    </main>
    <MaskEditor
      v-if="activeMask"
      :title="`${activeMask.low.name.replace(/0$/, '')}${activeMask.low.index === undefined ? '' : ` · override ${activeMask.low.index}`}`"
      :variants="maskVariants()"
      @apply="applyMask"
      @close="activeMask = undefined"
    />
    <BoardOverrides
      v-if="activeBoardField && numericConstraint(activeBoardField)"
      :field="activeBoardField"
      :constraint="numericConstraint(activeBoardField)!"
      :overrides="boardOverrides(activeBoardField)"
      @apply="applyBoardOverrides"
      @close="activeBoardField = undefined"
    />
    <ChannelOverrides
      v-if="activeChannelField && numericConstraint(activeChannelField)"
      :field="activeChannelField"
      :constraint="numericConstraint(activeChannelField)!"
      :overrides="channelOverrides(activeChannelField)"
      :nominal-bias="
        activeChannelField.name === 'HV_IndivAdj'
          ? effectiveBoardNumericValues('HV_Vbias')
          : undefined
      "
      :adjustment-range="
        activeChannelField.name === 'HV_IndivAdj' ? globalValue('HV_Adjust_Range') : undefined
      "
      @apply="applyChannelOverrides"
      @close="activeChannelField = undefined"
    />
  </div>
</template>
