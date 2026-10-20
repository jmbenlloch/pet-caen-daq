import { create } from '@bufbuild/protobuf'
import { computed, onBeforeUnmount, readonly, ref } from 'vue'
import type { DaqApi } from './api'
import {
  AppliedConfigurationSource,
  StartRunRequestSchema,
  StopRunRequestSchema,
  SystemState,
  type AppliedConfiguration,
  type TelemetrySnapshot,
  type RunSummary,
  type ValidationIssue,
  type HistogramDataset,
  type HistogramKind,
  type HistogramSelection,
  type SearchRunsRequest,
} from './gen/pet/caen/daq/v1/system_pb'

const staleAfterMs = 5_000
const operatorIdentity = 'operator'

function configurationRequestId() {
  // getRandomValues also works on the plain HTTP origins used by the DAQ.
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('')
}

// How the editor text relates to the configuration on the boards: nothing
// applied (disconnected), identical, edited since, or applied outside this
// page (another session, or before a reload) so it cannot be compared.
export type ConfigurationApplyStatus = 'none' | 'current' | 'changed' | 'unknown'

// The run number is added after acquisition starts, so it is not part of the
// identity of an application.
function appliedConfigurationKey(applied: AppliedConfiguration | undefined) {
  if (!applied) return ''
  const at = applied.appliedAt
  return `${at?.seconds ?? 0n}.${at?.nanos ?? 0}:${applied.source}:${applied.requestedBy}:${applied.configurationRequestId}`
}

