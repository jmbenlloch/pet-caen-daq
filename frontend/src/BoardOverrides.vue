<script setup lang="ts">
import { ref } from 'vue'
import type { ConfigurationField, NumericConstraint } from './configuration'

const props = defineProps<{
  field: ConfigurationField
  constraint: NumericConstraint
  overrides: Record<number, string>
}>()
defineEmits<{ apply: [values: Record<number, string>]; close: [] }>()
const values = ref<Record<number, string>>({ ...props.overrides })

function update(board: number, value: string) {
  const next = { ...values.value }
  if (value === '') delete next[board]
  else next[board] = value
  values.value = next
}
</script>

<template>
  <div class="modal-backdrop" @click.self="$emit('close')">
    <section
      class="board-dialog panel"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="`${field.id}-boards-heading`"
    >
      <div class="mask-heading">
        <div>
          <p class="eyebrow">Per-board exceptions</p>
          <h2 :id="`${field.id}-boards-heading`">{{ field.name }}</h2>
        </div>
      </div>
      <p class="channel-help">
        Global value: <strong>{{ field.value }}</strong
        >. Leave a board blank to inherit it.
      </p>
      <div class="board-values">
        <label v-for="board in 4" :key="board - 1">
          <span>Board {{ board - 1 }}</span>
          <input
            type="number"
            :aria-label="`${field.name} board ${board - 1}`"
            :placeholder="field.value"
            :value="values[board - 1] ?? ''"
            :min="constraint.min"
            :max="constraint.max"
            :step="constraint.step"
            @input="update(board - 1, ($event.target as HTMLInputElement).value)"
          />
        </label>
      </div>
      <div class="mask-footer">
        <button type="button" class="secondary" @click="values = {}">Clear overrides</button>
        <button type="button" class="secondary" @click="$emit('close')">Cancel</button>
        <button type="button" class="primary" @click="$emit('apply', values)">
          Apply overrides
        </button>
      </div>
    </section>
  </div>
</template>
