import { test, expect } from './fixtures';

function pwName(prefix: string) {
  return `PW-${test.info().parallelIndex}-${Math.random().toString(36).slice(2, 6)}-${prefix}`;
}

async function getDeviceId(page: import('@playwright/test').Page, name: string): Promise<string> {
  await page.goto('/admin/devices');
  const html = await page.content();
  const idx = html.indexOf(name);
  if (idx < 0) return '';
  const snippet = html.slice(Math.max(0, idx - 1200), idx + 1200);
  const m = snippet.match(/\/admin\/devices\/(\d+)\/delete/) || snippet.match(/\/admin\/devices\/(\d+)\/preview/);
  return m ? m[1] : '';
}

test.describe('Device lifecycle', () => {
  test('create via UI with 32x64 and verify row/token', async ({ page }) => {
    const name = pwName('DevA');
    await page.goto('/admin/devices/new');
    await page.fill('#name', name);
    await page.fill('#ip', '127.0.0.1');
    await page.fill('#width', '32');
    await page.fill('#height', '64');
    await page.fill('#refresh_interval', '1');
    await page.locator('#enabled').check();
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/devices$/);
    await expect(page.getByText(name)).toBeVisible();
    // token/URL exposed in table
    await expect(page.locator('tr:has-text("' + name + '")')).toContainText(/token|ws:/i);
    const id = await getDeviceId(page, name);
    expect(id).not.toBe('');
  });

  test('create via UI with 128x32 dims', async ({ page }) => {
    const name = pwName('DevWide');
    await page.goto('/admin/devices/new');
    await page.fill('#name', name);
    await page.fill('#ip', '127.0.0.1');
    await page.fill('#width', '128');
    await page.fill('#height', '32');
    await page.fill('#refresh_interval', '1');
    await page.locator('#enabled').check();
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/devices$/);
    await expect(page.getByText(name)).toBeVisible();
    await expect(page.getByRole('cell', { name: '128x32' })).toBeVisible();
  });

  test('per-device resolution preview streams at declared dims', async ({ page, wsFeed }) => {
    const name = pwName('ResCheck');
    await page.goto('/admin/devices/new');
    await page.fill('#name', name);
    await page.fill('#ip', '127.0.0.1');
    await page.fill('#width', '32');
    await page.fill('#height', '64');
    await page.fill('#refresh_interval', '1');
    await page.locator('#enabled').check();
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/devices$/);
    const id = await getDeviceId(page, name);
    expect(id).not.toBe('');
    // Connect to preview WS and check dimensions
    await page.goto('/');
    const feed = await wsFeed(`/ws/device/${id}/preview`);
    const frame = await feed.nextFrame(8000);
    expect(frame.format).toBe('PNG');
    // Decode base64 PNG dimensions via browser
    const dims = await page.evaluate((b64) => {
      const bin = atob(b64 as string);
      const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
      // PNG IHDR width at bytes 16-19, height 20-23 big-endian
      const w = (bytes[16] << 24) | (bytes[17] << 16) | (bytes[18] << 8) | bytes[19];
      const h = (bytes[20] << 24) | (bytes[21] << 16) | (bytes[22] << 8) | bytes[23];
      return { w, h };
    }, frame.image);
    expect(dims.w).toBe(32);
    expect(dims.h).toBe(64);
  });

  test('delete and token-reject check', async ({ page, request }) => {
    const name = pwName('ToDelete');
    await page.goto('/admin/devices/new');
    await page.fill('#name', name);
    await page.fill('#ip', '127.0.0.1');
    await page.fill('#width', '64');
    await page.fill('#height', '64');
    await page.fill('#refresh_interval', '1');
    await page.locator('#enabled').check();
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/devices$/);
    const id = await getDeviceId(page, name);
    expect(id).not.toBe('');
    // Get token from devices page html ws url
    const html = await page.content();
    const tokenMatch = html.match(/ws:\/\/[^/]+\/ws\/device\/([a-zA-Z0-9_-]+)/);
    let token = '';
    if (tokenMatch) token = tokenMatch[1];
    // Delete
    await page.locator(`tr:has-text("${name}") form[action$="/delete"] button`).click();
    await page.waitForLoadState('networkidle');
    await expect(page.getByText(name)).toHaveCount(0);
    if (token) {
      // Token should be rejected - ws connect fails or http 401
      // Try via page evaluate WebSocket error
      await page.goto('/');
      const rejected = await page.evaluate(async (tok) => {
        return new Promise<boolean>((resolve) => {
          const ws = new WebSocket(`ws://${location.host}/ws/device/${tok}`);
          let done = false;
          ws.onerror = () => { if (!done) { done = true; resolve(true); } };
          ws.onopen = () => { setTimeout(() => { if (!done) { done = true; ws.close(); resolve(false); } }, 1000); };
          setTimeout(() => { if (!done) { done = true; try{ws.close();}catch{} resolve(true); } }, 2000);
        });
      }, token);
      expect(rejected).toBeTruthy();
    }
  });
});
