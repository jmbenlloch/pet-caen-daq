import { describe, expect, it } from 'vitest'
import { janusParameterCatalog, janusParameters } from './catalog'

describe('JANUS 5202 5.0.0 parameter catalog', () => {
  it('accounts for every upstream parameter and monitor', () => {
    expect(janusParameters).toHaveLength(96)
    expect(new Set(janusParameters.map((parameter) => parameter.name)).size).toBe(96)
  })

  it('preserves scopes, widgets, options, constraints, and dependencies', () => {
    expect(janusParameterCatalog.get('TD_CoarseThreshold')).toMatchObject({
      scope: 'board',
      widget: 'integer',
      min: 0,
      max: 2047,
      step: 1,
    })
    expect(janusParameterCatalog.get('TD_FineThreshold')).toMatchObject({
      scope: 'channel',
      min: 0,
      max: 15,
    })
    expect(janusParameterCatalog.get('AcquisitionMode')?.options).toEqual([
      'SPECTROSCOPY',
      'SPECT_TIMING',
      'TIMING_CSTART',
      'TIMING_CSTOP',
      'COUNTING',
      'WAVEFORM',
    ])
    expect(janusParameterCatalog.get('PresetTime')?.activeWhen).toEqual({
      parameter: 'StopRunMode',
      values: ['PRESET_TIME'],
    })
    expect(janusParameterCatalog.get('Vnom')?.widget).toBe('monitor')
  })
})
