<script setup lang="ts">
import { computed, ref, watch, type DeepReadonly } from 'vue'
import type {
  BoardStatistics,
  PipelineTelemetry,
  StatisticsTelemetry,
  StorageTelemetry,
} from './gen/pet/caen/daq/v1/system_pb'
import { bytes, compact } from './presentation'

const props = defineProps<{
  statistics?: DeepReadonly<StatisticsTelemetry>
  pipeline?: DeepReadonly<PipelineTelemetry>
  storage?: DeepReadonly<StorageTelemetry>
}>()

type Metric = 'channelTriggerCounts' | 'timestampCounts' | 'phaCounts'
const metric = ref<Metric>('channelTriggerCounts')
const integral = ref(false)
const selectedBoard = ref<number | 'all'>('all')
const previous = ref<DeepReadonly<StatisticsTelemetry>>()

watch(
  () => props.statistics,
  (next, old) => {
    if (next && old && next.elapsedMilliseconds > old.elapsedMilliseconds) previous.value = old
    else if (!next || !old || next.elapsedMilliseconds < old.elapsedMilliseconds)
      previous.value = undefined
  },
)

const boards = computed(() => props.statistics?.boards ?? [])
const active = computed(() =>
  selectedBoard.value === 'all'
    ? undefined
    : boards.value.find((board) => board.chain === selectedBoard.value),
)

function prior(board: DeepReadonly<BoardStatistics>) {
  return previous.value?.boards.find(
    (candidate) => candidate.chain === board.chain && candidate.node === board.node,
  )
}

function elapsedSeconds() {
  const current = Number(props.statistics?.elapsedMilliseconds ?? 0n)
  const baseline = integral.value ? 0 : Number(previous.value?.elapsedMilliseconds ?? current)
  return Math.max((current - baseline) / 1000, 0)
}

function difference(value: bigint, before: bigint | undefined) {
  if (integral.value) return value
  return value >= (before ?? value) ? value - (before ?? value) : value
}

function count(board: DeepReadonly<BoardStatistics>, channel: number) {
  const current = board[metric.value][channel] ?? 0n
  const old = prior(board)?.[metric.value][channel]
  return difference(current, old)
}

function channelValue(board: DeepReadonly<BoardStatistics>, channel: number) {
  const value = count(board, channel)
  if (metric.value.endsWith('Counts') && !integral.value) {
    const seconds = elapsedSeconds()
    return seconds > 0 ? `${(Number(value) / seconds).toFixed(1)} Hz` : '—'
  }
  return compact(value)
}

function boardRate(board: DeepReadonly<BoardStatistics>, field: 'triggerCount' | 'dataBytes') {
  const seconds = elapsedSeconds()
  if (seconds <= 0) return '—'
  const value = difference(board[field], prior(board)?.[field])
  return field === 'dataBytes' ? `${bytes(value)}/s` : `${(Number(value) / seconds).toFixed(1)} Hz`
}

function percent(numerator: bigint, denominator: bigint) {
  return denominator > 0n
    ? `${((Number(numerator) / Number(denominator)) * 100).toFixed(2)}%`
    : '0.00%'
}

const metricLabel = computed(
  () =>
    ({
      channelTriggerCounts: 'Channel trigger',
      timestampCounts: 'Timestamp',
      phaCounts: 'PHA',
    })[metric.value],
)
</script>

<template>
  <section class="statistics panel" aria-labelledby="statistics-heading">
    <div class="section-title statistics-title">
      <div>
        <p class="eyebrow">Live runtime view</p>
        <h2 id="statistics-heading">Statistics</h2>
      </div>
      <div class="statistics-controls">
        <label>
          Statistics type
          <select v-model="metric">
            <option value="channelTriggerCounts">Channel trigger</option>
            <option value="timestampCounts">Timestamp</option>
            <option value="phaCounts">PHA</option>
          </select>
        </label>
        <label class="switch compact-switch">
          <input v-model="integral" type="checkbox" />
          <span>Integral</span>
        </label>
      </div>
    </div>

    <div class="statistics-summary" aria-label="Global statistics">
      <span
        ><strong>{{ compact(pipeline?.decodedEvents) }}</strong> decoded events</span
      >
      <span
        ><strong>{{ compact(pipeline?.acceptedBatches) }}</strong> accepted batches</span
      >
      <span
        ><strong>{{ compact(pipeline?.rejectedBatches) }}</strong> rejected batches</span
      >
      <span
        ><strong>{{ bytes(storage?.bytesWritten) }}</strong> persisted</span
      >
      <span
        ><strong>{{ (Number(statistics?.elapsedMilliseconds ?? 0n) / 1000).toFixed(1) }} s</strong>
        elapsed</span
      >
    </div>

    <div class="statistics-board-tabs" role="tablist" aria-label="Statistics board">
      <button
        type="button"
        role="tab"
        :aria-selected="selectedBoard === 'all'"
        @click="selectedBoard = 'all'"
      >
        All boards
      </button>
      <button
        v-for="board in boards"
        :key="board.chain"
        type="button"
        role="tab"
        :aria-selected="selectedBoard === board.chain"
        @click="selectedBoard = board.chain"
      >
        Board {{ board.chain }}
      </button>
    </div>

    <div v-if="selectedBoard === 'all'" class="statistics-table-wrap">
      <table class="statistics-table">
        <thead>
          <tr>
            <th>Board</th>
            <th>Timestamp</th>
            <th>Trigger ID</th>
            <th>Trigger rate</th>
            <th>Lost trigger</th>
            <th>Event build</th>
            <th>Data rate</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="board in boards" :key="`${board.chain}-${board.node}`">
            <th>B{{ board.chain }}</th>
            <td>{{ compact(board.timestamp) }}</td>
            <td>{{ compact(board.triggerId) }}</td>
            <td>{{ boardRate(board, 'triggerCount') }}</td>
            <td>
              {{ percent(board.lostTriggerCount, board.triggerCount + board.lostTriggerCount) }}
            </td>
            <td>
              {{ percent(board.eventBuildCount, board.eventBuildCount + board.lostTriggerCount) }}
            </td>
            <td>{{ boardRate(board, 'dataBytes') }}</td>
          </tr>
          <tr v-if="!boards.length">
            <td colspan="7" class="empty">Statistics become available while a run is active.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div
      v-else-if="active"
      class="channel-statistics"
      :aria-label="`Board ${active.chain} channel statistics`"
    >
      <div v-for="channel in 64" :key="channel - 1" class="channel-statistic">
        <span>CH {{ channel - 1 }}</span>
        <strong>{{ channelValue(active, channel - 1) }}</strong>
      </div>
      <p class="statistics-caption">
        {{ metricLabel }}
        {{ integral ? 'integrated count' : 'rate over the latest telemetry interval' }}
      </p>
    </div>
  </section>
</template>
