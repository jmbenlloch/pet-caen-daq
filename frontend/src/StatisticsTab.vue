<script setup lang="ts">
import { computed, ref, watch, type DeepReadonly } from 'vue'
import type { DaqApi } from './api'
import type {
  BoardStatistics,
  PipelineTelemetry,
  RunSummary,
  StatisticsTelemetry,
  StorageTelemetry,
} from './gen/pet/caen/daq/v1/system_pb'
import { bytes, compact } from './presentation'
import RunDataPicker from './RunDataPicker.vue'

const props = withDefaults(
  defineProps<{
    api: DaqApi
    statistics?: DeepReadonly<StatisticsTelemetry>
    pipeline?: DeepReadonly<PipelineTelemetry>
    storage?: DeepReadonly<StorageTelemetry>
    liveRunId?: string
    runPickerEnabled?: boolean
  }>(),
  {
    statistics: undefined,
    pipeline: undefined,
    storage: undefined,
    liveRunId: undefined,
    runPickerEnabled: true,
  },
)

type Metric = 'channelTriggerCounts' | 'timestampCounts' | 'phaCounts'
const metric = ref<Metric>('channelTriggerCounts')
const integral = ref(false)
const selectedBoard = ref<string | 'all'>('all')
const selectedRunId = ref('live')
const selectedRun = ref<DeepReadonly<RunSummary>>()
const previous = ref<DeepReadonly<StatisticsTelemetry>>()
const viewingHistorical = computed(() => selectedRunId.value !== 'live')
const displayedStatistics = computed(() =>
  viewingHistorical.value ? selectedRun.value?.finalStatistics : props.statistics,
)

watch(displayedStatistics, (next, old) => {
  if (!viewingHistorical.value && next && old && next.elapsedMilliseconds > old.elapsedMilliseconds)
    previous.value = old
  else if (!next || !old || next.elapsedMilliseconds < old.elapsedMilliseconds)
    previous.value = undefined
})

function selectHistoricalRun(run: DeepReadonly<RunSummary>) {
  selectedRun.value = run
  selectedRunId.value = run.runId
}

function selectLive() {
  selectedRun.value = undefined
  selectedRunId.value = 'live'
}

watch(
  () => props.liveRunId,
  (next, previous) => {
    if (next && next !== previous) selectLive()
  },
)

const boards = computed(() => displayedStatistics.value?.boards ?? [])
function boardKey(board: DeepReadonly<BoardStatistics>) {
  return `${board.chain}:${board.node}`
}

function boardLabel(board: DeepReadonly<BoardStatistics>) {
  return board.logicalIndex === undefined
    ? `Chain ${board.chain} · Node ${board.node}`
    : `Board ${board.logicalIndex}`
}

const active = computed(() =>
  selectedBoard.value === 'all'
    ? undefined
    : boards.value.find((board) => boardKey(board) === selectedBoard.value),
)
watch(
  boards,
  (next) => {
    if (
      selectedBoard.value !== 'all' &&
      !next.some((board) => boardKey(board) === selectedBoard.value)
    )
      selectedBoard.value = 'all'
  },
  { immediate: true },
)

function prior(board: DeepReadonly<BoardStatistics>) {
  return previous.value?.boards.find(
    (candidate) => candidate.chain === board.chain && candidate.node === board.node,
  )
}

function elapsedSeconds() {
  const current = Number(displayedStatistics.value?.elapsedMilliseconds ?? 0n)
  const baseline =
    integral.value || viewingHistorical.value
      ? 0
      : Number(previous.value?.elapsedMilliseconds ?? current)
  return Math.max((current - baseline) / 1000, 0)
}

