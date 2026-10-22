<script setup lang="ts">
import { computed } from 'vue'
import type { ConfigurationField } from './configuration'
import { boardChannelVoltages } from './hvBias'

const props = defineProps<{ fields: ConfigurationField[]; boards: number[] }>()
defineEmits<{ inspect: [] }>()
const summaries = computed(() =>
  props.boards.map((board) => {
    const voltages = boardChannelVoltages(props.fields, board)
    if (voltages.some((value) => value === undefined)) return { board, text: 'Unavailable' }
    const values = voltages as number[]
    const minimum = Math.min(...values)
    const maximum = Math.max(...values)
    return {
      board,
      text:
        minimum === maximum
          ? `${minimum.toFixed(2)} V`
          : `${minimum.toFixed(2)}–${maximum.toFixed(2)} V`,
    }
  }),
)
</script>

<template>
  <div class="board-value-summary hv-bias-summary" aria-label="Estimated channel bias by board">
    <p>Estimated channel bias (Vnom)</p>
    <span
      v-for="item in summaries"
      :key="item.board"
      :aria-label="`B${item.board} estimated channel bias`"
    >
      <strong>B{{ item.board }}</strong> {{ item.text }}
    </span>
    <p class="field-range">
      Calculated from bias and channel DAC; not Vmon. Ranges show differing channels.
    </p>
    <button type="button" class="board-overrides-button secondary" @click="$emit('inspect')">
      View channel voltages
    </button>
  </div>
</template>
