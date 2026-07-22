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
const boardKey = ref('0:0')
const channels = ref('0')
const autoRefresh = ref(true)
const logarithmic = ref(false)
const selectionError = ref('')
let timer: number | undefined

watch(
  () => props.boards,
  (boards) => {
    if (boards.length && !boards.some((board) => `${board.chain}:${board.node}` === boardKey.value))
      boardKey.value = `${boards[0].chain}:${boards[0].node}`
  },
  { immediate: true },
)

function parseChannels(value: string) {
  const selected = new Set<number>()
  for (const token of value.split(',')) {
    const part = token.trim()
    if (!part) continue
    const match = /^(\d+)(?:-(\d+))?$/.exec(part)
    if (!match) throw new Error(`Invalid channel selection “${part}”`)
    const first = Number(match[1]),
      last = Number(match[2] ?? match[1])
    if (first > last || first < 0 || last > 63)
      throw new Error(`Channel range ${part} is outside 0–63`)
    for (let channel = first; channel <= last; channel++) selected.add(channel)
  }
  if (!selected.size) throw new Error('Select at least one channel')
  return [...selected].sort((a, b) => a - b)
}

function request() {
  if (!props.running) return
  try {
    const [chain, node] = boardKey.value.split(':').map(Number)
    const selections = parseChannels(channels.value).map((channel) => ({
      $typeName: 'pet.caen.daq.v1.HistogramSelection' as const,
      chain,
      node,
      channel,
    }))
    selectionError.value = ''
    emit('request', kind.value, selections)
  } catch (reason) {
    selectionError.value = reason instanceof Error ? reason.message : String(reason)
  }
}

function updateTimer() {
  window.clearInterval(timer)
  timer = undefined
  if (autoRefresh.value && props.running) timer = window.setInterval(request, 1000)
}
watch([autoRefresh, () => props.running], updateTimer, { immediate: true })
onBeforeUnmount(() => window.clearInterval(timer))

function populated(dataset: DeepReadonly<HistogramDataset>) {
  return dataset.bins
    .map((count, index) => ({ index, count }))
    .filter((bin) => bin.count > 0n)
    .slice(0, 12)
}
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
      <label
        >Board<select v-model="boardKey">
          <option
            v-for="board in boards"
            :key="`${board.chain}:${board.node}`"
            :value="`${board.chain}:${board.node}`"
          >
            Board {{ board.chain }} · node {{ board.node }}
          </option>
        </select></label
      >
      <label>Channels<input v-model="channels" placeholder="0, 2, 8-15" /></label>
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
    <p v-if="selectionError" class="field-error" role="alert">{{ selectionError }}</p>
    <p v-if="!running" class="empty">Start a run to request accumulated histogram data.</p>
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
        <div class="histogram-bin-preview" aria-label="Populated bin preview">
          <span>First populated bins</span
          ><code v-for="bin in populated(dataset)" :key="bin.index"
            >{{ bin.index }}:{{ compact(bin.count) }}</code
          ><small v-if="!populated(dataset).length">No populated bins yet</small>
        </div>
      </article>
    </div>
  </section>
</template>
