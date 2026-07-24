<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { create } from '@bufbuild/protobuf'
import defaultConfiguration from '../../test/fixtures/janus/config_same4_v3_good.txt?raw'
import { createDaqApi, type DaqApi } from './api'
import BoardOverrides from './BoardOverrides.vue'
import ChannelOverrides from './ChannelOverrides.vue'
import HvStatusPanel from './HvStatusPanel.vue'
import MaskEditor from './MaskEditor.vue'
import NumericField from './NumericField.vue'
import PlotWorkspace from './PlotWorkspace.vue'
import RunHistoryTable from './RunHistoryTable.vue'
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
import {
  ConfigurationLayer,
  DiagnosticSeverity,
  HealthStatus,
  SearchRunsRequestSchema,
  SystemState,
  type SearchRunsRequest,
} from './gen/pet/caen/daq/v1/system_pb'
import { compact, healthLabel, localDateTime, stateLabel } from './presentation'
import { useDaq } from './useDaq'
import { janusParameterCatalog, janusParameters, type JanusParameter } from './janus/catalog'

const props = defineProps<{ api?: DaqApi }>()
const api = props.api ?? createDaqApi()
const daq = useDaq(api)
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
const configurationDocument = ref(parseConfiguration(defaultConfiguration))
const configuration = computed({
  get: () => configurationDocument.value.source,
  set: (source: string) => (configurationDocument.value = parseConfiguration(source)),
})
const captureRaw = ref(false)
const journalTransport = ref(false)
const persistHistograms = ref(false)
const hdf5SegmentSizeMb = ref(500)
const configFile = ref<HTMLInputElement>()
type WorkspaceTab = 'acquisition' | 'statistics' | 'plots' | 'hardware' | 'runs'
const workspaceTabs: { id: WorkspaceTab; label: string; description: string }[] = [
  { id: 'acquisition', label: 'Acquisition', description: 'Configure and control runs' },
  { id: 'statistics', label: 'Statistics', description: 'Live rates and counters' },
  { id: 'plots', label: 'Plots', description: 'Histograms and channels' },
  { id: 'hardware', label: 'Hardware', description: 'Boards and high voltage' },
  { id: 'runs', label: 'Runs', description: 'History and artifacts' },
]
const activeWorkspaceTab = ref<WorkspaceTab>('acquisition')
const selectedSection = ref('Connect')
const parameterSearch = ref('')
const showRawConfiguration = ref(false)
const activeMask = ref<{ low: ConfigurationField; high: ConfigurationField }>()
const activeBoardField = ref<ConfigurationField>()
const activeChannelField = ref<ConfigurationField>()

type SearchValueType = 'integer' | 'real' | 'text'
type SearchScope = 'global' | 'board' | 'channel'
interface SearchPredicateInput {
  id: number
  parameter: string
  numericMatch: 'exact' | 'range'
  board: string
  channel: string
  value: string
  maximum: string
}
let nextSearchPredicateId = 1
function newSearchPredicate(): SearchPredicateInput {
  return {
    id: nextSearchPredicateId++,
    parameter: '',
    numericMatch: 'exact',
    board: '',
    channel: '',
    value: '',
    maximum: '',
  }
}
const searchPredicates = ref<SearchPredicateInput[]>([newSearchPredicate()])
const searchMinimumEvents = ref('')
const searchRunNumber = ref('')
const searchMaximumRunNumber = ref('')
const searchFormError = ref('')
const lastSearchRequest = ref<SearchRunsRequest>()
const openSearchParameterId = ref<number>()
const showRunSearch = ref(false)
function addSearchPredicate() {
  searchPredicates.value.push(newSearchPredicate())
}

function removeSearchPredicate(id: number) {
  searchPredicates.value = searchPredicates.value.filter((predicate) => predicate.id !== id)
  if (!searchPredicates.value.length) searchPredicates.value = [newSearchPredicate()]
}

