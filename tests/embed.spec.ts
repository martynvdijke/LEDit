import { test, expect, type Page } from './fixtures';

// E2E runs with LEDIT_AUTH_DISABLE=true, so the admin JSON API is open and guest
// tokens can be created without a session. Embed auth itself is independent of
// session auth: it uses the guest secret exchanged for the ledit_embed cookie.

const DEVICE_NAME = 'PW Embed Wall';

async function seedSettings(page: Page): Promise<void> {
  // The shared embed needs GeneralSettings (built-in Clock/System Stats sources
  // attach to it) — a fresh DB has none until settings are saved.
  await page.request.post('/admin/settings', {
    form: { timeout: '5', width: '64', height: '64' },
  });
}

async function createEmbedToken(
  request: import('@playwright/test').APIRequestContext,
  scopes: string[],
  label = 'E2E embed',
): Promise<{ id: number; secret: string }> {
  const res = await request.post('/admin/api/guest-remotes', {
    data: { label, scopes, expires_in_hours: 1 },
  });
  expect(res.status()).toBe(201);
  return (await res.json()) as { id: number; secret: string };
}

async function createDevice(page: Page): Promise<number> {
  await page.goto('/admin/devices/new');
  await page.fill('#name', DEVICE_NAME);
  await page.fill('#ip', '127.0.0.1');
  await page.fill('#width', '32');
  await page.fill('#height', '64');
  await page.fill('#refresh_interval', '2');
  await page.locator('#enabled').check();
  await page.click('button[type="submit"]');
  await expect(page).toHaveURL(/\/admin\/devices$/);
  const body = (await (await page.request.get('/api/devices')).json()) as {
    devices: Array<{ id: number; name: string }>;
  };
  const matches = body.devices.filter((d) => d.name === DEVICE_NAME);
  expect(matches.length).toBeGreaterThan(0);
  return matches[matches.length - 1].id;
}

test.describe('Embed surfaces', () => {
  test('shared embed streams a chrome-free live frame', async ({ page }) => {
    await seedSettings(page);
    await page.goto('/embed');

    await expect(page.locator('body')).toHaveAttribute('data-state', 'live', { timeout: 20000 });

    // No admin navigation or login affordances on an embed page.
    await expect(page.locator('a[href="/login"]')).toHaveCount(0);
    await expect(page.locator('nav, .sidebar, [data-sidebar]')).toHaveCount(0);

    const size = await page.locator('#embed-canvas').evaluate((el) => ({
      w: (el as HTMLCanvasElement).width,
      h: (el as HTMLCanvasElement).height,
    }));
    expect(size.w).toBeGreaterThan(0);
    expect(size.h).toBeGreaterThan(0);
  });

  test('embed honours the requested frame size', async ({ page }) => {
    await seedSettings(page);
    await page.goto('/embed?w=256&h=128');
    await expect(page.locator('body')).toHaveAttribute('data-state', 'live', { timeout: 20000 });
    await expect(page.locator('#embed-canvas')).toHaveAttribute('width', '256');
    await expect(page.locator('#embed-canvas')).toHaveAttribute('height', '128');
  });

  test('bg=transparent keeps the page background transparent', async ({ page }) => {
    await seedSettings(page);
    await page.goto('/embed?bg=transparent&w=64&h=64');
    await expect(page.locator('body')).toHaveAttribute('data-state', 'live', { timeout: 20000 });
    const background = await page.evaluate(
      () => getComputedStyle(document.body).backgroundColor,
    );
    expect(background).toBe('rgba(0, 0, 0, 0)');
  });

  test('device embed streams with a valid embed token', async ({ page }) => {
    await seedSettings(page);
    const deviceId = await createDevice(page);
    const token = await createEmbedToken(page.request, ['embed']);

    await page.goto(`/embed/device/${deviceId}#${token.secret}`);
    await expect(page.locator('body')).toHaveAttribute('data-state', 'live', { timeout: 20000 });

    // The secret is exchanged for a cookie and stripped from the address bar.
    await expect(page).toHaveURL(new RegExp(`/embed/device/${deviceId}$`));
    await expect(page.locator('#embed-canvas')).toHaveAttribute('width', '32');
    await expect(page.locator('#embed-canvas')).toHaveAttribute('height', '64');
  });

  test('invalid token shows the static error and never redirects to login', async ({ page }) => {
    await seedSettings(page);
    const deviceId = await createDevice(page);

    await page.goto(`/embed/device/${deviceId}#not-a-real-secret`);
    await expect(page.locator('#embed-error')).toBeVisible({ timeout: 10000 });
    await expect(page).toHaveURL(new RegExp(`/embed/device/${deviceId}$`));
    expect(page.url()).not.toContain('/login');
  });

  test('revoked token stops the device embed', async ({ page }) => {
    await seedSettings(page);
    const deviceId = await createDevice(page);
    const token = await createEmbedToken(page.request, ['embed']);

    await page.goto(`/embed/device/${deviceId}#${token.secret}`);
    await expect(page.locator('body')).toHaveAttribute('data-state', 'live', { timeout: 20000 });

    const revoked = await page.request.post(`/admin/api/guest-remotes/${token.id}/revoke`);
    expect(revoked.ok()).toBeTruthy();

    await page.reload();
    await expect(page.locator('#embed-error')).toBeVisible({ timeout: 10000 });
  });

  test('snapshot endpoint returns an image for the shared wall', async ({ page }) => {
    await seedSettings(page);
    const res = await page.request.get('/api/frame');
    expect(res.status()).toBe(200);
    expect(res.headers()['content-type']).toContain('image/png');
    expect(res.headers()['cache-control']).toContain('no-store');
  });
});
