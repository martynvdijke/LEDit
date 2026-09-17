import { test, expect } from './fixtures';

async function setColorInput(page: import('@playwright/test').Page, selector: string, color: string) {
  const loc = page.locator(selector);
  try {
    await loc.fill(color);
  } catch {
    await loc.evaluate((el: HTMLInputElement, val: string) => {
      el.value = val;
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
    }, color);
    return;
  }
  // Ensure input/change events fire even if fill succeeded (some PW versions don't trigger for type=color)
  await loc.evaluate((el: HTMLInputElement) => {
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
  });
}

test.describe('Theme designer', () => {
  test('create with live preview and edit accent color', async ({ page }) => {
    const uniqueName = `E2E Theme ${Date.now()}-${Math.floor(Math.random() * 10000)}`;

    // --- Create ---
    await page.goto('/admin/themes/new/edit');
    await expect(page.locator('[data-theme-editor]')).toBeVisible();
    await expect(page.locator('[data-live-preview-img]')).toBeAttached();
    await expect(page.locator('[data-theme-preview-target]')).toBeAttached();

    await page.locator('input[name="name"]').fill(uniqueName);
    await setColorInput(page, 'input[name="accent_color"]', '#ff0000');

    // Live preview fetches /admin/preview and sets img src to blob: URL (debounced 300ms).
    await expect(page.locator('[data-live-preview-img]')).toHaveAttribute('src', /^blob:/, { timeout: 15000 });

    await page.locator('[data-theme-save]').click();
    await page.waitForURL(/\/admin\/themes\/?(?:\?.*)?$/);
    await expect(page).toHaveURL(/\/admin\/themes/);

    const createdRow = page.locator('[data-theme-list-item]', { hasText: uniqueName });
    await expect(createdRow).toBeVisible();

    // --- Edit the just-created theme ---
    const editLink = createdRow.locator('a[href*="/edit"]').first();
    await expect(editLink).toBeVisible();
    await editLink.click();
    await expect(page).toHaveURL(/\/admin\/themes\/\d+\/edit/);
    await expect(page.locator('[data-theme-editor]')).toBeVisible();

    // Change accent color again and save.
    await setColorInput(page, 'input[name="accent_color"]', '#00ff00');
    // Optionally also verify live preview updates again on edit page.
    await expect(page.locator('[data-live-preview-img]')).toHaveAttribute('src', /^blob:/, { timeout: 15000 });

    await page.locator('[data-theme-save]').click();
    await page.waitForURL(/\/admin\/themes/);
    await expect(page).toHaveURL(/\/admin\/themes/);
    await expect(page.locator('[data-theme-list-item]', { hasText: uniqueName })).toBeVisible();
  });
});
