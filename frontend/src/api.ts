import { createClient } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import {
  RunService,
  ScanService,
  SystemService,
  type StartRunRequest,
  type StopRunRequest,
  type RunSummary,
  type TelemetrySnapshot,
  type ValidationIssue,
  type HistogramDataset,
  type HistogramKind,
  type HistogramSelection,
  type ListRunsResponse,
  type SearchRunsRequest,
  type SearchRunsResponse,
  type StaircaseScan,
  type HoldDelayScan,
  type ScanSummary,
  type ScanType,
  type StartHoldDelayScanRequest,
  type StartStaircaseRequest,
} from './gen/pet/caen/daq/v1/system_pb'

export interface DaqApi {
  snapshot(): Promise<TelemetrySnapshot | undefined>
  configurationTemplate(): Promise<string>
  telemetry(signal: AbortSignal): AsyncIterable<TelemetrySnapshot>
  connectHardware(requestedBy: string): Promise<TelemetrySnapshot>
  disconnectHardware(requestedBy: string): Promise<TelemetrySnapshot>
  discoverHardware(requestedBy: string): Promise<TelemetrySnapshot>
  validate(configuration: string): Promise<{ valid: boolean; issues: ValidationIssue[] }>
  start(request: StartRunRequest): Promise<RunCommandResult>
  stop(request: StopRunRequest): Promise<RunCommandResult>
  setHighVoltage(
    boards: number[],
    enabled: boolean,
    requestedBy: string,
  ): Promise<TelemetrySnapshot>
  listRuns(limit?: number, pageToken?: string): Promise<ListRunsResponse>
  searchRuns(request: SearchRunsRequest): Promise<SearchRunsResponse>
  runConfiguration(runId: string): Promise<string>
  downloadArtifact(runId: string, artifactName: string): Promise<Blob>
  histograms(
    runId: string,
    kind: HistogramKind,
    selections: HistogramSelection[],
  ): Promise<HistogramDataset[]>
  startStaircase(request: StartStaircaseRequest): Promise<StaircaseScan | undefined>
  startHoldDelay(request: StartHoldDelayScanRequest): Promise<HoldDelayScan | undefined>
  cancelScan(scanId: string, requestedBy: string): Promise<StaircaseScan | undefined>
  listScans(
    limit?: number,
    offset?: number,
    board?: number,
    scanType?: ScanType,
  ): Promise<ScanHistoryPage>
  staircase(scanId: string): Promise<StaircaseScan | undefined>
  holdDelay(scanId: string): Promise<HoldDelayScan | undefined>
}

export interface RunCommandResult {
  run?: RunSummary
  snapshot?: TelemetrySnapshot
}

export interface ScanHistoryPage {
  scans: ScanSummary[]
  totalCount: number
}

export function createDaqApi(baseUrl = window.location.origin): DaqApi {
  const transport = createConnectTransport({ baseUrl })
  const system = createClient(SystemService, transport)
  const runs = createClient(RunService, transport)
  const scans = createClient(ScanService, transport)

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
    async connectHardware(requestedBy) {
      const response = await system.connectHardware({ requestedBy })
      if (!response.snapshot) throw new Error('Connect command returned no telemetry snapshot')
      return response.snapshot
    },
    async disconnectHardware(requestedBy) {
      const response = await system.disconnectHardware({ requestedBy })
      if (!response.snapshot) throw new Error('Disconnect command returned no telemetry snapshot')
      return response.snapshot
    },
    async discoverHardware(requestedBy) {
      const response = await system.discoverHardware({ requestedBy })
      if (!response.snapshot) throw new Error('Discovery command returned no telemetry snapshot')
      return response.snapshot
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
    async listRuns(limit = 50, pageToken = '') {
      return await runs.listRuns({ limit, pageToken })
    },
    async searchRuns(request) {
      return await runs.searchRuns(request)
    },
    async runConfiguration(runId) {
      return (await runs.getRunConfiguration({ runId })).janusConfiguration
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
    async startStaircase(request) {
      return (await scans.startStaircase(request)).scan
    },
    async startHoldDelay(request) {
      return (await scans.startHoldDelayScan(request)).scan
    },
    async cancelScan(scanId, requestedBy) {
      return (await scans.cancelScan({ scanId, requestedBy })).scan
    },
    async listScans(limit = 50, offset = 0, board, scanType) {
      return await scans.listScans({ limit, offset, board, scanType })
    },
    async staircase(scanId) {
      return (await scans.getStaircase({ scanId })).scan
    },
    async holdDelay(scanId) {
      return (await scans.getHoldDelayScan({ scanId })).scan
    },
  }
}