function searchParameter(predicate: SearchPredicateInput): JanusParameter | undefined {
  return janusParameterCatalog.get(predicate.parameter)
}

function searchValueType(parameter: JanusParameter): SearchValueType {
  if (parameter.widget === 'integer') return 'integer'
  if (parameter.widget === 'number') return 'real'
  return 'text'
}

function searchOptions(parameter: JanusParameter): string[] {
  if (parameter.options.length) return parameter.options
  return parameter.widget === 'boolean' ? ['0', '1'] : []
}

function searchAllowedRange(parameter: JanusParameter) {
  if (parameter.min !== undefined && parameter.max !== undefined)
    return `Allowed: ${parameter.min}–${parameter.max}`
  if (parameter.min !== undefined) return `Minimum allowed: ${parameter.min}`
  if (parameter.max !== undefined) return `Maximum allowed: ${parameter.max}`
  return ''
}

function selectSearchParameter(predicate: SearchPredicateInput) {
  predicate.value = ''
  predicate.maximum = ''
  predicate.numericMatch = 'exact'
  predicate.board = ''
  predicate.channel = ''
}

function matchingSearchParameters(predicate: SearchPredicateInput) {
  const query = predicate.parameter.trim().toLocaleLowerCase()
  if (!query) return janusParameters
  return janusParameters.filter(
    (parameter) =>
      parameter.name.toLocaleLowerCase().includes(query) ||
      parameter.section.toLocaleLowerCase().includes(query) ||
      parameter.description.toLocaleLowerCase().includes(query),
  )
}

function chooseSearchParameter(predicate: SearchPredicateInput, parameter: JanusParameter) {
  predicate.parameter = parameter.name
  selectSearchParameter(predicate)
  openSearchParameterId.value = undefined
}

function closeSearchParameterList(event: FocusEvent, predicateId: number) {
  const container = event.currentTarget as HTMLElement
  if (event.relatedTarget instanceof Node && container.contains(event.relatedTarget)) return
  if (openSearchParameterId.value === predicateId) openSearchParameterId.value = undefined
}

