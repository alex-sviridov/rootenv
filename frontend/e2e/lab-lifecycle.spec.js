import { test, expect } from '@playwright/test'

const email = `e2e-lifecycle-${Date.now()}@test.local`
const password = 'password123'

// Navigate home → open first group → open first lab. Returns the lab URL.
async function navigateToFirstLab(page) {
  await page.goto('/')
  await expect(page.locator('text=Loading…')).not.toBeVisible({ timeout: 10_000 })
  await page.locator('[class*="grid"] button').first().click()
  await expect(page.locator('[class*="grid"] button').first()).toBeVisible()
  await page.locator('[class*="grid"] button').first().click()
  await expect(page).toHaveURL(/\/lab\//)
  await expect(page.locator('text=Loading…')).not.toBeVisible({ timeout: 10_000 })
  return page.url()
}

// Navigate home → open second group → open its first lab.
// Returns the lab URL, or null if there is no second group.
async function navigateToSecondGroupLab(page) {
  await page.goto('/')
  await expect(page.locator('text=Loading…')).not.toBeVisible({ timeout: 10_000 })
  const groups = page.locator('[class*="grid"] button')
  if (await groups.count() < 2) return null
  await groups.nth(1).click()
  await expect(page.locator('[class*="grid"] button').first()).toBeVisible()
  await page.locator('[class*="grid"] button').first().click()
  await expect(page).toHaveURL(/\/lab\//)
  await expect(page.locator('text=Loading…')).not.toBeVisible({ timeout: 10_000 })
  return page.url()
}

// Locator scoped to the Lab Session sidebar panel.
const labSession = (page) => page.locator('text=Lab Session').locator('../../..')

test.describe.serial('lab lifecycle flow', () => {
  let page
  let labUrl

  test.beforeAll(async ({ browser }) => {
    page = await (await browser.newContext()).newPage()
    await page.goto('/login')
    await page.getByRole('button', { name: 'Register' }).click()
    const form = page.locator('form')
    await form.getByPlaceholder('you@example.com').fill(email)
    await form.locator('input[autocomplete="current-password"]').fill(password)
    await form.locator('input[autocomplete="new-password"]').fill(password)
    await form.getByRole('button', { name: 'Create account' }).click()
    await expect(page).toHaveURL('/')
  })

  test.afterAll(async () => {
    // Leave no active attempt behind
    if (labUrl) {
      await page.goto(labUrl)
      await expect(page.locator('text=Loading…')).not.toBeVisible({ timeout: 10_000 })
      const decommBtn = page.getByRole('button', { name: 'Decommission', exact: true })
      const visible = await decommBtn.waitFor({ state: 'visible', timeout: 30_000 }).then(() => true).catch(() => false)
      if (visible) {
        await decommBtn.click()
        await expect(page.getByRole('button', { name: 'Provision Lab', exact: true })).toBeVisible({ timeout: 30_000 })
      }
    }
    // Delete the test account
    await page.goto('/account')
    await expect(page.locator('text=Loading…')).not.toBeVisible({ timeout: 10_000 })
    await page.getByPlaceholder('Enter your password to continue').fill(password)
    await page.getByRole('button', { name: 'Delete account', exact: true }).click()
    await page.getByRole('button', { name: 'Yes, delete my account', exact: true }).click()
    await expect(page).toHaveURL('/login', { timeout: 10_000 })
    await page.context().close()
  })

  test('navigate to a lab', async () => {
    labUrl = await navigateToFirstLab(page)
  })

  test('Provision Lab button visible before provisioning', async () => {
    await expect(page.getByRole('button', { name: 'Provision Lab', exact: true })).toBeVisible()
  })

  test('provision lab — reaches Running state', async () => {
    await page.getByRole('button', { name: 'Provision Lab', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Provision Lab', exact: true })).not.toBeVisible({ timeout: 15_000 })
    await expect(labSession(page).locator('text=Running')).toBeVisible({ timeout: 30_000 })
  })

  test('servers appear in sidebar after provisioning', async () => {
    // Decommission button requires activeAttempt + servers loaded
    await expect(page.getByRole('button', { name: 'Decommission', exact: true })).toBeVisible({ timeout: 15_000 })
    // At least one server name row is visible in the sidebar
    await expect(labSession(page).locator('.text-slate-200').first()).toBeVisible()
  })

  test('visiting a different lab shows "Another lab is active" and blocks provisioning', async () => {
    const otherUrl = await navigateToSecondGroupLab(page)
    if (!otherUrl || otherUrl === labUrl) {
      test.skip()
      return
    }
    await expect(page.locator('text=Another lab is active')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByRole('button', { name: 'Decommission', exact: true })).not.toBeVisible()
    await expect(page.getByRole('button', { name: 'Provision Lab', exact: true })).toBeDisabled()
  })

  test('"Another lab is active" link navigates to and fully loads that lab', async () => {
    // We're already on the second lab with "Another lab is active" showing
    const activeLabLink = page.locator('text=Another lab is active').locator('../..').locator('button').last()
    await activeLabLink.click()
    await expect(page).toHaveURL(new RegExp(labUrl.replace(/.*\/lab\//, '/lab/')))
    await expect(page.locator('text=Loading…')).not.toBeVisible({ timeout: 10_000 })
    // The lab content and sidebar must reflect the active lab, not the previous one
    await expect(page.getByRole('button', { name: 'Decommission', exact: true })).toBeVisible({ timeout: 10_000 })
    await expect(page.locator('text=Another lab is active')).not.toBeVisible()
  })

  test('decommission — Provision Lab button reappears', async () => {
    await page.goto(labUrl)
    await expect(page.locator('text=Loading…')).not.toBeVisible({ timeout: 10_000 })
    await expect(page.getByRole('button', { name: 'Decommission', exact: true })).toBeVisible({ timeout: 10_000 })
    await page.getByRole('button', { name: 'Decommission', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Provision Lab', exact: true })).toBeVisible({ timeout: 30_000 })
  })

  test('can provision again after decommission', async () => {
    await page.getByRole('button', { name: 'Provision Lab', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Provision Lab', exact: true })).not.toBeVisible({ timeout: 15_000 })
    await expect(
      labSession(page).locator('text=Running').or(labSession(page).locator('text=Provisioning'))
    ).toBeVisible({ timeout: 15_000 })
  })

  test('delete account blocked when lab is active', async () => {
    await page.goto('/account')
    await page.getByPlaceholder('Enter your password to continue').fill(password)
    await page.getByRole('button', { name: 'Delete account' }).click()
    await expect(page.locator('text=active lab session')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByRole('button', { name: 'Yes, delete my account' })).not.toBeVisible()
  })
})
