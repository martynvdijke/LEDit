import { test, expect } from './fixtures';

function pwName(prefix: string) {
  return `PW-${test.info().parallelIndex}-${Math.random().toString(36).slice(2, 6)}-${prefix}`;
}

test.describe('Brightness scheduling', () => {
  test('create device with brightness window, badge, override and clear', async ({ page, request }) => {
    const name = pwName('Bright');
    await page.goto('/admin/devices/new');
    await page.fill('#name', name);
    await page.fill('#ip', '127.0.0.1');
    await page.fill('#width', '64');
    await page.fill('#height', '64');
    await page.fill('#refresh_interval', '1');
    await page.locator('#enabled').check();
    // enable brightness
    await page.locator('#brightness_enabled').check();
    // set schedule JSON directly via hidden input via evaluate
    await page.evaluate(() => {
      const el = document.getElementById('brightness_schedules') as HTMLInputElement;
      if (el) el.value = JSON.stringify([{ days: [0,1,2,3,4,5,6], start: '22:00', end: '23:00', level: 30 }]);
    });
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/devices$/);
    await expect(page.getByText(name)).toBeVisible();

    // extract id
    const html = await page.content();
    const idx = html.indexOf(name);
    const snippet = html.slice(Math.max(0, idx - 1500), idx + 1500);
    const m = snippet.match(/\/admin\/devices\/(\d+)\//);
    const id = m ? m[1] : '';
    expect(id).not.toBe('');

    // reopen edit and assert brightness values persisted
    await page.goto(`/admin/devices/${id}/edit`);
    await expect(page.locator('#brightness_enabled')).toBeChecked();
    const schedVal = await page.locator('#brightness_schedules').inputValue();
    expect(schedVal).toContain('30');

    // set override 80
    await page.fill('#brightness_override', '80');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/devices$/);
    await page.goto(`/admin/devices/${id}/edit`);
    const ov = await page.locator('#brightness_override').inputValue();
    expect(ov).toBe('80');

    // clear to auto
    await page.evaluate(() => {
      const el = document.getElementById('brightness_override') as HTMLInputElement;
      if (el) el.value = '';
    });
    await page.click('button:has-text("Auto")');
    // ensure cleared then submit
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/devices$/);
    await page.goto(`/admin/devices/${id}/edit`);
    const ov2 = await page.locator('#brightness_override').inputValue();
    expect(ov2).toBe('');
  });
});