function buildSearchRequest(pageToken = ''): SearchRunsRequest | undefined {
  searchFormError.value = ''
  try {
    const configuration = searchPredicates.value
      .filter((predicate) => predicate.parameter.trim() || String(predicate.value).trim())
      .map((predicate) => {
        const requestedValue = String(predicate.value).trim()
        const requestedMaximum = String(predicate.maximum).trim()
        if (!predicate.parameter.trim() || !requestedValue)
          throw new Error('Each configuration filter needs a parameter and value.')
        const parameter = searchParameter(predicate)
        if (!parameter) throw new Error(`Choose "${predicate.parameter}" from the parameter list.`)
        const valueType = searchValueType(parameter)
        const scope = parameter.scope as SearchScope
        const layer =
          scope === 'global' ? ConfigurationLayer.REQUESTED : ConfigurationLayer.RESOLVED
        const scopeValue =
          scope === 'global'
            ? { case: 'global' as const, value: true }
            : !predicate.board
              ? undefined
              : scope === 'board' || !predicate.channel
                ? { case: 'board' as const, value: Number(predicate.board) }
                : {
                    case: 'channel' as const,
                    value: { board: Number(predicate.board), channel: Number(predicate.channel) },
                  }
        const scopedNumbers =
          scopeValue?.case === 'board'
            ? [scopeValue.value]
            : scopeValue?.case === 'channel'
              ? [scopeValue.value.board, scopeValue.value.channel]
              : []
        if (scopedNumbers.some((value) => !Number.isInteger(value) || value < 0))
          throw new Error('Board and channel must be non-negative integers.')
        const requestScope = scopeValue ? { scope: scopeValue } : undefined
        if (valueType === 'text') {
          return {
            parameter: parameter.name,
            layer,
            scope: requestScope,
            comparison: { case: 'text' as const, value: { equal: requestedValue } },
          }
        }
        const isRange = predicate.numericMatch === 'range'
        if (isRange && !requestedMaximum)
          throw new Error('Range filters need both a minimum and maximum.')
        if (valueType === 'integer') {
          const value = BigInt(requestedValue)
          return {
            parameter: parameter.name,
            layer,
            scope: requestScope,
            comparison: {
              case: 'integer' as const,
              value: isRange
                ? { minimum: value, maximum: BigInt(requestedMaximum) }
                : { equal: value },
            },
          }
        }
        const value = Number(requestedValue)
        const maximum = Number(requestedMaximum)
        if (!Number.isFinite(value) || (isRange && !Number.isFinite(maximum)))
          throw new Error('Real-number filters need valid numeric values.')
        return {
          parameter: parameter.name,
          layer,
          scope: requestScope,
          comparison: {
            case: 'real' as const,
            value: isRange ? { minimum: value, maximum } : { equal: value },
          },
        }
      })
    const minimumEvents = String(searchMinimumEvents.value).trim()
    const requestedRunNumber = String(searchRunNumber.value).trim()
    const requestedMaximumRunNumber = String(searchMaximumRunNumber.value).trim()
    const minimumEventCount = minimumEvents ? BigInt(minimumEvents) : 0n
    const runNumber = requestedRunNumber ? BigInt(requestedRunNumber) : undefined
    const maximumRunNumber = requestedMaximumRunNumber
      ? BigInt(searchMaximumRunNumber.value)
      : undefined
    if (runNumber !== undefined && runNumber < 0n)
      throw new Error('Run number must be non-negative.')
    if (maximumRunNumber !== undefined && maximumRunNumber < 0n)
      throw new Error('Maximum run number must be non-negative.')
    if (maximumRunNumber !== undefined && runNumber === undefined)
      throw new Error('A maximum run number requires a run number.')
    if (runNumber !== undefined && maximumRunNumber !== undefined && runNumber > maximumRunNumber)
      throw new Error('Run number must not exceed the maximum.')
    return create(SearchRunsRequestSchema, {
      configuration,
      minimumEventCount,
      runNumber,
      maximumRunNumber,
      limit: 20,
      pageToken,
    })
  } catch (reason) {
    searchFormError.value = reason instanceof Error ? reason.message : String(reason)
    return undefined
  }
}

async function submitRunSearch() {
  const request = buildSearchRequest()
  if (!request) return
  lastSearchRequest.value = request
  await daq.searchRuns(request)
}

async function loadMoreSearchResults() {
  if (!lastSearchRequest.value || !daq.searchNextPageToken.value) return
  const request = create(SearchRunsRequestSchema, {
    ...lastSearchRequest.value,
    pageToken: daq.searchNextPageToken.value,
  })
  await daq.searchRuns(request, true)
}

function clearRunSearch() {
  searchPredicates.value = [newSearchPredicate()]
  searchMinimumEvents.value = ''
  searchRunNumber.value = ''
  searchMaximumRunNumber.value = ''
  searchFormError.value = ''
  lastSearchRequest.value = undefined
  daq.clearSearch()
}

