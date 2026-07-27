<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch, type DeepReadonly } from 'vue'
import type { DaqApi } from './api'
import {
  HistogramKind,
  type Board,
  type HistogramDataset,
  type HistogramSelection,
  type RunSummary,
} from './gen/pet/caen/daq/v1/system_pb'
import { compact } from './presentation'
import HistogramPlot from './HistogramPlot.vue'
import RunDataPicker from './RunDataPicker.vue'

const props = withDefaults(
  defineProps<{
    api: DaqApi
    boards: Array<{ chain: number; node: number; logicalIndex: number } & DeepReadonly<Board>>
    activeRunId?: string
    running: boolean
    loading: boolean
    datasets: readonly DeepReadonly<HistogramDataset>[]
    theme: 'dark' | 'light'
    runPickerEnabled?: boolean
  }>(),
  { activeRunId: undefined, runPickerEnabled: true },
)
const emit = defineEmits<{
  request: [runId: string, kind: HistogramKind, selections: HistogramSelection[]]
}>()
const kind = ref(HistogramKind.PHA_HIGH_GAIN)
const selectedRunId = ref('')
const selected = ref(new Set<string>())
const selectorOpen = ref(false)
const autoRefresh = ref(true)
const logarithmic = ref(false)
const selectionError = ref('')
const selectionLimit = 64
let timer: number | undefined
let lastAutomaticRequest = ''

watch(
  () => props.activeRunId,
  (activeRunId, previousActiveRunId) => {
    if (activeRunId && activeRunId !== previousActiveRunId) {
      selectedRunId.value = activeRunId
    }
  },
  { immediate: true },
)

function selectRun(run: DeepReadonly<RunSummary>) {
  selectedRunId.value = run.runId
}

watch(
  () => props.boards,
  (boards) => {
    const available = new Set(boards.map((board) => `${board.chain}:${board.node}`))
    const retained = new Set(
      [...selected.value].filter((key) => available.has(key.split(':').slice(0, 2).join(':'))),
    )
    if (!retained.size && boards.length) retained.add(`${boards[0].chain}:${boards[0].node}:0`)
    selected.value = retained
  },
  { immediate: true },
)

function selectionKey(chain: number, node: number, channel: number) {
  return `${chain}:${node}:${channel}`
}

function toggleSelection(chain: number, node: number, channel: number) {
  const next = new Set(selected.value)
  const key = selectionKey(chain, node, channel)
  if (next.has(key)) {
    next.delete(key)
    selectionError.value = ''
  } else {
    if (next.size >= selectionLimit) {
      selectionError.value = `Select no more than ${selectionLimit} channels at a time`
      return
    }
    next.add(key)
  }
  selected.value = next
}

function selectBoard(chain: number, node: number, value: boolean) {
  const next = new Set(selected.value)
  for (let channel = 0; channel < 64; channel++) {
    const key = selectionKey(chain, node, channel)
    if (!value) next.delete(key)
    else if (next.size < selectionLimit) next.add(key)
  }
  selectionError.value =
    value && next.size === selectionLimit
      ? `Selection is limited to ${selectionLimit} channels; clear another board to choose different channels`
      : ''
  selected.value = next
}

function histogramSelections() {
  return [...selected.value]
    .map((key) => key.split(':').map(Number))
    .sort(
      ([chainA, nodeA, channelA], [chainB, nodeB, channelB]) =>
        chainA - chainB || nodeA - nodeB || channelA - channelB,
    )
    .map(([chain, node, channel]) => ({
      $typeName: 'pet.caen.daq.v1.HistogramSelection' as const,
      chain,
      node,
      channel,
    }))
}

function requestSelections(selections: HistogramSelection[]) {
  if (!selectedRunId.value) return
  if (!selections.length) {
    selectionError.value = 'Select at least one channel'
    return
  }
  if (selections.length > selectionLimit) {
    selectionError.value = `Select no more than ${selectionLimit} channels at a time`
    return
  }
  emit('request', selectedRunId.value, kind.value, selections)
}

function request() {
  requestSelections(histogramSelections())
}

watch(
  [selectedRunId, kind, selected],
  ([runId, histogramKind, selectionKeys]) => {
    if (!runId || !selectionKeys.size) return
    const selections = histogramSelections()
    const requestKey = `${runId}:${histogramKind}:${selections
      .map(({ chain, node, channel }) => `${chain}:${node}:${channel}`)
      .join(',')}`
    if (requestKey === lastAutomaticRequest) return
    lastAutomaticRequest = requestKey
    requestSelections(selections)
  },
  { immediate: true },
)

function populatedBins(dataset: DeepReadonly<HistogramDataset>) {
  return dataset.bins.reduce((count, value) => count + (value > 0 ? 1 : 0), 0)
}

function peakCount(dataset: DeepReadonly<HistogramDataset>) {
  return dataset.bins.reduce((peak, value) => (value > peak ? value : peak), 0)
}

function updateTimer() {
  window.clearInterval(timer)
  timer = undefined
  if (autoRefresh.value && props.running && selectedRunId.value === props.activeRunId)
    timer = window.setInterval(request, 1000)
}
watch([autoRefresh, () => props.running, selectedRunId, () => props.activeRunId], updateTimer, {
  immediate: true,
})
onBeforeUnmount(() => window.clearInterval(timer))

