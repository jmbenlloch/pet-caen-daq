<script setup lang="ts">
import { create } from '@bufbuild/protobuf'
import { computed, onMounted, ref, watch, type DeepReadonly } from 'vue'
import type { DaqApi } from './api'
import { RunType, SearchRunsRequestSchema, type RunSummary } from './gen/pet/caen/daq/v1/system_pb'
import { compact, localDateTime } from './presentation'

const props = withDefaults(
  defineProps<{
    api: DaqApi
    capability: 'statistics' | 'histograms'
    selectedRunId?: string
    activeRunId?: string
    autoSelectFirst?: boolean
    enabled?: boolean
  }>(),
  { selectedRunId: undefined, activeRunId: undefined, autoSelectFirst: false, enabled: true },
)

const emit = defineEmits<{
  select: [run: DeepReadonly<RunSummary>]
}>()

const runs = ref<RunSummary[]>([])
const nextPageToken = ref('')
const pageTokens = ref([''])
const page = ref(1)
const runNumber = ref('')
const appliedRunNumber = ref<bigint>()
const loading = ref(false)
const error = ref('')
let requestSequence = 0

const capabilityLabel = computed(() =>
  props.capability === 'statistics' ? 'final statistics' : 'persisted histograms',
)

function available(run: DeepReadonly<RunSummary>) {
  return props.capability === 'statistics'
    ? Boolean(run.finalStatistics)
    : run.artifacts.some((artifact) => artifact.kind === 'histograms')
}

async function load(token = pageTokens.value[page.value - 1] ?? '') {
  const sequence = ++requestSequence
  loading.value = true
  error.value = ''
  try {
    const response = await props.api.searchRuns(
      create(SearchRunsRequestSchema, {
        limit: 8,
        pageToken: token,
        runType: RunType.DATA,
        runNumber: appliedRunNumber.value,
      }),
    )
    if (sequence !== requestSequence) return
    runs.value = response.runs
    nextPageToken.value = response.nextPageToken
    const first = response.runs.find(available)
    if (
      props.autoSelectFirst &&
      !props.activeRunId &&
      !props.selectedRunId &&
      page.value === 1 &&
      first
    )
      emit('select', first)
  } catch (reason) {
    if (sequence !== requestSequence) return
    error.value = reason instanceof Error ? reason.message : String(reason)
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

function search() {
  const input = String(runNumber.value).trim()
  if (input && !/^\d+$/.test(input)) {
    error.value = 'Enter a non-negative run number.'
    return
  }
  appliedRunNumber.value = input ? BigInt(input) : undefined
  pageTokens.value = ['']
  page.value = 1
  void load('')
}

function clearSearch() {
  runNumber.value = ''
  search()
}

function previousPage() {
  if (page.value === 1) return
  page.value--
  void load()
}

function nextPage() {
  if (!nextPageToken.value) return
  pageTokens.value[page.value] = nextPageToken.value
  page.value++
  void load(nextPageToken.value)
}

watch(
  () => props.activeRunId,
  (next, previous) => {
    if (props.enabled && previous && !next) void load()
  },
)

watch(
  () => props.enabled,
  (enabled, wasEnabled) => {
    if (enabled && !wasEnabled) void load()
  },
)

onMounted(() => {
  if (props.enabled) void load()
})
</script>

<template>
  <details class="run-data-picker" :aria-label="`Select run with ${capabilityLabel}`">
    <summary class="run-data-picker-summary">
      <div>
        <strong>Stored data runs</strong>
        <span>Select a run with {{ capabilityLabel }}.</span>
      </div>
      <span class="run-data-picker-state" aria-hidden="true"></span>
    </summary>
    <div class="run-data-picker-content">
      <form class="run-data-search" role="search" @submit.prevent="search">
        <label>
          Run number
          <input v-model="runNumber" type="number" min="0" inputmode="numeric" placeholder="Any" />
        </label>
        <button type="submit" class="secondary" :disabled="loading">Search</button>
        <button type="button" class="link-button" :disabled="loading" @click="clearSearch">
          Clear
        </button>
      </form>

      <div v-if="runs.length" class="run-data-list">
        <div class="run-data-list-header" aria-hidden="true">
          <span>Run</span>
          <span>Started</span>
          <span>Events</span>
          <span>Available data</span>
        </div>
        <button
          v-for="run in runs"
          :key="run.runId"
          type="button"
          class="run-data-row"
          :class="{ selected: selectedRunId === run.runId }"
          :disabled="!available(run)"
          :aria-pressed="selectedRunId === run.runId"
          @click="emit('select', run)"
        >
          <strong data-label="Run">{{ run.runId }}</strong>
          <span data-label="Started">{{ localDateTime(run.startedAt) }}</span>
          <span data-label="Events">{{ compact(run.eventCount) }}</span>
          <span data-label="Available data">
            {{ available(run) ? capabilityLabel : `No ${capabilityLabel}` }}
          </span>
        </button>
      </div>
      <p v-else-if="!loading && !error" class="empty">
        {{
          appliedRunNumber === undefined ? 'No stored data runs on this page.' : 'Run not found.'
        }}
      </p>
      <p v-if="loading" class="muted" aria-live="polite">Loading runs…</p>
      <p v-if="error" class="field-error" role="alert">{{ error }}</p>

      <nav
        v-if="appliedRunNumber === undefined && (page > 1 || nextPageToken)"
        class="run-pagination"
        aria-label="Stored data run pages"
      >
        <button type="button" :disabled="page === 1 || loading" @click="previousPage">
          Previous
        </button>
        <span>Page {{ page }}</span>
        <button type="button" :disabled="!nextPageToken || loading" @click="nextPage">Next</button>
      </nav>
    </div>
  </details>
</template>
