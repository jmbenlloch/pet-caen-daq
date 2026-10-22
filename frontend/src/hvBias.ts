import { parseNumericValue, type ConfigurationField } from './configuration'

export function biasVoltageInVolts(value: string): number | undefined {
  const parsed = parseNumericValue(value)
  if (!parsed) return undefined
  const scale = { '': 1, V: 1, mV: 1e-3, uV: 1e-6 }[parsed.unit]
  if (scale === undefined) return undefined
  const voltage = parsed.number * scale
  return voltage >= 20 && voltage <= 85 ? voltage : undefined
}

export function nominalChannelVoltage(
  bias: number | undefined,
  range: string | undefined,
  adjustment: string | undefined,
): number | undefined {
  if (bias === undefined || !Number.isFinite(bias)) return undefined
  if (range === 'DISABLED') return bias
  const span = range === '4.5' ? 4.2 : range === '2.5' ? 2.5 : undefined
  if (span === undefined || adjustment === undefined || adjustment.trim() === '') return undefined
  const code = Number(adjustment)
  if (!Number.isInteger(code) || code < 0 || code > 255) return undefined
  return bias - (span * (255 - code)) / 255
}

export function effectiveHVValue(
  fields: ConfigurationField[],
  name: string,
  board?: number,
  channel?: number,
): string | undefined {
  const assignment = (index?: number, ch?: number) =>
    [...fields]
      .reverse()
      .find(
        (field) =>
          field.name === name &&
          field.index === (index === undefined ? undefined : String(index)) &&
          field.channel === (ch === undefined ? undefined : String(ch)),
      )?.value
  return (
    (channel === undefined ? undefined : assignment(board, channel)) ??
    assignment(board) ??
    assignment()
  )
}

export function boardChannelVoltages(fields: ConfigurationField[], board: number) {
  const bias = biasVoltageInVolts(effectiveHVValue(fields, 'HV_Vbias', board) ?? '')
  const range = effectiveHVValue(fields, 'HV_Adjust_Range')
  return Array.from({ length: 64 }, (_, channel) =>
    nominalChannelVoltage(bias, range, effectiveHVValue(fields, 'HV_IndivAdj', board, channel)),
  )
}
