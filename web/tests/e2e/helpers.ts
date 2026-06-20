import { Page, expect } from '@playwright/test';

/**
 * Common selectors used across tests
 */
export const SELECTORS = {
  // Auth
  usernameInput: 'input[id="username"]',
  passwordInput: 'input[id="password"]',
  loginButton: 'button[type="submit"]',
  userMenu: 'button[aria-label="User menu"]',
  signOutButton: 'button:has-text("Sign out")',

  // Navigation
  sidebar: 'aside, [class*="sidebar"]',
  topbar: 'header, [class*="topbar"]',
  mainContent: 'main',

  // Common Elements
  errorAlert: '[role="alert"], .error, .alert-danger, .text-red',
  searchInput: 'input[type="text"], input[type="search"], input[placeholder*="search" i]',
  filterButton: 'button:has-text("Filter"), button:has-text("Classification"), select',
  exportButton: 'button:has-text("Export"), button:has-text("Download"), button[title*="Export" i]',
  refreshButton: 'button[title*="refresh" i], button[aria-label*="refresh" i], button:has-text("Refresh")',
  closeButton: 'button[aria-label*="close" i], button:has-text("Close"), button[aria-label*="dismiss" i]',
};

/**
 * URL patterns for navigation validation
 */
export const ROUTES = {
  login: '/login',
  dashboard: '/dashboard',
  discovery: '/discovery',
  audit: '/audit',
  profile: '/profile',
  settings: '/settings',
};

/**
 * Wait configuration for different operations
 */
export const WAITS = {
  form_submit: 1500,
  page_load: 5000,
  modal_open: 500,
  element_render: 2000,
  network_settle: 1000,
  debounce_search: 800,
};

/**
 * Verify login page is displayed
 */
export async function verifyLoginPage(page: Page) {
  await expect(page.locator(SELECTORS.usernameInput)).toBeVisible();
  await expect(page.locator(SELECTORS.passwordInput)).toBeVisible();
  await expect(page.locator(SELECTORS.loginButton)).toBeVisible();
}

/**
 * Verify dashboard page is displayed
 */
export async function verifyDashboardPage(page: Page) {
  const mainContent = page.locator(SELECTORS.mainContent);
  await expect(mainContent).toBeVisible({ timeout: WAITS.page_load });
}

/**
 * Verify specific page by URL pattern
 */
export async function verifyPageRoute(page: Page, route: string, timeout = 5000) {
  await page.waitForURL(new RegExp(route), { timeout });
  expect(page.url()).toContain(route);
}

/**
 * Check if element exists and is visible
 */
export async function isElementVisible(page: Page, selector: string, timeout = 2000): Promise<boolean> {
  try {
    await page.locator(selector).isVisible({ timeout });
    return true;
  } catch {
    return false;
  }
}

/**
 * Get all text content from a page section
 */
export async function getPageText(page: Page, selector?: string): Promise<string> {
  if (selector) {
    return await page.locator(selector).textContent({ timeout: 5000 }) || '';
  }
  return await page.textContent('body') || '';
}

/**
 * Count visible elements matching selector
 */
export async function countElements(page: Page, selector: string): Promise<number> {
  return await page.locator(selector).count();
}

/**
 * Wait for element to appear and click it
 */
export async function waitAndClick(page: Page, selector: string, timeout = 5000) {
  const element = page.locator(selector);
  await element.waitFor({ state: 'visible', timeout });
  await element.click();
}

/**
 * Wait for element to appear and fill it with text
 */
export async function waitAndFill(page: Page, selector: string, text: string, timeout = 5000) {
  const element = page.locator(selector);
  await element.waitFor({ state: 'visible', timeout });
  await element.fill(text);
}

/**
 * Verify error message is visible
 */
export async function verifyErrorMessage(page: Page, expectedText?: string): Promise<boolean> {
  const errorLocator = page.locator(SELECTORS.errorAlert);

  try {
    await errorLocator.isVisible({ timeout: WAITS.element_render });

    if (expectedText) {
      const actualText = await errorLocator.textContent();
      return actualText?.includes(expectedText) || false;
    }

    return true;
  } catch {
    return false;
  }
}

/**
 * Perform complete login flow
 */
export async function performCompleteLogin(
  page: Page,
  username: string,
  password: string,
  shouldSucceed = true
) {
  // Navigate to login
  await page.goto(ROUTES.login);
  await verifyLoginPage(page);

  // Fill credentials
  await page.fill(SELECTORS.usernameInput, username);
  await page.fill(SELECTORS.passwordInput, password);

  // Submit
  await page.click(SELECTORS.loginButton);

  if (shouldSucceed) {
    // Wait for redirect to dashboard
    await page.waitForURL(new RegExp(ROUTES.dashboard), { timeout: WAITS.page_load });
    await verifyDashboardPage(page);
  } else {
    // Wait for error message
    await page.waitForTimeout(WAITS.form_submit);
    const hasError = await verifyErrorMessage(page);
    return hasError;
  }

  return true;
}

