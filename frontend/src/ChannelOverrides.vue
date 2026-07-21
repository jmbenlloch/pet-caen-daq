<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { parseNumericValue, type ConfigurationField, type NumericConstraint } from './configuration'

const props = defineProps<{
  field: ConfigurationField
  constraint: NumericConstraint
  overrides: Record<number, Record<number, string>>
  nominalBias?: Record<number, number>
  adjustmentRange?: string
}>()
defineEmits<{ apply: [board: number, values: Record<number, string>]; close: [] }>()
const board = ref(0)
const values = ref<Record<number, string>>({})
const general = computed(() => parseNumericValue(props.field.value)?.number ?? 0)

function loadBoard() {
  values.value = { ...(props.overrides[board.value] ?? {}) }
}
watch(board, loadBoard, { immediate: true })

function update(channel: number, value: string) {
  const next = { ...values.value }
  if (value === '') delete next[channel]
  else next[channel] = value
  values.value = next
}

function nominalVoltage(channel: number) {
  if (!props.nominalBias || !props.adjustmentRange) return undefined
  const fullScale =
    props.adjustmentRange === '4.5' ? 4.2 : props.adjustmentRange === '2.5' ? 2.5 : 0
  const adjustment = Number(values.value[channel] ?? general.value)
  return props.nominalBias[board.value] - (fullScale * (255 - adjustment)) / 255
}
</script>

<template>
  <div class="modal-backdrop" @click.self="$emit('close')">
    <section
      class="channel-dialog panel"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="`${field.id}-channels-heading`"
    >
      <div class="mask-heading">
        <div>
          <p class="eyebrow">Per-channel exceptions</p>
          <h2 :id="`${field.id}-channels-heading`">{{ field.name }}</h2>
        </div>
        <label class="board-select"
          >Board
          <select v-model.number="board">
            <option v-for="index in 4" :key="index - 1" :value="index - 1">{{ index - 1 }}</option>
          </select>
        </label>
      </div>
      <p class="channel-help">
        General value: <strong>{{ field.value }}</strong
        >. Leave a channel blank to inherit it.
      </p>
      <div class="channel-values">
        <label v-for="channel in 64" :key="channel - 1">
          <span>Ch {{ channel - 1 }}</span>
          <input
            type="number"
            :aria-label="`${field.name} board ${board} channel ${channel - 1}`"
            :placeholder="String(general)"
            :value="values[channel - 1] ?? ''"
            :min="constraint.min"
            :max="constraint.max"
            :step="constraint.step"
            @input="update(channel - 1, ($event.target as HTMLInputElement).value)"
          />
          <small v-if="nominalVoltage(channel - 1) !== undefined">
            Vnom {{ nominalVoltage(channel - 1)?.toFixed(2) }} V
          </small>
        </label>
      </div>
      <div class="mask-footer">
        <button type="button" class="secondary" @click="values = {}">Clear board overrides</button>
        <button type="button" class="secondary" @click="$emit('close')">Cancel</button>
        <button type="button" class="primary" @click="$emit('apply', board, values)">
          Apply overrides
        </button>
      </div>
    </section>
  </div>
</template>
