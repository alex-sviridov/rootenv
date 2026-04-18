import { test, expect } from '@playwright/test'

const email = `e2e-${Date.now()}@test.local`
const password = 'password123'
const newPassword = 'newpassword456'

test.describe.serial('user flow', () => {
  let sharedPage

  test.beforeAll(async ({ browser }) => {
    sharedPage = await (await browser.newContext()).newPage()
  })

  test.afterAll(async () => {
    await sharedPage.context().close()
  })

  test('labs visible without auth', async () => {
    await sharedPage.goto('/')
    await expect(sharedPage.getByText('Lab Groups')).toBeVisible()
    await expect(sharedPage.getByText('Loading…')).not.toBeVisible({ timeout: 10_000 })
    await expect(sharedPage.locator('[class*="grid"] button').first()).toBeVisible()
  })

  test('register', async () => {
    await sharedPage.goto('/login')
    const form = sharedPage.locator('form')
    await sharedPage.getByRole('button', { name: 'Register' }).click()
    await form.getByPlaceholder('you@example.com').fill(email)
    await form.locator('input[autocomplete="current-password"]').fill(password)
    await form.locator('input[autocomplete="new-password"]').fill(password)
    await form.getByRole('button', { name: 'Create account' }).click()
    await expect(sharedPage).toHaveURL('/')
  })

  test('change password', async () => {
    await sharedPage.goto('/account')
    const form = sharedPage.locator('form')
    await form.locator('input[autocomplete="current-password"]').fill(password)
    await form.locator('input[autocomplete="new-password"]').nth(0).fill(newPassword)
    await form.locator('input[autocomplete="new-password"]').nth(1).fill(newPassword)
    await sharedPage.getByRole('button', { name: 'Update password' }).click()
    await expect(sharedPage).toHaveURL('/login')
  })

  test('login with new password', async () => {
    const form = sharedPage.locator('form')
    await form.getByPlaceholder('you@example.com').fill(email)
    await form.locator('input[autocomplete="current-password"]').fill(newPassword)
    await form.getByRole('button', { name: 'Sign in' }).click()
    await expect(sharedPage).toHaveURL('/')
  })

  test('open a lab', async () => {
    await expect(sharedPage.getByText('Loading…')).not.toBeVisible({ timeout: 10_000 })
    await sharedPage.locator('[class*="grid"] button').first().click()
    await expect(sharedPage.locator('[class*="grid"] button').first()).toBeVisible()
    await sharedPage.locator('[class*="grid"] button').first().click()
    await expect(sharedPage).toHaveURL(/\/lab\//)
    await expect(sharedPage.locator('text=Loading…')).not.toBeVisible({ timeout: 10_000 })
  })

  test('delete account', async () => {
    await sharedPage.goto('/account')
    await sharedPage.getByPlaceholder('Enter your password to continue').fill(newPassword)
    await sharedPage.getByRole('button', { name: 'Delete account' }).click()
    await sharedPage.getByRole('button', { name: 'Yes, delete my account' }).click()
    await expect(sharedPage).toHaveURL('/login')
  })

  test('login fails after deletion', async () => {
    const form = sharedPage.locator('form')
    await form.getByPlaceholder('you@example.com').fill(email)
    await form.locator('input[autocomplete="current-password"]').fill(newPassword)
    await form.getByRole('button', { name: 'Sign in' }).click()
    await expect(sharedPage.locator('text=/invalid|failed|not found/i')).toBeVisible()
  })
})
