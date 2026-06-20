# E2E Tests - User Journey

End-to-end tests using Playwright to validate complete user workflows across the DSO web application.

## Overview

This test suite covers 10 comprehensive user journeys:

1. **Complete User Journey** - Login → Dashboard → Logout
2. **Audit Page Workflow** - Login → Navigate audit → Search → Export
3. **Discovery Page Workflow** - Login → Navigate discovery → Filter containers → Open details
4. **Session Management** - Verify session persists across page refresh
5. **Error Handling** - Invalid login → Retry with valid credentials
6. **Responsive Design (Desktop)** - Verify desktop layout (1920x1080)
7. **Responsive Design (Mobile)** - Verify mobile layout (375x667)
8. **Performance Check** - Measure page load time
9. **Export & Download** - Test export functionality
10. **Protected Route Access** - Verify redirect to login without session
11. **Multi-page Navigation** - Navigate between dashboard, discovery, audit

## Running Tests

### Prerequisites

1. Ensure dev server is running:
   ```bash
   npm run dev
   ```
   The server should be accessible at `http://localhost:3000`

2. Valid test credentials must exist:
   - Username: `admin`
   - Password: `admin`

### Run All Tests

```bash
npm run test:e2e
```

### Run Specific Test

```bash
npm run test:e2e -- --grep "Complete User Journey"
```

### Run Tests in UI Mode (Recommended for Development)

```bash
npm run test:e2e -- --ui
```

This opens an interactive Playwright Inspector where you can:
- Watch tests run step-by-step
- Pause and inspect elements
- View network requests
- See detailed error information

### Run Tests in Headed Mode (See Browser)

```bash
npm run test:e2e -- --headed
```

### Run Tests in Debug Mode

```bash
npm run test:e2e -- --debug
```

### Run Specific File

```bash
npm run test:e2e tests/e2e/user-journey.spec.ts
```

### Run Tests on Specific Browser

```bash
npm run test:e2e -- --project chromium
npm run test:e2e -- --project firefox
npm run test:e2e -- --project webkit
```

## Configuration

Tests are configured in `playwright.config.ts`:

- **Base URL**: `http://localhost:3000`
- **Timeout**: 30 seconds per test
- **Retries**: 
  - CI: 2 retries
  - Local: No retries
- **Parallel**: 
  - CI: Sequential (1 worker)
  - Local: Parallel (4 workers)
- **Screenshots**: Only on failure
- **Traces**: On first retry

## Test Fixtures

Shared test utilities are available in `fixtures.ts`:

```typescript
import { test, TEST_CONFIG, waitForNetworkIdle } from './fixtures';

test('my test', async ({ authenticatedPage }) => {
  await authenticatedPage.goto('/dashboard');
});
```

### Available Helpers

- `authenticatedPage` - Pre-authenticated page fixture
- `waitForNetworkIdle()` - Wait for network to settle
- `measureNavigationTime()` - Measure page load time
- `isElementInViewport()` - Check if element is visible
- `setupConsoleCapture()` - Capture console logs
- `captureNetworkErrors()` - Collect HTTP errors

## Test Structure

Each test follows this pattern:

1. **Setup** - Authenticate and navigate to page
2. **Action** - Simulate user interactions
3. **Assertion** - Verify expected behavior
4. **Cleanup** - Logout or close resources

## Best Practices

### Do
- ✓ Use `waitForURL()` instead of fixed delays
- ✓ Verify element visibility before interaction
- ✓ Use meaningful assertion messages
- ✓ Keep tests independent (no shared state)
- ✓ Use page.waitForTimeout() sparingly
- ✓ Test happy path workflows

### Don't
- ✗ Don't use `page.goto()` without waiting for navigation
- ✗ Don't rely on timing instead of waiting for elements
- ✗ Don't modify test data within tests (read-only)
- ✗ Don't test implementation details
- ✗ Don't create large test fixtures

## Debugging

### View Test Report

After tests run, view the HTML report:

```bash
npx playwright show-report
```

### Inspect Failed Test

```bash
npm run test:e2e -- --last-failed
```

### View Network Activity

Tests capture network requests. Check in test report for detailed network tabs.

### View Screenshots

Failed tests automatically capture screenshots at `./test-results/`

## Performance Benchmarks

Expected performance metrics:

- Login + Dashboard: < 15 seconds
- Page navigation: < 5 seconds
- Element visibility: < 2 seconds
- Export functionality: < 10 seconds

## Troubleshooting

### Tests Timeout
- Verify dev server is running
- Check network connectivity
- Increase timeout in playwright.config.ts

### "element not found" error
- Element may be hidden or out of viewport
- Check CSS selectors are correct
- Verify page is fully loaded before interaction

### Download not detected
- Downloads may not work in all test environments
- This is expected and handled gracefully

### Session expires mid-test
- Increase auth token TTL in DSO agent
- Verify localhost:3000 is accessible

## CI/CD Integration

In CI environments:

```bash
GITHUB_ACTIONS=true npm run test:e2e
```

Tests run with:
- Chromium only (faster)
- 1 worker (isolated)
- 2 retries (flaky test resilience)
- HTML report generation

## Adding New Tests

1. Create test function in `user-journey.spec.ts`
2. Use existing helpers for common flows
3. Follow naming convention: `should [action] [expectation]`
4. Add comments for complex assertions
5. Update this README with test description

Example:

```typescript
test('should perform new workflow', async ({ page }) => {
  // Login
  await performLogin(page, TEST_USERNAME, TEST_PASSWORD);

  // Navigate to page
  await page.goto('/new-page');
  await page.waitForURL(/\/new-page/, { timeout: 5000 });

  // Verify page loaded
  const content = page.locator('main');
  await expect(content).toBeVisible();

  // Interact with element
  await page.click('button:has-text("Action")');

  // Verify result
  const result = page.locator('data-testid=result');
  await expect(result).toContainText('Expected');
});
```

## Performance Analysis

To profile test execution:

```bash
npm run test:e2e -- --trace on
```

Then view traces in Playwright Inspector.

## Related Files

- `playwright.config.ts` - Test configuration
- `fixtures.ts` - Shared test utilities
- `user-journey.spec.ts` - Main test suite
