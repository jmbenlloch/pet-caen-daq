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

test('operator configures bounded values and channel masks without editing text', async ({
  page,
}) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()

  await page.getByRole('tab', { name: 'AcqMode', exact: true }).click()
  await page.getByRole('button', { name: 'Configure channels' }).click()
  const mask = page.getByRole('dialog', { name: 'ChEnableMask' })
  await expect(mask.getByText('64 enabled')).toBeVisible()
  await mask.getByRole('button', { name: 'Channel 0', exact: true }).click()
  await expect(mask.getByText('63 enabled')).toBeVisible()
  await mask.getByRole('button', { name: 'Apply mask' }).click()

  await page.getByRole('tab', { name: 'All', exact: true }).click()
  await page.getByLabel('Find a parameter').fill('MajorityLevel')
  const majority = page.getByRole('spinbutton', { name: 'MajorityLevel', exact: true })
  await majority.fill('65')
  await expect(majority).toHaveValue('64')
  await majority.press('ArrowDown')
  await expect(majority).toHaveValue('63')
  await page.getByRole('button', { name: 'Decrease MajorityLevel' }).click()
  await expect(majority).toHaveValue('62')

  await page.getByRole('button', { name: 'Edit source' }).click()
  await expect(page.getByLabel('JANUS configuration source')).toHaveValue(
    /ChEnableMask0\s+0xFFFFFFFE/,
  )
})
