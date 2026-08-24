import { test, expect } from './fixtures';

test.describe('Adaptive ordering', () => {
  test('analytics shows weights strip and settings adaptive disclosure', async ({ page }) => {
    await page.goto('/admin/settings');
    await expect(page.locator('#ordering_adaptive')).toBeVisible();
    await page.check('#ordering_adaptive');
    await expect(page.locator('#adaptive-disclosure')).toBeVisible();
    await page.check('#ordering_random');
    await expect(page.locator('#adaptive-disclosure')).toBeHidden();
    await page.check('#ordering_adaptive');
    await page.click('button[type="submit"]');
    await page.goto('/admin/analytics');
    await expect(page.locator('#weights-strip')).toBeVisible({ timeout: 5000 });
    await expect(page.locator('#collecting-notice')).toBeVisible();
  });
});
