import { test, expect, type Page } from './fixtures';

async function seedCleanState(page: Page): Promise<void> {
  await page.request.post('/admin/settings', {
    form: { timeout: '5', width: '64', height: '64' },
  });
}

test.describe('Alert settings (outbound alerting)', () => {
  test('save + per-channel test results (4.4)', async ({ page }) => {
    await seedCleanState(page);

    // Sidebar links to the alert settings page.
    await page.goto('/admin/');
    await expect(page.locator('.sidebar a[href="/admin/settings/alerts"]')).toHaveCount(1);

    // Page renders with default fields.
    await page.goto('/admin/settings/alerts');
    await expect(page.locator('h1')).toContainText('Alert');
    await expect(page.locator('#gotify_enabled')).not.toBeChecked();

    // Save Gotify config pointing at an unreachable server + custom rules.
    await page.locator('#gotify_enabled').check();
    await page.fill('#gotify_url', 'http://127.0.0.1:9');
    await page.fill('#gotify_token', 'pw-token');
    await page.fill('#failure_threshold', '5');
    await page.fill('#cooldown_minutes', '30');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/settings\/alerts$/);

    // Reload: persisted values round-trip.
    await page.reload();
    await expect(page.locator('#gotify_enabled')).toBeChecked();
    await expect(page.locator('#gotify_url')).toHaveValue('http://127.0.0.1:9');
    await expect(page.locator('#failure_threshold')).toHaveValue('5');

    // Test button reports the per-channel failure (Gotify unreachable).
    await page.click('#testAlertsBtn');
    await expect(page.locator('#testResult')).toContainText('gotify', { timeout: 15000 });
    await expect(page.locator('#testResult')).toContainText('failed');
  });
});
