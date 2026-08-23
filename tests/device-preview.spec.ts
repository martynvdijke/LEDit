import { test, expect, type Page } from './fixtures';

const DEVICE_NAME = 'PW Preview Wall';

// Seed GeneralSettings (fresh DB has none until /admin/settings is saved) and
// remove any leftover devices so the "no selector" assertion is deterministic.
async function seedCleanState(page: Page): Promise<void> {
  await page.request.post('/admin/settings', {
    form: { timeout: '5', width: '64', height: '64' },
  });
  await deleteAllDevices(page);
}

async function deleteAllDevices(page: Page): Promise<void> {
  await page.goto('/admin/devices');
  let del = page.locator('form[action$="/delete"] button');
  while ((await del.count()) > 0) {
    await del.first().click();
    await page.waitForLoadState('networkidle');
    del = page.locator('form[action$="/delete"] button');
  }
}

async function createDevice(page: Page, name: string): Promise<void> {
  await page.goto('/admin/devices/new');
  await page.fill('#name', name);
  await page.fill('#ip', '127.0.0.1');
  await page.fill('#width', '32');
  await page.fill('#height', '64');
  await page.fill('#refresh_interval', '2');
  await page.locator('#enabled').check();
  await page.click('button[type="submit"]');
  await expect(page).toHaveURL(/\/admin\/devices$/);
  await expect(page.getByText(name)).toBeVisible();
}

test.describe('Web live preview', () => {
  test('device selector + per-device preview page (2.4, 3.4)', async ({ page }) => {
    await seedCleanState(page);

    // 3.4 — no devices: selector hidden, shared preview still connects.
    await page.goto('/');
    await expect(page.locator('[data-device-select]')).toHaveCount(0);
    await expect(page.locator('#status-text')).toContainText(/Receiving|Connected/, { timeout: 15000 });

    // Create a device, then the selector appears with it listed.
    await createDevice(page, DEVICE_NAME);
    await page.goto('/');
    const select = page.locator('[data-device-select]');
    await expect(select).toBeVisible();
    await expect(select.locator('option')).toHaveCount(2); // shared + device
    // Options in a closed native <select> report as hidden to Playwright, so
    // assert DOM presence (and the round-trip via selectOption below) instead
    // of visibility.
    await expect(select.locator(`option:has-text("${DEVICE_NAME}")`)).toHaveCount(1);

    // 3.4 — switching reconnects to the device-accurate endpoint and frames
    // continue flowing.
    await select.selectOption({ label: `${DEVICE_NAME} (32×64)` });
    await expect(page.locator('#status-text')).toContainText(/Receiving|Connected/, { timeout: 15000 });
    await expect(page.locator('#media-display')).toHaveAttribute('src', /^data:image\/png/, { timeout: 15000 });

    // 2.4 — the per-device preview page shows metadata and streams frames.
    await page.goto('/admin/devices');
    const previewLink = page.locator(`tr:has-text("${DEVICE_NAME}") a:has-text("Preview")`);
    await expect(previewLink).toBeVisible();
    await previewLink.click();
    await expect(page).toHaveURL(/\/admin\/devices\/\d+\/preview/);
    await expect(page.locator('h1')).toHaveText(DEVICE_NAME);
    await expect(page.locator('[data-device-preview]')).toHaveCount(1);
    await expect(page.getByText('32×64', { exact: false }).first()).toBeVisible();
    await expect(page.locator('#status-text')).toContainText(/Receiving|Connected/, { timeout: 15000 });
    await expect(page.locator('#media-display')).toHaveAttribute('src', /^data:image\/png/, { timeout: 15000 });

    // Cleanup: remove the device so other specs see an empty devices page.
    await deleteAllDevices(page);
  });
});
