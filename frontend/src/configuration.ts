export interface ConfigurationField {
  id: string
  name: string
  index?: string
  channel?: string
  section: string
  value: string
  help: string
  options: string[]
  line: number
}

export interface NumericConstraint {
  min?: number
  max?: number
  step: number
  integer: boolean
}

const timeScales = { ns: 1, us: 1e3, ms: 1e6, s: 1e9 }

export interface NumericValue {
  number: number
  unit: string
}

export interface ConfigurationDocument {
  source: string
  fields: ConfigurationField[]
}

const assignment =
  /^(\s*)([A-Za-z][A-Za-z0-9_]*)(?:\[([^\]]+)\])?(?:\[([^\]]+)\])?(\s+)(.*?)(\s*)(#.*)?$/

export function parseConfiguration(source: string): ConfigurationDocument {
  const fields: ConfigurationField[] = []
  let section = 'Connection'
  const lines = source.split(/\r?\n/)
  for (let index = 0; index < lines.length; index++) {
    const line = lines[index]
    const heading = line.match(/^#\s+([^*-][^-]*?)\s*$/)?.[1]?.trim()
    if (heading && !heading.startsWith('params File')) section = heading
    const match = line.match(assignment)
    if (!match || line.trimStart().startsWith('#')) continue
    const catalog = janusParameterCatalog.get(match[2])
    const commentHelp = (match[8] ?? '').replace(/^#\s*/, '').trim()
    const help = catalog?.description || commentHelp
    const optionsAt = Math.max(help.lastIndexOf('Options:'), help.lastIndexOf('Option:'))
    const optionsText = optionsAt >= 0 ? help.slice(help.indexOf(':', optionsAt) + 1) : ''
    fields.push({
      id: `${match[2]}[${match[3] ?? 'default'}]${match[4] === undefined ? '' : `[${match[4]}]`}@${index + 1}`,
      name: match[2],
      index: match[3],
      channel: match[4],
      section: catalog?.section ?? section,
      value: match[6].trimEnd(),
      help,
      options:
        catalog?.options.length || !optionsText
          ? [...(catalog?.options ?? [])]
          : optionsText.split(',').map((item) => item.trim().replace(/\.$/, '')),
      line: index + 1,
    })
  }
  return { source, fields }
}

export function updateConfiguration(
  document: ConfigurationDocument,
  field: ConfigurationField,
  value: string,
) {
  const lines = document.source.split(/\r?\n/)
  const lineIndex = field.line - 1
  const match = lines[lineIndex]?.match(assignment)
  if (!match) return document
  lines[lineIndex] =
    `${match[1]}${match[2]}${match[3] === undefined ? '' : `[${match[3]}]`}${match[4] === undefined ? '' : `[${match[4]}]`}${match[5]}${value}${match[7]}${match[8] ?? ''}`
  return parseConfiguration(lines.join(document.source.includes('\r\n') ? '\r\n' : '\n'))
}

export function setConfigurationValue(
  document: ConfigurationDocument,
  name: string,
  index: number | undefined,
  channel: number | undefined,
  value: string | undefined,
) {
  const existing = document.fields.find(
    (field) =>
      field.name === name &&
      field.index === (index === undefined ? undefined : String(index)) &&
      field.channel === (channel === undefined ? undefined : String(channel)),
  )
  if (existing && value !== undefined) return updateConfiguration(document, existing, value)
  const newline = document.source.includes('\r\n') ? '\r\n' : '\n'
  if (existing && value === undefined) {
    const lines = document.source.split(/\r?\n/)
    lines.splice(existing.line - 1, 1)
    return parseConfiguration(lines.join(newline))
  }
  if (value === undefined) return document
  const key = `${name}${index === undefined ? '' : `[${index}]`}${channel === undefined ? '' : `[${channel}]`}`
  return parseConfiguration(
    `${document.source.replace(/\s*$/, '')}${newline}${key} ${value} # operator override${newline}`,
  )
}

export function parameterScope(field: ConfigurationField): 'global' | 'board' | 'channel' {
  return janusParameterCatalog.get(field.name)?.scope ?? 'global'
}

export function parameterActive(document: ConfigurationDocument, field: ConfigurationField) {
  const dependency = janusParameterCatalog.get(field.name)?.activeWhen
  if (!dependency) return true
  const controller = [...document.fields]
    .reverse()
    .find(
      (candidate) =>
        candidate.name === dependency.parameter &&
        candidate.index === undefined &&
        candidate.channel === undefined,
    )
  return !controller || dependency.values.includes(controller.value)
}

export function isBooleanField(field: ConfigurationField) {
  return janusParameterCatalog.get(field.name)?.widget === 'boolean'
}

export function numericConstraint(field: ConfigurationField): NumericConstraint | undefined {
  if (field.options.length || isBooleanField(field) || isMaskField(field)) return undefined
  const parsed = parseNumericValue(field.value)
  if (!parsed) return undefined
  const explicit = janusParameterCatalog.get(field.name)
  if (!explicit || explicit.step === undefined) return undefined
  const unitScales =
    field.name === 'HV_Vbias'
      ? { V: 1, mV: 1e-3, uV: 1e-6 }
      : field.name === 'HV_Imax'
        ? { mA: 1, uA: 1e-3, A: 1e3 }
        : explicit.units?.some((unit) => unit === 'ns')
          ? timeScales
          : undefined
  const scale = unitScales?.[parsed.unit as keyof typeof unitScales] ?? 1
  return {
    min: explicit.min === undefined ? undefined : explicit.min / scale,
    max: explicit.max === undefined ? undefined : explicit.max / scale,
    step: explicit.step / scale,
    integer: explicit.widget === 'integer' && parsed.unit === '',
  }
}

export function parseNumericValue(value: string): NumericValue | undefined {
  const match = value.trim().match(/^([+-]?(?:\d+(?:\.\d*)?|\.\d+))(?:\s+(.+))?$/)
  if (!match) return undefined
  const number = Number(match[1])
  if (!Number.isFinite(number)) return undefined
  return { number, unit: match[2] ?? '' }
}

export function formatNumericValue(number: number, unit: string) {
  return `${number}${unit ? ` ${unit}` : ''}`
}

export function numericError(field: ConfigurationField): string {
  const constraint = numericConstraint(field)
  if (!constraint) return ''
  const parsed = parseNumericValue(field.value)
  if (!parsed) return 'Enter a numeric value.'
  if (constraint.integer && !Number.isInteger(parsed.number)) return 'Enter a whole number.'
  if (constraint.min !== undefined && parsed.number < constraint.min)
    return `Minimum: ${constraint.min}${parsed.unit ? ` ${parsed.unit}` : ''}.`
  if (constraint.max !== undefined && parsed.number > constraint.max)
    return `Maximum: ${constraint.max}${parsed.unit ? ` ${parsed.unit}` : ''}.`
  const origin = constraint.min ?? 0
  const steps = (parsed.number - origin) / constraint.step
  if (Math.abs(steps - Math.round(steps)) > 1e-8)
    return `Use increments of ${constraint.step}${parsed.unit ? ` ${parsed.unit}` : ''}.`
  return ''
}

export function isMaskField(field: ConfigurationField) {
  return /^(ChEnableMask|Tlogic_Mask|Q_DiscrMask)[01]$/.test(field.name)
}

export function maskBits(low: string, high: string): boolean[] {
  const parse = (value: string) => {
    try {
      return BigInt(value.startsWith('0x') ? value : `0x${value}`) & 0xffffffffn
    } catch {
      return 0n
    }
  }
  const halves = [parse(low), parse(high)]
  return Array.from({ length: 64 }, (_, channel) => {
    const half = channel < 32 ? halves[0] : halves[1]
    return (half & (1n << BigInt(channel % 32))) !== 0n
  })
}

export function masksFromBits(bits: boolean[]): [string, string] {
  const halves = [0n, 0n]
  for (let channel = 0; channel < 64; channel++) {
    if (bits[channel]) halves[channel < 32 ? 0 : 1] |= 1n << BigInt(channel % 32)
  }
  return halves.map((value) => `0x${value.toString(16).toUpperCase().padStart(8, '0')}`) as [
    string,
    string,
  ]
}
import { janusParameterCatalog } from './janus/catalog'
