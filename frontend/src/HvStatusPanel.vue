<script setup lang="ts">
import { computed } from 'vue'
import { hvState, type HvState } from './hvStatus'

type HvBoard = {
  logicalIndex: number
  chain: number
  node: number
  hvVoltageV: number
  hvTargetVoltageV: number
  hvOn: boolean
  hvRamping: boolean
  hvOverCurrent: boolean
  hvOverVoltage: boolean
}

const props = defineProps<{ boards: readonly HvBoard[] }>()

const stateLabel: Record<HvState, string> = {
  off: 'Off',
  on: 'On',
  ramping: 'Ramping',
  fault: 'Fault',
}

const globalState = computed<HvState>(() => {
  const states = props.boards.map(hvState)
  if (states.includes('fault')) return 'fault'
  if (states.includes('ramping')) return 'ramping'
  if (states.includes('on')) return 'on'
  return 'off'
})

const globalLabel = computed(() => {
  if (globalState.value === 'fault') return 'Fault'
  if (globalState.value === 'ramping') return 'Ramping'
  const enabled = props.boards.filter((board) => hvState(board) === 'on').length
  if (!enabled) return 'Off'
  if (enabled === props.boards.length) return 'On'
  return `${enabled}/${props.boards.length} on`
})
</script>

<template>
  <section class="hv-monitor" aria-label="SiPM high-voltage status">
    <div class="hv-monitor-summary">
      <span>SiPM HV</span>
      <strong :class="globalState" :aria-label="`HV summary: ${globalLabel}`">
        {{ globalLabel }}
      </strong>
    </div>
    <div v-if="boards.length" class="hv-led-strip" aria-label="High-voltage status by board">
      <span
        v-for="board in boards"
        :key="`${board.chain}-${board.node}`"
        class="hv-board-led"
        :class="hvState(board)"
        :aria-label="`Board ${board.logicalIndex}, chain ${board.chain} node ${board.node} HV: ${stateLabel[hvState(board)]}`"
        :title="`Board ${board.logicalIndex} · Chain ${board.chain} · Node ${board.node}: ${stateLabel[hvState(board)]}`"
      >
        <span class="hv-led" aria-hidden="true" />
        <span>B{{ board.logicalIndex }}</span>
      </span>
    </div>
    <span v-else class="hv-unavailable">Waiting…</span>
  </section>
</template>
