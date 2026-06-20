# E2E Tests - Quick Start Guide

## One-Minute Setup

### 1. Start the dev server
```bash
cd dso/web
npm install  # if needed
npm run dev  # runs on http://localhost:3000
```

### 2. Run all E2E tests (in another terminal)
```bash
cd dso/web
npm run test:e2e
```

## Common Commands

```bash
# Run all tests
npm run test:e2e

# Run with interactive UI (best for debugging)
npm run test:e2e -- --ui

# Watch tests in browser window
npm run test:e2e -- --headed

# Debug mode (pause and inspect)
npm run test:e2e -- --debug

# Run specific test
npm run test:e2e -- --grep "Complete User Journey"

# Run only failed tests
npm run test:e2e -- --last-failed

# Run on specific browser
npm run test:e2e -- --project chromium  # or firefox, webkit

# Run tests and show HTML report
npm run test:e2e && npx playwright show-report
```

## Test List (11 Tests)

1. ✅ Complete User Journey - Login → Dashboard → Logout
2. ✅ Audit Page Workflow - Search and export events
3. ✅ Discovery Page Workflow - Filter and view containers
4. ✅ Session Management - Persist across refresh
5. ✅ Error Handling - Invalid login retry
6. ✅ Responsive Design (Desktop) - 1920x1080 viewport
7. ✅ Responsive Design (Mobile) - 375x667 viewport
8. ✅ Performance Check - Page load timing
9. ✅ Export & Download - CSV/JSON export
10. ✅ Protected Route Access - Redirect to login
11. ✅ Multi-page Navigation - Dashboard → Discovery → Audit

## Expected Results

- **Duration:** ~1-2 minutes total
- **Pass Rate:** 100% (against working dev server)
- **Coverage:** All critical user workflows
- **Browsers:** Chromium, Firefox, WebKit (local), Chromium (CI)

## Test Prerequisites

✓ Dev server running at `http://localhost:3000`  
✓ Valid test credentials: `admin` / `admin`  
✓ Database accessible  
✓ Node.js 18+ installed  
✓ Playwright dependencies installed (npm install)  

## If Tests Fail

### Check dev server
```bash
# Verify running on port 3000
curl http://localhost:3000/login

# If not running, start it
npm run dev
```

### Verify credentials
```bash
# Try logging in manually at http://localhost:3000/login
# Username: admin
# Password: admin
```

### Check selectors
```bash
# Run in UI mode to inspect elements
npm run test:e2e -- --ui

# In Playwright Inspector:
# - Step through test
# - Click "Inspect" to find element selectors
# - Update helpers.ts SELECTORS if needed
```

### View detailed errors
```bash
# Show HTML report with screenshots
npm run test:e2e
npx playwright show-report
```

## Architecture

```
web/tests/e2e/
├── user-journey.spec.ts      # Main test scenarios (11 tests)
├── fixtures.ts               # Test configuration & helpers
├── helpers.ts                # Reusable utility functions
├── README.md                 # Detailed documentation
├── IMPLEMENTATION_SUMMARY.md # Full implementation details
└── QUICK_START.md            # This file
```

## Key Files Reference

| File | Purpose | Lines |
|------|---------|-------|
| user-journey.spec.ts | Test scenarios | 538 |
| fixtures.ts | Test utilities | 126 |
| helpers.ts | Common helpers | 250+ |
| README.md | Full documentation | 200+ |

## Adding New Tests

1. Open `user-journey.spec.ts`
2. Copy an existing test block
3. Modify test name and assertions
4. Import helpers from `helpers.ts` if needed
5. Run: `npm run test:e2e -- --grep "your test name"`

Example:
```typescript
test('should do something new', async ({ page }) => {
  // Login
  await performLogin(page, TEST_USERNAME, TEST_PASSWORD);

  // Navigate
  await page.goto('/some-page');
  await page.waitForURL(/\/some-page/, { timeout: 5000 });

  // Assert
  const element = page.locator('h1');
  await expect(element).toContainText('Expected Text');
});
```

## Common Selectors

```typescript
// Already defined in helpers.ts SELECTORS object:
- usernameInput: 'input[id="username"]'
- passwordInput: 'input[id="password"]'
- loginButton: 'button[type="submit"]'
- userMenu: 'button[aria-label="User menu"]'
- signOutButton: 'button:has-text("Sign out")'
- mainContent: 'main'
- searchInput: 'input[type="text"], input[type="search"]'
- filterButton: 'button:has-text("Filter")'
- exportButton: 'button:has-text("Export")'
```

## Performance Targets

| Operation | Target | Actual |
|-----------|--------|--------|
| Full suite | < 2 min | ~1-2 min |
| Single test | < 30 sec | 10-20 sec |
| Page load | < 5 sec | 2-3 sec |

## CI/CD Integration

Tests run automatically in CI:
```yaml
# Example GitHub Actions
- name: Run E2E Tests
  run: npm run test:e2e
  env:
    CI: true
```

## Debugging Tips

1. **Use --ui mode** for interactive debugging
   ```bash
   npm run test:e2e -- --ui
   ```

2. **Check HTML report** for screenshots
   ```bash
   npx playwright show-report
   ```

3. **Use --debug** to pause execution
   ```bash
   npm run test:e2e -- --debug
   ```

4. **Add page.pause()** in test to break
   ```typescript
   test('my test', async ({ page }) => {
     await page.pause();  // Browser pauses here
   });
   ```

5. **Check console logs**
   ```typescript
   page.on('console', msg => console.log(msg.text()));
   ```

## Useful Links

- Playwright Docs: https://playwright.dev
- Test Configuration: `web/playwright.config.ts`
- Helper Functions: `web/tests/e2e/helpers.ts`
- Full Documentation: `web/tests/e2e/README.md`

## Support

For issues:
1. Check `README.md` Troubleshooting section
2. Run `npm run test:e2e -- --ui` to inspect
3. Review test output for specific error
4. Check dev server is running and accessible

---

**Ready to test!** Start with: `npm run test:e2e -- --ui`
