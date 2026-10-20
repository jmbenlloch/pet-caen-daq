import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'

test('source grid explains invalid bounds and pitch before allowing start', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: /Scans/ }).click()
  const source = page.getByRole('region', { name: 'Source XY scan' })
  await source.getByRole('button', { name: 'Expand', exact: true }).click()
  for (const axis of ['X', 'Y']) {
    await source.getByLabel(`${axis} minimum (mm)`, { exact: true }).fill('-5.5')
    await source.getByLabel(`${axis} maximum (mm)`, { exact: true }).fill('5.5')
  }
  await source.getByRole('checkbox', { name: /I placed the source/ }).check()
  const reasons = source.locator('#source-start-reasons')
  await expect(reasons).toContainText('X span is 11 mm')
  await expect(reasons).toContainText('Y span is 11 mm')
  await expect(reasons).toContainText('7.333333 intervals, not a whole number')
  const start = source.getByRole('button', { name: 'Start source scan', exact: true })
  await expect(start).toBeDisabled()
  await source.getByLabel('Grid pitch (mm)').fill('1')
  await expect(source.getByText(/144 positions, serpentine/)).toBeVisible()
  await expect(start).toBeEnabled()
  await source.getByLabel('Point timeout (s)').fill('1')
  await expect(reasons).toContainText('Point timeout must exceed')
  await expect(start).toBeDisabled()
})

test('source scan matches collapsible scan cards and aligns its controls', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 1100 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Switch to light theme' }).click()
  await page.getByRole('tab', { name: /Scans/ }).click()
  const source = page.getByRole('region', { name: 'Source XY scan' })
  const expand = source.getByRole('button', { name: 'Expand', exact: true })
  await expect(expand).toHaveAttribute('aria-expanded', 'false')
  await expect(source.getByLabel('X minimum (mm)', { exact: true })).toBeHidden()
  await expand.click()
  for (const name of ['Grid pitch (mm)', 'Travel per turn (mm)']) {
    const input = source.getByLabel(name)
    await input.fill('2')
    await input.press('ArrowUp')
    await expect(input).toHaveValue('2.01')
    await input.press('ArrowDown')
    await expect(input).toHaveValue('2')
  }
  const fullSteps = await source.getByLabel('Full steps per turn').boundingBox()
  const microsteps = await source.getByLabel('Microsteps (driver DIP setting)').boundingBox()
  expect(Math.abs(fullSteps!.y - microsteps!.y)).toBeLessThan(1)
  const direction = source.locator('.source-directions label').first()
  const checkbox = await direction.getByRole('checkbox').boundingBox()
  const text = await direction.locator('span').boundingBox()
  expect(Math.abs(checkbox!.y + checkbox!.height / 2 - text!.y - text!.height / 2)).toBeLessThan(1)
  await source.getByLabel('X minimum (mm)', { exact: true }).fill('-12')
  await source.getByRole('button', { name: 'Collapse', exact: true }).click()
  await expect(source.getByLabel('X minimum (mm)', { exact: true })).toBeHidden()
  await expand.click()
  await expect(source.getByLabel('X minimum (mm)', { exact: true })).toHaveValue('-12')
  await source.screenshot({ path: '/tmp/pet-source-scan-desktop.png' })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await source.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true)
  await source.screenshot({ path: '/tmp/pet-source-scan-mobile.png' })
})

test('event-limited source acquisition stops automatically with persisted event counts', async ({
  page,
}) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: /Acquisition/ }).click()
  await page.getByRole('tab', { name: 'RunCtrl', exact: true }).click()
  await page.locator('#persist-histograms').uncheck()
  await page.getByRole('tab', { name: /Scans/ }).click()
  const source = page.getByRole('region', { name: 'Source XY scan' })
  await source.getByRole('button', { name: 'Expand', exact: true }).click()
  for (const label of ['X minimum (mm)', 'X maximum (mm)', 'Y minimum (mm)', 'Y maximum (mm)']) {
    await source.getByLabel(label, { exact: true }).fill('0')
  }
  await source.getByLabel('Stop each point after').selectOption('events')
  await source.getByLabel('Events per point').fill('10')
  await source.getByRole('checkbox', { name: /I placed the source/ }).check()
  await source.getByRole('button', { name: 'Start source scan', exact: true }).click()
  await expect(source.getByText(/1\/1 completed/)).toBeVisible({ timeout: 20_000 })
  await source.getByText('Position and run metadata', { exact: true }).click()
  const runId = (await source.locator('tbody tr td:nth-child(5)').textContent())!.trim()
  const manifest = JSON.parse(
    await readFile(`/tmp/pet-caen-playwright-runs/run-${runId}/manifest.json`, 'utf8'),
  )
  expect(BigInt(manifest.event_count)).toBeGreaterThanOrEqual(10n)
  expect(manifest.termination_reason).toBe('source_scan_events')
  expect(JSON.parse(manifest.source_position.configuration_json).eventCount).toBe('10')
})

