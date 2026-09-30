import { describe, expect, it } from 'vitest'
import { validateSourceScan, type SourceScanInputs } from './sourceScanValidation'

const valid: SourceScanInputs = {
  xMin: -5,
  xMax: 5,
  yMin: -5,
  yMax: 5,
  pitch: 1,
  mmPerTurn: 0.5,
  fullSteps: 200,
  microsteps: 1,
  pulseHigh: 20,
  pulseLow: 3000,
  settle: 250,
  timeout: 300,
  dwell: 1,
  criterion: 'time',
  events: '100',
}
describe('source scan preflight', () => {
  it('accepts endpoint-inclusive grids and floating point roundoff', () => {
    expect(validateSourceScan(valid)).toEqual({ errors: [], points: 121 })
    expect(
      validateSourceScan({ ...valid, xMin: 0, xMax: 0.3, yMin: 0, yMax: 0.3, pitch: 0.1 }).points,
    ).toBe(16)
  })
  it('explains the reported 11 mm / 1.5 mm case on both axes', () => {
    const result = validateSourceScan({
      ...valid,
      xMin: -5.5,
      xMax: 5.5,
      yMin: -5.5,
      yMax: 5.5,
      pitch: 1.5,
    })
    expect(result.points).toBeUndefined()
    expect(result.errors).toHaveLength(2)
    expect(result.errors[0]).toContain('X span is 11 mm')
    expect(result.errors[1]).toContain('7.333333 intervals, not a whole number')
  })
  it.each([
    [{ xMin: '' }, 'minimum must be a finite'],
    [{ pitch: 0 }, 'Grid pitch must be greater'],
    [{ xMin: 6 }, 'minimum must not exceed'],
    [{ pitch: 0.01 }, 'maximum is 10,000'],
    [{ fullSteps: 1.5 }, 'Full steps per turn must be a whole'],
    [{ microsteps: 0 }, 'Microsteps must be'],
    [{ mmPerTurn: 0 }, 'Travel per turn must be'],
    [{ pulseHigh: 1 }, 'Pulse high'],
    [{ settle: -1 }, 'Settling time'],
    [{ dwell: '' }, 'Acquisition time'],
    [{ timeout: 1 }, 'Point timeout must exceed'],
    [{ criterion: 'events', events: '2.5' }, 'Events per point'],
    [{ criterion: 'events', events: '18446744073709551616' }, 'Events per point'],
    [{ xMin: 0, xMax: 0, yMin: 0, yMax: 0, pitch: 0.001 }, 'smaller than one motor step'],
    [{ xMin: 30000, xMax: 30000 }, 'exceeds ±10,000,000'],
  ] as [Partial<SourceScanInputs>, string][])('rejects invalid values %o', (changes, message) => {
    expect(validateSourceScan({ ...valid, ...changes }).errors.join(' ')).toContain(message)
  })
})