const kindLabel = computed(
  () =>
    ({
      [HistogramKind.PHA_HIGH_GAIN]: 'PHA high gain',
      [HistogramKind.PHA_LOW_GAIN]: 'PHA low gain',
      [HistogramKind.TOA]: 'Time of arrival',
      [HistogramKind.TOT]: 'Time over threshold',
      [HistogramKind.UNSPECIFIED]: 'Unspecified',
    })[kind.value],
)
</script>

<template>
  <section class="plots panel" aria-labelledby="plots-heading">
    <div class="section-title">
      <div>
        <p class="eyebrow">Server-side accumulated data</p>
        <h2 id="plots-heading">Plots and histograms</h2>
      </div>
      <span class="safety">uPlot · drag horizontally to zoom</span>
    </div>
    <div class="plot-controls">
      <div class="plot-run-source">
        <span>Selected run</span>
        <strong>
          {{
            selectedRunId
              ? `Run ${selectedRunId}${selectedRunId === activeRunId ? ' · live' : ''}`
              : 'None'
          }}
        </strong>
      </div>
      <label
        >Histogram<select v-model="kind">
          <option :value="HistogramKind.PHA_HIGH_GAIN">PHA high gain</option>
          <option :value="HistogramKind.PHA_LOW_GAIN">PHA low gain</option>
          <option :value="HistogramKind.TOA">Time of arrival</option>
          <option :value="HistogramKind.TOT">Time over threshold</option>
        </select></label
      >
      <label class="histogram-channel-control"
        >Channels
        <button
          type="button"
          class="secondary"
          aria-haspopup="true"
          :aria-expanded="selectorOpen"
          @click="selectorOpen = !selectorOpen"
        >
          {{ selected.size }} / {{ selectionLimit }} selected
        </button>
      </label>
      <label class="switch compact-switch"
        ><input v-model="autoRefresh" type="checkbox" /><span>Live refresh</span></label
      >
      <label class="switch compact-switch"
        ><input v-model="logarithmic" type="checkbox" /><span>Log Y</span></label
      >
      <button
        type="button"
        class="secondary-accent plot-request-button"
        :disabled="!selectedRunId"
        :aria-busy="loading"
        @click="request"
      >
        Request data
      </button>
    </div>
    <section v-if="selectorOpen" class="histogram-channel-selector" aria-label="Histogram channels">
      <article
        v-for="board in boards"
        :key="`${board.chain}:${board.node}`"
        class="histogram-board-selector"
      >
        <header>
          <strong
            >Board {{ board.logicalIndex }} · Chain {{ board.chain }} · Node
            {{ board.node }}</strong
          >
          <span>
            <button
              type="button"
              class="secondary histogram-board-action"
              @click="selectBoard(board.chain, board.node, true)"
            >
              All
            </button>
            <button
              type="button"
              class="secondary histogram-board-action"
              @click="selectBoard(board.chain, board.node, false)"
            >
              Clear
            </button>
          </span>
        </header>
        <div class="channel-grid histogram-channel-grid">
          <button
            v-for="channel in 64"
            :key="channel - 1"
            type="button"
            :class="{ active: selected.has(selectionKey(board.chain, board.node, channel - 1)) }"
            :disabled="
              selected.size >= selectionLimit &&
              !selected.has(selectionKey(board.chain, board.node, channel - 1))
            "
            :aria-pressed="selected.has(selectionKey(board.chain, board.node, channel - 1))"
            :aria-label="`Board ${board.logicalIndex}, chain ${board.chain} node ${board.node}, channel ${channel - 1}`"
            @click="toggleSelection(board.chain, board.node, channel - 1)"
          >
            {{ channel - 1 }}
          </button>
        </div>
      </article>
    </section>
    <p v-if="selectionError" class="field-error" role="alert">{{ selectionError }}</p>
    <p v-if="!selectedRunId && !datasets.length" class="empty">
      No live or persisted run histograms are available.
    </p>
    <p v-else-if="selectedRunId !== activeRunId" class="empty">
      Viewing persisted histograms from run {{ selectedRunId }}.
    </p>
    <HistogramPlot
      v-if="datasets.length"
      :datasets="datasets"
      :theme="theme"
      :logarithmic="logarithmic"
    />
    <div v-if="datasets.length" class="histogram-datasets" aria-label="Histogram datasets">
      <article
        v-for="dataset in datasets"
        :key="`${dataset.chain}-${dataset.node}-${dataset.channel}`"
        class="histogram-dataset"
      >
        <div class="histogram-metadata">
          <strong>B{{ dataset.chain }} · CH {{ dataset.channel }}</strong
          ><span>{{ kindLabel }} · {{ dataset.bins.length }} bins</span
          ><span
            >{{ compact(dataset.entries) }} entries · bin width
            {{ dataset.binWidth.toPrecision(4) }}</span
          ><span
            >{{ populatedBins(dataset) }} populated bins · peak
            {{ compact(peakCount(dataset)) }}</span
          ><span v-if="dataset.underflow || dataset.overflow"
            >Underflow {{ compact(dataset.underflow) }} · overflow
            {{ compact(dataset.overflow) }}</span
          >
        </div>
      </article>
    </div>
    <RunDataPicker
      :api="api"
      capability="histograms"
      :selected-run-id="selectedRunId"
      :active-run-id="activeRunId"
      auto-select-first
      :enabled="runPickerEnabled"
      @select="selectRun"
    />
  </section>
</template>
