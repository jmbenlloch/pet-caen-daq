import { expect, test } from '@playwright/test'

test('assets, RPCs and telemetry stay under the DAQ mount path', async ({ page }) => {
  const paths: string[] = []
  // Emulate the ingress stripping /daq before forwarding to the simulator-backed
  // server. Reject root requests so an escaped API or asset URL cannot pass.
  await page.route('http://127.0.0.1:18080/**', async (route) => {
    const url = new URL(route.request().url())
    paths.push(url.pathname)
    if (!url.pathname.startsWith('/daq/')) {
      await route.abort()
      return
    }
    url.pathname = url.pathname.slice('/daq'.length)
    await route.continue({ url: url.href })
  })
  await page.goto('/daq/')
  await expect(page.getByRole('heading', { name: 'Ready', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: /Scans/ }).click()
  await expect(page.getByRole('region', { name: 'Source XY scan' })).toBeVisible()
  await expect.poll(() => paths.some((path) => path.endsWith('/GetSourceScan'))).toBe(true)
  expect(paths.some((path) => path.endsWith('/StreamTelemetry'))).toBe(true)
  expect(paths.some((path) => path.startsWith('/daq/assets/') && path.endsWith('.js'))).toBe(true)
  expect(paths.some((path) => path.startsWith('/daq/assets/') && path.endsWith('.css'))).toBe(true)
  expect(paths.every((path) => path.startsWith('/daq/'))).toBe(true)
})
