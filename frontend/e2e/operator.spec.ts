import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'

const configurationPath = new URL(
  '../../test/fixtures/janus/config_same4_v3_good.txt',
  import.meta.url,
)

test('operator completes a simulated run and downloads its persisted artifact', async ({
  page,
}) => {
  const configuration = await readFile(configurationPath, 'utf8')
  const runId = `browser-${Date.now()}`

  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
  await expect(page.getByText('DT5202 · node 0')).toHaveCount(4)
  await expect(page.getByText('Live telemetry')).toBeVisible()

  await page.getByLabel('Run ID').fill(runId)
  await page.getByLabel('JANUS configuration').fill(configuration)
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
  await page.getByLabel('JANUS configuration').fill('Open TDlink 0 0')
  await page.getByRole('button', { name: 'Validate' }).click()
  await expect(page.getByLabel('Validation issues')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Ready' })).toBeVisible()
})
