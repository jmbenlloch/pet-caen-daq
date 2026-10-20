// UI preflight only: the backend remains authoritative and validates every scan.
export interface SourceScanInputs {
  xMin: number | string
  xMax: number | string
  yMin: number | string
  yMax: number | string
  pitch: number | string
  mmPerTurn: number | string
  fullSteps: number | string
  microsteps: number | string
  pulseHigh: number | string
  pulseLow: number | string
  settle: number | string
  timeout: number | string
  dwell: number | string
  criterion: string
  events: string
}

export function validateSourceScan(v: SourceScanInputs) {
  const errors: string[] = []
  const finite = (n: number | string): n is number => typeof n === 'number' && Number.isFinite(n)
  const fmt = (n: number) => Number(n.toFixed(6)).toString()
  function range(
    key: keyof SourceScanInputs,
    label: string,
    min: number,
    max: number,
    integer = false,
  ) {
    const n = v[key]
    if (!finite(n) || n < min || n > max || (integer && !Number.isInteger(n))) {
      errors.push(
        `${label} must be ${integer ? 'a whole number' : 'a number'} from ${min} to ${max}.`,
      )
      return false
    }
    return true
  }
  for (const key of ['xMin', 'xMax', 'yMin', 'yMax'] as const) {
    if (!finite(v[key]))
      errors.push(
        `${key[0]!.toUpperCase()} ${key.endsWith('Min') ? 'minimum' : 'maximum'} must be a finite number in mm.`,
      )
  }
  for (const [key, label] of [
    ['pitch', 'Grid pitch'],
    ['mmPerTurn', 'Travel per turn'],
  ] as const) {
    if (!finite(v[key]) || v[key] <= 0) errors.push(`${label} must be greater than zero (mm).`)
  }
  range('fullSteps', 'Full steps per turn', 1, 100000, true)
  range('microsteps', 'Microsteps', 1, 256, true)
  range('pulseHigh', 'Pulse high (µs)', 2, 10000, true)
  range('pulseLow', 'Pulse low (µs)', 2, 10000, true)
  range('settle', 'Settling time (ms)', 0, 60000, true)
  range('timeout', 'Point timeout (s)', 1, 86400, true)
  if (v.criterion === 'time') range('dwell', 'Acquisition time (s)', 0.001, 86400)
  else if (!/^[1-9][0-9]*$/.test(v.events) || BigInt(v.events) > 18446744073709551615n) {
    errors.push(
      'Events per point must be a positive whole number, no greater than 18446744073709551615.',
    )
  }
  const counts: number[] = []
  for (const [axis, low, high] of [
    ['X', v.xMin, v.xMax],
    ['Y', v.yMin, v.yMax],
  ] as const) {
    if (!finite(low) || !finite(high)) continue
    if (low > high) {
      errors.push(`${axis} minimum must not exceed ${axis} maximum.`)
      continue
    }
    if (!finite(v.pitch) || v.pitch <= 0) continue
    const span = high - low
    const intervals = span / v.pitch
    if (!Number.isFinite(intervals) || intervals > 9999) {
      errors.push(
        `${axis} has too many positions. Increase grid pitch or reduce the axis span (maximum 10,000 positions per axis).`,
      )
    } else if (Math.abs(intervals - Math.round(intervals)) > 1e-7) {
      errors.push(
        `${axis} span is ${fmt(span)} mm (${fmt(high)} − ${fmt(low)}). Dividing by pitch ${fmt(v.pitch)} mm gives ${fmt(intervals)} intervals, not a whole number. Adjust the bounds or pitch so both endpoints lie on the grid.`,
      )
    } else counts.push(Math.round(intervals) + 1)
  }
  const points = counts.length === 2 ? counts[0]! * counts[1]! : undefined
  if (points !== undefined && points > 10000)
    errors.push(
      `This grid contains ${points.toLocaleString('en-US')} positions; the maximum is 10,000. Increase pitch or reduce the bounds.`,
    )
  // Only evaluate motor planning once input types and grid counts are valid.
  if (errors.length === 0) {
    const scale = (Number(v.fullSteps) * Number(v.microsteps)) / Number(v.mmPerTurn)
    if (!Number.isFinite(scale) || scale <= 0)
      errors.push(
        'Calibration produces an invalid steps/mm value. Check travel per turn, full steps, and microsteps.',
      )
    else if (Number(v.pitch) * scale < 1)
      errors.push(
        `Grid pitch is smaller than one motor step (${fmt(1 / scale)} mm). Increase pitch or check the motor calibration.`,
      )
    else {
      const round = (n: number) => Math.sign(n) * Math.round(Math.abs(n))
      let lastX = 0,
        lastY = 0,
        longest = 0
      let outOfRange = false
      for (let row = 0; row < counts[1]!; row++) {
        for (let k = 0; k < counts[0]!; k++) {
          const col = row % 2 ? counts[0]! - 1 - k : k
          const x = round((Number(v.xMin) + col * Number(v.pitch)) * scale)
          const y = round((Number(v.yMax) - row * Number(v.pitch)) * scale)
          outOfRange ||= Math.abs(x) > 10000000 || Math.abs(y) > 10000000
          longest = Math.max(
            longest,
            ((Math.abs(x - lastX) + Math.abs(y - lastY)) *
              (Number(v.pulseHigh) + Number(v.pulseLow))) /
              1e6,
          )
          lastX = x
          lastY = y
        }
      }
      if (outOfRange)
        errors.push(
          'A requested position exceeds ±10,000,000 motor steps. Reduce the bounds or check calibration.',
        )
      const dwell = v.criterion === 'time' ? Math.round(Number(v.dwell) * 1000) / 1000 : 0
      const minimum = longest + Number(v.settle) / 1000 + dwell
      if (minimum >= Number(v.timeout))
        errors.push(
          `Point timeout must exceed ${fmt(minimum)} s for the longest move, settling, and acquisition. Increase timeout and allow extra time for DAQ configuration${v.criterion === 'events' ? ' and collecting the requested events' : ''}.`,
        )
    }
  }
  return { errors, points }
}
