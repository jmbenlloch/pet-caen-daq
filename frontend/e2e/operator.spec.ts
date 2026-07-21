import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'

test('operator completes a simulated run and downloads its persisted artifact', async ({
  page,
}) => {
  const runId = `browser-${Date.now()}`

  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
  await expect(page.getByText('DT5202 · node 0')).toHaveCount(4)
  await expect(page.getByText('Live telemetry')).toBeVisible()
  await expect(page.getByLabel('Configuration parameters')).toBeVisible()
  await page.getByLabel('Find a parameter').fill('PresetTime')
  await page.getByRole('spinbutton', { name: 'PresetTime', exact: true }).fill('30')

  await page.getByLabel('Run ID').fill(runId)
  await page.getByRole('button', { name: 'Start run' }).click()
  await expect(page.getByRole('heading', { name: 'Running' })).toBeVisible()
  await expect(page.getByText(runId, { exact: true })).toBeVisible()
  const statistics = page.getByRole('region', { name: 'Statistics' })
  await expect(statistics.getByRole('tab', { name: 'Board 0' })).toBeVisible()
  await statistics.getByRole('tab', { name: 'Board 0' }).click()
  await expect(statistics.locator('.channel-statistic')).toHaveCount(64)
  await statistics.getByLabel('Statistics type').selectOption('phaCounts')
  await expect(statistics).toContainText('PHA rate over the latest telemetry interval')
  await statistics.getByLabel('Integral').check()
  await expect(statistics).toContainText('PHA integrated count')

  await page.getByRole('button', { name: 'Stop and drain' }).click()
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
  await expect(page.getByRole('heading', { name: runId })).toBeVisible()
  await expect(page.getByLabel('Stored runs').getByText(runId, { exact: true })).toBeVisible()

  const downloadPromise = page.waitForEvent('download')
  await page
    .getByLabel('Stored runs')
    .getByRole('button', { name: /events\.jsonl/ })
    .click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toBe('events.jsonl')
  expect((await readFile(await download.path())).byteLength).toBeGreaterThan(0)

  await page.reload()
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
  await expect(page.getByLabel('Stored runs').getByText(runId, { exact: true })).toBeVisible()
})

test('operator receives structured validation feedback before hardware mutation', async ({
  page,
}) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
  await page.getByRole('button', { name: 'Edit source' }).click()
  await page.getByLabel('JANUS configuration source').fill('Open TDlink 0 0')
  await page.getByRole('button', { name: 'Validate' }).click()
  await expect(page.getByLabel('Validation issues')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
})

