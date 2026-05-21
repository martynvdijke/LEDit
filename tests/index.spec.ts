import { test, expect } from '@playwright/test';

test.describe('Index / Live Feed', () => {
  test('should load the index page', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('h1')).toContainText('LEDit Live Feed');
  });

  test('should have WebSocket status indicator', async ({ page }) => {
    await page.goto('/');
    const status = page.locator('#status-text');
    await expect(status).toBeVisible();
    const text = await status.textContent() ?? '';
    expect(text.length).toBeGreaterThan(0);
  });

  test('should have media display elements in DOM', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#media-container')).toBeVisible();
    await expect(page.locator('#media-display')).toBeAttached();
    await expect(page.locator('#video-display')).toBeAttached();
  });

  test('should show LEDit branding', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.fs-4')).toContainText('LEDit');
  });

  test('should have sidebar with navigation links', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'Home' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Dashboard' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Settings' })).toBeVisible();
  });
});
