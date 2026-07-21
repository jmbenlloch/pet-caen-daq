<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { maskBits, masksFromBits } from './configuration'

const props = defineProps<{ title: string; low: string; high: string }>()
const emit = defineEmits<{ apply: [low: string, high: string]; close: [] }>()
const bits = ref(maskBits(props.low, props.high))
watch(
  () => [props.low, props.high],
  () => (bits.value = maskBits(props.low, props.high)),
)
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
  emit('apply', ...masksFromBits(bits.value))
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
        <strong>{{ enabled }} enabled</strong>
      </div>
      <div class="mask-actions">
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
