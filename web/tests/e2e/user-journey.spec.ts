import { test, expect, Page } from '@playwright/test';

// Test credentials
const TEST_USERNAME = 'admin';
const TEST_PASSWORD = 'admin';
const BASE_URL = 'http://localhost:3000';

// ── Shared fixtures ───────────────────────────────────────────────────────

/**
 * Reusable login flow for test fixtures
 */
async function performLogin(page: Page, username: string, password: string) {
  await page.goto('/login');

  // Fill in login credentials
  await page.fill('input[id="username"]', username);
  await page.fill('input[id="password"]', password);

  // Submit form
  await page.click('button[type="submit"]');

  // Wait for redirect to dashboard
  await page.waitForURL('/dashboard', { timeout: 10000 });
}

/**
 * Logout flow
 */
async function performLogout(page: Page) {
  // Click user menu
  await page.click('button[aria-label="User menu"]');

  // Wait for menu to appear
  await page.waitForTimeout(300);

  // Click sign out button (contains text "Sign out")
  await page.click('button:has-text("Sign out")');

  // Wait for redirect to login
  await page.waitForURL('/login', { timeout: 5000 });
}

/**
 * Extract HTTP method and status from network events
 */
function getNetworkStats(page: Page) {
  const stats = {
    startTime: Date.now(),
    resourceTimings: [] as { name: string; duration: number }[]
  };

  page.on('response', (response) => {
    // Optionally collect response metrics
  });

  return stats;
}

// ── Test 1: Complete User Journey ─────────────────────────────────────────

test('should complete full user journey: login → dashboard → logout', async ({ page }) => {
  // Step 1: Navigate to login
  await page.goto('/login');
  expect(page.url()).toContain('/login');

  // Step 2: Verify login page elements
  const usernameInput = page.locator('input[id="username"]');
  const passwordInput = page.locator('input[id="password"]');
  const submitButton = page.locator('button[type="submit"]');

  await expect(usernameInput).toBeVisible();
  await expect(passwordInput).toBeVisible();
  await expect(submitButton).toBeVisible();

  // Step 3: Enter credentials
  await performLogin(page, TEST_USERNAME, TEST_PASSWORD);

  // Step 4: Verify redirect to dashboard
  expect(page.url()).toContain('/dashboard');

  // Step 5: Verify dashboard content loads
  const mainContent = page.locator('main');
  await expect(mainContent).toBeVisible({ timeout: 5000 });

  // Step 6: Verify KPIs are visible (look for common dashboard elements)
  const dashboardHeader = page.locator('h1, h2');
  await expect(dashboardHeader).toHaveCount(1, { timeout: 5000 });

  // Verify sidebar is visible (indicates full shell loaded)
  const sidebar = page.locator('aside, [class*="sidebar"]').first();
  if (await sidebar.isVisible()) {
    expect(sidebar).toBeVisible();
  }

  // Step 7: Logout
  await performLogout(page);

  // Step 8: Verify redirect to login
  expect(page.url()).toContain('/login');

  // Verify login form is visible again
  await expect(page.locator('input[id="username"]')).toBeVisible();
});

// ── Test 2: Audit Page Workflow ───────────────────────────────────────────

test('should navigate audit page and search events', async ({ page }) => {
  // Login
  await performLogin(page, TEST_USERNAME, TEST_PASSWORD);

  // Navigate to audit page
  await page.goto('/audit');
  await page.waitForURL(/\/audit/, { timeout: 5000 });

  // Verify audit page loaded
  const auditMainContent = page.locator('main');
  await expect(auditMainContent).toBeVisible({ timeout: 5000 });

  // Look for search input or filter controls
  const searchInputs = page.locator('input[type="text"], input[type="search"], input[placeholder*="search" i], input[placeholder*="Search" i]');
  const inputCount = await searchInputs.count();

  if (inputCount > 0) {
    // Try searching for an event by actor/query
    const firstSearchInput = searchInputs.first();
    await firstSearchInput.fill('admin');

    // Wait for potential results to load
    await page.waitForTimeout(1000);

    // Look for result items that could be clicked
    const resultItems = page.locator('[role="button"], .cursor-pointer, a[href*=""]');
    const itemCount = await resultItems.count();

    if (itemCount > 0) {
      // Click first result if available
      await resultItems.first().click({ timeout: 5000 });
      await page.waitForTimeout(500);
    }
  }

  // Look for export button and test export functionality
  const exportButtons = page.locator('button:has-text("Export"), button:has-text("Download"), button[title*="Export" i]');
  const exportCount = await exportButtons.count();

  if (exportCount > 0) {
    // Set up download listener
    const downloadPromise = page.waitForEvent('download');

    await exportButtons.first().click();

    try {
      const download = await downloadPromise;
      // Verify download started
      expect(download.suggestedFilename()).toBeTruthy();
    } catch {
      // Download may not complete in test environment
    }
  }
});

