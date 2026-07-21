import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  outputDir: '/tmp/pet-caen-playwright-results',
  fullyParallel: false,
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI
    ? [['line'], ['html', { outputFolder: '/tmp/pet-caen-playwright-report', open: 'never' }]]
    : 'line',
  use: {
    baseURL: 'http://127.0.0.1:18080',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: './e2e/server.sh',
    url: 'http://127.0.0.1:18080',
    reuseExistingServer: false,
    timeout: 30_000,
  },
})
