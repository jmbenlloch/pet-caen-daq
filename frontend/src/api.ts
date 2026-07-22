import { createClient } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import {
  RunService,
  SystemService,
  type StartRunRequest,
  type StopRunRequest,
  type RunSummary,
  type TelemetrySnapshot,
  type ValidationIssue,
  type HistogramDataset,
  type HistogramKind,
  type HistogramSelection,
} from './gen/pet/caen/daq/v1/system_pb'

export interface DaqApi {
  snapshot(): Promise<TelemetrySnapshot | undefined>
  configurationTemplate(): Promise<string>
  telemetry(signal: AbortSignal): AsyncIterable<TelemetrySnapshot>
  validate(configuration: string): Promise<{ valid: boolean; issues: ValidationIssue[] }>
  start(request: StartRunRequest): Promise<RunCommandResult>
  stop(request: StopRunRequest): Promise<RunCommandResult>
  setHighVoltage(
    boards: number[],
    enabled: boolean,
    requestedBy: string,
  ): Promise<TelemetrySnapshot>
  listRuns(limit?: number): Promise<RunSummary[]>
  downloadArtifact(runId: string, artifactName: string): Promise<Blob>
  histograms(
    runId: string,
    kind: HistogramKind,
    selections: HistogramSelection[],
  ): Promise<HistogramDataset[]>
}

export interface RunCommandResult {
  run?: RunSummary
  snapshot?: TelemetrySnapshot
}

export function createDaqApi(baseUrl = window.location.origin): DaqApi {
  const transport = createConnectTransport({ baseUrl })
  const system = createClient(SystemService, transport)
  const runs = createClient(RunService, transport)

  return {
    async snapshot() {
      return (await system.getSystemSnapshot({})).snapshot
    },
    async configurationTemplate() {
      return (await system.getConfigurationTemplate({})).janusConfiguration
    },
    async *telemetry(signal) {
      for await (const response of system.streamTelemetry({}, { signal })) {
        if (response.snapshot) yield response.snapshot
      }
    },
    async validate(janusConfiguration) {
      const response = await system.validateConfiguration({ janusConfiguration })
      return { valid: response.valid, issues: response.issues }
    },
    async start(request) {
      const response = await runs.startRun(request)
      return { run: response.run, snapshot: response.snapshot }
    },
    async stop(request) {
      const response = await runs.stopRun(request)
      return { run: response.run, snapshot: response.snapshot }
    },
    async setHighVoltage(boards, enabled, requestedBy) {
      const response = await system.setHighVoltage({ boards, enabled, requestedBy })
      if (!response.snapshot) throw new Error('HV command returned no telemetry snapshot')
      return response.snapshot
    },
    async listRuns(limit = 50) {
      return (await runs.listRuns({ limit })).runs
    },
    async downloadArtifact(runId, artifactName) {
      const chunks: Uint8Array[] = []
      let size = 0
      for await (const response of runs.downloadArtifact({ runId, artifactName })) {
        chunks.push(response.data)
        size += response.data.byteLength
      }
      const data = new Uint8Array(size)
      let offset = 0
      for (const chunk of chunks) {
        data.set(chunk, offset)
        offset += chunk.byteLength
      }
      return new Blob([data], { type: 'application/octet-stream' })
    },
    async histograms(runId, kind, selections) {
      return (await runs.getHistograms({ runId, kind, selections })).datasets
    },
  }
}