function difference(value: bigint, before: bigint | undefined) {
  if (integral.value || viewingHistorical.value) return value
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

function boardRate(
  board: DeepReadonly<BoardStatistics>,
  field: 'triggerCount' | 'dataBytes' | 'tOrCount',
) {
  const seconds = elapsedSeconds()
  if (seconds <= 0) return '—'
  const value = difference(board[field], prior(board)?.[field])
  const rate =
    field === 'dataBytes' ? `${bytes(value)}/s` : `${(Number(value) / seconds).toFixed(1)} Hz`
  if (!viewingHistorical.value) return rate
  const total = field === 'dataBytes' ? bytes(value) : compact(value)
  return `${rate} · ${total} total`
}

function timestampSeconds(timestamp: bigint) {
  return `${(Number(timestamp) * 8e-9).toFixed(3)} s`
}

function lostTriggerCount(board: DeepReadonly<BoardStatistics>) {
  return difference(board.lostTriggerCount, prior(board)?.lostTriggerCount)
}

function lostTriggerPercent(board: DeepReadonly<BoardStatistics>) {
  const lost = lostTriggerCount(board)
  const received = difference(board.triggerCount, prior(board)?.triggerCount)
  return percent(lost, received + lost)
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

const metricDescription = computed(
  () =>
    ({
      channelTriggerCounts: 'Discriminator firings reported for each detector channel.',
      timestampCounts: 'Events carrying timing information for each detector channel.',
      phaCounts: 'Pulse-height measurements decoded for each detector channel.',
    })[metric.value],
)
</script>

<template>
  <section class="statistics panel" aria-labelledby="statistics-heading">
    <div class="section-title statistics-title">
      <div>
        <p class="eyebrow">{{ viewingHistorical ? 'Final run snapshot' : 'Live runtime view' }}</p>
        <h2 id="statistics-heading">Statistics</h2>
      </div>
      <button
        type="button"
        class="statistics-live-source"
        :class="{ selected: !viewingHistorical }"
        :aria-pressed="!viewingHistorical"
        @click="selectLive"
      >
        <span>Live source</span>
        <strong>{{ liveRunId ? `Run ${liveRunId}` : 'No active run' }}</strong>
      </button>
    </div>
    <p class="statistics-controls-hint">
      {{
        selectedBoard === 'all'
          ? 'Select a board for per-channel metrics.'
          : `Viewing ${active ? boardLabel(active) : 'selected board'} channels`
      }}
    </p>

    <div class="statistics-overview-row">
      <div v-if="!viewingHistorical" class="statistics-summary" aria-label="Global statistics">
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
          ><strong
            >{{
              (Number(displayedStatistics?.elapsedMilliseconds ?? 0n) / 1000).toFixed(1)
            }}
            s</strong
          >
          elapsed</span
        >
      </div>
      <div v-else class="statistics-summary" aria-label="Historical run statistics">
        <span class="statistics-run-identity"
          ><strong>Run {{ selectedRun?.runId }}</strong> finalized snapshot</span
        >
        <span
          ><strong>{{ compact(selectedRun?.eventCount) }}</strong> decoded events</span
        >
        <span
          ><strong
            >{{
              (Number(displayedStatistics?.elapsedMilliseconds ?? 0n) / 1000).toFixed(1)
            }}
            s</strong
          >
          elapsed</span
        >
      </div>

      <div v-if="selectedBoard !== 'all'" class="statistics-channel-toolbar">
        <label>
          Per-channel metric
          <select v-model="metric">
            <option value="channelTriggerCounts">Channel trigger</option>
            <option value="timestampCounts">Timestamp</option>
            <option value="phaCounts">PHA</option>
          </select>
        </label>
        <label class="switch compact-switch">
          <input v-model="integral" type="checkbox" />
          <span>Cumulative counts</span>
        </label>
        <div class="statistics-metric-description">
          <strong>{{ metricLabel }}</strong>
          <span>{{ metricDescription }}</span>
        </div>
      </div>
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
        :key="boardKey(board)"
        type="button"
        role="tab"
        :aria-selected="selectedBoard === boardKey(board)"
        @click="selectedBoard = boardKey(board)"
      >
        {{ boardLabel(board) }}
      </button>
    </div>

    <div v-if="selectedBoard === 'all'" class="statistics-table-wrap">
      <table class="statistics-table">
        <thead>
          <tr>
            <th>Board</th>
            <th>Timestamp</th>
            <th>Trigger ID</th>
            <th>Received event rate</th>
            <th>Estimated lost triggers</th>
            <th>T-OR rate</th>
            <th>Decoded payload rate</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="board in boards" :key="`${board.chain}-${board.node}`">
            <th>{{ boardLabel(board) }}</th>
            <td>{{ timestampSeconds(board.timestamp) }}</td>
            <td>{{ compact(board.triggerId) }}</td>
            <td>{{ boardRate(board, 'triggerCount') }}</td>
            <td>{{ compact(lostTriggerCount(board)) }} · {{ lostTriggerPercent(board) }}</td>
            <td>{{ boardRate(board, 'tOrCount') }}</td>
            <td>{{ boardRate(board, 'dataBytes') }}</td>
          </tr>
          <tr v-if="!boards.length">
            <td colspan="7" class="empty">
              {{
                viewingHistorical
                  ? 'No final statistics are available for this run.'
                  : 'Statistics become available while a run is active.'
              }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div
      v-else-if="active"
      class="channel-statistics"
      :aria-label="`${boardLabel(active)} channel statistics`"
    >
      <div v-for="channel in 64" :key="channel - 1" class="channel-statistic">
        <span>CH {{ channel - 1 }}</span>
        <strong>{{ channelValue(active, channel - 1) }}</strong>
      </div>
      <p class="statistics-caption">
        {{ metricLabel }}
        {{
          integral
            ? 'integrated count'
            : viewingHistorical
              ? 'average rate over the completed run'
              : 'rate over the latest telemetry interval'
        }}
      </p>
    </div>

    <RunDataPicker
      :api="api"
      capability="statistics"
      :selected-run-id="viewingHistorical ? selectedRunId : undefined"
      :active-run-id="liveRunId"
      :enabled="runPickerEnabled"
      @select="selectHistoricalRun"
    />

    <details class="statistics-guide">
      <summary>How to read and configure this panel</summary>
      <dl>
        <div>
          <dt>All boards</dt>
          <dd>
            Compares board-level throughput, latest hardware timestamp, trigger continuity, T-OR
            activity, and decoded payload rate.
          </dd>
        </div>
        <div>
          <dt>Channel trigger</dt>
          <dd>Counts channel discriminator firings, before requiring a PHA or timestamp value.</dd>
        </div>
        <div>
          <dt>Timestamp</dt>
          <dd>Counts channel events that include timing information.</dd>
        </div>
        <div>
          <dt>PHA</dt>
          <dd>Counts decoded pulse-height measurements used for energy spectra.</dd>
        </div>
        <div>
          <dt>Interval mode</dt>
          <dd>
            Live data shows rates calculated between the two latest telemetry snapshots. Historical
            board-level rates are averages over the completed run.
          </dd>
        </div>
        <div>
          <dt>Cumulative counts</dt>
          <dd>
            Shows totals accumulated since the selected run started. Turn it off to show historical
            channel averages over the completed run.
          </dd>
        </div>
      </dl>
    </details>
  </section>
</template>
