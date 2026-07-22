import { create } from '@bufbuild/protobuf'
import { computed, onBeforeUnmount, readonly, ref } from 'vue'
import type { DaqApi } from './api'
import {
  StartRunRequestSchema,
  StopRunRequestSchema,
  SystemState,
  type TelemetrySnapshot,
  type RunSummary,
  type ValidationIssue,
  type HistogramDataset,
  type HistogramKind,
  type HistogramSelection,
  type SearchRunsRequest,
} from './gen/pet/caen/daq/v1/system_pb'

const staleAfterMs = 5_000

export function useDaq(api: DaqApi) {
  const snapshot = ref<TelemetrySnapshot>()
  const connected = ref(false)
  const stale = ref(true)
  const error = ref('')
  const busy = ref(false)
  const validationIssues = ref<ValidationIssue[]>([])
  const latestCompletedRun = ref<RunSummary>()
  const runHistory = ref<RunSummary[]>([])
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
  let searchRequestSequence = 0
  let stopped = false

  function accept(next: TelemetrySnapshot | undefined) {
    if (!next) return
    snapshot.value = next
    if (!next.currentRun) histogramDatasets.value = []
    if (next.latestCompletedRun) latestCompletedRun.value = next.latestCompletedRun
    connected.value = true
    stale.value = false
    error.value = ''
    window.clearTimeout(staleTimer)
    staleTimer = window.setTimeout(() => (stale.value = true), staleAfterMs)
  }

  async function connect() {
    if (stopped) return
    streamController?.abort()
    streamController = new AbortController()
    try {
      void refreshHistory()
      if (!configurationTemplate.value) {
        configurationTemplate.value = await api.configurationTemplate()
      }
      accept(await api.snapshot())
      for await (const next of api.telemetry(streamController.signal)) accept(next)
      if (!streamController.signal.aborted) throw new Error('Telemetry stream ended')
    } catch (reason) {
      if (streamController.signal.aborted || stopped) return
      connected.value = false
      stale.value = true
      error.value = reason instanceof Error ? reason.message : String(reason)
      reconnectTimer = window.setTimeout(connect, 2_000)
    }
  }

  async function refreshHistory() {
    try {
      runHistory.value = await api.listRuns(50)
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
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
    runId: string
    requestedBy: string
    configuration: string
    captureRaw: boolean
    journalTransport: boolean
  }) {
    busy.value = true
    error.value = ''
    try {
      const valid = await validate(input.configuration)
      if (!valid) return
      busy.value = true
      const result = await api.start(
        create(StartRunRequestSchema, {
          runId: input.runId,
          requestedBy: input.requestedBy,
          janusConfiguration: input.configuration,
          captureRaw: input.captureRaw,
          journalTransport: input.journalTransport,
        }),
      )
      accept(result.snapshot)
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
    }
  }

  async function stopRun(requestedBy: string) {
    const runId = snapshot.value?.currentRun?.runId
    if (!runId) return
    busy.value = true
    error.value = ''
    try {
      const result = await api.stop(create(StopRunRequestSchema, { runId, requestedBy }))
      accept(result.snapshot)
      if (result.run) latestCompletedRun.value = result.run
      await refreshHistory()
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
    }
  }

  async function setHighVoltage(boards: number[], enabled: boolean, requestedBy: string) {
    busy.value = true
    error.value = ''
    try {
      accept(await api.setHighVoltage(boards, enabled, requestedBy))
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
    }
  }

  async function loadHistograms(kind: HistogramKind, selections: HistogramSelection[]) {
    const runId = snapshot.value?.currentRun?.runId
    if (!runId) {
      histogramDatasets.value = []
      return
    }
    histogramsLoading.value = true
    const sequence = ++histogramRequestSequence
    try {
      const datasets = await api.histograms(runId, kind, selections)
      if (sequence === histogramRequestSequence && snapshot.value?.currentRun?.runId === runId)
        histogramDatasets.value = datasets
    } catch (reason) {
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
    validationIssues: readonly(validationIssues),
    latestCompletedRun: readonly(latestCompletedRun),
    runHistory: readonly(runHistory),
    searchResults: readonly(searchResults),
    searchNextPageToken: readonly(searchNextPageToken),
    searchLoading: readonly(searchLoading),
    searchError: readonly(searchError),
    searchPerformed: readonly(searchPerformed),
    configurationTemplate: readonly(configurationTemplate),
    histogramDatasets: readonly(histogramDatasets),
    histogramsLoading: readonly(histogramsLoading),
    canStart: computed(() => snapshot.value?.state === SystemState.READY && !busy.value),
    canStop: computed(() => snapshot.value?.state === SystemState.RUNNING && !busy.value),
    canSwitchHV: computed(() => snapshot.value?.state === SystemState.READY && !busy.value),
    connect,
    disconnect,
    validate,
    startRun,
    stopRun,
    setHighVoltage,
    loadHistograms,
    refreshHistory,
    searchRuns,
    clearSearch,
    downloadArtifact,
  }
}