test('backend automatically stops runs at time and event presets while manual stop remains available', async ({
  page,
}) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()

  const timedRun = `preset-time-${Date.now()}`
  await page.getByLabel('Run stop').selectOption('PRESET_TIME')
  await page.getByLabel('Preset time (seconds)').fill('1')
  await page.getByLabel('Run ID').fill(timedRun)
  await page.getByRole('button', { name: 'Start run' }).click()
  await expect(page.getByRole('heading', { name: 'Running' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Stop and drain' })).toBeEnabled()
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible({ timeout: 10_000 })
  await expect(page.getByRole('heading', { name: timedRun })).toBeVisible()
  await expect(page.getByText('preset_time', { exact: true })).toBeVisible()

  const countedRun = `preset-count-${Date.now()}`
  await page.getByLabel('Run stop').selectOption('PRESET_COUNTS')
  await page.getByLabel('Preset event count').fill('3')
  await page.getByLabel('Run ID').fill(countedRun)
  await page.getByRole('button', { name: 'Start run' }).click()
  await expect(page.getByRole('heading', { name: 'Running' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible({ timeout: 10_000 })
  await expect(page.getByRole('heading', { name: countedRun })).toBeVisible()
  await expect(page.getByText('preset_counts', { exact: true })).toBeVisible()
})

test('operator configures bounded values and channel masks without editing text', async ({
  page,
}) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()

  await page.getByRole('tab', { name: 'AcqMode', exact: true }).click()
  await page.getByRole('button', { name: 'Configure channels' }).click()
  const mask = page.getByRole('dialog', { name: 'ChEnableMask' })
  await expect(mask.getByText('64 enabled')).toBeVisible()
  await mask.getByLabel('Target').selectOption('2')
  await expect(mask.getByText(/64 enabled · inherited/)).toBeVisible()
  await mask.getByRole('button', { name: 'Channel 0', exact: true }).click()
  await expect(mask.getByText('63 enabled')).toBeVisible()
  await mask.getByRole('button', { name: 'Apply mask' }).click()
  const maskSummary = page.getByLabel('ChEnableMask0 values by board')
  const maskRows = maskSummary.locator('.mask-board-value')
  await expect(maskRows).toHaveCount(4)
  await expect(maskRows.nth(0)).toContainText(/B0.*0xFFFFFFFF · 0xFFFFFFFF/)
  await expect(maskRows.nth(1)).toContainText(/B1.*0xFFFFFFFF · 0xFFFFFFFF/)
  await expect(maskRows.nth(2)).toContainText(/B2.*0xFFFFFFFE · 0xFFFFFFFF/)
  await expect(maskRows.nth(3)).toContainText(/B3.*0xFFFFFFFF · 0xFFFFFFFF.*global/)

  await page.getByRole('tab', { name: 'All', exact: true }).click()
  await page.getByLabel('Find a parameter').fill('MajorityLevel')
  const majority = page.getByRole('spinbutton', { name: 'MajorityLevel', exact: true })
  await majority.fill('65')
  await expect(majority).toHaveValue('64')
  await majority.press('ArrowDown')
  await expect(majority).toHaveValue('63')
  const decrease = page.getByRole('button', { name: 'Decrease MajorityLevel' })
  const increase = page.getByRole('button', { name: 'Increase MajorityLevel' })
  const [decreaseBox, increaseBox] = await Promise.all([
    decrease.boundingBox(),
    increase.boundingBox(),
  ])
  expect(decreaseBox?.width).toBeGreaterThanOrEqual(34)
  expect(increaseBox?.width).toBe(decreaseBox?.width)
  await decrease.click()
  await expect(majority).toHaveValue('62')

  await page.getByLabel('Find a parameter').fill('TD_FineThreshold')
  await page.getByRole('button', { name: 'Per-channel overrides' }).click()
  const channels = page.getByRole('dialog', { name: 'TD_FineThreshold' })
  await channels.getByRole('combobox').selectOption('2')
  await channels.getByLabel('TD_FineThreshold board 2 channel 17', { exact: true }).fill('9')
  await channels.getByRole('button', { name: 'Apply overrides' }).click()
  await expect(page.getByLabel('TD_FineThreshold non-zero individual values')).toContainText(
    'B2: 1 non-zero',
  )

  await page.getByLabel('Find a parameter').fill('TD_CoarseThreshold')
  const coarseBoards = page.getByLabel('TD_CoarseThreshold values by board')
  await expect(coarseBoards).toContainText(/B0.*181/)
  await expect(coarseBoards).toContainText(/B1.*183.*global/)
  await expect(coarseBoards).toContainText(/B2.*179/)
  await expect(coarseBoards).toContainText(/B3.*178/)
  await page.getByRole('button', { name: 'Per-board overrides' }).click()
  const coarse = page.getByRole('dialog', { name: 'TD_CoarseThreshold' })
  await coarse.getByLabel('TD_CoarseThreshold board 2', { exact: true }).fill('220')
  await coarse.getByRole('button', { name: 'Apply overrides' }).click()
  await expect(coarseBoards).toContainText(/B2.*220/)

  await page.getByLabel('Find a parameter').fill('HV_Vbias')
  const hvBoards = page.getByLabel('HV_Vbias values by board')
  const hvRows = hvBoards.locator('span')
  await expect(hvRows).toHaveCount(4)
  for (let board = 0; board < 4; board++)
    await expect(hvRows.nth(board)).toContainText(new RegExp(`B${board}.*45\\.4.*global`))

  await page.getByLabel('Find a parameter').fill('HV_IndivAdj')
  await page.getByRole('button', { name: 'Per-channel overrides' }).click()
  const hvChannels = page.getByRole('dialog', { name: 'HV_IndivAdj' })
  await hvChannels.getByRole('combobox').selectOption('1')
  await expect(
    hvChannels.getByLabel('HV_IndivAdj board 1 channel 4', { exact: true }).locator('..'),
  ).toContainText('Vnom 41.20 V')
  await hvChannels.getByLabel('HV_IndivAdj board 1 channel 4', { exact: true }).fill('12')
  await expect(
    hvChannels.getByLabel('HV_IndivAdj board 1 channel 4', { exact: true }).locator('..'),
  ).toContainText('Vnom 41.40 V')
  await hvChannels.getByRole('button', { name: 'Apply overrides' }).click()
  await expect(page.getByLabel('HV_IndivAdj non-zero individual values')).toContainText(
    'B1: 1 non-zero',
  )

  await page.getByLabel('Find a parameter').fill('TempSensType')
  await page.getByLabel('TempSensType', { exact: true }).fill('1 2 3')

  await page.getByRole('button', { name: 'Edit source' }).click()
  await expect(page.getByLabel('JANUS configuration source')).toHaveValue(
    /ChEnableMask0\[2\]\s+0xFFFFFFFE/,
  )
  await expect(page.getByLabel('JANUS configuration source')).toHaveValue(
    /TD_FineThreshold\[2\]\[17\]\s+9/,
  )
  await expect(page.getByLabel('JANUS configuration source')).toHaveValue(
    /TD_CoarseThreshold\[2\]\s+220/,
  )
  await expect(page.getByLabel('JANUS configuration source')).toHaveValue(
    /HV_IndivAdj\[1\]\[4\]\s+12/,
  )
  await expect(page.getByLabel('JANUS configuration source')).toHaveValue(/TempSensType\s+1 2 3/)
})

test('operator monitors and safely switches high voltage while ready', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()

  const board0 = page.locator('.board-card').filter({ hasText: 'Chain 0' })
  await expect(board0.getByText('HV off')).toBeVisible()
  await expect(board0).toContainText('Vmon0.00 V')
  await expect(board0).toContainText('Imon0.000 mA')
  await expect(board0).toContainText('HV temp.30.7 °C')
  await board0.getByRole('button', { name: 'Turn board 0 HV on' }).click()
  await expect(board0.getByText('HV on')).toBeVisible()
  await expect(board0).toContainText('Vmon45.40 V')

  await page.getByRole('button', { name: 'All HV off' }).click()
  await expect(board0.getByText('HV off')).toBeVisible()
})
