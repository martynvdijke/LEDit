import { test, expect } from './fixtures';

function pwName(p: string) {
  return `PW-${test.info().parallelIndex}-${Math.random().toString(36).slice(2, 6)}-${p}`;
}

test.describe('QR datasource E2E', () => {
  test('admin creates text QR → appears in list → feed renders it', async ({ page, wsFeed, request }) => {
    const name = pwName('QRText');
    const content = `hello-qr-${Date.now()}-${Math.random().toString(36).slice(2, 4)}`;
    // Create via UI form
    await page.goto('/admin/qrcodes/new');
    await expect(page.locator('h1')).toContainText('QR Code');
    await page.check('#mode_text');
    await page.fill('#content', content);
    await page.fill('#caption', name);
    await page.selectOption('#error_correction', 'M');
    await page.fill('#quiet_zone', '4');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/qrcodes$/);
    await expect(page.locator(`tr:has-text("${content}")`)).toBeVisible({ timeout: 5000 });

    // Also verify via API list
    const listRes = await request.get('/admin/api/qrcodes');
    expect(listRes.ok()).toBeTruthy();
    const list = await listRes.json() as any[];
    const found = list.find((r: any) => r.content === content || r.Content === content);
    expect(found).toBeTruthy();

    // Feed should render it: connect to WS feed and get a PNG frame (contains QR, but we just check PNG)
    await page.goto('/');
    const feed = await wsFeed('/ws/feed');
    let gotPNG = false;
    for (let i = 0; i < 8; i++) {
      try {
        const frame = await feed.nextFrame(8000);
        if (frame.format === 'PNG' && typeof frame.image === 'string' && (frame.image as string).length > 100) {
          // Verify it's a PNG (base64 starts with iVBOR)
          const b64 = frame.image as string;
          // PNG magic base64: iVBORw0KGgo
          expect(b64.startsWith('iVBOR')).toBeTruthy();
          gotPNG = true;
          break;
        }
      } catch {}
    }
    expect(gotPNG).toBeTruthy();

    // cleanup
    if (found) {
      const id = (found as any).ID ?? (found as any).id;
      await request.delete(`/admin/api/qrcodes/${id}`).catch(() => {});
      // also try POST delete
      await request.post(`/admin/qrcodes/${id}/delete`).catch(() => {});
    }
  });

  test('create wifi QR → verify payload formatting via API GET', async ({ request }) => {
    const ssid = `TestWifi-${Math.random().toString(36).slice(2, 6)}`;
    const password = 's3cret!;:,\\';
    const body = {
      content: 'wifi-content',
      mode: 'wifi',
      wifi_ssid: ssid,
      wifi_password: password,
      wifi_auth: 'WPA',
      error_correction: 'M',
      quiet_zone: 4,
    };
    const createRes = await request.post('/admin/api/qrcodes', { data: body });
    expect(createRes.status()).toBe(201);
    const created = await createRes.json() as any;
    const id = created.ID ?? created.id;
    expect(id).toBeTruthy();

    const getRes = await request.get(`/admin/api/qrcodes/${id}`);
    expect(getRes.ok()).toBeTruthy();
    const data = await getRes.json() as any;
    const payload = data.payload as string;
    expect(payload).toContain('WIFI:T:WPA');
    expect(payload).toContain(`S:${ssid}`);
    // password should be escaped: ; \ , : should be escaped
    // Original password contains ; :, \, so payload should contain escaped versions
    expect(payload).toContain('P:');
    expect(payload.endsWith(';;')).toBeTruthy();

    // Verify escaping: the payload P segment should contain \; and \:
    // Our password s3cret!;:,\\ => escaped should be s3cret!\;\:\,\\ ?
    // Just check that raw unescaped ; not present as delimiter inside P value
    // Simpler: ensure payload contains escaped sequences
    expect(payload).toContain('\\;');
    expect(payload).toContain('\\:');
    expect(payload).toContain('\\,');

    // cleanup
    await request.delete(`/admin/api/qrcodes/${id}`).catch(() => {});
  });
});
