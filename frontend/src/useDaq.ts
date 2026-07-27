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
const operatorIdentity = 'operator'

export function useDaq(api: DaqApi) {
  const snapshot = ref<TelemetrySnapshot>()
  const connected = ref(false)
  const stale = ref(true)
  const error = ref('')
  const busy = ref(false)
  const startingRun = ref(false)
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
    try {
      const valid = await validate(input.configuration)
      if (!valid) return
      busy.value = true
      const result = await api.start(
        create(StartRunRequestSchema, {
          requestedBy: operatorIdentity,
          janusConfiguration: input.configuration,
          captureRaw: input.captureRaw,
          journalTransport: input.journalTransport,
          persistHistograms: input.persistHistograms,
          hdf5SegmentSizeMb: input.hdf5SegmentSizeMb,
          hdf5Compression: input.hdf5Compression,
        }),
      )
      accept(result.snapshot)
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      startingRun.value = false
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
      accept(await api.connectHardware(operatorIdentity, configuration))
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