/**
 * Perform logout flow
 */
export async function performCompleteLogout(page: Page) {
  // Click user menu
  await waitAndClick(page, SELECTORS.userMenu, WAITS.element_render);

  // Wait for menu animation
  await page.waitForTimeout(WAITS.modal_open);

  // Click sign out
  await waitAndClick(page, SELECTORS.signOutButton, WAITS.element_render);

  // Wait for redirect to login
  await page.waitForURL(new RegExp(ROUTES.login), { timeout: WAITS.page_load });
  await verifyLoginPage(page);
}

/**
 * Navigate to a specific route and verify
 */
export async function navigateToPage(page: Page, route: string) {
  await page.goto(route);
  await verifyPageRoute(page, route.replace(/^\//, ''));
  await verifyDashboardPage(page);
}

/**
 * Search for something on current page
 */
export async function performSearch(page: Page, query: string) {
  const searchInput = page.locator(SELECTORS.searchInput).first();

  if (await isElementVisible(page, SELECTORS.searchInput)) {
    await searchInput.fill(query);
    await page.waitForTimeout(WAITS.debounce_search);
    return true;
  }

  return false;
}

/**
 * Apply a filter on the page
 */
export async function applyFilter(page: Page) {
  const filterButton = page.locator(SELECTORS.filterButton).first();

  if (await isElementVisible(page, SELECTORS.filterButton)) {
    await filterButton.click();
    await page.waitForTimeout(WAITS.modal_open);

    // Try to select first option
    const options = page.locator('[role="option"], [role="menuitem"], .dropdown-item');
    if ((await options.count()) > 0) {
      await options.first().click();
      return true;
    }
  }

  return false;
}

/**
 * Click export/download button
 */
export async function clickExportButton(page: Page): Promise<boolean> {
  const exportButton = page.locator(SELECTORS.exportButton).first();

  if (await isElementVisible(page, SELECTORS.exportButton)) {
    await exportButton.click();
    return true;
  }

  return false;
}

/**
 * Click refresh button
 */
export async function clickRefreshButton(page: Page): Promise<boolean> {
  const refreshButton = page.locator(SELECTORS.refreshButton).first();

  if (await isElementVisible(page, SELECTORS.refreshButton)) {
    await refreshButton.click();
    await page.waitForTimeout(WAITS.network_settle);
    return true;
  }

  return false;
}

/**
 * Close modal/drawer
 */
export async function closeModal(page: Page) {
  const closeButton = page.locator(SELECTORS.closeButton).first();

  if (await isElementVisible(page, SELECTORS.closeButton)) {
    await closeButton.click();
  } else {
    // Try pressing Escape
    await page.press('body', 'Escape');
  }

  await page.waitForTimeout(WAITS.modal_open);
}

/**
 * Verify page is fully loaded
 */
export async function waitForPageReady(page: Page, timeout = 5000) {
  // Wait for main content
  await page.locator(SELECTORS.mainContent).waitFor({ state: 'visible', timeout });

  // Wait for network idle
  try {
    await page.waitForLoadState('networkidle', { timeout: 2000 });
  } catch {
    // Network idle timeout is acceptable
  }

  // Wait for any animations to complete
  await page.waitForTimeout(500);
}

/**
 * Get all console errors during test
 */
export function setupErrorCapture(page: Page): string[] {
  const errors: string[] = [];

  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      errors.push(msg.text());
    }
  });

  page.on('pageerror', (error) => {
    errors.push(error.message);
  });

  return errors;
}

/**
 * Verify accessibility attributes
 */
export async function verifyAccessibility(page: Page, selector: string) {
  const element = page.locator(selector).first();

  if (await element.isVisible()) {
    // Check for aria-label or text content
    const ariaLabel = await element.getAttribute('aria-label');
    const text = await element.textContent();

    return !!(ariaLabel || text?.trim());
  }

  return false;
}

/**
 * Set viewport size
 */
export async function setViewportSize(page: Page, width: number, height: number) {
  await page.setViewportSize({ width, height });
  await page.waitForTimeout(500);
}

/**
 * Check if sidebar is visible (typically means authenticated)
 */
export async function isSidebarVisible(page: Page): Promise<boolean> {
  return await isElementVisible(page, SELECTORS.sidebar);
}
