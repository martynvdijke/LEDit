import { test, expect, type Page } from './fixtures';

async function clearBindings(page: Page) {
  await page.goto('/admin/input-bindings');
  for (;;) {
    const rows = page.locator('#input-bindings-body tr');
    if ((await rows.count()) === 0) break;
    page.once('dialog', (d) => d.accept());
    await rows.first().locator('.btn-delete').click();
    await page.waitForLoadState('networkidle');
    await page.goto('/admin/input-bindings');
  }
}

test.describe('input bindings admin', () => {
  test.setTimeout(90_000);

  test.beforeEach(async ({ page }) => {
    await clearBindings(page);
  });

  test('create, toggle and edit a binding', async ({ page }) => {
    await page.click('#btn-add');
    await expect(page.locator('#binding-modal')).toBeVisible();
    await page.selectOption('#f-source', 'nfc');
    await page.selectOption('#f-event', 'tap');
    await page.fill('#f-match', '04a1b2c3');
    await page.fill('#f-action', '{"kind":"brightness","level":25}');
    await page.fill('#f-order', '3');
    await page.click('#binding-form button[type="submit"]');
    await page.waitForLoadState('networkidle');

    const row = page.locator('#input-bindings-body tr').first();
    await expect(row).toContainText('nfc');
    await expect(row).toContainText('tap');
    await expect(row).toContainText('04a1b2c3');
    await expect(row).toContainText('brightness');
    await expect(row.locator('.badge')).toContainText('on');

    await row.locator('.btn-toggle').click();
    await page.waitForLoadState('networkidle');
    await expect(page.locator('#input-bindings-body tr').first().locator('.badge')).toContainText('off');

    await page.locator('#input-bindings-body tr').first().locator('.btn-edit').click();
    await expect(page.locator('#binding-modal')).toBeVisible();
    await page.fill('#f-action', '{"kind":"brightness","level":80}');
    await page.click('#binding-form button[type="submit"]');
    await page.waitForLoadState('networkidle');
    await expect(page.locator('#input-bindings-body tr').first()).toContainText('80');
  });

  test('invalid action target is rejected with a visible error', async ({ page }) => {
    await page.click('#btn-add');
    await page.selectOption('#f-source', 'button:next');
    await page.selectOption('#f-event', 'press');
    await page.fill('#f-action', '{"kind":"scene","scene_id":99999}');
    const dialogPromise = page.waitForEvent('dialog');
    await page.click('#binding-form button[type="submit"]');
    const dialog = await dialogPromise;
    expect(dialog.message()).toContain('not found or disabled');
    await dialog.accept();
    await expect(page.locator('#input-bindings-body tr')).toHaveCount(0);
  });

  test('delete removes a binding', async ({ page }) => {
    await page.click('#btn-add');
    await page.selectOption('#f-source', 'pir');
    await page.selectOption('#f-event', 'presence');
    await page.fill('#f-action', '{"kind":"feed","verb":"next"}');
    await page.click('#binding-form button[type="submit"]');
    await page.waitForLoadState('networkidle');
    await expect(page.locator('#input-bindings-body tr')).toHaveCount(1);

    page.once('dialog', (d) => d.accept());
    await page.locator('#input-bindings-body tr').first().locator('.btn-delete').click();
    await page.waitForLoadState('networkidle');
    await expect(page.locator('#input-bindings-body tr')).toHaveCount(0);
  });
});