export function useDaq(api: DaqApi) {
  const snapshot = ref<TelemetrySnapshot>()
  const connected = ref(false)
  const stale = ref(true)
  const error = ref('')
  const busy = ref(false)
  const startingRun = ref(false)
  const applyingConfiguration = ref(false)
  const lastApplied = ref<{ text: string; key: string }>()
  const validationIssues = ref<ValidationIssue[]>([])
  const latestCompletedRun = ref<RunSummary>()
  const runHistory = ref<RunSummary[]>([])
  const runHistoryNextPageToken = ref('')
  const runHistoryLoading = ref(false)
  const searchResults = ref<RunSummary[]>([])
  const searchNextPageToken = ref('')
  const searchLoading = ref(false)
  const searchError = ref('')
  const searchPerformed = ref(false)
  const configurationTemplate = ref('')
  const histogramDatasets = ref<HistogramDataset[]>([])
  const histogramsLoading = ref(false)
  let streamController: AbortController | undefined
  let staleTimer: number | undefined
  let reconnectTimer: number | undefined
  let histogramRequestSequence = 0
  let historyRequestSequence = 0
  let searchRequestSequence = 0
  let stopped = false

  function invalidateHistogramRequests() {
    histogramRequestSequence++
    histogramsLoading.value = false
  }

  function accept(next: TelemetrySnapshot | undefined) {
    if (!next) return
    const activeRunChanged = snapshot.value?.currentRun?.runId !== next.currentRun?.runId
    if (activeRunChanged) invalidateHistogramRequests()
    snapshot.value = next
    if (next.latestCompletedRun) latestCompletedRun.value = next.latestCompletedRun
    connected.value = true
    stale.value = false
    window.clearTimeout(staleTimer)
    staleTimer = window.setTimeout(() => (stale.value = true), staleAfterMs)
  }

  async function connect() {
    if (stopped) return
    window.clearTimeout(reconnectTimer)
    streamController?.abort()
    const controller = new AbortController()
    streamController = controller
    try {
      void refreshHistory()
      if (!configurationTemplate.value) {
        configurationTemplate.value = await api.configurationTemplate()
      }
      accept(await api.snapshot())
      error.value = ''
      for await (const next of api.telemetry(controller.signal)) accept(next)
      if (!controller.signal.aborted) throw new Error('Telemetry stream ended')
    } catch (reason) {
      if (controller.signal.aborted || stopped) return
      connected.value = false
      stale.value = true
      error.value = reason instanceof Error ? reason.message : String(reason)
      reconnectTimer = window.setTimeout(connect, 2_000)
    }
  }

  async function refreshHistory() {
    const sequence = ++historyRequestSequence
    runHistoryLoading.value = true
    try {
      const response = await api.listRuns(20)
      if (sequence !== historyRequestSequence) return
      runHistory.value = response.runs
      runHistoryNextPageToken.value = response.nextPageToken
    } catch (reason) {
      if (sequence === historyRequestSequence)
        error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      if (sequence === historyRequestSequence) runHistoryLoading.value = false
    }
  }

  async function loadMoreHistory() {
    if (!runHistoryNextPageToken.value || runHistoryLoading.value) return
    const sequence = ++historyRequestSequence
    runHistoryLoading.value = true
    try {
      const response = await api.listRuns(20, runHistoryNextPageToken.value)
      if (sequence !== historyRequestSequence) return
      runHistory.value = [...runHistory.value, ...response.runs]
      runHistoryNextPageToken.value = response.nextPageToken
    } catch (reason) {
      if (sequence === historyRequestSequence)
        error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      if (sequence === historyRequestSequence) runHistoryLoading.value = false
    }
  }

  async function searchRuns(request: SearchRunsRequest, append = false) {
    const sequence = ++searchRequestSequence
    searchLoading.value = true
    searchError.value = ''
    if (!append) {
      searchPerformed.value = true
      searchResults.value = []
      searchNextPageToken.value = ''
    }
    try {
      const response = await api.searchRuns(request)
      if (sequence !== searchRequestSequence) return
      if (request.runNumber !== undefined) {
        const minimum = request.runNumber
        const maximum = request.maximumRunNumber ?? minimum
        const invalidRun = response.runs.find((run) => {
          try {
            const runNumber = BigInt(run.runId)
            return runNumber < minimum || runNumber > maximum
          } catch {
            return true
          }
        })
        if (invalidRun)
          throw new Error(
            'The server did not apply the run-number filter. Restart it with the updated backend.',
          )
      }
      searchResults.value = append ? [...searchResults.value, ...response.runs] : response.runs
      searchNextPageToken.value = response.nextPageToken
    } catch (reason) {
      if (sequence === searchRequestSequence)
        searchError.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      if (sequence === searchRequestSequence) searchLoading.value = false
    }
  }

  function clearSearch() {
    searchRequestSequence++
    searchResults.value = []
    searchNextPageToken.value = ''
    searchLoading.value = false
    searchError.value = ''
    searchPerformed.value = false
  }

  async function downloadArtifact(runId: string, artifactName: string) {
    busy.value = true
    error.value = ''
    try {
      const blob = await api.downloadArtifact(runId, artifactName)
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = artifactName
      anchor.click()
      URL.revokeObjectURL(url)
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
    }
  }

  async function validate(configuration: string) {
    busy.value = true
    error.value = ''
    try {
      const result = await api.validate(configuration)
      validationIssues.value = result.issues
      return result.valid
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
      return false
    } finally {
      busy.value = false
    }
  }

  async function startRun(input: {
    configuration: string
    captureRaw: boolean
    journalTransport: boolean
    persistHistograms: boolean
    hdf5SegmentSizeMb: number
    hdf5Compression: string
  }) {
    busy.value = true
    startingRun.value = true
    error.value = ''
    const requestId = configurationRequestId()
    try {
      const valid = await validate(input.configuration)
      if (!valid) return
      busy.value = true
      const result = await api.start(
        create(StartRunRequestSchema, {
          requestedBy: operatorIdentity,
          configurationRequestId: requestId,
          janusConfiguration: input.configuration,
          captureRaw: input.captureRaw,
          journalTransport: input.journalTransport,
          persistHistograms: input.persistHistograms,
          hdf5SegmentSizeMb: input.hdf5SegmentSizeMb,
          hdf5Compression: input.hdf5Compression,
        }),
      )
      accept(result.snapshot)
      rememberApplied(input.configuration, result.snapshot)
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
      await rememberAfterFailure(input.configuration, requestId, AppliedConfigurationSource.RUN)
    } finally {
      startingRun.value = false
      busy.value = false
    }
  }

  function rememberApplied(configuration: string, next: TelemetrySnapshot | undefined) {
    const key = appliedConfigurationKey(next?.appliedConfiguration)
    lastApplied.value = key ? { text: configuration, key } : undefined
  }

  // A request can fail after the boards were written (an audit rejection).
  // The backend echoes our request ID even after an audit rejection. The
  // shared operator identity and a newer timestamp do not prove ownership.
  async function rememberAfterFailure(
    configuration: string,
    requestId: string,
    source: AppliedConfigurationSource,
  ) {
    try {
      const next = await api.snapshot()
      const applied = next?.appliedConfiguration
      accept(next)
      if (applied?.source !== source || applied.configurationRequestId !== requestId) return
      rememberApplied(configuration, next)
    } catch {
      // The original command error remains the report shown to the operator.
    }
  }

  function configurationStatus(configuration: string): ConfigurationApplyStatus {
    const applied = snapshot.value?.appliedConfiguration
    if (!applied) return 'none'
    if (lastApplied.value?.key !== appliedConfigurationKey(applied)) return 'unknown'
    return lastApplied.value.text === configuration ? 'current' : 'changed'
  }

  async function applyConfiguration(configuration: string) {
    busy.value = true
    applyingConfiguration.value = true
    error.value = ''
    const requestId = configurationRequestId()
    try {
      const valid = await validate(configuration)
      if (!valid) return false
      busy.value = true
      const result = await api.applyConfiguration(operatorIdentity, configuration, requestId)
      accept(result.snapshot)
      rememberApplied(configuration, result.snapshot)
      return true
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
      await rememberAfterFailure(configuration, requestId, AppliedConfigurationSource.APPLY)
      return false
    } finally {
      applyingConfiguration.value = false
      busy.value = false
    }
  }

  async function stopRun() {
    const runId = snapshot.value?.currentRun?.runId
    if (!runId) return
    busy.value = true
    error.value = ''
    invalidateHistogramRequests()
    try {
      const result = await api.stop(
        create(StopRunRequestSchema, { runId, requestedBy: operatorIdentity }),
      )
      accept(result.snapshot)
      if (result.run) latestCompletedRun.value = result.run
      void refreshHistory()
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
    }
  }

  async function setHighVoltage(boards: number[], enabled: boolean) {
    busy.value = true
    error.value = ''
    try {
      accept(await api.setHighVoltage(boards, enabled, operatorIdentity))
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
    }
  }

  async function connectHardware(configuration: string) {
    busy.value = true
    error.value = ''
    try {
      const next = await api.connectHardware(operatorIdentity, configuration)
      accept(next)
      rememberApplied(configuration, next)
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
    }
  }

  async function disconnectHardware() {
    busy.value = true
    error.value = ''
    try {
      accept(await api.disconnectHardware(operatorIdentity))
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
    }
  }

  async function discoverHardware() {
    busy.value = true
    error.value = ''
    try {
      accept(await api.discoverHardware(operatorIdentity))
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
    }
  }

  async function loadHistograms(
    runId: string,
    kind: HistogramKind,
    selections: HistogramSelection[],
  ) {
    if (!runId) {
      histogramDatasets.value = []
      return
    }
    if (busy.value) return
    histogramsLoading.value = true
    const sequence = ++histogramRequestSequence
    try {
      const datasets = await api.histograms(runId, kind, selections)
      if (sequence === histogramRequestSequence) histogramDatasets.value = datasets
    } catch (reason) {
      if (sequence === histogramRequestSequence)
        error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      if (sequence === histogramRequestSequence) histogramsLoading.value = false
    }
  }

  function disconnect() {
    stopped = true
    streamController?.abort()
    window.clearTimeout(staleTimer)
    window.clearTimeout(reconnectTimer)
  }

  onBeforeUnmount(disconnect)

  return {
    snapshot: readonly(snapshot),
    connected: readonly(connected),
    stale: readonly(stale),
    error: readonly(error),
    busy: readonly(busy),
    startingRun: readonly(startingRun),
    applyingConfiguration: readonly(applyingConfiguration),
    appliedConfiguration: computed(() => snapshot.value?.appliedConfiguration),
    validationIssues: readonly(validationIssues),
    latestCompletedRun: readonly(latestCompletedRun),
    runHistory: readonly(runHistory),
    runHistoryNextPageToken: readonly(runHistoryNextPageToken),
    runHistoryLoading: readonly(runHistoryLoading),
    searchResults: readonly(searchResults),
    searchNextPageToken: readonly(searchNextPageToken),
    searchLoading: readonly(searchLoading),
    searchError: readonly(searchError),
    searchPerformed: readonly(searchPerformed),
    configurationTemplate: readonly(configurationTemplate),
    histogramDatasets: readonly(histogramDatasets),
    histogramsLoading: readonly(histogramsLoading),
    canStart: computed(() => snapshot.value?.state === SystemState.READY && !busy.value),
    // Apply is offered exactly where Start run is.
    canApplyConfiguration: computed(
      () => snapshot.value?.state === SystemState.READY && !busy.value,
    ),
    canStop: computed(() => snapshot.value?.state === SystemState.RUNNING && !busy.value),
    canSwitchHV: computed(() => snapshot.value?.state === SystemState.READY && !busy.value),
    canConnectHardware: computed(
      () => snapshot.value?.state === SystemState.DISCONNECTED && connected.value && !busy.value,
    ),
    canDiscoverHardware: computed(
      () => snapshot.value?.state === SystemState.DISCONNECTED && connected.value && !busy.value,
    ),
    canDisconnectHardware: computed(
      () =>
        (snapshot.value?.state === SystemState.IDLE ||
          snapshot.value?.state === SystemState.READY ||
          snapshot.value?.state === SystemState.FAULT) &&
        !busy.value,
    ),
    connect,
    disconnect,
    validate,
    startRun,
    applyConfiguration,
    configurationStatus,
    stopRun,
    setHighVoltage,
    connectHardware,
    disconnectHardware,
    discoverHardware,
    loadHistograms,
    refreshHistory,
    loadMoreHistory,
    searchRuns,
    clearSearch,
    downloadArtifact,
  }
}