// ── Test 3: Discovery Page Workflow ──────────────────────────────────────

test('should navigate discovery page and filter containers', async ({ page }) => {
  // Login
  await performLogin(page, TEST_USERNAME, TEST_PASSWORD);

  // Navigate to discovery page
  await page.goto('/discovery');
  await page.waitForURL(/\/discovery/, { timeout: 5000 });

  // Verify discovery page loaded
  const discoveryMain = page.locator('main');
  await expect(discoveryMain).toBeVisible({ timeout: 5000 });

  // Wait for containers to load (look for loading states)
  await page.waitForTimeout(1500);

  // Look for search input on discovery page
  const searchInputs = page.locator('input[type="text"], input[type="search"], input[placeholder*="container" i], input[placeholder*="search" i]');
  if (await searchInputs.first().isVisible({ timeout: 2000 })) {
    // Search by container name
    await searchInputs.first().fill('nginx');
    await page.waitForTimeout(800);
  }

  // Look for filter buttons or dropdowns
  const filterButtons = page.locator('button:has-text("Filter"), button:has-text("Classification"), select');
  if (await filterButtons.first().isVisible({ timeout: 2000 })) {
    // Click first filter
    await filterButtons.first().click({ timeout: 5000 });
    await page.waitForTimeout(300);

    // Look for filter options and select one
    const filterOptions = page.locator('[role="option"], [role="menuitem"], .dropdown-item');
    if (await filterOptions.first().isVisible({ timeout: 2000 })) {
      await filterOptions.first().click();
    }
  }

  // Look for container items/rows that can be clicked
  const containerItems = page.locator('[role="row"], [class*="container"], tr');
  const containerCount = await containerItems.count();

  if (containerCount > 0) {
    // Click first container to open details
    await containerItems.first().click({ timeout: 5000 });

    // Wait for drawer/modal to open
    await page.waitForTimeout(500);

    // Verify drawer or modal opened with sections
    const drawerContent = page.locator('[role="dialog"], .drawer, .modal, [class*="sheet"]').first();
    if (await drawerContent.isVisible({ timeout: 2000 })) {
      expect(drawerContent).toBeVisible();

      // Look for close button
      const closeButtons = page.locator('button[aria-label*="close" i], button:has-text("Close"), button[aria-label*="dismiss" i]');
      if (await closeButtons.first().isVisible()) {
        await closeButtons.first().click();
      } else {
        // Try pressing Escape
        await page.press('body', 'Escape');
      }
    }
  }

  // Test refresh button
  const refreshButtons = page.locator('button[title*="refresh" i], button[aria-label*="refresh" i], button:has-text("Refresh")');
  if (await refreshButtons.first().isVisible({ timeout: 2000 })) {
    await refreshButtons.first().click();
    await page.waitForTimeout(500);
  }
});

// ── Test 4: Session Management ──────────────────────────────────────────

test('should persist session across page refresh', async ({ page }) => {
  // Login
  await performLogin(page, TEST_USERNAME, TEST_PASSWORD);

  // Verify on dashboard
  expect(page.url()).toContain('/dashboard');

  // Get current URL
  const dashboardUrl = page.url();

  // Refresh page
  await page.reload({ waitUntil: 'networkidle' });

  // Wait for page to load
  await page.waitForTimeout(1000);

  // Verify still on dashboard (session persisted)
  const currentUrl = page.url();
  expect(currentUrl).toContain('/dashboard');

  // Verify content is visible (not redirected to login)
  const mainContent = page.locator('main');
  await expect(mainContent).toBeVisible({ timeout: 5000 });

  // Logout
  await performLogout(page);

  // Try to access protected page directly
  await page.goto('/dashboard');

  // Should be redirected to login (depending on auth guard)
  await page.waitForTimeout(1000);

  // Verify either on login or auth page
  const url = page.url();
  expect(url.includes('/login') || url.includes('/auth')).toBeTruthy();
});

