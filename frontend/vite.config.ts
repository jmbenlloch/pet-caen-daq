import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  server: {
    proxy: {
      '/pet.caen.daq.v1': {
        target: process.env.DAQ_API_URL ?? 'http://127.0.0.1:8080',
      },
    },
  },
  test: {
    environment: 'jsdom',
  },
})
