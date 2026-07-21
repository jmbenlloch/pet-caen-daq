<script setup lang="ts">
import { computed } from 'vue'
import {
  formatNumericValue,
  numericError,
  parseNumericValue,
  type ConfigurationField,
  type NumericConstraint,
} from './configuration'

const props = defineProps<{ field: ConfigurationField; constraint: NumericConstraint }>()
const emit = defineEmits<{ change: [value: string] }>()
const parsed = computed(() => parseNumericValue(props.field.value) ?? { number: 0, unit: '' })
const error = computed(() => numericError(props.field))

function set(value: number) {
  const bounded = Math.min(
    props.constraint.max ?? Number.POSITIVE_INFINITY,
    Math.max(props.constraint.min ?? Number.NEGATIVE_INFINITY, value),
  )
  emit('change', formatNumericValue(bounded, parsed.value.unit))
}
</script>

<template>
  <div class="numeric-control">
    <div class="stepper" :class="{ invalid: error }">
      <button
        type="button"
        :aria-label="`Decrease ${field.name}`"
        @click="set(parsed.number - constraint.step)"
      >
        −
      </button>
      <input
        :id="field.id"
        type="number"
        :value="parsed.number"
        :min="constraint.min"
        :max="constraint.max"
        :step="constraint.step"
        :aria-invalid="Boolean(error)"
        :aria-describedby="error ? `${field.id}-error` : undefined"
        @input="set(Number(($event.target as HTMLInputElement).value))"
      />
      <span v-if="parsed.unit" class="unit">{{ parsed.unit }}</span>
      <button
        type="button"
        :aria-label="`Increase ${field.name}`"
        @click="set(parsed.number + constraint.step)"
      >
        +
      </button>
    </div>
    <span v-if="error" :id="`${field.id}-error`" class="field-error">{{ error }}</span>
    <span v-else class="field-range">
      <template v-if="constraint.min !== undefined && constraint.max !== undefined"
        >{{ constraint.min }}–{{ constraint.max }}</template
      >
      <template v-else-if="constraint.min !== undefined">≥ {{ constraint.min }}</template>
      · step {{ constraint.step }}{{ parsed.unit ? ` ${parsed.unit}` : '' }}
    </span>
  </div>
</template>
