import { test, expect } from './fixtures';

type TokenUrlType = {
  endpoint: string;
  typeName: string;
};

const tokenUrlTypes: TokenUrlType[] = [
  { endpoint: 'qbittorrent', typeName: 'QBittorrent' },
  { endpoint: 'sabnzbd', typeName: 'SABnzbd' },
  { endpoint: 'overseerr', typeName: 'Overseerr' },
  { endpoint: 'uptimekuma', typeName: 'Uptime Kuma' },
  { endpoint: 'speedtest', typeName: 'Speedtest' },
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
    const token = `test-token-${ds.endpoint}-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
    const url = 'http://127.0.0.1:9';
    const res = await request.post(`/admin/datasources/${ds.endpoint}/new`, { form: { token, url } });
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
    expect(afterHtml).not.toContain(`/admin/datasources/${ds.endpoint}/${id}/delete`);
  });
}

test('create + delete immich datasource via request', async ({ request }) => {
  const token = `test-token-immich-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
  const url = 'http://127.0.0.1:9';
  const config = JSON.stringify({ mode: 'random' });
  const res = await request.post('/admin/datasources/immich/new', { form: { token, url, config } });
  expect([200, 302]).toContain(res.status());

  const adminRes = await request.get('/admin/');
  expect(adminRes.ok()).toBeTruthy();
  const html = await adminRes.text();
  expect(html).toContain('Immich');
  expect(html).toContain(token);

  const id = extractDeleteId(html, 'immich');
  expect(id).not.toBeNull();

  const delRes = await request.post(`/admin/datasources/immich/${id}/delete`);
  expect([200, 302]).toContain(delRes.status());

  const after = await request.get('/admin/');
  const afterHtml = await after.text();
  expect(afterHtml).not.toContain(token);
  expect(afterHtml).not.toContain(`/admin/datasources/immich/${id}/delete`);
});

test.describe('new-form render per homelab type', () => {
  const formChecks: Array<{ endpoint: string; fields: string[] }> = [
    { endpoint: 'qbittorrent', fields: ['name="token"', 'name="url"'] },
    { endpoint: 'sabnzbd', fields: ['name="token"', 'name="url"'] },
    { endpoint: 'overseerr', fields: ['name="token"', 'name="url"'] },
    { endpoint: 'uptimekuma', fields: ['name="token"', 'name="url"'] },
    { endpoint: 'speedtest', fields: ['name="token"', 'name="url"'] },
    { endpoint: 'immich', fields: ['name="token"', 'name="url"', 'name="config"'] },
  ];

  for (const { endpoint, fields } of formChecks) {
    test(`${endpoint} new form returns 200 and contains ${fields.join(', ')}`, async ({ request }) => {
      const res = await request.get(`/admin/datasources/${endpoint}/new`);
      expect(res.status()).toBe(200);
      const html = await res.text();
      for (const f of fields) expect(html).toContain(f);
      expect(html).toContain(`/admin/datasources/${endpoint}/new`);
    });
  }
});

test('sidebar disclosure Add datasource contains homelab links', async ({ page }) => {
  await page.goto('/');
  await page.locator('summary', { hasText: 'Add datasource' }).click();
  await expect(page.getByRole('link', { name: 'Immich' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'QBittorrent' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'SABnzbd' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Overseerr' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Uptime Kuma' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Speedtest' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Immich' })).toHaveAttribute('href', '/admin/datasources/immich/new');
  await expect(page.getByRole('link', { name: 'QBittorrent' })).toHaveAttribute('href', '/admin/datasources/qbittorrent/new');
  await expect(page.getByRole('link', { name: 'SABnzbd' })).toHaveAttribute('href', '/admin/datasources/sabnzbd/new');
  await expect(page.getByRole('link', { name: 'Overseerr' })).toHaveAttribute('href', '/admin/datasources/overseerr/new');
  await expect(page.getByRole('link', { name: 'Uptime Kuma' })).toHaveAttribute('href', '/admin/datasources/uptimekuma/new');
  await expect(page.getByRole('link', { name: 'Speedtest' })).toHaveAttribute('href', '/admin/datasources/speedtest/new');
});
