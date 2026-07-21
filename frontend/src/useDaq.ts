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
  let streamController: AbortController | undefined
  let staleTimer: number | undefined
  let reconnectTimer: number | undefined
  let stopped = false

  function accept(next: TelemetrySnapshot | undefined) {
    if (!next) return
    snapshot.value = next
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
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      busy.value = false
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
    canStart: computed(() => snapshot.value?.state === SystemState.READY && !busy.value),
    canStop: computed(() => snapshot.value?.state === SystemState.RUNNING && !busy.value),
    connect,
    disconnect,
    validate,
    startRun,
    stopRun,
  }
}
