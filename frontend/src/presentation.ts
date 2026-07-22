import { HealthStatus, SystemState } from './gen/pet/caen/daq/v1/system_pb'

export const stateLabel: Record<SystemState, string> = {
  [SystemState.UNSPECIFIED]: 'Unknown',
  [SystemState.DISCONNECTED]: 'Disconnected',
  [SystemState.IDLE]: 'Idle',
  [SystemState.FAULT]: 'Fault',
  [SystemState.CONNECTING]: 'Connecting',
  [SystemState.CONFIGURING]: 'Configuring',
  [SystemState.READY]: 'Ready',
  [SystemState.STARTING]: 'Starting',
  [SystemState.RUNNING]: 'Running',
  [SystemState.STOPPING]: 'Stopping',
  [SystemState.DRAINING]: 'Draining',
  [SystemState.RECOVERING]: 'Recovering',
}

export const healthLabel: Record<HealthStatus, string> = {
  [HealthStatus.UNSPECIFIED]: 'Unreported',
  [HealthStatus.UNKNOWN]: 'Unknown',
  [HealthStatus.OK]: 'Healthy',
  [HealthStatus.DEGRADED]: 'Degraded',
  [HealthStatus.FAULT]: 'Fault',
}

export function compact(value: bigint | undefined) {
  return new Intl.NumberFormat().format(value ?? 0n)
}

export function bytes(value: bigint | undefined) {
  const amount = Number(value ?? 0n)
  if (amount < 1024) return `${amount} B`
  if (amount < 1024 ** 2) return `${(amount / 1024).toFixed(1)} KiB`
  if (amount < 1024 ** 3) return `${(amount / 1024 ** 2).toFixed(1)} MiB`
  return `${(amount / 1024 ** 3).toFixed(1)} GiB`
}

export function localDateTime(value: { seconds: bigint; nanos: number } | undefined) {
  if (!value) return 'Not reported'
  const milliseconds = Number(value.seconds) * 1000 + value.nanos / 1_000_000
  const date = new Date(milliseconds)
  return Number.isNaN(date.getTime()) ? 'Not reported' : date.toLocaleString()
}
