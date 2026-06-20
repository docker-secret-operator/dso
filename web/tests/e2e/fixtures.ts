import { test as base, Page } from '@playwright/test';

// Test environment configuration
export const TEST_CONFIG = {
  baseURL: 'http://localhost:3000',
  username: 'admin',
  password: 'admin',
  timeouts: {
    navigation: 10000,
    element: 5000,
    action: 2000,
    debounce: 500,
  },
};

/**
 * Extended test fixtures with authentication
 */
export const test = base.extend<{
  authenticatedPage: Page;
}>({
  authenticatedPage: async ({ page }, use) => {
    // Perform login before test
    await page.goto('/login');
    await page.fill('input[id="username"]', TEST_CONFIG.username);
    await page.fill('input[id="password"]', TEST_CONFIG.password);
    await page.click('button[type="submit"]');

    // Wait for redirect to dashboard
    await page.waitForURL('/dashboard', { timeout: TEST_CONFIG.timeouts.navigation });

    // Use the authenticated page in the test
    await use(page);

    // Cleanup after test
    await page.close();
  },
});

/**
 * Helper to wait for network idle
 */
export async function waitForNetworkIdle(page: Page, timeout = 2000): Promise<void> {
  try {
    await page.waitForLoadState('networkidle', { timeout });
  } catch {
    // Network idle timeout is acceptable in real-world testing
    await new Promise(resolve => setTimeout(resolve, 500));
  }
}

/**
 * Helper to get HTTP response time
 */
export async function measureNavigationTime(page: Page, url: string): Promise<number> {
  const startTime = Date.now();

  try {
    await page.goto(url, { waitUntil: 'networkidle', timeout: 10000 });
  } catch {
    // Continue if navigation times out
  }

  return Date.now() - startTime;
}

/**
 * Helper to check if element is in viewport
 */
export async function isElementInViewport(page: Page, selector: string): Promise<boolean> {
  return page.evaluate((sel) => {
    const element = document.querySelector(sel);
    if (!element) return false;

    const rect = element.getBoundingClientRect();
    return (
      rect.top >= 0 &&
      rect.left >= 0 &&
      rect.bottom <= window.innerHeight &&
      rect.right <= window.innerWidth
    );
  }, selector);
}

/**
 * Helper to collect console messages during test
 */
export function setupConsoleCapture(page: Page): { messages: string[]; errors: string[] } {
  const messages: string[] = [];
  const errors: string[] = [];

  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      errors.push(msg.text());
    } else if (msg.type() === 'log') {
      messages.push(msg.text());
    }
  });

  return { messages, errors };
}

/**
 * Helper to verify no network errors
 */
export async function captureNetworkErrors(page: Page): Promise<Array<{ url: string; status: number }>> {
  const errors: Array<{ url: string; status: number }> = [];

  page.on('response', (response) => {
    if (response.status() >= 400) {
      errors.push({
        url: response.url(),
        status: response.status(),
      });
    }
  });

  return errors;
}

/**
 * Helper to take screenshot on failure (automatically done by Playwright config, but useful for manual)
 */
export async function takeScreenshot(page: Page, name: string): Promise<void> {
  await page.screenshot({ path: `./tests/e2e/screenshots/${name}.png` });
}
