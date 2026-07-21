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
