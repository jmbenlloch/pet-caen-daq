import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'

test('effective channel voltage readouts follow bias, range, and channel edits', async ({
  page,
}) => {
  await page.goto('/')
  await page.getByRole('tab', { name: /Acquisition/ }).click()
  const fixture = await readFile('../test/fixtures/janus/config_same4_v3_good.txt', 'utf8')
  await page
    .locator('input[type=file]')
    .setInputFiles({ name: 'hv-test.txt', mimeType: 'text/plain', buffer: Buffer.from(fixture) })
  await page.getByRole('tab', { name: 'All', exact: true }).click()
  const search = page.getByLabel('Find a parameter')
  await search.fill('HV_Vbias')
  const board0 = page.getByLabel('B0 estimated channel bias', { exact: true })
  await expect(board0).toContainText('41.20 V')
  await page.getByLabel('HV_Vbias', { exact: true }).fill('55')
  await expect(board0).toContainText('50.80 V')
  await search.fill('HV_Adjust_Range')
  await page.getByLabel('HV_Adjust_Range', { exact: true }).selectOption('2.5')
  await search.fill('HV_Vbias')
  await expect(board0).toContainText('52.50 V')
  await page.getByRole('button', { name: 'View channel voltages' }).click()
  const dialog = page.getByRole('dialog', { name: 'HV_IndivAdj', exact: true })
  const channel = dialog.getByLabel('HV_IndivAdj board 0 channel 0', { exact: true })
  await channel.fill('55')
  await expect(channel.locator('..')).toContainText('Vnom 53.04 V')
  await dialog.getByRole('button', { name: 'Apply overrides' }).click()
  await expect(board0).toContainText('52.50–53.04 V')
  await page.getByRole('button', { name: 'Per-board overrides' }).click()
  const boards = page.getByRole('dialog', { name: 'HV_Vbias', exact: true })
  await boards.getByLabel('HV_Vbias board 1', { exact: true }).fill('56')
  await boards.getByRole('button', { name: 'Apply overrides' }).click()
  await expect(page.getByLabel('B1 estimated channel bias', { exact: true })).toContainText(
    '53.50 V',
  )
  await search.fill('HV_Adjust_Range')
  await page.getByLabel('HV_Adjust_Range', { exact: true }).selectOption('DISABLED')
  await search.fill('HV_Vbias')
  await expect(board0).toContainText('55.00 V')
  await expect(page.getByLabel('B1 estimated channel bias', { exact: true })).toContainText(
    '56.00 V',
  )
})
