import definitions from './param_defs_5.0.0.txt?raw'

export type JanusScope = 'global' | 'board' | 'channel'
export type JanusWidget = 'boolean' | 'select' | 'integer' | 'number' | 'text' | 'monitor'

export interface JanusDependency {
  parameter: string
  values: string[]
}

export interface JanusParameter {
  name: string
  section: string
  defaultValue: string
  scope: JanusScope
  widget: JanusWidget
  description: string
  options: string[]
  min?: number
  max?: number
  step?: number
  units?: string[]
  activeWhen?: JanusDependency
}

const constraints: Record<string, Pick<JanusParameter, 'min' | 'max' | 'step' | 'units'>> = {
  HV_Vbias: { min: 20, max: 85, step: 0.1, units: ['V', 'mV', 'uV'] },
  HV_Imax: { min: 0, step: 0.1, units: ['mA', 'uA', 'A'] },
  HV_IndivAdj: { min: 0, max: 255, step: 1 },
  TempFeedbackCoeff: { step: 0.1, units: ['mV/degC'] },
  TstampCoincWindow: { min: 0, step: 1, units: ['ns', 'us', 'ms', 's'] },
  PresetTime: { min: 0, step: 1, units: ['s', 'ms', 'us', 'ns'] },
  PresetCounts: { min: 0, step: 1 },
  JobFirstRun: { min: 0, step: 1 },
  JobLastRun: { min: 0, step: 1 },
  RunSleep: { min: 0, step: 1, units: ['s', 'ms', 'us', 'ns'] },
  OF_MaxSize: { min: 1, step: 1, units: ['MB', 'GB'] },
  ChTrg_Width: { min: 8, max: 2032, step: 8, units: ['ns', 'us'] },
  Tlogic_Width: { min: 0, step: 8, units: ['ns', 'us'] },
  MajorityLevel: { min: 1, max: 64, step: 1 },
  PtrgPeriod: { min: 0, step: 8, units: ['ns', 'us', 'ms', 's'] },
  TrefWindow: { min: 0, step: 8, units: ['ns', 'us', 'ms', 's'] },
  TrefDelay: { min: -4194304, max: 4194296, step: 1, units: ['ns', 'us', 'ms', 's'] },
  TD_CoarseThreshold: { min: 0, max: 2047, step: 1 },
  TD_FineThreshold: { min: 0, max: 15, step: 1 },
  Hit_HoldOff: { min: 0, step: 8, units: ['ns', 'us', 'ms', 's'] },
  QD_CoarseThreshold: { min: 0, max: 2047, step: 1 },
  QD_FineThreshold: { min: 0, max: 15, step: 1 },
  HG_Gain: { min: 1, max: 63, step: 1 },
  LG_Gain: { min: 1, max: 63, step: 1 },
  Pedestal: { min: 0, max: 16383, step: 1 },
  ZS_Threshold_LG: { min: 0, max: 65535, step: 1 },
  ZS_Threshold_HG: { min: 0, max: 65535, step: 1 },
  HoldDelay: { min: 0, step: 1, units: ['ns', 'us'] },
  MuxClkPeriod: { min: 0, step: 1, units: ['ns', 'us'] },
  ToARebin: { min: 1, step: 1 },
  ToAHistoMin: { step: 1, units: ['ns', 'us', 'ms', 's'] },
  ProbeChannel0: { min: 0, max: 31, step: 1 },
  ProbeChannel1: { min: 32, max: 63, step: 1 },
  TestPulseAmplitude: { min: 0, max: 4095, step: 1 },
}

const dependencies: Record<string, JanusDependency> = {
  ExtRunSource: { parameter: 'StartRunMode', values: ['TDL_EXTRUN'] },
  GPSTimeUTC: { parameter: 'StartRunMode', values: ['TDL_GPS'] },
  TstampCoincWindow: {
    parameter: 'EventBuildingMode',
    values: ['TRGTIME_SORTING', 'TRGID_SORTING'],
  },
  PresetTime: { parameter: 'StopRunMode', values: ['PRESET_TIME'] },
  PresetCounts: { parameter: 'StopRunMode', values: ['PRESET_COUNTS'] },
  JobFirstRun: { parameter: 'EnableJobs', values: ['1'] },
  JobLastRun: { parameter: 'EnableJobs', values: ['1'] },
  RunSleep: { parameter: 'EnableJobs', values: ['1'] },
  OF_MaxSize: { parameter: 'OF_EnMaxSize', values: ['1'] },
  ChTrg_Width: { parameter: 'CountingMode', values: ['PAIRED_AND'] },
  MajorityLevel: { parameter: 'TriggerLogic', values: ['MAJ64', 'MAJ32_AND2'] },
  TestPulseAmplitude: {
    parameter: 'TestPulseSource',
    values: ['EXT', 'T0-IN', 'T1-IN', 'PTRG', 'SW-CMD'],
  },
  TestPulseDestination: {
    parameter: 'TestPulseSource',
    values: ['EXT', 'T0-IN', 'T1-IN', 'PTRG', 'SW-CMD'],
  },
  TestPulsePreamp: {
    parameter: 'TestPulseSource',
    values: ['EXT', 'T0-IN', 'T1-IN', 'PTRG', 'SW-CMD'],
  },
}

function unquote(value: string) {
  return value.startsWith('"') && value.endsWith('"') ? value.slice(1, -1) : value
}

function widget(type: string): JanusWidget {
  if (type === 'm') return 'monitor'
  if (type === 'b') return 'boolean'
  if (type === 'c') return 'select'
  if (type.startsWith('d')) return 'integer'
  if (type.startsWith('f') || type === 'u') return 'number'
  return 'text'
}

function parseDefinitions(source: string): JanusParameter[] {
  let section = ''
  let current: JanusParameter | undefined
  const result: JanusParameter[] = []
  for (const line of source.split(/\r?\n/)) {
    const heading = line.match(/^\[([^\]]+)\]$/)
    if (heading) {
      section = heading[1]
      continue
    }
    const option = line.match(/^\s+-\s+(.+?)\s*$/)
    if (option && current) {
      current.options.push(unquote(option[1]))
      continue
    }
    const match = line.match(
      /^([A-Za-z][A-Za-z0-9_]*)\s+("[^"]*"|\S+)\s+([gbc-])\s+([a-z-]+)\s*(?:#\s*(.*))?$/,
    )
    if (!match || match[3] === '-' || match[4] === '-') {
      current = undefined
      continue
    }
    current = {
      name: match[1],
      section,
      defaultValue: unquote(match[2]),
      scope: ({ g: 'global', b: 'board', c: 'channel' } as const)[match[3] as 'g' | 'b' | 'c'],
      widget: widget(match[4]),
      description: match[5]?.trim() ?? '',
      options: [],
      ...constraints[match[1]],
      activeWhen: dependencies[match[1]],
    }
    result.push(current)
  }
  return result
}

export const janusParameters = parseDefinitions(definitions)
export const janusParameterCatalog = new Map(
  janusParameters.map((parameter) => [parameter.name, parameter]),
)
