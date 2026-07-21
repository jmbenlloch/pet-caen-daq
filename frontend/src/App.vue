<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { createDaqApi, type DaqApi } from './api'
import { DiagnosticSeverity, HealthStatus } from './gen/pet/caen/daq/v1/system_pb'
import { bytes, compact, healthLabel, stateLabel } from './presentation'
import { useDaq } from './useDaq'

const props = defineProps<{ api?: DaqApi }>()
const daq = useDaq(props.api ?? createDaqApi())
const runId = ref('')
const requestedBy = ref('operator')
const configuration = ref('')
const captureRaw = ref(true)
const journalTransport = ref(true)
const configFile = ref<HTMLInputElement>()

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
            <label for="configuration">JANUS configuration</label>
            <button class="link-button" type="button" @click="configFile?.click()">
              Load file
            </button>
            <input
              ref="configFile"
              class="visually-hidden"
              type="file"
              accept=".txt,.cfg,text/plain"
              @change="loadConfiguration"
            />
          </div>
          <textarea
            id="configuration"
            v-model="configuration"
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
              :disabled="daq.busy.value || !configuration"
              @click="daq.validate(configuration)"
            >
              Validate
            </button>
            <button
              class="primary"
              type="button"
              :disabled="!daq.canStart.value || !runId || !requestedBy || !configuration"
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
  </div>
</template>
