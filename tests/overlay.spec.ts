import { test, expect } from './fixtures';
import type { WsFeed } from './helpers/ws';

function pwName(prefix: string) {
  return `PW-${test.info().parallelIndex}-${Math.random().toString(36).slice(2, 6)}-${prefix}`;
}

async function getDeviceId(page: import('@playwright/test').Page, name: string): Promise<string> {
  await page.goto('/admin/devices');
  const html = await page.content();
  const idx = html.indexOf(name);
  if (idx < 0) return '';
  const snippet = html.slice(Math.max(0, idx - 1200), idx + 1200);
  const m = snippet.match(/\/admin\/devices\/(\d+)\/preview/) || snippet.match(/\/admin\/devices\/(\d+)\/delete/);
  return m ? m[1] : '';
}

async function sampleStrips(page: import('@playwright/test').Page, image: string) {
  return page.evaluate(async (b64) => {
    const img = new Image();
    img.src = 'data:image/png;base64,' + b64;
    await img.decode();
    const c = document.createElement('canvas');
    c.width = img.width;
    c.height = img.height;
    const ctx = c.getContext('2d')!;
    ctx.drawImage(img, 0, 0);
    const top = ctx.getImageData(0, 0, 1, 1).data;
    const bottom = ctx.getImageData(0, img.height - 1, 1, 1).data;
    return { top: [top[0], top[1], top[2]], bottom: [bottom[0], bottom[1], bottom[2]] };
  }, image);
}

const isRed = (px: number[]) => px[0] > 200 && px[1] < 60 && px[2] < 60;

// The first frames on a fresh feed may be the last-known-good/transition frame,
// which by design carries no overlay. Scan a bounded number of frames until the
// predicate holds, returning the matching sample (or null).
async function findFrame(
  feed: WsFeed,
  page: import('@playwright/test').Page,
  pred: (px: { top: number[]; bottom: number[] }) => boolean,
  tries = 10,
) {
  for (let i = 0; i < tries; i++) {
    // Reconnects can't use the push channel (exposeFunction registers once), so
    // drain the browser-side buffer explicitly.
    await feed.pollBrowserBuffer();
    let frame;
    try {
      frame = await feed.nextFrame(3000);
    } catch {
      continue;
    }
    if (!frame.image) continue;
    const px = await sampleStrips(page, frame.image as string);
    if (pred(px)) return px;
  }
  return null;
}

test.describe('Device overlay strip', () => {
  test('enable, reposition, then disable round-trips through the preview feed', async ({ page, wsFeed }) => {
    const name = pwName('Overlay');
    await page.goto('/admin/devices/new');
    await page.fill('#name', name);
    await page.fill('#ip', '127.0.0.1');
    await page.fill('#width', '64');
    await page.fill('#height', '64');
    await page.fill('#refresh_interval', '1');
    await page.locator('#enabled').check();
    await page.locator('#overlay_enabled').check();
    await page.fill('#overlay_text', 'OVL');
    await page.selectOption('select[name="overlay_position"]', 'bottom');
    await page.fill('input[name="overlay_height"]', '8');
    await page.fill('input[name="overlay_speed_px"]', '0');
    await page.fill('input[name="overlay_bg"]', '#ff0000');
    await page.fill('input[name="overlay_fg"]', '#ffffff');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/devices$/);

    const id = await getDeviceId(page, name);
    expect(id).not.toBe('');

    // Enabled, bottom: eventually a frame carries the red strip at the bottom.
    await page.goto('/');
    let feed = await wsFeed(`/ws/device/${id}/preview`);
    let px = await findFrame(feed, page, (p) => isRed(p.bottom));
    expect(px, 'no frame with red bottom strip').not.toBeNull();
    expect(isRed(px!.top), `top should not be red, got ${px!.top}`).toBe(false);

    // Reposition to top and grow the strip.
    await page.goto(`/admin/devices/${id}/edit`);
    await page.selectOption('select[name="overlay_position"]', 'top');
    await page.fill('input[name="overlay_height"]', '10');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/devices$/);

    await page.goto('/');
    feed = await wsFeed(`/ws/device/${id}/preview`);
    px = await findFrame(feed, page, (p) => isRed(p.top));
    expect(px, 'no frame with red top strip after reposition').not.toBeNull();

    // Disable: no frame should ever show a red strip.
    await page.goto(`/admin/devices/${id}/edit`);
    await page.locator('#overlay_enabled').uncheck();
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/devices$/);

    await page.goto('/');
    feed = await wsFeed(`/ws/device/${id}/preview`);
    const red = await findFrame(feed, page, (p) => isRed(p.top) || isRed(p.bottom), 4);
    expect(red, 'overlay strip still visible after disabling').toBeNull();
  });
});
