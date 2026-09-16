import { test, expect } from './fixtures';

// Composition editor: free-form region layout over the source registry. Tests
// clean up after themselves so the shared database stays pristine.

test.describe('Composition editor', () => {
  test('create with a clock and a failing region, preview, edit and delete', async ({ page, request }) => {
    await page.goto('/admin/compositions/new');

    // Live preview updates after the debounce once regions are valid.
    await page.locator('#name').fill('Wall');
    await page.locator('#rows').fill('1');
    await page.locator('#cols').fill('2');
    await page.locator('#regions').fill(JSON.stringify([
      { id: 'left', row: 0, col: 0, source_type: 'clock', source_id: 0 },
      { id: 'right', row: 0, col: 1, source_type: 'weather', source_id: 999 },
    ]));
    await expect.poll(
      () => page.locator('[data-live-preview-img]').getAttribute('src'),
      { timeout: 8000 },
    ).toMatch(/^blob:/);

    await page.getByRole('button', { name: 'Create' }).click();
    await page.waitForURL('/admin/compositions');
    const row = page.getByRole('row', { name: /Wall/ });
    await expect(row).toHaveCount(1);

    // One region is unresolvable, so its placeholder renders while the clock
    // region still composites: the frame is served, not a 500/502.
    const id = (await row.locator('td').first().textContent())?.trim();
    const preview = await request.get(`/admin/preview?type=composition&id=${id}&w=128&h=64`);
    expect(preview.status()).toBe(200);
    expect(preview.headers()['content-type']).toContain('image/png');

    // Edit round-trips the region JSON.
    await row.getByRole('link', { name: 'Edit' }).click();
    await expect(page.locator('h1')).toContainText('Edit Composition');
    await expect(page.locator('#regions')).toHaveValue(/weather/);

    await page.goto('/admin/compositions');
    await page.getByRole('row', { name: /Wall/ }).getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByRole('row', { name: /Wall/ })).toHaveCount(0);
  });

  test('invalid regions are rejected', async ({ page }) => {
    await page.goto('/admin/compositions/new');
    await page.locator('#name').fill('Broken');
    await page.locator('#regions').fill('{not json');
    await page.getByRole('button', { name: 'Create' }).click();
    await expect(page).toHaveURL(/\/admin\/compositions\/new/);
    await expect(page.locator('[role="alert"]')).toContainText(/[Rr]egions/);
  });
});
