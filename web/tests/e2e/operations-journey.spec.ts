import { test, expect } from '@playwright/test'

test.describe('Operations Console E2E', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('http://localhost:3000/login')
    await page.fill('input[name="username"]', 'test@example.com')
    await page.fill('input[name="password"]', 'password123')
    await page.click('button[type="submit"]')
    await page.waitForNavigation()
  })

  test('navigate to operations console', async ({ page }) => {
    await page.goto('http://localhost:3000/operations')
    await expect(page.locator('h1')).toContainText('Operations Console')
  })

  test('displays KPI cards', async ({ page }) => {
    await page.goto('http://localhost:3000/operations')
    await expect(page.locator('text=Success Rate|Throughput|Utilization')).toBeVisible({ timeout: 5000 })
  })

  test('execution table loads', async ({ page }) => {
    await page.goto('http://localhost:3000/operations')
    await expect(page.locator('table, [role="table"]')).toBeVisible({ timeout: 5000 })
  })

  test('search executions', async ({ page }) => {
    await page.goto('http://localhost:3000/operations')
    const searchInput = page.locator('input[placeholder*="search" i]').first()
    if (await searchInput.isVisible()) {
      await searchInput.fill('exec-')
      await expect(page.locator('text=exec-')).toBeVisible({ timeout: 2000 })
    }
  })

  test('open execution details', async ({ page }) => {
    await page.goto('http://localhost:3000/operations')
    const firstExecRow = page.locator('table tbody tr, [role="row"]').first()
    if (await firstExecRow.isVisible()) {
      await firstExecRow.click()
      await expect(page.locator('text=Execution Details|General|Plan')).toBeVisible({ timeout: 2000 })
    }
  })

  test('close drawer on ESC', async ({ page }) => {
    await page.goto('http://localhost:3000/operations')
    const firstExecRow = page.locator('table tbody tr, [role="row"]').first()
    if (await firstExecRow.isVisible()) {
      await firstExecRow.click()
      await page.keyboard.press('Escape')
      await expect(page.locator('[role="dialog"]')).not.toBeVisible()
    }
  })

  test('responsive on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 })
    await page.goto('http://localhost:3000/operations')
    await expect(page.locator('h1')).toContainText('Operations Console')
    await expect(page.locator('main, [role="main"]')).toBeVisible()
  })

  test('page loads within 3 seconds', async ({ page }) => {
    const startTime = Date.now()
    await page.goto('http://localhost:3000/operations')
    await page.waitForLoadState('networkidle')
    const loadTime = Date.now() - startTime
    expect(loadTime).toBeLessThan(3000)
  })
})
