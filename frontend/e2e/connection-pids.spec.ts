import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'

test('connection addresses identify discovered cards by PID', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('tab', { name: /Acquisition/ }).click()
  const fixture = await readFile('../test/fixtures/janus/config_same4_v3_good.txt', 'utf8')
  await page.locator('input[type=file]').setInputFiles({
    name: 'connections.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from(fixture),
  })
  await page.getByRole('tab', { name: 'Connect', exact: true }).click()
  const devices = page.getByLabel('Discovered card addresses')
  const details = devices.locator('details')
  if ((await details.getAttribute('open')) === null) await details.locator('summary').click()
  const snapshot = await page.request.post('/pet.caen.daq.v1.SystemService/GetSystemSnapshot', {
    data: {},
  })
  expect(snapshot.ok()).toBeTruthy()
  const data = await snapshot.json()
  for (const chain of data.chains) {
    for (const board of chain.boards ?? []) {
      const suffix = `:tdl:${chain.index ?? 0}:${board.node ?? 0}`
      const row = devices.locator('li').filter({ hasText: suffix })
      await expect(row).toContainText(`PID ${board.productId}`)
    }
  }
  const configured = page.getByRole('textbox', { name: /Open.*Override 0/ })
  const firstAddress = await devices
    .locator('li')
    .filter({ hasText: ':tdl:0:0' })
    .locator('code')
    .innerText()
  await configured.fill(firstAddress)
  await expect(configured.locator('..')).toContainText(`PID ${data.chains[0].boards[0].productId}`)
  await configured.fill('usb:172.16.0.11:tdl:0:15')
  await expect(configured.locator('..')).toContainText('PID unavailable')
})
