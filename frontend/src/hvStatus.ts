export type HvState = 'off' | 'on' | 'ramping' | 'fault'

export type HvStatus = {
  hvVoltageV: number
  hvTargetVoltageV: number
  hvOn: boolean
  hvRamping: boolean
  hvOverCurrent: boolean
  hvOverVoltage: boolean
}

const targetToleranceV = 1
const offThresholdV = 5

// The A7585 status bits change before the physical voltage finishes moving.
// Use the measured voltage as JANUS does, while retaining the hardware ramp
// bit as supporting evidence and the hardware fault bits as authoritative.
export function hvState(status: HvStatus): HvState {
  if (status.hvOverCurrent || status.hvOverVoltage) return 'fault'
  if (status.hvRamping) return 'ramping'
  if (status.hvOn) {
    if (
      status.hvTargetVoltageV > offThresholdV &&
      status.hvVoltageV < status.hvTargetVoltageV - targetToleranceV
    )
      return 'ramping'
    return 'on'
  }
  return status.hvVoltageV > offThresholdV ? 'ramping' : 'off'
}
