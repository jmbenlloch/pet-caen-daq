import { expect, test, type Route } from '@playwright/test'
import { readFile } from 'node:fs/promises'

test("a rejected apply does not adopt another page's configuration", async ({ page, context }) => {
  const other = await context.newPage()
  const fixture = await readFile('../test/fixtures/janus/config_same4_v3_good.txt', 'utf8')
  for (const [target, gain] of [
    [page, 42],
    [other, 43],
  ] as const) {
    await target.goto('/')
    await expect(target.getByRole('heading', { name: 'Ready', exact: true })).toBeVisible()
    const configuration = fixture.replace(/^HG_Gain\s+55/m, `HG_Gain ${gain}`)
    await target.locator('input[type=file]').setInputFiles({
      name: `gain-${gain}.txt`,
      mimeType: 'text/plain',
      buffer: Buffer.from(configuration),
    })
  }

  // Delay this page's rejection until the other page has really applied its
  // different settings. Both pages identify themselves as "operator".
  let pending!: Route
  let arrived!: () => void
  const intercepted = new Promise<void>((resolve) => {
    arrived = resolve
  })
  await page.route('**/pet.caen.daq.v1.RunService/ApplyConfiguration', (route) => {
    pending = route
    arrived()
  })
  await page.getByRole('button', { name: 'Apply config', exact: true }).click()
  await intercepted
  const rejectedRequest = pending.request().postDataJSON()
  expect(rejectedRequest.configurationRequestId).toMatch(/^[0-9a-f]{32}$/)

  const appliedResponse = other.waitForResponse((r) =>
    r.url().endsWith('.RunService/ApplyConfiguration'),
  )
  await other.getByRole('button', { name: 'Apply config', exact: true }).click()
  const response = await appliedResponse
  expect(response.ok()).toBe(true)
  const body = await response.json()
  const acceptedRequest = response.request().postDataJSON()
  expect(acceptedRequest.janusConfiguration).toMatch(/^HG_Gain\s+43/m)
  expect(acceptedRequest.configurationRequestId).not.toBe(rejectedRequest.configurationRequestId)
  expect(body.appliedConfiguration.configurationRequestId).toBe(
    acceptedRequest.configurationRequestId,
  )
  expect(body.snapshot.state).toBe('SYSTEM_STATE_READY')
  expect(body.snapshot.currentRun).toBeUndefined()
  await expect(other.getByLabel('Applied configuration')).toContainText('Applied with Apply config')
  await expect(other.getByLabel('Applied configuration')).not.toContainText('Editor not compared')

  await pending.fulfill({
    status: 409,
    contentType: 'application/json',
    body: JSON.stringify({
      code: 'aborted',
      message: '[CONCURRENT_OPERATION] another run-control operation is in progress',
    }),
  })
  await expect(page.getByText(/CONCURRENT_OPERATION/)).toBeVisible()
  await expect(page.getByLabel('Applied configuration')).toContainText(
    'Editor not compared with the boards',
  )
  await page.getByRole('tab', { name: 'Scans', exact: true }).click()
  await expect(page.getByLabel('Scan configuration')).toContainText(
    'Editor not compared with the boards',
  )
  await other.close()
})