const sections = computed(() => [
  'All',
  ...new Set(configurationDocument.value.fields.map((field) => field.section)),
])
const visibleFields = computed(() => {
  const query = selectedSection.value === 'All' ? parameterSearch.value.trim().toLowerCase() : ''
  return configurationDocument.value.fields.filter(
    (field) =>
      !(isMaskField(field) && (field.name.endsWith('1') || field.index !== undefined)) &&
      !(
        parameterScope(field) === 'board' &&
        field.index !== undefined &&
        configurationDocument.value.fields.some(
          (candidate) =>
            candidate.name === field.name &&
            candidate.index === undefined &&
            candidate.channel === undefined,
        )
      ) &&
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
const backendOnline = computed(() => daq.connected.value && !daq.stale.value)
const hardwareDisconnected = computed(() => daq.snapshot.value?.state === SystemState.DISCONNECTED)
const hardwareConnecting = computed(() => daq.snapshot.value?.state === SystemState.CONNECTING)
const hardwareConnectionLabel = computed(() => {
  if (!backendOnline.value) return 'Hardware state unknown'
  if (hardwareConnecting.value) return 'Hardware connecting'
  return hardwareDisconnected.value ? 'Hardware disconnected' : 'Hardware connected'
})
const hardwareActionLabel = computed(() => {
  if (hardwareConnecting.value) return 'Connecting hardware…'
  return hardwareDisconnected.value ? 'Connect hardware' : 'Disconnect hardware'
})
const hardwareActionDisabled = computed(() => {
  if (hardwareConnecting.value) return true
  return hardwareDisconnected.value
    ? !daq.canConnectHardware.value
    : !daq.canDisconnectHardware.value
})

function toggleHardwareConnection() {
  if (hardwareDisconnected.value) {
    void daq.connectHardware()
  } else {
    void daq.disconnectHardware()
  }
}
const enabledLinkCount = computed(
  () => daq.snapshot.value?.chains.filter((chain) => chain.enabled).length ?? 0,
)
const enabledLinkLabel = computed(
  () => `${enabledLinkCount.value} enabled link${enabledLinkCount.value === 1 ? '' : 's'}`,
)
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

function selectAdjacentWorkspaceTab(event: KeyboardEvent, index: number) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const last = workspaceTabs.length - 1
  const nextIndex =
    event.key === 'Home'
      ? 0
      : event.key === 'End'
        ? last
        : event.key === 'ArrowRight'
          ? (index + 1) % workspaceTabs.length
          : (index - 1 + workspaceTabs.length) % workspaceTabs.length
  activeWorkspaceTab.value = workspaceTabs[nextIndex].id
  requestAnimationFrame(() => {
    document.getElementById(`workspace-tab-${workspaceTabs[nextIndex].id}`)?.focus()
  })
}

onMounted(() => daq.connect())
</script>

<template>
  <div class="shell">
    <main>
      <section class="hero panel" aria-labelledby="system-heading">
        <div class="hero-overview">
          <div class="product-identity">
            <p class="eyebrow">PET detector control</p>
            <h1>CAEN acquisition</h1>
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
          <div class="state-summary">
            <p class="eyebrow">System state</p>
            <h2 id="system-heading">{{ state }}</h2>
            <p class="muted">
              Sequence {{ compact(daq.snapshot.value?.sequence) }} ·
              {{ enabledLinkLabel }}
            </p>
          </div>
          <HvStatusPanel :boards="boards" />
        </div>
        <div class="hero-control" aria-label="Operations">
          <div class="system-utilities">
            <div class="connection" role="status" aria-live="polite">
              <span class="status-dot" :class="{ live: backendOnline }" aria-hidden="true" />
              <span class="backend-state" :class="{ offline: !backendOnline }">
                {{ backendOnline ? 'Backend online' : 'Backend offline' }}
              </span>
              <span
                class="status-dot hardware-status-dot"
                :class="{
                  live: backendOnline && !hardwareDisconnected && !hardwareConnecting,
                  disconnected: backendOnline && hardwareDisconnected,
                }"
                aria-hidden="true"
              />
              <span class="hardware-state">{{ hardwareConnectionLabel }}</span>
              <button
                v-if="!backendOnline"
                type="button"
                class="connection-action backend-retry-action"
                @click="daq.connect()"
              >
                Retry backend
              </button>
              <button
                v-if="backendOnline && !hardwareConnecting"
                type="button"
                class="connection-action hardware-connection-action"
                :class="{ connect: hardwareDisconnected, disconnect: !hardwareDisconnected }"
                :disabled="hardwareActionDisabled"
                @click="toggleHardwareConnection"
              >
                {{ hardwareActionLabel }}
              </button>
            </div>
          </div>
          <div class="run-control">
            <div class="actions hero-actions">
              <button
                v-if="!daq.snapshot.value?.currentRun"
                class="primary"
                type="button"
                :disabled="
                  !daq.canStart.value ||
                  !configuration ||
                  configurationErrors.length > 0 ||
                  !!stopPolicyError ||
                  !Number.isInteger(hdf5SegmentSizeMb) ||
                  hdf5SegmentSizeMb < 1 ||
                  hdf5SegmentSizeMb > 1048576
                "
                @click="
                  daq.startRun({
                    configuration,
                    captureRaw,
                    journalTransport,
                    persistHistograms,
                    hdf5SegmentSizeMb,
                  })
                "
              >
                Start run
              </button>
              <button
                v-else
                class="danger"
                type="button"
                :disabled="!daq.canStop.value"
                @click="daq.stopRun()"
              >
                Stop and drain
              </button>
            </div>
            <div v-if="daq.snapshot.value?.currentRun" class="run-now">
              <span>
                Active run <strong>{{ daq.snapshot.value.currentRun.runId }}</strong> ·
                {{ compact(daq.snapshot.value.currentRun.eventCount) }} events
              </span>
              <small>{{ activeStopPolicy() }}</small>
            </div>
            <div v-else class="run-now quiet" role="status">
              <span>No active run</span>
              <small>{{ configuredStopPolicy }}</small>
            </div>
          </div>
        </div>
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

      <nav class="workspace-tabs" role="tablist" aria-label="Operator workspace">
        <button
          v-for="(tab, index) in workspaceTabs"
          :id="`workspace-tab-${tab.id}`"
          :key="tab.id"
          type="button"
          role="tab"
          :aria-controls="`workspace-panel-${tab.id}`"
          :aria-selected="activeWorkspaceTab === tab.id"
          :tabindex="activeWorkspaceTab === tab.id ? 0 : -1"
          @click="activeWorkspaceTab = tab.id"
          @keydown="selectAdjacentWorkspaceTab($event, index)"
        >
          <span>{{ tab.label }}</span>
          <small>{{ tab.description }}</small>
        </button>
      </nav>

      <section
        v-show="activeWorkspaceTab === 'acquisition'"
        id="workspace-panel-acquisition"
        class="workspace"
        role="tabpanel"
        aria-labelledby="workspace-tab-acquisition"
      >
        <div class="panel control-panel">
          <div class="section-title">
            <div>
              <p class="eyebrow">Run control</p>
              <h2>New acquisition</h2>
            </div>
            <span class="safety">Configuration is validated before start</span>
          </div>

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
                {{ showRawConfiguration ? 'Use parameter editor' : 'View raw configuration' }}
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
            <div v-if="selectedSection === 'All'" class="parameter-toolbar">
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
                      <span>{{ summary.inherited ? 'inherited' : 'override' }}</span>
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
                    @input="setField(field, ($event.target as HTMLInputElement).value)"
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
                  @input="setField(field, ($event.target as HTMLInputElement).value)"
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
                    <small>{{ item.inherited ? 'inherited' : 'override' }}</small>
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
              <template v-if="selectedSection === 'RunCtrl'">
                <article class="parameter-row">
                  <div class="parameter-copy">
                    <label for="persist-histograms">Store run histograms</label>
                    <p>
                      Write a standalone run_&lt;run-id&gt;.histograms.h5 artifact for later
                      viewing.
                    </p>
                  </div>
                  <label class="switch">
                    <input id="persist-histograms" v-model="persistHistograms" type="checkbox" />
                    <span>{{ persistHistograms ? 'Enabled' : 'Disabled' }}</span>
                  </label>
                </article>
                <article class="parameter-row">
                  <div class="parameter-copy">
                    <label for="capture-raw">Preserve complete raw batches</label>
                    <p>Application output setting. Keep complete raw batches from the backend.</p>
                  </div>
                  <label class="switch">
                    <input id="capture-raw" v-model="captureRaw" type="checkbox" />
                    <span>{{ captureRaw ? 'Enabled' : 'Disabled' }}</span>
                  </label>
                </article>
                <article class="parameter-row">
                  <div class="parameter-copy">
                    <label for="journal-transport">Journal socket evidence</label>
                    <p>Application output setting. Record socket traffic for diagnostics.</p>
                  </div>
                  <label class="switch">
                    <input id="journal-transport" v-model="journalTransport" type="checkbox" />
                    <span>{{ journalTransport ? 'Enabled' : 'Disabled' }}</span>
                  </label>
                </article>
                <article class="parameter-row">
                  <div class="parameter-copy">
                    <label for="hdf5-segment-size">HDF5 file size (MiB)</label>
                    <p>
                      Application output setting. Files rotate as run_&lt;run-id&gt;.0000.h5,
                      .0001.h5, and so on.
                    </p>
                  </div>
                  <input
                    id="hdf5-segment-size"
                    v-model.number="hdf5SegmentSizeMb"
                    aria-label="HDF5 file size in MiB"
                    type="number"
                    min="1"
                    max="1048576"
                    step="1"
                  />
                </article>
              </template>
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
            v-if="showRawConfiguration"
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
        v-show="activeWorkspaceTab === 'runs'"
        id="workspace-panel-runs"
        class="completed panel"
        role="tabpanel"
        aria-labelledby="workspace-tab-runs"
      >
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
        <div class="run-search">
          <button
            class="search-toggle"
            type="button"
            aria-controls="run-search-form"
            :aria-expanded="showRunSearch"
            @click="showRunSearch = !showRunSearch"
          >
            <div>
              <strong>Search configurations</strong>
              <span>Filter runs by configuration, run number, or event count.</span>
            </div>
            <span aria-hidden="true">{{ showRunSearch ? 'Hide' : 'Open' }}</span>
          </button>
          <form
            v-if="showRunSearch"
            id="run-search-form"
            class="search-form"
            aria-label="Search stored runs"
            @submit.prevent="submitRunSearch"
          >
            <div class="search-heading">
              <p>All filters must match. Numeric values use the catalog's canonical units.</p>
              <button class="link-button" type="button" @click="addSearchPredicate">
                Add filter
              </button>
            </div>
            <div
              v-for="(predicate, index) in searchPredicates"
              :key="predicate.id"
              class="search-predicate"
            >
              <div
                class="search-parameter-field"
                @focusout="closeSearchParameterList($event, predicate.id)"
              >
                <label :for="`search-parameter-${predicate.id}`">Parameter</label>
                <input
                  :id="`search-parameter-${predicate.id}`"
                  v-model="predicate.parameter"
                  :aria-label="`Parameter ${index + 1}`"
                  role="combobox"
                  aria-autocomplete="list"
                  :aria-expanded="openSearchParameterId === predicate.id"
                  :aria-controls="`search-parameters-${predicate.id}`"
                  placeholder="Search parameters…"
                  autocomplete="off"
                  @focus="openSearchParameterId = predicate.id"
                  @click="openSearchParameterId = predicate.id"
                  @input="selectSearchParameter(predicate)"
                  @change="selectSearchParameter(predicate)"
                />
                <div
                  v-if="openSearchParameterId === predicate.id"
                  :id="`search-parameters-${predicate.id}`"
                  class="search-parameter-options"
                  role="listbox"
                  :aria-label="`Parameters ${index + 1}`"
                >
                  <button
                    v-for="parameter in matchingSearchParameters(predicate)"
                    :key="parameter.name"
                    type="button"
                    role="option"
                    :aria-selected="predicate.parameter === parameter.name"
                    @click="chooseSearchParameter(predicate, parameter)"
                  >
                    <strong>{{ parameter.name }}</strong>
                    <span>{{ parameter.section }} — {{ parameter.description }}</span>
                  </button>
                  <p v-if="!matchingSearchParameters(predicate).length">No parameters found.</p>
                </div>
              </div>
              <div v-if="searchParameter(predicate)" class="search-scope">
                Scope
                <strong>{{ searchParameter(predicate)?.scope }}</strong>
              </div>
              <label v-if="searchParameter(predicate)?.scope !== 'global'">
                Board
                <select
                  v-model="predicate.board"
                  :aria-label="`Board ${index + 1}`"
                  @change="predicate.channel = ''"
                >
                  <option value="">All boards</option>
                  <option v-for="board in 4" :key="board - 1" :value="String(board - 1)">
                    Board {{ board - 1 }}
                  </option>
                </select>
              </label>
              <label v-if="searchParameter(predicate)?.scope === 'channel'">
                Channel
                <select
                  v-model="predicate.channel"
                  :disabled="!predicate.board"
                  :aria-label="`Channel ${index + 1}`"
                >
                  <option value="">All channels</option>
                  <option v-for="channel in 64" :key="channel - 1" :value="String(channel - 1)">
                    Channel {{ channel - 1 }}
                  </option>
                </select>
              </label>
              <label
                v-if="
                  searchParameter(predicate) &&
                  searchValueType(searchParameter(predicate)!) !== 'text'
                "
                class="search-match"
              >
                Match
                <select v-model="predicate.numericMatch" :aria-label="`Match ${index + 1}`">
                  <option value="exact">Exact value</option>
                  <option value="range">Range</option>
                </select>
              </label>
              <label v-if="searchParameter(predicate)">
                <span>
                  {{
                    searchValueType(searchParameter(predicate)!) === 'text' ||
                    predicate.numericMatch === 'exact'
                      ? 'Value'
                      : 'Minimum'
                  }}
                  <small
                    v-if="
                      searchValueType(searchParameter(predicate)!) !== 'text' &&
                      searchAllowedRange(searchParameter(predicate)!)
                    "
                    class="search-range-hint"
                  >
                    {{ searchAllowedRange(searchParameter(predicate)!) }}
                  </small>
                </span>
                <select
                  v-if="searchOptions(searchParameter(predicate)!).length"
                  v-model="predicate.value"
                  :aria-label="`Value ${index + 1}`"
                >
                  <option value="" disabled>Select a value…</option>
                  <option
                    v-for="option in searchOptions(searchParameter(predicate)!)"
                    :key="option"
                    :value="option"
                  >
                    {{ option }}
                  </option>
                </select>
                <input
                  v-else
                  v-model="predicate.value"
                  :type="
                    searchValueType(searchParameter(predicate)!) === 'text' ? 'text' : 'number'
                  "
                  :min="searchParameter(predicate)?.min"
                  :max="searchParameter(predicate)?.max"
                  :step="searchParameter(predicate)?.step"
                  :aria-label="`Value ${index + 1}`"
                />
              </label>
              <label
                v-if="
                  searchParameter(predicate) &&
                  searchValueType(searchParameter(predicate)!) !== 'text' &&
                  predicate.numericMatch === 'range'
                "
              >
                Maximum
                <input
                  v-model="predicate.maximum"
                  type="number"
                  :min="searchParameter(predicate)?.min"
                  :max="searchParameter(predicate)?.max"
                  :step="searchParameter(predicate)?.step"
                  :aria-label="`Maximum ${index + 1}`"
                />
              </label>
              <button
                v-if="searchPredicates.length > 1"
                class="link-button remove-filter"
                type="button"
                :aria-label="`Remove filter ${index + 1}`"
                @click="removeSearchPredicate(predicate.id)"
              >
                Remove
              </button>
            </div>
            <div class="search-metadata">
              <label>
                Run number
                <input v-model="searchRunNumber" type="number" min="0" aria-label="Run number" />
              </label>
              <label>
                Maximum run number <span class="optional">(optional)</span>
                <input
                  v-model="searchMaximumRunNumber"
                  type="number"
                  min="0"
                  aria-label="Maximum run number"
                />
              </label>
              <label>
                Minimum events
                <input v-model="searchMinimumEvents" type="number" min="0" />
              </label>
            </div>
            <p v-if="searchFormError" class="field-error" role="alert">{{ searchFormError }}</p>
            <p v-if="daq.searchError.value" class="field-error" role="alert">
              Search failed: {{ daq.searchError.value }}
            </p>
            <div class="actions">
              <button class="primary" type="submit" :disabled="daq.searchLoading.value">
                {{ daq.searchLoading.value ? 'Searching…' : 'Search runs' }}
              </button>
              <button type="button" :disabled="daq.searchLoading.value" @click="clearRunSearch">
                Clear
              </button>
            </div>
          </form>
        </div>
        <div v-if="daq.searchPerformed.value" class="search-results" aria-live="polite">
          <p
            v-if="
              !daq.searchLoading.value && !daq.searchResults.value.length && !daq.searchError.value
            "
            class="empty"
          >
            No runs match these filters.
          </p>
          <RunHistoryTable
            v-else
            :runs="daq.searchResults.value"
            :configuration="api.runConfiguration"
            :download-artifact="daq.downloadArtifact"
            label="Search results"
          />
          <button
            v-if="daq.searchNextPageToken.value"
            class="link-button load-more"
            type="button"
            :disabled="daq.searchLoading.value"
            @click="loadMoreSearchResults"
          >
            Load more
          </button>
        </div>
        <RunHistoryTable
          v-else
          :runs="daq.runHistory.value"
          :configuration="api.runConfiguration"
          :download-artifact="daq.downloadArtifact"
        />
      </section>

      <div
        v-show="activeWorkspaceTab === 'statistics'"
        id="workspace-panel-statistics"
        role="tabpanel"
        aria-labelledby="workspace-tab-statistics"
      >
        <StatisticsTab
          :statistics="daq.snapshot.value?.statistics"
          :pipeline="daq.snapshot.value?.pipeline"
          :storage="daq.snapshot.value?.storage"
          :runs="daq.runHistory.value"
          :live-run-id="daq.snapshot.value?.currentRun?.runId"
        />
      </div>

      <div
        v-show="activeWorkspaceTab === 'plots'"
        id="workspace-panel-plots"
        role="tabpanel"
        aria-labelledby="workspace-tab-plots"
      >
        <PlotWorkspace
          :boards="boards"
          :runs="daq.runHistory.value"
          :active-run-id="daq.snapshot.value?.currentRun?.runId"
          :running="daq.snapshot.value?.state === SystemState.RUNNING"
          :loading="daq.histogramsLoading.value"
          :datasets="daq.histogramDatasets.value"
          :theme="theme"
          @request="daq.loadHistograms"
        />
      </div>

      <section
        v-show="activeWorkspaceTab === 'hardware'"
        id="workspace-panel-hardware"
        class="boards-section"
        role="tabpanel"
        aria-labelledby="workspace-tab-hardware"
      >
        <div class="section-title">
          <div>
            <p class="eyebrow">Four-link topology</p>
            <h2 id="boards-heading">Detector boards</h2>
          </div>
          <div class="hv-global-actions">
            <button
              type="button"
              class="primary"
              :disabled="!daq.canSwitchHV.value"
              @click="daq.setHighVoltage([], true)"
            >
              All HV on
            </button>
            <button
              type="button"
              class="danger"
              :disabled="!daq.canSwitchHV.value"
              @click="daq.setHighVoltage([], false)"
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
              <div>
                <dt>Telemetry updated</dt>
                <dd>{{ localDateTime(board.telemetryObservedAt) }}</dd>
              </div>
            </dl>
            <button
              type="button"
              :class="board.hvOn ? 'danger' : 'secondary'"
              :disabled="!daq.canSwitchHV.value"
              @click="daq.setHighVoltage([board.chain], !board.hvOn)"
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
