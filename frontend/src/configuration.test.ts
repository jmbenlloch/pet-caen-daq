import { describe, expect, it } from 'vitest'
import { isBooleanField, parseConfiguration, updateConfiguration } from './configuration'

const source = [
  '# ------------------------------------------------------------------------------------------',
  '# AcqMode',
  '# ------------------------------------------------------------------------------------------',
  'AcquisitionMode SPECT_TIMING # Acquisition mode. Options: SPECTROSCOPY, SPECT_TIMING, COUNTING',
  'EnableToT 0 # Enable ToT',
  'TD_CoarseThreshold[2] 179 # board override',
].join('\n')

describe('JANUS configuration editor', () => {
  it('discovers sections, choices, switches, and board overrides', () => {
    const document = parseConfiguration(source)
    expect(document.fields).toHaveLength(3)
    expect(document.fields[0]).toMatchObject({
      name: 'AcquisitionMode',
      section: 'AcqMode',
      options: ['SPECTROSCOPY', 'SPECT_TIMING', 'COUNTING'],
    })
    expect(isBooleanField(document.fields[1])).toBe(true)
    expect(document.fields[2]).toMatchObject({ name: 'TD_CoarseThreshold', index: '2' })
  })

  it('changes only the selected assignment and preserves comments', () => {
    const document = parseConfiguration(source)
    const changed = updateConfiguration(document, document.fields[2], '181')
    expect(changed.source).toContain('TD_CoarseThreshold[2] 181 # board override')
    expect(changed.source).toContain('AcquisitionMode SPECT_TIMING # Acquisition mode')
  })
})
