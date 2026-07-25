import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'

test('operator workspace tabs fit on one desktop row', async ({ page }) => {
  await page.goto('/')
  const tabs = page.getByRole('tablist', { name: 'Operator workspace' }).getByRole('tab')
  await expect(tabs).toHaveCount(6)

  const topEdges = await tabs.evaluateAll((elements) =>
    elements.map((element) => element.getBoundingClientRect().top),
  )
  expect(new Set(topEdges).size).toBe(1)
})

test('operator selects a persistent light or dark interface theme', async ({ page }) => {
  await page.goto('/')
  const darkBackground = await page
    .locator('body')
    .evaluate((element) => getComputedStyle(element).backgroundImage)
  await page.getByRole('button', { name: 'Switch to light theme' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  const lightBackground = await page
    .locator('body')
    .evaluate((element) => getComputedStyle(element).backgroundImage)
  expect(lightBackground).not.toBe(darkBackground)
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await page.getByRole('button', { name: 'Switch to dark theme' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
})

test('operator completes a simulated run and downloads its persisted artifact', async ({
  page,
}) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
  await page.getByRole('tab', { name: /Hardware/ }).click()
  await expect(page.getByText('DT5202 · node 0')).toHaveCount(4)
  await expect(page.getByText('Backend online')).toBeVisible()
  await page.getByRole('tab', { name: /Acquisition/ }).click()
  await expect(page.getByLabel('Configuration parameters')).toBeVisible()
  await page.getByRole('tab', { name: 'All', exact: true }).click()
  await page.getByLabel('Find a parameter').fill('PresetTime')
  await page.getByRole('spinbutton', { name: 'PresetTime', exact: true }).fill('30')

  await page.getByRole('tab', { name: 'RunCtrl', exact: true }).click()
  const persistHistograms = page.locator('#persist-histograms')
  await expect(persistHistograms).toBeChecked()
  await persistHistograms.uncheck()
  await page.getByRole('button', { name: 'Start run' }).click()
  await expect(page.getByRole('heading', { name: 'Running' })).toBeVisible()
  const activeRunId = page.locator('.run-now strong')
  await expect(activeRunId).toBeVisible()
  await expect(activeRunId).toHaveText(/^\d+$/)
  const runId = (await activeRunId.textContent())!
  await page.getByRole('tab', { name: /Statistics/ }).click()
  const statistics = page.getByRole('region', { name: 'Statistics' })
  await expect(statistics.getByRole('tab', { name: 'Board 0' })).toBeVisible()
  await statistics.getByRole('tab', { name: 'Board 0' }).click()
  await expect(statistics.locator('.channel-statistic')).toHaveCount(64)
  await statistics.getByLabel('Per-channel metric').selectOption('phaCounts')
  await expect(statistics).toContainText('PHA rate over the latest telemetry interval')
  await statistics.getByLabel('Cumulative counts').check()
  await expect(statistics).toContainText('PHA integrated count')

  await page.getByRole('tab', { name: /Plots/ }).click()
  const plots = page.getByRole('region', { name: 'Plots and histograms' })
  await plots.getByLabel('Channels').click()
  await plots.getByRole('button', { name: 'Board 0 node 0 channel 1', exact: true }).click()
  await plots.getByRole('button', { name: 'Request data' }).click()
  await expect(plots.getByLabel('Histogram datasets').locator('.histogram-dataset')).toHaveCount(2)
  const histogramPlot = plots.getByLabel('Live selected-channel histogram plot')
  await expect(histogramPlot).toBeVisible()
  await expect(histogramPlot.locator('canvas').first()).toBeVisible()
  await plots.getByLabel('Log Y').check()
  await expect(histogramPlot.locator('canvas').first()).toBeVisible()

  await page.getByRole('tab', { name: /Acquisition/ }).click()
  await page.getByRole('button', { name: 'Stop and drain' }).click()
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
  await page.getByRole('tab', { name: /Runs/ }).click()
  const storedRuns = page.getByLabel('Stored runs')
  await expect(storedRuns.getByRole('button', { name: runId, exact: true })).toBeVisible()
  await storedRuns.getByRole('button', { name: runId, exact: true }).click()
  const runDetails = page.getByRole('region', { name: `Details for run ${runId}` })
  await expect(runDetails).toBeVisible()

  const downloadPromise = page.waitForEvent('download')
  await runDetails.getByRole('button', { name: /events\.jsonl/ }).click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toBe('events.jsonl')
  expect((await readFile(await download.path())).byteLength).toBeGreaterThan(0)

  await page.reload()
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
  await page.getByRole('tab', { name: /Runs/ }).click()
  await expect(page.getByLabel('Stored runs').getByText(runId, { exact: true })).toBeVisible()
})

test('start reports structured validation feedback before hardware mutation', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
  await page.getByRole('button', { name: 'View raw configuration' }).click()
  await page.getByLabel('JANUS configuration source').fill('Open TDlink 0 0')
  await page.getByRole('button', { name: 'Start run' }).click()
  await expect(page.getByLabel('Validation issues')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
})

test('hold-delay scan plots spectra produced by the simulator', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
  await page.getByRole('tab', { name: /Scans/ }).click()

  const scan = page.getByRole('region', { name: 'Hold delay scan' })
  const staircase = page.getByRole('region', { name: 'Threshold staircase' })
  await expect(scan.getByRole('button', { name: 'Expand' })).toHaveAttribute(
    'aria-expanded',
    'false',
  )
  await expect(staircase.getByRole('button', { name: 'Expand' })).toHaveAttribute(
    'aria-expanded',
    'false',
  )
  await scan.getByRole('button', { name: 'Expand' }).click()
  await expect(scan.getByRole('spinbutton', { name: 'Maximum (ns)' })).toHaveValue('256')
  await scan.getByRole('spinbutton', { name: 'Maximum (ns)' }).fill('8')
  await scan.getByRole('spinbutton', { name: 'Events / delay' }).fill('10')
  await scan.getByRole('button', { name: 'Start scan' }).click()
  const runLabel = scan.locator('.staircase-status strong')
  await expect(runLabel).toHaveText(/^Run \d+$/)
  const runName = (await runLabel.textContent())!

  await expect(scan.getByRole('img', { name: 'Hold delay heatmap for channel 0' })).toBeVisible({
    timeout: 10_000,
  })
  await expect(scan.getByLabel('Logarithmic events per bin color scale')).toBeVisible()
  const initialCanvas = scan.locator('canvas').first()
  await initialCanvas.evaluate((canvas) => {
    canvas.dataset.livePlotIdentity = 'initial'
  })
  await expect(scan.getByRole('progressbar')).toHaveAttribute('value', '2', { timeout: 10_000 })
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
  await expect(scan.getByRole('img', { name: 'Hold delay heatmap for channel 0' })).toBeVisible()
  await expect(scan.locator('canvas').first()).toHaveAttribute('data-live-plot-identity', 'initial')
  const storedScan = scan.locator('.scan-history > button').filter({ hasText: runName })
  await expect(storedScan).toContainText('COMPLETED')
})

test('backend automatically stops runs at time and event presets while manual stop remains available', async ({
  page,
}) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()

  await page.getByRole('tab', { name: 'RunCtrl', exact: true }).click()
  await page.getByRole('combobox', { name: 'StopRunMode', exact: true }).selectOption('PRESET_TIME')
  await page.getByRole('spinbutton', { name: 'PresetTime', exact: true }).fill('1')
  await page.locator('#persist-histograms').uncheck()
  await page.getByRole('button', { name: 'Start run' }).click()
  await expect(page.getByRole('heading', { name: 'Running' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Stop and drain' })).toBeEnabled()
  const activeRunId = page.locator('.run-now strong')
  await expect(activeRunId).toHaveText(/^\d+$/)
  const timedRun = (await activeRunId.textContent())!
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible({ timeout: 10_000 })
  await page.getByRole('tab', { name: /Runs/ }).click()
  await page.getByRole('button', { name: 'Refresh' }).click()
  const storedRuns = page.getByRole('table', { name: 'Stored runs' })
  const timedRunRow = storedRuns
    .getByRole('button', { name: timedRun, exact: true })
    .locator('..')
    .locator('..')
  await expect(timedRunRow.getByText('preset_time', { exact: true })).toBeVisible()

  await page.getByRole('tab', { name: /Acquisition/ }).click()
  await page
    .getByRole('combobox', { name: 'StopRunMode', exact: true })
    .selectOption('PRESET_COUNTS')
  await page.getByRole('spinbutton', { name: 'PresetCounts', exact: true }).fill('30')
  await page.getByRole('button', { name: 'Start run' }).click()
  await expect(activeRunId).toHaveText(/^\d+$/)
  await expect(activeRunId).not.toHaveText(timedRun)
  const countedRun = (await activeRunId.textContent())!
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible({ timeout: 10_000 })
  await page.getByRole('tab', { name: /Runs/ }).click()
  await page.getByRole('button', { name: 'Refresh' }).click()
  const countedRunRow = storedRuns
    .getByRole('button', { name: countedRun, exact: true })
    .locator('..')
    .locator('..')
  await expect(countedRunRow.getByText('preset_counts', { exact: true })).toBeVisible()
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
  await expect(maskRows.nth(3)).toContainText(/B3.*0xFFFFFFFF · 0xFFFFFFFF.*inherited/)

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
  await expect(coarseBoards).toContainText(/B1.*183.*inherited/)
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
    await expect(hvRows.nth(board)).toContainText(new RegExp(`B${board}.*45\\.4.*inherited`))

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

  await page.getByRole('button', { name: 'View raw configuration' }).click()
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

  await page.getByRole('tab', { name: /Hardware/ }).click()
  const hvPanel = page.getByRole('region', { name: 'SiPM high-voltage status' })
  await expect(hvPanel.getByLabel('HV summary: Off')).toBeVisible()
  await expect(hvPanel.getByLabel('Chain 0 node 0 HV: Off')).toBeVisible()
  const board0 = page.locator('.board-card').filter({ hasText: 'Chain 0' })
  await expect(board0.getByText('HV off')).toBeVisible()
  await expect(board0).toContainText('Vmon0.00 V')
  await expect(board0).toContainText('Imon0.000 mA')
  await expect(board0).toContainText('HV temp.30.7 °C')
  await board0.getByRole('button', { name: 'Turn board 0 HV on' }).click()
  await expect(board0.getByText('Ramping')).toBeVisible()
  await expect(hvPanel.getByLabel('Chain 0 node 0 HV: Ramping')).toBeVisible()
  await expect(board0.getByText('HV on')).toBeVisible()
  await expect(hvPanel.getByLabel('Chain 0 node 0 HV: On')).toBeVisible()
  await expect(hvPanel.getByLabel('HV summary: 1/4 on')).toBeVisible()
  await expect(board0).toContainText('Vmon45.40 V')

  await page.getByRole('button', { name: 'All HV off' }).click()
  await expect(board0.getByText('Ramping')).toBeVisible()
  await expect(hvPanel.getByLabel('HV summary: Ramping')).toBeVisible()
  await expect(board0.getByText('HV off')).toBeVisible()
  await expect(hvPanel.getByLabel('HV summary: Off')).toBeVisible()
})
