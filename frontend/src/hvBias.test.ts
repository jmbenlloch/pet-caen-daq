import { describe, expect, it } from 'vitest'
import { parseConfiguration } from './configuration'
import { biasVoltageInVolts, boardChannelVoltages, nominalChannelVoltage } from './hvBias'

describe('Janus nominal channel voltage', () => {
  it('matches the Janus screenshot and both adjustment ranges', () => {
    expect(nominalChannelVoltage(55, '2.5', '55')).toBeCloseTo(53.0392157)
    expect(nominalChannelVoltage(55, '2.5', '255')).toBe(55)
    expect(nominalChannelVoltage(45.4, '4.5', '0')).toBeCloseTo(41.2)
    expect(nominalChannelVoltage(45.4, '4.5', '128')).toBeCloseTo(43.3082353)
    expect(nominalChannelVoltage(55, 'DISABLED', '0')).toBe(55)
    expect(nominalChannelVoltage(55, 'DISABLED', '')).toBe(55)
  })

  it('normalizes supported bias units and rejects invalid inputs', () => {
    for (const value of ['55', '55 V', '55000 mV', '55000000 uV'])
      expect(biasVoltageInVolts(value)).toBe(55)
    for (const value of ['', '4 V', '90 V', '55 bananas'])
      expect(biasVoltageInVolts(value)).toBeUndefined()
    for (const code of ['', '-1', '256', '12.5', 'bad'])
      expect(nominalChannelVoltage(55, '4.5', code)).toBeUndefined()
    expect(nominalChannelVoltage(Number.NaN, '4.5', '0')).toBeUndefined()
    expect(nominalChannelVoltage(55, 'bad', '0')).toBeUndefined()
  })

  it('resolves global defaults, board overrides, and individual channels independently', () => {
    const fields = parseConfiguration(
      [
        'HV_Vbias 55000 mV',
        'HV_Vbias[1] 56 V',
        'HV_Adjust_Range 2.5',
        'HV_IndivAdj 255',
        'HV_IndivAdj[1] 0',
        'HV_IndivAdj[1][0] 55',
      ].join('\n'),
    ).fields
    expect(boardChannelVoltages(fields, 0)).toEqual(Array(64).fill(55))
    const board = boardChannelVoltages(fields, 1)
    expect(board[0]).toBeCloseTo(54.0392157)
    expect(board.slice(1)).toEqual(Array(63).fill(53.5))
  })
})
