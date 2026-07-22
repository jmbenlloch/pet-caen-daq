import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  server: {
    fs: {
      // The bundled default is the repository's canonical JANUS fixture, so
      // frontend builds and tests consume the same bytes as backend tests.
      allow: ['..'],
    },
    proxy: {
      '/pet.caen.daq.v1': {
        target: process.env.DAQ_API_URL ?? 'http://127.0.0.1:8080',
      },
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
    setupFiles: ['./src/test-setup.ts'],
  },
})
