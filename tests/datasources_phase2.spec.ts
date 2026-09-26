import { test, expect } from './fixtures';

type TokenUrlType = {
  endpoint: string;
  typeName: string;
};

const tokenUrlTypes: TokenUrlType[] = [
  { endpoint: 'adguard', typeName: 'AdGuard' },
  { endpoint: 'frigate', typeName: 'Frigate' },
  { endpoint: 'zigbee2mqtt', typeName: 'Zigbee2MQTT' },
  { endpoint: 'transmission', typeName: 'Transmission' },
  { endpoint: 'proxmox', typeName: 'Proxmox' },
  { endpoint: 'waste', typeName: 'Waste' },
  { endpoint: 'airquality', typeName: 'Air Quality' },
  { endpoint: 'parcel', typeName: 'Parcel' },
];

function extractDeleteId(html: string, endpoint: string): string | null {
  const re = new RegExp(`/admin/datasources/${endpoint}/(\\d+)/delete`, 'g');
  let m: RegExpExecArray | null;
  let last: string | null = null;
  while ((m = re.exec(html)) !== null) last = m[1];
  return last;
}

for (const ds of tokenUrlTypes) {
  test(`create + delete ${ds.endpoint} datasource via request`, async ({ request }) => {
    const token = `test-token-${ds.endpoint}-${Date.now()}-${Math.random().toString(36).slice(2, 4)}`;
    const res = await request.post(`/admin/datasources/${ds.endpoint}/new`, {
      form: { token, url: 'http://127.0.0.1:9' },
    });
    expect([200, 302]).toContain(res.status());

    const adminRes = await request.get('/admin/');
    expect(adminRes.ok()).toBeTruthy();
    const html = await adminRes.text();
    expect(html).toContain(ds.typeName);
    expect(html).toContain(token);

    const id = extractDeleteId(html, ds.endpoint);
    expect(id).not.toBeNull();

    const delRes = await request.post(`/admin/datasources/${ds.endpoint}/${id}/delete`);
    expect([200, 302]).toContain(delRes.status());

    const after = await request.get('/admin/');
    const afterHtml = await after.text();
    expect(afterHtml).not.toContain(token);
  });
}

test.describe('new-form render per type phase2', () => {
  for (const ds of tokenUrlTypes) {
    test(`${ds.endpoint} new form returns 200 and contains token/url`, async ({ request }) => {
      const res = await request.get(`/admin/datasources/${ds.endpoint}/new`);
      expect(res.status()).toBe(200);
      const html = await res.text();
      expect(html).toContain('name="token"');
      expect(html).toContain(`/admin/datasources/${ds.endpoint}/new`);
    });
  }
});

test('sidebar lists the phase2 datasource links', async ({ page }) => {
  await page.goto('/admin/');
  for (const ds of tokenUrlTypes) {
    await expect(page.locator(`a[href="/admin/datasources/${ds.endpoint}/new"]`).first()).toBeAttached();
  }
});
