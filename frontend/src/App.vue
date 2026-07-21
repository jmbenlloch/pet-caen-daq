<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import defaultConfiguration from '../../test/fixtures/janus/config_same4_v3_good.txt?raw'
import { createDaqApi, type DaqApi } from './api'
import MaskEditor from './MaskEditor.vue'
import NumericField from './NumericField.vue'
import {
  isBooleanField,
  isMaskField,
  numericConstraint,
  numericError,
  parseConfiguration,
  updateConfiguration,
  type ConfigurationField,
} from './configuration'
import { DiagnosticSeverity, HealthStatus } from './gen/pet/caen/daq/v1/system_pb'
import { bytes, compact, healthLabel, stateLabel } from './presentation'
import { useDaq } from './useDaq'

const props = defineProps<{ api?: DaqApi }>()
const daq = useDaq(props.api ?? createDaqApi())
const runId = ref('')
const requestedBy = ref('operator')
const configurationDocument = ref(parseConfiguration(defaultConfiguration))
const configuration = computed({
  get: () => configurationDocument.value.source,
  set: (source: string) => (configurationDocument.value = parseConfiguration(source)),
})
const captureRaw = ref(true)
const journalTransport = ref(true)
const configFile = ref<HTMLInputElement>()
const selectedSection = ref('All')
const parameterSearch = ref('')
const showRawConfiguration = ref(false)
const activeMask = ref<{ low: ConfigurationField; high: ConfigurationField }>()

const sections = computed(() => [
  'All',
  ...new Set(configurationDocument.value.fields.map((field) => field.section)),
])
const visibleFields = computed(() => {
  const query = parameterSearch.value.trim().toLowerCase()
  return configurationDocument.value.fields.filter(
    (field) =>
      !(isMaskField(field) && field.name.endsWith('1')) &&
      (selectedSection.value === 'All' || field.section === selectedSection.value) &&
      (!query ||
        field.name.toLowerCase().includes(query) ||
        field.help.toLowerCase().includes(query) ||
        field.value.toLowerCase().includes(query)),
  )
})
const configurationErrors = computed(() =>
  configurationDocument.value.fields
    .map((field) => ({ field, error: numericError(field) }))
    .filter((item) => item.error),
)

const state = computed(() => stateLabel[daq.snapshot.value?.state ?? 0])
const boards = computed(() =>
  (daq.snapshot.value?.chains ?? []).flatMap((chain) =>
    chain.boards.map((board) => ({ chain: chain.index, ...board })),
  ),
)
const severeDiagnostics = computed(() =>
  (daq.snapshot.value?.diagnostics ?? []).filter(
    (item) => item.severity >= DiagnosticSeverity.WARNING,
  ),
)

async function loadConfiguration(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (file) configuration.value = await file.text()
}

function setField(field: ConfigurationField, value: string) {
  configurationDocument.value = updateConfiguration(configurationDocument.value, field, value)
}

function openMask(field: ConfigurationField) {
  const highName = field.name.replace(/0$/, '1')
  const high = configurationDocument.value.fields.find(
    (candidate) => candidate.name === highName && candidate.index === field.index,
  )
  if (high) activeMask.value = { low: field, high }
}

function applyMask(low: string, high: string) {
  if (!activeMask.value) return
  configurationDocument.value = updateConfiguration(
    configurationDocument.value,
    activeMask.value.low,
    low,
  )
  const refreshedHigh = configurationDocument.value.fields.find(
    (field) =>
      field.name === activeMask.value?.high.name && field.index === activeMask.value?.high.index,
  )
  if (refreshedHigh)
    configurationDocument.value = updateConfiguration(
      configurationDocument.value,
      refreshedHigh,
      high,
    )
  activeMask.value = undefined
}

function loadDefaultConfiguration() {
  configuration.value = defaultConfiguration
}

function loadBackendConfiguration() {
  if (daq.configurationTemplate.value) configuration.value = daq.configurationTemplate.value
}

onMounted(() => daq.connect())
</script>

