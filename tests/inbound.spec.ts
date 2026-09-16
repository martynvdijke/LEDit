import { test, expect } from './fixtures';

test.describe('Inbound and guest photo frame', () => {
  test('push bridge end-to-end', async ({ request }) => {
    const secret = `pw-ntfy-${Date.now()}`;
    const title = `PW-ntfy-${Date.now()}`;

    // Configure ntfy adapter: enabled, secret, allowlist *, config {}
    const save = await request.post('/admin/inbound/ntfy', {
      form: { enabled: 'on', secret, allowlist: '*', config: '{}' },
    });
    expect([200, 302]).toContain(save.status());

    const res = await request.post('/api/inbound/ntfy', {
      headers: { 'X-Inbound-Secret': secret },
      data: { title, message: 'hello from playwright', priority: 3 },
    });
    expect(res.status()).toBe(200);

    // Verify message appears in public TRMNL messages feed
    await expect.poll(async () => {
      const r = await request.get('/api/trmnl/messages');
      const j = await r.json();
      const msgs: any[] = j.messages ?? [];
      return msgs.some((m) => m.title === title || m.body === 'hello from playwright');
    }, { timeout: 5000 }).toBe(true);

    // cleanup: disable adapter
    await request.post('/admin/inbound/ntfy', {
      form: { secret, allowlist: '', config: '{}' },
    }).catch(() => {});
  });

  test('frame upload + moderation', async ({ page, request }) => {
    page.on('dialog', (d) => d.accept());

    // create photo-scoped guest remote
    const label = `pw-photo-${Date.now()}`;
    const create = await request.post('/admin/api/guest-remotes', {
      data: { label, scopes: ['photo'], expires_in_hours: 24 },
    });
    expect(create.status()).toBe(201);
    const body = await create.json();
    const secret: string = body.secret;
    expect(secret).toBeTruthy();
    expect(body.frame_link).toBeTruthy();
    const tokenId: number = body.id;

    // Open frame page with fragment secret
    await page.goto('/frame#' + secret);

    // fragment cleared by history.replaceState
    await expect.poll(() => page.evaluate(() => location.hash), { timeout: 5000 }).toBe('');
    expect(await page.evaluate(() => location.href)).not.toContain(secret);

    // secret must not appear in rendered HTML source
    const content = await page.content();
    expect(content).not.toContain(secret);

    // sessionStorage should hold secret (frame.ts stores it)
    const stored = await page.evaluate(() => sessionStorage.getItem('ledit_frame_secret'));
    expect(stored).toBe(secret);

    // Build a genuine 1x1 PNG (transparent) - decodeable
    const pngBase64 = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4//8/AAX+Av4N70a4AAAAAElFTkSuQmCC';
    const pngBuffer = Buffer.from(pngBase64, 'base64');

    // Drive file input
    const fileInput = page.locator('[data-frame-file]');
    await expect(fileInput).toBeVisible();
    await fileInput.setInputFiles({ name: 'tiny.png', mimeType: 'image/png', buffer: pngBuffer });

    // preview should appear (src becomes blob:)
    await expect.poll(() => page.locator('[data-frame-preview]').getAttribute('src'), { timeout: 5000 }).toMatch(/^blob:/);

    // upload button should be enabled
    const uploadBtn = page.locator('[data-frame-upload]');
    await expect(uploadBtn).toBeEnabled();
    await uploadBtn.click();

    // status should become "Submitted for approval"
    await expect.poll(async () => page.locator('[data-frame-status]').textContent(), { timeout: 8000 }).toMatch(/Submitted for approval/i);

    // Verify via admin API: photo row created (pending)
    // Use admin photo-frame page HTML
    await page.goto('/admin/photo-frame');
    await expect(page.locator('table')).toBeVisible();

    // Find pending photo id from table (most recent first)
    // Extract id from first row's approve form action
    let photoId = '';
    await expect.poll(async () => {
      const res = await request.get('/admin/photo-frame?status=pending');
      const html = await res.text();
      const m = html.match(/\/api\/guest\/photo\/(\d+)\/approve/);
      if (m) { photoId = m[1]; return true; }
      return false;
    }, { timeout: 5000 }).toBe(true);
    expect(photoId).not.toBe('');

    // Approve via API (admin auth bypassed)
    const approve = await request.post(`/api/guest/photo/${photoId}/approve`);
    expect([200, 202]).toContain(approve.status());

    // Verify approved filter shows it
    await expect.poll(async () => {
      const res = await request.get('/admin/photo-frame?status=approved');
      const html = await res.text();
      return html.includes(`/api/guest/photo/${photoId}/`) || html.includes(`>${photoId}<`);
    }, { timeout: 5000 }).toBe(true);

    // Also verify via page navigation
    await page.goto('/admin/photo-frame?status=approved');
    await expect(page.locator('table')).toContainText(photoId);

    // Cleanup guest token (if delete endpoint exists, try; otherwise revoke)
    await request.post(`/admin/api/guest-remotes/${tokenId}/revoke`).catch(() => {});
  });

  test('bad secret rejected', async ({ request }) => {
    const secret = `pw-ntfy-bad-${Date.now()}`;
    await request.post('/admin/inbound/ntfy', {
      form: { enabled: 'on', secret, allowlist: '*', config: '{}' },
    });
    const res = await request.post('/api/inbound/ntfy', {
      headers: { 'X-Inbound-Secret': 'wrong' },
      data: { title: 'x', message: 'y' },
    });
    expect(res.status()).toBe(401);
    await request.post('/admin/inbound/ntfy', {
      form: { secret, allowlist: '', config: '{}' },
    }).catch(() => {});
  });
});
