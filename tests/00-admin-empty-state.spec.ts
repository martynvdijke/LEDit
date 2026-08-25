import { test, expect } from './fixtures';

// These tests assert the *unconfigured* dashboard empty state, which only
// exists before any spec seeds GeneralSettings. Playwright runs spec files
// alphabetically on a shared server/database, so the numeric prefix keeps
// this file ahead of every seeding spec.
test.describe('Admin Dashboard (unconfigured)', () => {
  test.use({ autoSeed: false });

  test('should show no settings message when unconfigured', async ({ page }) => {
    await page.goto('/admin/');
    await expect(page.getByText('No settings configured yet')).toBeVisible();
  });

  test('should have settings link in the page', async ({ page }) => {
    await page.goto('/admin/');
    await expect(page.getByRole('link', { name: 'Configure settings' })).toBeVisible();
  });
});
