import { expect, test } from '@playwright/test'

test('operator saves exactly N non-service events from every simulated card', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: /Acquisition/ }).click()
  await page.getByRole('tab', { name: 'RunCtrl', exact: true }).click()
  await page
    .getByRole('combobox', { name: 'StopRunMode', exact: true })
    .selectOption('PRESET_COUNTS_PER_BOARD')
  await page.getByRole('spinbutton', { name: 'PresetCounts', exact: true }).fill('30')
  await page.locator('#persist-histograms').uncheck()
  await expect(page.getByText('Automatic stop after 30 non-service events per card')).toBeVisible()
  const started = page.waitForResponse((response) =>
    response.url().endsWith('.RunService/StartRun'),
  )
  await page.getByRole('button', { name: 'Start run', exact: true }).click()
  const response = await started
  expect(response.ok()).toBeTruthy()
  const { run } = await response.json()
  await expect(page.getByRole('heading', { name: 'Ready', exact: true })).toBeVisible({
    timeout: 30_000,
  })
  const snapshotResponse = await page.request.post(
    '/pet.caen.daq.v1.SystemService/GetSystemSnapshot',
    { data: {} },
  )
  const { snapshot } = await snapshotResponse.json()
  const completed = snapshot.latestCompletedRun
  expect(completed.runId).toBe(run.runId)
  expect(completed.terminationReason).toBe('preset_counts_per_board')
  expect(completed.presetEventCount).toBe('30')
  expect(completed.finalStatistics.boards).toHaveLength(4)
  for (const board of completed.finalStatistics.boards) expect(board.triggerCount).toBe('30')
})
