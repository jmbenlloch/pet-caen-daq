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
} from './gen/pet/caen/daq/v1/system_pb'

export interface DaqApi {
  snapshot(): Promise<TelemetrySnapshot | undefined>
  telemetry(signal: AbortSignal): AsyncIterable<TelemetrySnapshot>
  validate(configuration: string): Promise<{ valid: boolean; issues: ValidationIssue[] }>
  start(request: StartRunRequest): Promise<RunCommandResult>
  stop(request: StopRunRequest): Promise<RunCommandResult>
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
  }
}
