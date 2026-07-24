import { onBeforeUnmount, onMounted } from 'vue'

export function useEscapeClose(close: () => void) {
  function closeOnEscape(event: KeyboardEvent) {
    if (event.key === 'Escape') close()
  }

  onMounted(() => window.addEventListener('keydown', closeOnEscape))
  onBeforeUnmount(() => window.removeEventListener('keydown', closeOnEscape))
}
