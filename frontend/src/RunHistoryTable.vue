<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { isMaskField, parseConfiguration, type ConfigurationField } from './configuration'
import { bytes, compact, localDateTime } from './presentation'
import { RunType } from './gen/pet/caen/daq/v1/system_pb'

interface RunRecord {
  runId: string
  startedAt?: { seconds: bigint; nanos: number }
  completedAt?: { seconds: bigint; nanos: number }
  terminationReason: string
  eventCount: bigint
  rawBatchCount: bigint
  incomplete: boolean
  artifacts: readonly { kind: string; name: string; sizeBytes: bigint; sha256: string }[]
  stopMode: string
  presetTimeMilliseconds: bigint
  presetEventCount: bigint
  runType: RunType
}

const props = withDefaults(
  defineProps<{
    runs: readonly RunRecord[]
    configuration: (runId: string) => Promise<string>
    downloadArtifact: (runId: string, artifactName: string) => Promise<void>
    label?: string
  }>(),
  { label: 'Stored runs' },
)

const selectedRunId = ref('')
const configurations = ref<Record<string, string>>({})
const configurationLoading = ref('')
const configurationErrors = ref<Record<string, string>>({})
const visibleRuns = computed(() => props.runs)
const configurationSections = computed(() => {
  const source = configurations.value[selectedRunId.value]
  if (!source) return []
  const sections = new Map<string, ReturnType<typeof parseConfiguration>['fields']>()
  for (const field of parseConfiguration(source).fields) {
    const fields = sections.get(field.section) ?? []
    fields.push(field)
    sections.set(field.section, fields)
  }
  return [...sections].map(([name, fields]) => ({ name, fields }))
})

watch(
  () => props.runs,
  () => {
    if (selectedRunId.value && !props.runs.some((run) => run.runId === selectedRunId.value))
      selectedRunId.value = ''
  },
)

function totalSize(run: RunRecord) {
  return run.artifacts.reduce((total, artifact) => total + artifact.sizeBytes, 0n)
}

function duration(run: RunRecord) {
  if (!run.startedAt || !run.completedAt) return run.incomplete ? 'In progress' : 'Not reported'
  const milliseconds =
    Number(run.completedAt.seconds - run.startedAt.seconds) * 1000 +
    (run.completedAt.nanos - run.startedAt.nanos) / 1_000_000
  if (milliseconds < 0) return 'Not reported'
  const seconds = Math.floor(milliseconds / 1000)
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const remainder = seconds % 60
  return [hours ? `${hours}h` : '', minutes ? `${minutes}m` : '', `${remainder}s`]
    .filter(Boolean)
    .join(' ')
}

async function selectRun(run: RunRecord) {
  if (selectedRunId.value === run.runId) {
    selectedRunId.value = ''
    return
  }
  selectedRunId.value = run.runId
  if (isScan(run)) return
  if (Object.hasOwn(configurations.value, run.runId)) return
  configurationLoading.value = run.runId
  delete configurationErrors.value[run.runId]
  try {
    configurations.value[run.runId] = await props.configuration(run.runId)
  } catch (reason) {
    configurationErrors.value[run.runId] = reason instanceof Error ? reason.message : String(reason)
  } finally {
    if (configurationLoading.value === run.runId) configurationLoading.value = ''
  }
}

function typeLabel(run: RunRecord) {
  if (run.runType === RunType.STAIRCASE) return 'Staircase scan'
  if (run.runType === RunType.HOLD_DELAY_SCAN) return 'Hold-delay scan'
  return 'Data run'
}

function isScan(run: RunRecord) {
  return run.runType === RunType.STAIRCASE || run.runType === RunType.HOLD_DELAY_SCAN
}

function scanWorkspaceDescription(run: RunRecord) {
  return run.runType === RunType.HOLD_DELAY_SCAN
    ? 'Open the Scans workspace to inspect the hold-delay spectra.'
    : 'Open the Scans workspace to compare channels, T-OR, and Q-OR curves.'
}

