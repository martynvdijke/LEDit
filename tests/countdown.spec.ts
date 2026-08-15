import { test, expect, Page } from '@playwright/test';

const CD_NAME = 'PW Ship Window';

async function seedCleanState(page: Page): Promise<void> {
  await page.request.post('/admin/settings', {
    form: { timeout: '5', width: '64', height: '64' },
  });
  // Remove any leftover countdowns.
  await page.goto('/admin/');
  let del = page.locator('tr:has-text("Countdown") form[action$="/delete"] button');
  while ((await del.count()) > 0) {
    await del.first().click();
    await page.waitForLoadState('networkidle');
    await page.goto('/admin/');
    del = page.locator('tr:has-text("Countdown") form[action$="/delete"] button');
  }
}

async function createCountdown(page: Page, name: string, target: string): Promise<void> {
  await page.goto('/admin/countdowns/new');
  await page.fill('#name', name);
  await page.fill('#target_time', target);
  await page.fill('#label', 'Launch');
  await page.locator('#enabled').check();
  await page.click('button[type="submit"]');
  await expect(page).toHaveURL(/\/admin\/$/);
}

test.describe('Countdown (ambience modes)', () => {
  test('CRUD flow + live preview (3.4)', async ({ page }) => {
    await seedCleanState(page);

    // 3.4 — create via the admin form.
    const future = new Date(Date.now() + 2 * 60 * 60 * 1000);
    const target = `${future.toISOString().slice(0, 16)}`; // YYYY-MM-DDTHH:MM
    await createCountdown(page, CD_NAME, target);

    // Appears in the dashboard sources table as a Countdown row.
    const row = page.locator(`tr:has-text("${CD_NAME}")`);
    await expect(row).toBeVisible();
    await expect(row).toContainText('Countdown');

    // 3.4 — edit page is prefilled.
    await page.locator(`tr:has-text("${CD_NAME}") a:has-text("Edit")`).click();
    await expect(page).toHaveURL(/\/admin\/countdowns\/\d+\/edit/);
    await expect(page.locator('#name')).toHaveValue(CD_NAME);
    await expect(page.locator('#label')).toHaveValue('Launch');
    await expect(page.locator('#enabled')).toBeChecked();

    // 3.4 — live preview fetches a rendered PNG (target + label visible on wall).
    await expect(page.locator('#live-preview-img')).toHaveAttribute('src', /^(blob:|data:image\/png)/, { timeout: 15000 });

    // Sidebar links to the new form.
    await expect(page.locator('.sidebar a[href="/admin/countdowns/new"]')).toHaveCount(1);

    // Delete via the edit page's delete form.
    await page.click('button:has-text("Delete")');
    await expect(page).toHaveURL(/\/admin\/$/);
    await expect(page.locator(`tr:has-text("${CD_NAME}")`)).toHaveCount(0);
  });
});