<template>
  <div class="shell">
    <header class="masthead">
      <div>
        <p class="eyebrow">PET detector control</p>
        <h1>CAEN acquisition</h1>
      </div>
      <div class="connection" role="status" aria-live="polite">
        <span
          class="status-dot"
          :class="{ live: daq.connected.value && !daq.stale.value }"
          aria-hidden="true"
        />
        <span>{{ daq.stale.value ? 'Telemetry stale' : 'Live telemetry' }}</span>
        <small>{{ daq.snapshot.value?.instanceId || 'No backend' }}</small>
      </div>
    </header>

    <main>
      <section class="hero panel" aria-labelledby="system-heading">
        <div>
          <p class="eyebrow">System state</p>
          <h2 id="system-heading">{{ state }}</h2>
          <p class="muted">
            Sequence {{ compact(daq.snapshot.value?.sequence) }} ·
            {{ daq.snapshot.value?.chains.length ?? 0 }} provisioned links
          </p>
        </div>
        <div v-if="daq.snapshot.value?.currentRun" class="run-now">
          <span>Active run</span>
          <strong>{{ daq.snapshot.value.currentRun.runId }}</strong>
          <span>{{ compact(daq.snapshot.value.currentRun.eventCount) }} events</span>
        </div>
        <div v-else class="run-now quiet"><span>No active run</span></div>
      </section>

      <div v-if="daq.error.value" class="alert error" role="alert">
        <strong>Connection or command failed</strong>
        <span>{{ daq.error.value }}</span>
      </div>

      <div v-if="severeDiagnostics.length" class="alert warning" role="alert">
        <strong>System diagnostics</strong>
        <span v-for="diagnostic in severeDiagnostics" :key="diagnostic.code">
          {{ diagnostic.code }} — {{ diagnostic.message }}
        </span>
      </div>

      <section class="workspace">
        <div class="panel control-panel">
          <div class="section-title">
            <div>
              <p class="eyebrow">Run control</p>
              <h2>New acquisition</h2>
            </div>
            <span class="safety">Configuration is validated before start</span>
          </div>

          <div class="fields">
            <label>
              Run ID
              <input v-model.trim="runId" autocomplete="off" placeholder="run-0055" />
            </label>
            <label>
              Requested by
              <input v-model.trim="requestedBy" autocomplete="name" />
            </label>
          </div>

          <div class="config-heading">
            <div>
              <label>Acquisition parameters</label>
              <p class="muted">
                {{ configurationDocument.fields.length }} settings from the backend template
              </p>
            </div>
            <div class="config-tools">
              <button class="link-button" type="button" @click="loadDefaultConfiguration">
                Reset sample
              </button>
              <button
                class="link-button"
                type="button"
                :disabled="!daq.configurationTemplate.value"
                @click="loadBackendConfiguration"
              >
                Use backend config
              </button>
              <button
                class="link-button"
                type="button"
                @click="showRawConfiguration = !showRawConfiguration"
              >
                {{ showRawConfiguration ? 'Use parameter editor' : 'Edit source' }}
              </button>
              <button class="link-button" type="button" @click="configFile?.click()">
                Import file
              </button>
            </div>
            <input
              ref="configFile"
              class="visually-hidden"
              type="file"
              accept=".txt,.cfg,text/plain"
              @change="loadConfiguration"
            />
          </div>
          <div
            v-if="!showRawConfiguration"
            class="parameter-editor"
            aria-label="Configuration parameters"
          >
            <div class="parameter-toolbar">
              <label>
                Find a parameter
                <input
                  v-model="parameterSearch"
                  type="search"
                  placeholder="Threshold, trigger, gain…"
                />
              </label>
              <span>{{ visibleFields.length }} shown</span>
            </div>
            <div class="section-tabs" role="tablist" aria-label="Parameter categories">
              <button
                v-for="section in sections"
                :key="section"
                type="button"
                role="tab"
                :aria-selected="selectedSection === section"
                :class="{ active: selectedSection === section }"
                @click="selectedSection = section"
              >
                {{ section }}
              </button>
            </div>
            <div class="parameter-list">
              <article v-for="field in visibleFields" :key="field.id" class="parameter-row">
                <div class="parameter-copy">
                  <label :for="field.id">
                    {{ field.name }}
                    <span v-if="field.index !== undefined" class="override"
                      >Override {{ field.index }}</span
                    >
                  </label>
                  <p>{{ field.help || `JANUS configuration line ${field.line}` }}</p>
                </div>
                <label v-if="isBooleanField(field)" class="switch">
                  <input
                    :id="field.id"
                    type="checkbox"
                    :checked="field.value === '1'"
                    @change="
                      setField(field, ($event.target as HTMLInputElement).checked ? '1' : '0')
                    "
                  />
                  <span>{{ field.value === '1' ? 'Enabled' : 'Disabled' }}</span>
                </label>
                <div v-else-if="isMaskField(field)" class="mask-summary">
                  <code>{{ field.value }}</code>
                  <button type="button" class="secondary" @click="openMask(field)">
                    Configure channels
                  </button>
                </div>
                <select
                  v-else-if="field.options.length"
                  :id="field.id"
                  :value="field.value"
                  @change="setField(field, ($event.target as HTMLSelectElement).value)"
                >
                  <option v-if="!field.options.includes(field.value)" :value="field.value">
                    {{ field.value }}
                  </option>
                  <option v-for="option in field.options" :key="option" :value="option">
                    {{ option }}
                  </option>
                </select>
                <NumericField
                  v-else-if="numericConstraint(field)"
                  :field="field"
                  :constraint="numericConstraint(field)!"
                  @change="setField(field, $event)"
                />
                <input
                  v-else
                  :id="field.id"
                  :value="field.value"
                  @change="setField(field, ($event.target as HTMLInputElement).value)"
                />
              </article>
              <p v-if="!visibleFields.length" class="empty">No parameters match this filter.</p>
            </div>
          </div>
          <div v-if="configurationErrors.length" class="configuration-errors" role="alert">
            <strong
              >{{ configurationErrors.length }} parameter value{{
                configurationErrors.length === 1 ? '' : 's'
              }}
              outside the allowed range</strong
            >
            <span v-for="item in configurationErrors.slice(0, 3)" :key="item.field.id"
              >{{ item.field.name }}: {{ item.error }}</span
            >
          </div>
          <textarea
            v-else
            id="configuration"
            v-model="configuration"
            aria-label="JANUS configuration source"
            spellcheck="false"
            placeholder="Paste or load the production JANUS configuration"
          />

          <ul
            v-if="daq.validationIssues.value.length"
            class="issues"
            aria-label="Validation issues"
          >
            <li
              v-for="issue in daq.validationIssues.value"
              :key="`${issue.sourceLine}-${issue.field}`"
            >
              <strong
                >Line {{ issue.sourceLine || '—' }} · {{ issue.field || 'configuration' }}</strong
              >
              {{ issue.message }}
            </li>
          </ul>

          <div class="options">
            <label
              ><input v-model="captureRaw" type="checkbox" /> Preserve complete raw batches</label
            >
            <label
              ><input v-model="journalTransport" type="checkbox" /> Journal socket evidence</label
            >
          </div>

          <div class="actions">
            <button
              class="secondary"
              type="button"
              :disabled="daq.busy.value || !configuration || configurationErrors.length > 0"
              @click="daq.validate(configuration)"
            >
              Validate
            </button>
            <button
              class="primary"
              type="button"
              :disabled="
                !daq.canStart.value ||
                !runId ||
                !requestedBy ||
                !configuration ||
                configurationErrors.length > 0
              "
              @click="
                daq.startRun({ runId, requestedBy, configuration, captureRaw, journalTransport })
              "
            >
              Start run
            </button>
            <button
              class="danger"
              type="button"
              :disabled="!daq.canStop.value || !requestedBy"
              @click="daq.stopRun(requestedBy)"
            >
              Stop and drain
            </button>
          </div>
        </div>

        <aside class="side-column">
          <section class="panel" aria-labelledby="pipeline-heading">
            <p class="eyebrow">Data path</p>
            <h2 id="pipeline-heading">Pipeline</h2>
            <dl class="metrics">
              <div>
                <dt>Decoded events</dt>
                <dd>{{ compact(daq.snapshot.value?.pipeline?.decodedEvents) }}</dd>
              </div>
              <div>
                <dt>Queue depth</dt>
                <dd>
                  {{ compact(daq.snapshot.value?.pipeline?.queueDepth) }} /
                  {{ compact(daq.snapshot.value?.pipeline?.queueCapacity) }}
                </dd>
              </div>
              <div>
                <dt>Rejected</dt>
                <dd>{{ compact(daq.snapshot.value?.pipeline?.rejectedBatches) }}</dd>
              </div>
              <div>
                <dt>Decode failures</dt>
                <dd>{{ compact(daq.snapshot.value?.pipeline?.decodeFailures) }}</dd>
              </div>
            </dl>
          </section>

          <section class="panel" aria-labelledby="storage-heading">
            <p class="eyebrow">Persistence</p>
            <h2 id="storage-heading">Storage</h2>
            <div class="health-line">
              <span
                class="health-dot"
                :class="{ healthy: daq.snapshot.value?.storage?.health === HealthStatus.OK }"
              />
              {{ healthLabel[daq.snapshot.value?.storage?.health ?? 0] }}
            </div>
            <p class="path">
              {{ daq.snapshot.value?.storage?.runDirectory || 'No run directory' }}
            </p>
            <p class="muted">
              {{ compact(daq.snapshot.value?.storage?.bytesWritten) }} bytes written
            </p>
          </section>
        </aside>
      </section>

      <section
        v-if="daq.latestCompletedRun.value"
        class="completed panel"
        aria-labelledby="completed-heading"
      >
        <div class="completed-summary">
          <div>
            <p class="eyebrow">Latest completed run</p>
            <h2 id="completed-heading">{{ daq.latestCompletedRun.value.runId }}</h2>
          </div>
          <dl class="completed-counts">
            <div>
              <dt>Termination</dt>
              <dd>{{ daq.latestCompletedRun.value.terminationReason || 'Completed normally' }}</dd>
            </div>
            <div>
              <dt>Events</dt>
              <dd>{{ compact(daq.latestCompletedRun.value.eventCount) }}</dd>
            </div>
            <div>
              <dt>Raw batches</dt>
              <dd>{{ compact(daq.latestCompletedRun.value.rawBatchCount) }}</dd>
            </div>
          </dl>
        </div>
        <div v-if="daq.latestCompletedRun.value.incomplete" class="incomplete" role="alert">
          This run is incomplete. Preserve and inspect its evidence before recovery.
        </div>
        <div class="artifacts" aria-label="Run artifacts">
          <article
            v-for="artifact in daq.latestCompletedRun.value.artifacts"
            :key="artifact.name"
            class="artifact"
          >
            <div>
              <strong>{{ artifact.name }}</strong>
              <span>{{ artifact.kind }} · {{ bytes(artifact.sizeBytes) }}</span>
            </div>
            <code :title="artifact.sha256">{{ artifact.sha256 || 'Digest unavailable' }}</code>
          </article>
          <p v-if="!daq.latestCompletedRun.value.artifacts.length" class="empty">
            No artifact metadata was returned.
          </p>
        </div>
      </section>

      <section class="completed panel" aria-labelledby="history-heading">
        <div class="section-title">
          <div>
            <p class="eyebrow">Persistent storage</p>
            <h2 id="history-heading">Run history</h2>
          </div>
          <button
            class="link-button"
            type="button"
            :disabled="daq.busy.value"
            @click="daq.refreshHistory()"
          >
            Refresh
          </button>
        </div>
        <div class="artifacts" aria-label="Stored runs">
          <article
            v-for="run in daq.runHistory.value"
            :key="run.runId"
            class="artifact history-run"
          >
            <div>
              <strong>{{ run.runId }}</strong>
              <span
                >{{ compact(run.eventCount) }} events ·
                {{ run.terminationReason || (run.incomplete ? 'Incomplete' : 'Completed') }}</span
              >
            </div>
            <div class="history-actions">
              <button
                v-for="artifact in run.artifacts"
                :key="artifact.name"
                class="link-button"
                type="button"
                :disabled="daq.busy.value"
                @click="daq.downloadArtifact(run.runId, artifact.name)"
              >
                {{ artifact.name }} · {{ bytes(artifact.sizeBytes) }}
              </button>
            </div>
          </article>
          <p v-if="!daq.runHistory.value.length" class="empty">No stored runs found.</p>
        </div>
      </section>

      <section class="boards-section" aria-labelledby="boards-heading">
        <div class="section-title">
          <div>
            <p class="eyebrow">Four-link topology</p>
            <h2 id="boards-heading">Detector boards</h2>
          </div>
        </div>
        <div class="board-grid">
          <article v-for="board in boards" :key="`${board.chain}-${board.node}`" class="board-card">
            <div class="board-title">
              <div>
                <span>Chain {{ board.chain }}</span
                ><strong>DT5202 · node {{ board.node }}</strong>
              </div>
              <span class="health-pill" :class="healthLabel[board.health].toLowerCase()">{{
                healthLabel[board.health]
              }}</span>
            </div>
            <dl class="metrics board-metrics">
              <div>
                <dt>FPGA</dt>
                <dd>0x{{ board.fpgaFirmware.toString(16).toUpperCase() }}</dd>
              </div>
              <div>
                <dt>Board temp.</dt>
                <dd>{{ board.boardTemperatureC.toFixed(1) }} °C</dd>
              </div>
              <div>
                <dt>Detector temp.</dt>
                <dd>{{ board.detectorTemperatureC.toFixed(1) }} °C</dd>
              </div>
              <div>
                <dt>HV</dt>
                <dd>{{ board.hvOn ? `${board.hvVoltageV.toFixed(1)} V` : 'Off' }}</dd>
              </div>
              <div>
                <dt>Events</dt>
                <dd>{{ compact(board.eventCount) }}</dd>
              </div>
            </dl>
          </article>
          <p v-if="!boards.length" class="empty">Waiting for discovered boards…</p>
        </div>
      </section>
    </main>
    <MaskEditor
      v-if="activeMask"
      :title="`${activeMask.low.name.replace(/0$/, '')}${activeMask.low.index === undefined ? '' : ` · override ${activeMask.low.index}`}`"
      :low="activeMask.low.value"
      :high="activeMask.high.value"
      @apply="applyMask"
      @close="activeMask = undefined"
    />
  </div>
</template>
