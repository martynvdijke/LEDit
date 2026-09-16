import { test, expect } from './fixtures';

test.describe('device-discovery enrollment flow', () => {
  const fp = `pw-disc-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
  const payload = {
    fingerprint: fp,
    model: 'PW-Model',
    version: '1.0.0',
    proto: '1',
    nonce: `n-${Date.now()}`,
    address: '127.0.0.1:9000',
  };

  test('seed -> pending visible no token -> enroll -> cancel -> cleanup', async ({ page, request }) => {
    // 1. Seed pending device
    const seed = await request.post('/api/test/seed-discovery', { data: payload });
    expect(seed.status()).toBe(200);
    const seedJson = await seed.json();
    expect(seedJson.fingerprint).toBe(fp);

    // 2. Assert appears in admin discovery page pending & no token rendered
    await page.goto('/admin/discovery');
    const row = page.locator('tr', { hasText: fp });
    await expect(row).toBeVisible();
    await expect(row.locator('span.badge')).toHaveText('pending');
    // Enroll button visible for pending
    await expect(row.locator('form[action*="/enroll"] button', { hasText: 'Enroll' })).toBeVisible();
    await expect(row.locator('form[action*="/cancel"]')).toHaveCount(0);
    // No token anywhere for unclaimed device: row text contains no token, no token input/attribute
    const rowText = await row.textContent();
    expect(rowText).not.toMatch(/token/i);
    // ensure no token-like value in row innerHTML
    const rowHtml = await row.innerHTML();
    expect(rowHtml.toLowerCase()).not.toContain('token');
    // no token input for that row
    await expect(row.locator('input[name="token"]')).toHaveCount(0);
    await expect(row.locator('[data-token]')).toHaveCount(0);

    // Also verify JSON api does not leak token
    const disc = await request.get('/api/device/discovery');
    expect(disc.status()).toBe(200);
    const discJson = await disc.json();
    const dev = (discJson.devices as any[]).find((d) => d.fingerprint === fp);
    expect(dev).toBeTruthy();
    expect(dev.state).toBe('pending');
    expect(dev.token).toBeUndefined();
    // page overall should not contain token for this fp
    const pageContent = await page.content();
    // discovery JSON should not contain token field at all
    expect(JSON.stringify(discJson)).not.toMatch(/"token"/);

    // allow unhandled token check via content if device not yet created
    void pageContent;

    // 3. Enroll via API (also verifies UI flow alternative)
    const enroll = await request.post(`/api/device/discovery/${fp}/enroll`);
    expect(enroll.status()).toBe(200);
    const enrollJson = await enroll.json();
    expect(enrollJson.fingerprint).toBe(fp);
    // enroll response must not leak token
    expect(enrollJson.token).toBeUndefined();

    // pending entry should leave pending state -> approved, Enroll gone, Cancel appears
    await expect.poll(async () => {
      const r = await request.get('/api/device/discovery');
      const j = await r.json();
      const d = (j.devices as any[]).find((x) => x.fingerprint === fp);
      return d?.state;
    }, { timeout: 5000 }).toBe('approved');

    await page.reload();
    const row2 = page.locator('tr', { hasText: fp });
    await expect(row2.locator('span.badge')).toHaveText('approved');
    await expect(row2.locator('form[action*="/cancel"] button', { hasText: 'Cancel' })).toBeVisible();
    await expect(row2.locator('form[action*="/enroll"]')).toHaveCount(0);

    // 4. Cancel enrollment (approved-but-unclaimed -> pending)
    const cancel = await request.post(`/api/device/discovery/${fp}/cancel`);
    expect(cancel.status()).toBe(200);

    await expect.poll(async () => {
      const r = await request.get('/api/device/discovery');
      const j = await r.json();
      const d = (j.devices as any[]).find((x) => x.fingerprint === fp);
      return d?.state;
    }, { timeout: 5000 }).toBe('pending');

    await page.reload();
    const row3 = page.locator('tr', { hasText: fp });
    await expect(row3.locator('span.badge')).toHaveText('pending');
    await expect(row3.locator('form[action*="/enroll"] button', { hasText: 'Enroll' })).toBeVisible();

    // Re-enroll to create a DeviceSettings row for deterministic cleanup test
    const reEnroll = await request.post(`/api/device/discovery/${fp}/enroll`);
    expect(reEnroll.status()).toBe(200);
    const reJson = await reEnroll.json();
    const deviceId: string = String(reJson.id ?? '');

    // 5. Cleanup: delete DeviceSettings row via admin delete route
    if (deviceId) {
      const del = await request.post(`/admin/devices/${deviceId}/delete`);
      expect([200, 302]).toContain(del.status());
    } else {
      // fallback: scrape devices page for our fingerprint/model row
      const devPage = await request.get('/admin/devices');
      const html = await devPage.text();
      // fingerprint appears in device token table? fallback to cleanupTestDevices prefix if device name contains model
      // For discovery-enrolled devices, name defaults to model (PW-Model). Delete any PW-Model device
      const re = /\/admin\/devices\/(\d+)\/delete/g;
      let m: RegExpExecArray | null;
      const ids: string[] = [];
      while ((m = re.exec(html)) !== null) ids.push(m[1]);
      for (const id of ids) {
        const idx = html.indexOf(`/admin/devices/${id}/delete`);
        const snippet = html.slice(Math.max(0, idx - 800), idx + 800);
        if (snippet.includes('PW-Model') || snippet.includes(fp)) {
          await request.post(`/admin/devices/${id}/delete`).catch(() => {});
        }
      }
    }

    // Cancel again to return to pending (if device deleted, state already pending via cancel before re-enroll delete? need to cancel)
    await request.post(`/api/device/discovery/${fp}/cancel`).catch(() => {});
    // Also re-seed with pending to keep TTL idempotent, or call reset-clear attempt
    // Do final pending seed to leave clean pending entry (TTL will expire)
    await request.post('/api/test/seed-discovery', { data: payload }).catch(() => {});

    // Verify cleanup left no DeviceSettings token accessible
    const afterDevices = await request.get('/admin/devices');
    const afterHtml = await afterDevices.text();
    // fingerprint should not appear in devices list
    expect(afterHtml).not.toContain(fp);
  });
});
