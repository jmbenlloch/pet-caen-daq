<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { maskBits, masksFromBits } from './configuration'

export interface MaskVariant {
  target: string
  label: string
  low: string
  high: string
  inherited: boolean
}
const props = defineProps<{ title: string; variants: MaskVariant[] }>()
const emit = defineEmits<{ apply: [target: string, low: string, high: string]; close: [] }>()
const target = ref(props.variants[0]?.target ?? 'global')
const selected = computed(
  () => props.variants.find((variant) => variant.target === target.value) ?? props.variants[0],
)
const bits = ref(maskBits(selected.value.low, selected.value.high))
watch(target, () => (bits.value = maskBits(selected.value.low, selected.value.high)))
const enabled = computed(() => bits.value.filter(Boolean).length)

function setAll(value: boolean) {
  bits.value = Array.from({ length: 64 }, () => value)
}
function toggle(channel: number) {
  const next = [...bits.value]
  next[channel] = !next[channel]
  bits.value = next
}
function apply() {
  emit('apply', target.value, ...masksFromBits(bits.value))
}
</script>

<template>
  <div class="modal-backdrop" @click.self="$emit('close')">
    <section
      class="mask-dialog panel"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="`${title}-heading`"
    >
      <div class="mask-heading">
        <div>
          <p class="eyebrow">64 detector channels</p>
          <h2 :id="`${title}-heading`">{{ title }}</h2>
        </div>
        <label class="board-select"
          >Target
          <select v-model="target">
            <option v-for="variant in variants" :key="variant.target" :value="variant.target">
              {{ variant.label }}
            </option>
          </select>
        </label>
      </div>
      <div class="mask-actions">
        <span>{{ enabled }} enabled<span v-if="selected.inherited"> · inherited</span></span>
        <button type="button" @click="setAll(true)">Enable all</button>
        <button type="button" @click="setAll(false)">Disable all</button>
        <button type="button" @click="bits = bits.map((value) => !value)">Invert</button>
      </div>
      <div class="channel-grid" aria-label="Channel mask">
        <button
          v-for="(active, channel) in bits"
          :key="channel"
          type="button"
          :class="{ active }"
          :aria-pressed="active"
          :aria-label="`Channel ${channel}`"
          @click="toggle(channel)"
        >
          {{ channel }}
        </button>
      </div>
      <div class="mask-footer">
        <code>{{ masksFromBits(bits)[0] }} · {{ masksFromBits(bits)[1] }}</code>
        <button type="button" class="secondary" @click="$emit('close')">Cancel</button>
        <button type="button" class="primary" @click="apply">Apply mask</button>
      </div>
    </section>
  </div>
</template>
