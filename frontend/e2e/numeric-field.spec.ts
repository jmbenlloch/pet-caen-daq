import { expect, test } from '@playwright/test'

test('operator types HV bias directly, including a temporary value below the minimum', async ({
  page,
}) => {
  await page.goto('/')
  await page.getByRole('tab', { name: /Acquisition/ }).click()
  await page.getByRole('tab', { name: 'All', exact: true }).click()
  await page.getByLabel('Find a parameter').fill('HV_Vbias')
  const input = page.getByLabel('HV_Vbias', { exact: true })
  await input.fill('')
  await expect(input).toHaveValue('')
  await input.pressSequentially('4')
  await expect(input).toHaveValue('4')
  await expect(input).toHaveAttribute('aria-invalid', 'true')
  await input.pressSequentially('5.4')
  await expect(input).toHaveValue('45.4')
  await input.blur()
  await expect(input).toHaveValue('45.4')
  await expect(input).toHaveAttribute('aria-invalid', 'false')
})