function downloadConfiguration(runId: string) {
  const source = configurations.value[runId]
  if (source === undefined) return
  const url = URL.createObjectURL(new Blob([source], { type: 'text/plain;charset=utf-8' }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `run_${runId}_configuration.txt`
  anchor.click()
  URL.revokeObjectURL(url)
}

function maskChannels(field: ConfigurationField) {
  if (!isMaskField(field)) return []
  try {
    const value = BigInt(field.value)
    const offset = field.name.endsWith('1') ? 32 : 0
    return Array.from({ length: 32 }, (_, bit) => bit)
      .filter((bit) => (value & (1n << BigInt(bit))) !== 0n)
      .map((bit) => bit + offset)
  } catch {
    return []
  }
}
</script>

<template>
  <div class="run-table-region">
    <div v-if="runs.length" class="run-table-wrap">
      <table class="run-table" :aria-label="label">
        <thead>
          <tr>
            <th scope="col">Run</th>
            <th scope="col">Type</th>
            <th scope="col">Date</th>
            <th scope="col">Duration</th>
            <th scope="col">Events</th>
            <th scope="col">Data size</th>
            <th scope="col">Status</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="run in visibleRuns" :key="run.runId">
            <tr
              class="run-row"
              :class="{ selected: selectedRunId === run.runId }"
              @click="selectRun(run)"
            >
              <th scope="row">
                <button
                  class="run-link"
                  type="button"
                  :aria-expanded="selectedRunId === run.runId"
                  :aria-controls="`run-details-${run.runId}`"
                  @click.stop="selectRun(run)"
                >
                  {{ run.runId }}
                </button>
              </th>
              <td>
                <span :class="['run-type', { scan: isScan(run) }]">{{
                  typeLabel(run)
                }}</span>
              </td>
              <td>{{ localDateTime(run.startedAt) }}</td>
              <td>{{ duration(run) }}</td>
              <td>{{ isScan(run) ? '—' : compact(run.eventCount) }}</td>
              <td>{{ bytes(totalSize(run)) }}</td>
              <td>
                <span :class="['run-status', { incomplete: run.incomplete }]">
                  {{ run.terminationReason || (run.incomplete ? 'Incomplete' : 'Completed') }}
                </span>
              </td>
            </tr>
            <tr v-if="selectedRunId === run.runId" :id="`run-details-${run.runId}`">
              <td colspan="7" class="run-detail-cell">
                <section class="run-detail" :aria-label="`Details for run ${run.runId}`">
                  <div class="run-detail-heading">
                    <div>
                      <p class="eyebrow">Run {{ run.runId }}</p>
                      <h3>Run details</h3>
                    </div>
                    <button class="link-button" type="button" @click="selectedRunId = ''">
                      Close
                    </button>
                  </div>
                  <dl class="run-facts">
                    <div>
                      <dt>Type</dt>
                      <dd>{{ typeLabel(run) }}</dd>
                    </div>
                    <div>
                      <dt>Started</dt>
                      <dd>{{ localDateTime(run.startedAt) }}</dd>
                    </div>
                    <div>
                      <dt>Completed</dt>
                      <dd>{{ localDateTime(run.completedAt) }}</dd>
                    </div>
                    <div>
                      <dt>Duration</dt>
                      <dd>{{ duration(run) }}</dd>
                    </div>
                    <div>
                      <dt>Termination</dt>
                      <dd>{{ run.terminationReason || 'Not reported' }}</dd>
                    </div>
                    <div v-if="!isScan(run)">
                      <dt>Events</dt>
                      <dd>{{ compact(run.eventCount) }}</dd>
                    </div>
                    <div v-if="!isScan(run)">
                      <dt>Raw batches</dt>
                      <dd>{{ compact(run.rawBatchCount) }}</dd>
                    </div>
                    <div>
                      <dt>Data size</dt>
                      <dd>{{ bytes(totalSize(run)) }}</dd>
                    </div>
                    <div v-if="!isScan(run)">
                      <dt>Stop mode</dt>
                      <dd>{{ run.stopMode || 'Not reported' }}</dd>
                    </div>
                    <div v-if="run.presetTimeMilliseconds">
                      <dt>Preset time</dt>
                      <dd>{{ compact(run.presetTimeMilliseconds) }} ms</dd>
                    </div>
                    <div v-if="run.presetEventCount">
                      <dt>Preset events</dt>
                      <dd>{{ compact(run.presetEventCount) }}</dd>
                    </div>
                  </dl>

                  <div class="run-detail-section">
                    <h4>Artifacts</h4>
                    <div v-if="run.artifacts.length" class="run-artifact-list">
                      <button
                        v-for="artifact in run.artifacts"
                        :key="artifact.name"
                        class="artifact-download"
                        type="button"
                        @click="downloadArtifact(run.runId, artifact.name)"
                      >
                        <span
                          ><strong>{{ artifact.name }}</strong
                          ><small>{{ artifact.kind }}</small></span
                        >
                        <span>{{ bytes(artifact.sizeBytes) }} · Download</span>
                      </button>
                    </div>
                    <p v-else class="empty">No artifact metadata was recorded.</p>
                  </div>

                  <div v-if="!isScan(run)" class="run-detail-section">
                    <div class="configuration-heading">
                      <div>
                        <h4>Configuration</h4>
                        <p>Parameters are grouped by their JANUS section.</p>
                      </div>
                      <button
                        v-if="configurations[run.runId] !== undefined"
                        class="link-button"
                        type="button"
                        @click="downloadConfiguration(run.runId)"
                      >
                        Download configuration (.txt)
                      </button>
                    </div>
                    <p v-if="configurationLoading === run.runId" class="muted">
                      Loading configuration…
                    </p>
                    <p v-else-if="configurationErrors[run.runId]" class="field-error" role="alert">
                      Could not load configuration: {{ configurationErrors[run.runId] }}
                    </p>
                    <p v-else-if="!configurationSections.length" class="empty">
                      No configuration was recorded.
                    </p>
                    <div v-else class="configuration-sections">
                      <details
                        v-for="(section, index) in configurationSections"
                        :key="section.name"
                        :open="index === 0"
                      >
                        <summary>
                          {{ section.name }} <span>{{ section.fields.length }} parameters</span>
                        </summary>
                        <div class="configuration-table-wrap">
                          <table class="configuration-table">
                            <thead>
                              <tr>
                                <th>Parameter</th>
                                <th>Scope</th>
                                <th>Value</th>
                                <th>Description</th>
                              </tr>
                            </thead>
                            <tbody>
                              <tr v-for="field in section.fields" :key="field.id">
                                <th scope="row">{{ field.name }}</th>
                                <td>
                                  {{
                                    field.channel !== undefined
                                      ? `Board ${field.index}, channel ${field.channel}`
                                      : field.index !== undefined
                                        ? `Board ${field.index}`
                                        : 'Global'
                                  }}
                                </td>
                                <td>
                                  <details v-if="isMaskField(field)" class="mask-value">
                                    <summary>
                                      <code>{{ field.value }}</code>
                                    </summary>
                                    <span>
                                      {{
                                        maskChannels(field).length
                                          ? `Channels: ${maskChannels(field).join(', ')}`
                                          : 'No channels selected'
                                      }}
                                    </span>
                                  </details>
                                  <code v-else>{{ field.value }}</code>
                                </td>
                                <td>{{ field.help || '—' }}</td>
                              </tr>
                            </tbody>
                          </table>
                        </div>
                      </details>
                    </div>
                  </div>
                  <div v-else class="run-detail-section">
                    <h4>Scan dataset</h4>
                    <p>{{ scanWorkspaceDescription(run) }}</p>
                  </div>
                </section>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
    <p v-else class="empty">No stored runs found.</p>
  </div>
</template>
