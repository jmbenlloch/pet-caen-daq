export interface ConfigurationField {
  id: string
  name: string
  index?: string
  section: string
  value: string
  help: string
  options: string[]
  line: number
}

export interface ConfigurationDocument {
  source: string
  fields: ConfigurationField[]
}

const assignment = /^(\s*)([A-Za-z][A-Za-z0-9_]*)(?:\[([^\]]+)\])?(\s+)(.*?)(\s*)(#.*)?$/

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
    const help = (match[7] ?? '').replace(/^#\s*/, '').trim()
    const optionsText = help.match(/Options?:\s*(.+?)(?:\.|$)/i)?.[1]
    fields.push({
      id: `${match[2]}[${match[3] ?? 'default'}]@${index + 1}`,
      name: match[2],
      index: match[3],
      section,
      value: match[5].trimEnd(),
      help,
      options: optionsText ? optionsText.split(',').map((item) => item.trim()) : [],
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
    `${match[1]}${match[2]}${match[3] === undefined ? '' : `[${match[3]}]`}${match[4]}${value}${match[6]}${match[7] ?? ''}`
  return parseConfiguration(lines.join(document.source.includes('\r\n') ? '\r\n' : '\n'))
}

export function isBooleanField(field: ConfigurationField) {
  return (field.value === '0' || field.value === '1') && field.options.length === 0
}