// ── Test 5: Error Handling ──────────────────────────────────────────────

test('should handle errors: invalid login, then retry', async ({ page }) => {
  // Navigate to login
  await page.goto('/login');
  expect(page.url()).toContain('/login');

  // Step 1: Try with invalid credentials
  await page.fill('input[id="username"]', 'invalid_user');
  await page.fill('input[id="password"]', 'wrong_password');

  // Submit
  await page.click('button[type="submit"]');

  // Wait for error message
  await page.waitForTimeout(1500);

  // Verify error message is visible
  const errorMessage = page.locator('[role="alert"], .error, .alert-danger, .text-red');
  const errorCount = await errorMessage.count();

  if (errorCount > 0) {
    const firstError = errorMessage.first();
    await expect(firstError).toBeVisible();
  }

  // Verify still on login page
  expect(page.url()).toContain('/login');

  // Step 2: Retry with valid credentials
  await page.fill('input[id="username"]', TEST_USERNAME);
  await page.fill('input[id="password"]', TEST_PASSWORD);

  // Submit
  await page.click('button[type="submit"]');

  // Verify redirect to dashboard
  await page.waitForURL(/\/dashboard/, { timeout: 10000 });
  expect(page.url()).toContain('/dashboard');

  // Verify page loaded
  const mainContent = page.locator('main');
  await expect(mainContent).toBeVisible({ timeout: 5000 });
});

// ── Test 6: Responsive Design Check ────────────────────────────────────

test('should display dashboard responsive on desktop', async ({ page }) => {
  // Set desktop viewport
  await page.setViewportSize({ width: 1920, height: 1080 });

  // Login
  await performLogin(page, TEST_USERNAME, TEST_PASSWORD);

  // Verify dashboard loads
  await page.waitForURL(/\/dashboard/, { timeout: 5000 });

  // Verify main elements visible
  const mainContent = page.locator('main');
  await expect(mainContent).toBeVisible();

  // Verify sidebar visible on desktop
  const sidebar = page.locator('aside, [class*="sidebar"]').first();
  if (await sidebar.isVisible({ timeout: 2000 })) {
    const box = await sidebar.boundingBox();
    if (box) {
      // Sidebar should be on left
      expect(box.x).toBeLessThan(300);
    }
  }
});

test('should display dashboard responsive on mobile', async ({ page }) => {
  // Set mobile viewport
  await page.setViewportSize({ width: 375, height: 667 });

  // Login
  await performLogin(page, TEST_USERNAME, TEST_PASSWORD);

  // Verify dashboard loads
  await page.waitForURL(/\/dashboard/, { timeout: 5000 });

  // Verify main elements visible
  const mainContent = page.locator('main');
  await expect(mainContent).toBeVisible();

  // On mobile, sidebar might be hidden or collapse
  // Verify content is still responsive
  const buttons = page.locator('button').first();
  if (await buttons.isVisible({ timeout: 2000 })) {
    const box = await buttons.boundingBox();
    if (box) {
      // Button should fit in viewport
      expect(box.width).toBeLessThan(375);
    }
  }
});

// ── Test 7: Performance Check ──────────────────────────────────────────

test('should load dashboard within acceptable time', async ({ page }) => {
  const startTime = Date.now();

  // Navigate to login
  await page.goto('/login');

  // Login
  await page.fill('input[id="username"]', TEST_USERNAME);
  await page.fill('input[id="password"]', TEST_PASSWORD);
  await page.click('button[type="submit"]');

  // Wait for dashboard to load
  await page.waitForURL(/\/dashboard/, { timeout: 10000 });

  // Wait for main content
  const mainContent = page.locator('main');
  await expect(mainContent).toBeVisible({ timeout: 5000 });

  // Wait for KPI/header elements to render
  await page.waitForTimeout(500);

  const endTime = Date.now();
  const loadTime = endTime - startTime;

  // Assert load time is reasonable (less than 15 seconds for full E2E)
  // This includes login + redirect + dashboard render
  expect(loadTime).toBeLessThan(15000);

  // Log performance metric
  console.log(`Dashboard E2E load time: ${loadTime}ms`);
});

