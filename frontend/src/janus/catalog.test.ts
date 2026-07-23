import { describe, expect, it } from 'vitest'
import { janusParameterCatalog, janusParameters } from './catalog'

describe('JANUS 5202 5.0.0 parameter catalog', () => {
  it('accounts for every upstream parameter and monitor', () => {
    expect(janusParameters).toHaveLength(73)
    expect(new Set(janusParameters.map((parameter) => parameter.name)).size).toBe(73)
    for (const name of [
      'EventBuildingMode',
      'TstampCoincWindow',
      'JobFirstRun',
      'JobLastRun',
      'RunSleep',
      'EnableJobs',
      'DataAnalysis',
      'DataFilePath',
      'OF_OutFileUnit',
      'OF_EnMaxSize',
      'OF_MaxSize',
      'OF_RawData',
      'OF_ListBin',
      'OF_ListAscii',
      'OF_ListCSV',
      'OF_Sync',
      'OF_ServiceInfo',
      'OF_RunInfo',
      'OF_SpectHisto',
      'OF_ToAHisto',
      'OF_ToTHisto',
      'OF_MCS',
      'OF_Staircase',
    ]) {
      expect(janusParameterCatalog.has(name)).toBe(false)
    }
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
