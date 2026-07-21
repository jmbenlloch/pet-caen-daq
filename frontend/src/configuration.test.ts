import { describe, expect, it } from 'vitest'
import {
  isBooleanField,
  maskBits,
  masksFromBits,
  numericConstraint,
  numericError,
  setConfigurationValue,
  parseConfiguration,
  updateConfiguration,
} from './configuration'

const source = [
  '# ------------------------------------------------------------------------------------------',
  '# AcqMode',
  '# ------------------------------------------------------------------------------------------',
  'AcquisitionMode SPECT_TIMING # Acquisition mode. Options: SPECTROSCOPY, SPECT_TIMING, COUNTING',
  'EnableToT 0 # Enable ToT',
  'TstampCoincWindow 0 # Coincidence window',
  'HV_Adjust_Range 4.5 # Options: 4.5, 2.5, DISABLED',
  'TD_CoarseThreshold[2] 179 # board override',
].join('\n')

describe('JANUS configuration editor', () => {
  it('discovers sections, choices, switches, and board overrides', () => {
    const document = parseConfiguration(source)
    expect(document.fields).toHaveLength(5)
    expect(document.fields[0]).toMatchObject({
      name: 'AcquisitionMode',
      section: 'AcqMode',
      options: [
        'SPECTROSCOPY',
        'SPECT_TIMING',
        'TIMING_CSTART',
        'TIMING_CSTOP',
        'COUNTING',
        'WAVEFORM',
      ],
    })
    expect(isBooleanField(document.fields[1])).toBe(true)
    expect(isBooleanField(document.fields[2])).toBe(false)
    expect(document.fields[3].options).toEqual(['4.5', '2.5', 'DISABLED'])
    expect(document.fields[4]).toMatchObject({ name: 'TD_CoarseThreshold', index: '2' })
  })

  it('changes only the selected assignment and preserves comments', () => {
    const document = parseConfiguration(source)
    const changed = updateConfiguration(document, document.fields[4], '181')
    expect(changed.source).toContain('TD_CoarseThreshold[2] 181 # board override')
    expect(changed.source).toContain('AcquisitionMode SPECT_TIMING # Acquisition mode')
  })

  it('enforces numeric bounds and increments from JANUS parameter semantics', () => {
    const field = parseConfiguration('MajorityLevel 65 # Majority Level (1 to 64)').fields[0]
    expect(numericConstraint(field)).toMatchObject({ min: 1, max: 64, step: 1, integer: true })
    expect(numericError(field)).toBe('Maximum: 64.')
    field.value = '4.5'
    expect(numericError(field)).toBe('Enter a whole number.')
    field.value = '4'
    expect(numericError(field)).toBe('')

    const time = parseConfiguration('ChTrg_Width 2 us # width').fields[0]
    expect(numericConstraint(time)).toMatchObject({ min: 0.008, max: 2.032, step: 0.008 })
    expect(numericError(time)).toBe('')
  })

  it('round-trips the paired 32-bit masks used by the JANUS channel screen', () => {
    const bits = maskBits('0x00000001', '0x80000000')
    expect(bits[0]).toBe(true)
    expect(bits[63]).toBe(true)
    expect(bits.filter(Boolean)).toHaveLength(2)
    expect(masksFromBits(bits)).toEqual(['0x00000001', '0x80000000'])
  })

  it('adds, updates, and removes JANUS board/channel overrides', () => {
    let document = parseConfiguration('TD_FineThreshold 0 # general\n')
    document = setConfigurationValue(document, 'TD_FineThreshold', 2, 17, '9')
    expect(document.source).toContain('TD_FineThreshold[2][17] 9 # operator override')
    expect(document.fields.at(-1)).toMatchObject({ index: '2', channel: '17', value: '9' })
    document = setConfigurationValue(document, 'TD_FineThreshold', 2, 17, '8')
    expect(document.source.match(/TD_FineThreshold\[2\]\[17\]/g)).toHaveLength(1)
    document = setConfigurationValue(document, 'TD_FineThreshold', 2, 17, undefined)
    expect(document.source).not.toContain('[2][17]')
  })
})
