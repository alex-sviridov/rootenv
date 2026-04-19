import { test, expect } from '@playwright/test'

const email = `e2e-user-${Date.now()}@test.local`
const password = 'password123'
const newPassword = 'newpassword456'

test.describe.serial('user flow', () => {
  let page

  test.beforeAll(async ({ browser }) => {
    page = await (await browser.newContext()).newPage()
  })

  test.afterAll(async () => {
    await page.context().close()
  })

  test('browse labs without auth', async () => {
    await page.goto('/')
    await expect(page.locator('text=Loading…')).not.toBeVisible({ timeout: 10_000 })
    await expect(page.locator('[class*="grid"] button').first()).toBeVisible()
  })

  test('register', async () => {
    await page.goto('/login')
    await page.getByRole('button', { name: 'Register' }).click()
    const form = page.locator('form')
    await form.getByPlaceholder('you@example.com').fill(email)
    await form.locator('input[autocomplete="current-password"]').fill(password)
    await form.locator('input[autocomplete="new-password"]').fill(password)
    await form.getByRole('button', { name: 'Create account' }).click()
    await expect(page).toHaveURL('/')
  })

  test('change password', async () => {
    await page.goto('/account')
    const form = page.locator('form')
    await form.locator('input[autocomplete="current-password"]').fill(password)
    await form.locator('input[autocomplete="new-password"]').nth(0).fill(newPassword)
    await form.locator('input[autocomplete="new-password"]').nth(1).fill(newPassword)
    await page.getByRole('button', { name: 'Update password' }).click()
    await expect(page).toHaveURL('/login')
  })

  test('login with new password', async () => {
    const form = page.locator('form')
    await form.getByPlaceholder('you@example.com').fill(email)
    await form.locator('input[autocomplete="current-password"]').fill(newPassword)
    await form.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL('/')
  })

  test('lab requires auth — unauthenticated browser is redirected to login', async ({ browser }) => {
    // Use a fresh context with no auth to test the guard without breaking the main session
    const fresh = await (await browser.newContext()).newPage()
    await fresh.goto('/lab/rhcsa1')
    await expect(fresh).toHaveURL(/\/login/)
    const form = fresh.locator('form')
    await form.getByPlaceholder('you@example.com').fill(email)
    await form.locator('input[autocomplete="current-password"]').fill(newPassword)
    await form.getByRole('button', { name: 'Sign in' }).click()
    await expect(fresh).toHaveURL(/\/lab\/rhcsa1/)
    await fresh.context().close()
  })

  test('delete account', async () => {
    await page.goto('/account')
    await page.getByPlaceholder('Enter your password to continue').fill(newPassword)
    await page.getByRole('button', { name: 'Delete account' }).click()
    await page.getByRole('button', { name: 'Yes, delete my account' }).click()
    await expect(page).toHaveURL('/login')
  })

  test('login fails after deletion', async () => {
    const form = page.locator('form')
    await form.getByPlaceholder('you@example.com').fill(email)
    await form.locator('input[autocomplete="current-password"]').fill(newPassword)
    await form.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.locator('text=/invalid|failed|not found/i')).toBeVisible()
  })
})
