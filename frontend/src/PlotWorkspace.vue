<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch, type DeepReadonly } from 'vue'
import {
  HistogramKind,
  type Board,
  type HistogramDataset,
  type HistogramSelection,
} from './gen/pet/caen/daq/v1/system_pb'
import { compact } from './presentation'
import HistogramPlot from './HistogramPlot.vue'

const props = defineProps<{
  boards: Array<{ chain: number; node: number } & DeepReadonly<Board>>
  running: boolean
  loading: boolean
  datasets: readonly DeepReadonly<HistogramDataset>[]
  theme: 'dark' | 'light'
}>()
const emit = defineEmits<{ request: [kind: HistogramKind, selections: HistogramSelection[]] }>()
const kind = ref(HistogramKind.PHA_HIGH_GAIN)
const selected = ref(new Set<string>())
const selectorOpen = ref(false)
const autoRefresh = ref(true)
const logarithmic = ref(false)
const selectionError = ref('')
let timer: number | undefined

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
  if (next.has(key)) next.delete(key)
  else next.add(key)
  selected.value = next
}

function selectBoard(chain: number, node: number, value: boolean) {
  const next = new Set(selected.value)
  for (let channel = 0; channel < 64; channel++) {
    const key = selectionKey(chain, node, channel)
    if (value) next.add(key)
    else next.delete(key)
  }
  selected.value = next
}

function request() {
  if (!props.running) return
  if (!selected.value.size) {
    selectionError.value = 'Select at least one channel'
    return
  }
  const selections = [...selected.value]
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
  selectionError.value = ''
  emit('request', kind.value, selections)
}

function updateTimer() {
  window.clearInterval(timer)
  timer = undefined
  if (autoRefresh.value && props.running) timer = window.setInterval(request, 1000)
}
watch([autoRefresh, () => props.running], updateTimer, { immediate: true })
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
          {{ selected.size }} selected
        </button>
      </label>
      <label class="switch compact-switch"
        ><input v-model="autoRefresh" type="checkbox" /><span>Live refresh</span></label
      >
      <label class="switch compact-switch"
        ><input v-model="logarithmic" type="checkbox" /><span>Log Y</span></label
      >
      <button type="button" class="secondary" :disabled="!running || loading" @click="request">
        {{ loading ? 'Loading…' : 'Request data' }}
      </button>
    </div>
    <section v-if="selectorOpen" class="histogram-channel-selector" aria-label="Histogram channels">
      <article
        v-for="board in boards"
        :key="`${board.chain}:${board.node}`"
        class="histogram-board-selector"
      >
        <header>
          <strong>Board {{ board.chain }} · node {{ board.node }}</strong>
          <span>
            <button type="button" @click="selectBoard(board.chain, board.node, true)">All</button>
            <button type="button" @click="selectBoard(board.chain, board.node, false)">
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
            :aria-pressed="selected.has(selectionKey(board.chain, board.node, channel - 1))"
            :aria-label="`Board ${board.chain} node ${board.node} channel ${channel - 1}`"
            @click="toggleSelection(board.chain, board.node, channel - 1)"
          >
            {{ channel - 1 }}
          </button>
        </div>
      </article>
    </section>
    <p v-if="selectionError" class="field-error" role="alert">{{ selectionError }}</p>
    <p v-if="!running && !datasets.length" class="empty">
      Start a run to request accumulated histogram data.
    </p>
    <p v-else-if="!running" class="empty">
      Showing the last requested histogram from the completed run.
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
            >{{ compact(dataset.entries) }} entries · width
            {{ dataset.binWidth.toPrecision(4) }}</span
          ><span v-if="dataset.underflow || dataset.overflow"
            >Underflow {{ compact(dataset.underflow) }} · overflow
            {{ compact(dataset.overflow) }}</span
          >
        </div>
      </article>
    </div>
  </section>
</template>
