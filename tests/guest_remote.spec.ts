import { test, expect } from './fixtures';

// The feed controller is process-global: a guest test that pauses the wall must
// not leave it paused for later specs (feed/index/notifications/qrcode).
test.afterEach(async ({ request }) => {
  await request.post('/api/feed/resume').catch(() => {});
});

// E2E runs with LEDIT_AUTH_DISABLE=true, so the admin JSON API is open and no
// login is needed to create a guest token. Guest auth itself is header-based
// and independent of the disabled session auth.
async function createGuestToken(
  request: import('@playwright/test').APIRequestContext,
  label: string,
) {
  const res = await request.post('/admin/api/guest-remotes', {
    data: { label, scopes: ['pause', 'next', 'message'], expires_in_hours: 1 },
  });
  expect(res.status()).toBe(201);
  return (await res.json()) as { id: number; secret: string; link: string };
}

test.describe('Guest remote', () => {
  test('admin creates a token, remote drives the wall, revoke blocks it', async ({ page, request }) => {
    const created = await createGuestToken(request, 'E2E party');
    expect(created.secret).toBeTruthy();
    expect(created.link).toContain(`/remote#${created.secret}`);

    // The secret travels only in the fragment.
    await page.goto(`/remote#${created.secret}`);
    await expect(page.locator('#paused-state')).toHaveText(/Playing|Paused/);

    // Pause/resume and skip reuse the feed controller.
    await page.locator('#btn-pause').click();
    await expect(page.locator('#paused-state')).toHaveText('Paused');
    await page.locator('#btn-resume').click();
    await expect(page.locator('#paused-state')).toHaveText('Playing');

    await page.locator('#btn-next').click();
    await expect(page.locator('#notice')).toContainText('Done');

    // Push a message and confirm it reaches the notification pipeline.
    await page.locator('#message-text').fill('E2E hello wall');
    await page.locator('#btn-message').click();
    await expect(page.locator('#notice')).toContainText(/sent/i);

    const history = await request.get('/api/notifications');
    expect(history.ok()).toBeTruthy();
    expect(await history.text()).toContain('E2E hello wall');

    // Revoke: the remote must surface an auth error and stop acting.
    const revoked = await request.post(`/admin/api/guest-remotes/${created.id}/revoke`);
    expect(revoked.ok()).toBeTruthy();
    await page.reload();
    await expect(page.locator('#notice')).toContainText(/invalid|authorised/i);
    await expect(page.locator('#btn-pause')).toBeDisabled();
  });
});

test.describe('Guest remote on a phone viewport', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('controls fit without horizontal scroll and are keyboard operable', async ({ page, request }) => {
    const created = await createGuestToken(request, 'E2E phone');
    await page.goto(`/remote#${created.secret}`);
    await expect(page.locator('#paused-state')).toHaveText(/Playing|Paused/);

    const noHorizontalScroll = await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth + 1,
    );
    expect(noHorizontalScroll).toBe(true);

    await page.locator('#btn-pause').focus();
    await expect(page.locator('#btn-pause')).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.locator('#paused-state')).toHaveText('Paused');
  });
});