test('source raster acquires independent runs with positions using a separate stage process', async ({
  page,
}) => {
  test.setTimeout(90_000)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: /Acquisition/ }).click()
  await page.getByRole('tab', { name: 'RunCtrl', exact: true }).click()
  await page.locator('#persist-histograms').uncheck()
  await page.getByRole('tab', { name: /Scans/ }).click()
  const source = page.getByRole('region', { name: 'Source XY scan' })
  await source.getByRole('button', { name: 'Expand', exact: true }).click()
  await source.getByLabel('X minimum (mm)', { exact: true }).fill('0')
  await source.getByLabel('X maximum (mm)', { exact: true }).fill('0.5')
  await source.getByLabel('Y minimum (mm)', { exact: true }).fill('0')
  await source.getByLabel('Y maximum (mm)', { exact: true }).fill('0.5')
  await source.getByLabel('Grid pitch (mm)').fill('0.5')
  await source.getByLabel('Acquisition time (s)').fill('0.1')
  await source.getByLabel('Settling time (ms)').fill('0')
  await source.getByRole('checkbox', { name: /I placed the source/ }).check()
  await source.getByRole('button', { name: 'Start source scan', exact: true }).click()
  await expect(source.getByText(/4\/4 completed/)).toBeVisible({ timeout: 60_000 })
  await source.getByText('Position and run metadata', { exact: true }).click()
  const rows = source.locator('tbody tr')
  await expect(rows).toHaveCount(4)
  const runIds = await rows.locator('td:nth-child(5)').allTextContents()
  expect(new Set(runIds).size).toBe(4)
  const coordinates = [
    [0, 200],
    [200, 200],
    [200, 0],
    [0, 0],
  ]
  for (const [i, runId] of runIds.entries()) {
    const manifest = JSON.parse(
      await readFile(`/tmp/pet-caen-playwright-runs/run-${runId.trim()}/manifest.json`, 'utf8'),
    )
    expect(manifest.source_position.x_steps).toBe(coordinates[i][0])
    expect(manifest.source_position.y_steps).toBe(coordinates[i][1])
    expect(manifest.source_position.index).toBe(i)
    expect(manifest.source_position.configuration_json).toContain('mmPerTurn')
    expect(manifest.termination_reason).toBe('source_scan_time')
    expect(manifest.completed_at).toBeTruthy()
  }
  // A fresh browser reads the current/completed scan from the backend.
  await page.reload()
  await page.getByRole('tab', { name: /Scans/ }).click()
  await source.getByRole('button', { name: 'Expand', exact: true }).click()
  await expect(source.getByText(/4\/4 completed/)).toBeVisible()
  await expect(source.getByLabel('Load stored source scan by ID')).toHaveCount(0)
  const history = source.getByLabel('Source scan history', { exact: true })
  await expect(history.getByRole('button').first()).toContainText(/Source scan \d+/)
  await history.getByRole('button').first().click()
  await expect(history.getByRole('button').first()).toHaveAttribute('aria-pressed', 'true')
  await expect(source.getByText(/4\/4 completed/)).toBeVisible()
})

test('Apply config stays disabled between XY point runs until cancellation', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: /Scans/ }).click()
  const source = page.getByRole('region', { name: 'Source XY scan' })
  await source.getByRole('button', { name: 'Expand', exact: true }).click()
  for (const label of ['X minimum (mm)', 'X maximum (mm)', 'Y minimum (mm)', 'Y maximum (mm)']) {
    await source.getByLabel(label, { exact: true }).fill('0')
  }
  await source.getByLabel('Settling time (ms)').fill('10000')
  await source.getByRole('checkbox', { name: /I placed the source/ }).check()
  await source.getByRole('button', { name: 'Start source scan', exact: true }).click()
  try {
    await expect(
      source.locator('[aria-live="polite"]').getByText('settling', { exact: true }),
    ).toBeVisible()
    await page.getByRole('tab', { name: /Acquisition/ }).click()
    await page.getByRole('tab', { name: 'RunCtrl', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Ready', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Apply config', exact: true })).toBeDisabled()
  } finally {
    await page.getByRole('tab', { name: /Scans/ }).click()
    await source.getByRole('button', { name: 'Cancel source scan', exact: true }).click()
    await expect(
      source.locator('[aria-live="polite"]').getByText('cancelled', { exact: true }),
    ).toBeVisible()
  }
  await page.getByRole('tab', { name: /Acquisition/ }).click()
  await expect(page.getByRole('button', { name: 'Apply config', exact: true })).toBeEnabled()
})

test('operator cancels an event-limited source scan and the DAQ drains', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: /Acquisition/ }).click()
  await page.getByRole('tab', { name: 'RunCtrl', exact: true }).click()
  await page.locator('#persist-histograms').uncheck()
  await page.getByRole('tab', { name: /Scans/ }).click()
  const source = page.getByRole('region', { name: 'Source XY scan' })
  await source.getByRole('button', { name: 'Expand', exact: true }).click()
  for (const label of ['X minimum (mm)', 'X maximum (mm)', 'Y minimum (mm)', 'Y maximum (mm)']) {
    await source.getByLabel(label, { exact: true }).fill('0')
  }
  await source.getByLabel('Stop each point after').selectOption('events')
  await source.getByLabel('Events per point').fill('1000000000')
  await source.getByRole('checkbox', { name: /I placed the source/ }).check()
  await source.getByRole('button', { name: 'Start source scan', exact: true }).click()
  await expect(
    source.locator('[aria-live="polite"]').getByText('acquiring', { exact: true }),
  ).toBeVisible({ timeout: 20_000 })
  const previous = source
    .getByLabel('Source scan history', { exact: true })
    .getByRole('button')
    .filter({ hasText: 'completed' })
    .first()
  await previous.click()
  await expect(source.getByRole('button', { name: 'Show active scan' })).toBeVisible()
  await expect(
    source.getByRole('button', { name: 'Start source scan', exact: true }),
  ).toBeDisabled()
  await expect(
    source.getByRole('button', { name: 'Cancel source scan', exact: true }),
  ).toBeEnabled()
  await source.getByRole('button', { name: 'Show active scan' }).click()
  await source.getByRole('button', { name: 'Cancel source scan', exact: true }).click()
  await expect(
    source.locator('[aria-live="polite"]').getByText('cancelled', { exact: true }),
  ).toBeVisible({ timeout: 15_000 })
  await expect(page.getByRole('heading', { name: 'Ready', exact: true })).toBeVisible()
})