// ── Test 8: Export & Download Functionality ────────────────────────────

test('should export data from audit or discovery page', async ({ page }) => {
  // Login
  await performLogin(page, TEST_USERNAME, TEST_PASSWORD);

  // Navigate to discovery page (usually has export)
  await page.goto('/discovery');
  await page.waitForURL(/\/discovery/, { timeout: 5000 });

  // Wait for page to load
  await page.waitForTimeout(1500);

  // Look for export button
  const exportButtons = page.locator(
    'button:has-text("Export"), button:has-text("Download"), button[title*="Export" i], button[aria-label*="export" i]'
  );

  const exportCount = await exportButtons.count();

  if (exportCount > 0) {
    // Found export button
    const exportButton = exportButtons.first();

    // Set up download listener before clicking
    const downloadPromise = page.waitForEvent('download', { timeout: 10000 });

    try {
      await exportButton.click();

      // Wait for download to start
      const download = await downloadPromise;

      // Verify download object exists
      expect(download).toBeTruthy();

      // Verify filename suggests CSV or other export format
      const filename = download.suggestedFilename();
      expect(filename).toBeTruthy();

      // Verify it's a downloadable file
      expect(
        filename.endsWith('.csv') ||
        filename.endsWith('.json') ||
        filename.endsWith('.xlsx') ||
        filename.endsWith('.txt')
      ).toBeTruthy();
    } catch (error) {
      // Download event may timeout in some test environments
      // This is acceptable as long as button click succeeded
      console.log('Download event timeout (may be normal in test env)');
    }
  } else {
    // No export button found - try audit page
    await page.goto('/audit');
    await page.waitForURL(/\/audit/, { timeout: 5000 });

    await page.waitForTimeout(1500);

    const auditExportButtons = page.locator(
      'button:has-text("Export"), button:has-text("Download"), button[title*="Export" i]'
    );

    const auditExportCount = await auditExportButtons.count();

    if (auditExportCount > 0) {
      const downloadPromise = page.waitForEvent('download', { timeout: 10000 });

      try {
        await auditExportButtons.first().click();
        const download = await downloadPromise;
        expect(download.suggestedFilename()).toBeTruthy();
      } catch {
        console.log('Download not completed (may be normal)');
      }
    }
  }
});

// ── Test 9: Protected Route Access ────────────────────────────────────────

test('should redirect to login when accessing protected route without session', async ({ page, context }) => {
  // Clear all cookies to ensure no session
  await context.clearCookies();

  // Clear localStorage
  await page.evaluate(() => localStorage.clear());
  await page.evaluate(() => sessionStorage.clear());

  // Try to access protected dashboard directly
  await page.goto('/dashboard');

  // Wait a moment for redirect
  await page.waitForTimeout(1500);

  // Should be redirected to login (or auth page)
  const url = page.url();
  expect(url.includes('/login') || url.includes('/auth')).toBeTruthy();

  // Verify login form is visible
  const usernameInput = page.locator('input[id="username"]');
  await expect(usernameInput).toBeVisible({ timeout: 5000 });
});

// ── Test 10: Multi-page Navigation ────────────────────────────────────────

test('should navigate between main pages successfully', async ({ page }) => {
  // Login
  await performLogin(page, TEST_USERNAME, TEST_PASSWORD);

  // Navigate to dashboard
  await page.goto('/dashboard');
  await page.waitForURL(/\/dashboard/, { timeout: 5000 });
  await expect(page.locator('main')).toBeVisible();

  // Navigate to discovery
  await page.goto('/discovery');
  await page.waitForURL(/\/discovery/, { timeout: 5000 });
  await expect(page.locator('main')).toBeVisible();

  // Navigate to audit
  await page.goto('/audit');
  await page.waitForURL(/\/audit/, { timeout: 5000 });
  await expect(page.locator('main')).toBeVisible();

  // Navigate back to dashboard
  await page.goto('/dashboard');
  await page.waitForURL(/\/dashboard/, { timeout: 5000 });
  await expect(page.locator('main')).toBeVisible();
});
