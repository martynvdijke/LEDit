import { test, expect } from './fixtures';

type TokenUrlType = {
  endpoint: string;
  typeName: string;
  token: string;
  url: string;
};

const tokenUrlTypes: TokenUrlType[] = [
  { endpoint: 'transit', typeName: 'Transit', token: '900000003201', url: '' },
  { endpoint: 'pihole', typeName: 'Pi-hole', token: 'abc', url: '' },
  { endpoint: 'github', typeName: 'GitHub', token: 'octocat/hello-world', url: '' },
  { endpoint: 'sports', typeName: 'Sports', token: 'nfl', url: '' },
  { endpoint: 'sunmoon', typeName: 'Sun/Moon', token: '52.52,13.405', url: '' },
  { endpoint: 'jellyfin', typeName: 'Jellyfin', token: 'emby-key', url: 'http://localhost:8096' },
];

// Helper: extract delete id for endpoint from admin dashboard HTML
function extractDeleteId(html: string, endpoint: string): string | null {
  // Find all delete actions for endpoint, return first (most recent)
  const re = new RegExp(`/admin/datasources/${endpoint}/(\\d+)/delete`, 'g');
  let m: RegExpExecArray | null;
  let last: string | null = null;
  while ((m = re.exec(html)) !== null) last = m[1];
  return last;
}

for (const ds of tokenUrlTypes) {
  test(`create + delete ${ds.endpoint} datasource via request`, async ({ page, request }) => {
    const token = `${ds.token}-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
    // keep raw token for github/sports/transit/sunmoon where validation matters - use original token value
    // For uniqueness, use suffix only for delete lookup; but endpoint validation may reject suffix for github (owner/repo).
    // So use realistic base token for github/sports/transit/sunmoon, and unique suffix for others via url param or skip.
    // We handle per-type:
    let createToken = ds.token;
    if (ds.endpoint === 'pihole' || ds.endpoint === 'jellyfin') {
      createToken = `test-token-${ds.endpoint}-${Date.now()}`;
    }

    const form: Record<string, string> = { token: createToken, url: ds.url };
    const res = await request.post(`/admin/datasources/${ds.endpoint}/new`, { form });
    // POST should redirect (302); Playwright follows redirects, so final status is 200 or 302
    expect([200, 302]).toContain(res.status());

    // Verify via dashboard: fetch HTML and check TypeName appears and count
    const adminRes = await request.get('/admin/');
    expect(adminRes.ok()).toBeTruthy();
    const html = await adminRes.text();
    // Dashboard renders card title TypeName for transit/uptime/etc; check at least TypeName visible
    // For jellyfin/pi-hole token types, also visible in table; we just assert TypeName present.
    expect(html).toContain(ds.typeName);

    // Also verify table contains our token (for token/url types)
    expect(html).toContain(createToken);

    // Find delete id and clean up
    const id = extractDeleteId(html, ds.endpoint);
    expect(id).not.toBeNull();

    const delRes = await request.post(`/admin/datasources/${ds.endpoint}/${id}/delete`);
    expect([200, 302]).toContain(delRes.status());

    // Verify deletion
    const after = await request.get('/admin/');
    const afterHtml = await after.text();
    // Token no longer in table (if only one entry with that token, else at least delete succeeded via 302)
    // We assert delete POST was 302 and that the specific delete endpoint no longer in listing OR token gone.
    // For uniqueness we check token absence when we used unique token; for non-unique like nfl, skip token check.
    if (ds.endpoint === 'pihole' || ds.endpoint === 'jellyfin') {
      expect(afterHtml).not.toContain(createToken);
    } else {
      // generic: ensure at least the delete id action gone
      expect(afterHtml).not.toContain(`/admin/datasources/${ds.endpoint}/${id}/delete`);
    }

    // Edit page for deleted should redirect
    void page; // suppress unused
  });
}

test('uptime datasource create via url+config and new-form renders textarea', async ({ page, request }) => {
  const config = JSON.stringify([{ name: 'R', url: 'http://127.0.0.1:1', timeout_seconds: 2 }]);
  const res = await request.post('/admin/datasources/uptime/new', {
    form: { url: '', config },
  });
  expect([200, 302]).toContain(res.status());

  // New form should render textarea with name="config" (has_config=true)
  const newPage = await request.get('/admin/datasources/uptime/new');
  expect(newPage.status()).toBe(200);
  const html = await newPage.text();
  expect(html).toContain('name="config"');
  // Also check textarea element exists
  await page.goto('/admin/datasources/uptime/new');
  await expect(page.locator('#config')).toBeAttached();
  await expect(page.locator('textarea[name="config"]')).toBeAttached();

  // Dashboard shows Uptime card
  const admin = await request.get('/admin/');
  const adminHtml = await admin.text();
  expect(adminHtml).toContain('Uptime');

  // Cleanup: delete created uptime
  const id = extractDeleteId(adminHtml, 'uptime');
  if (id) {
    const del = await request.post(`/admin/datasources/uptime/${id}/delete`);
    expect([200, 302]).toContain(del.status());
  }
});

test('sidebar disclosure Add datasource contains all seven new links', async ({ page }) => {
  await page.goto('/');
  await page.locator('summary', { hasText: 'Add datasource' }).click();
  await expect(page.getByRole('link', { name: 'Transit' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Uptime' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Pi-hole' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'GitHub' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Sports' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Sun/Moon' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Jellyfin' })).toBeVisible();
  // Also verify hrefs point to /new pages
  await expect(page.getByRole('link', { name: 'Transit' })).toHaveAttribute('href', '/admin/datasources/transit/new');
  await expect(page.getByRole('link', { name: 'Uptime' })).toHaveAttribute('href', '/admin/datasources/uptime/new');
  await expect(page.getByRole('link', { name: 'Pi-hole' })).toHaveAttribute('href', '/admin/datasources/pihole/new');
  await expect(page.getByRole('link', { name: 'GitHub' })).toHaveAttribute('href', '/admin/datasources/github/new');
  await expect(page.getByRole('link', { name: 'Sports' })).toHaveAttribute('href', '/admin/datasources/sports/new');
  await expect(page.getByRole('link', { name: 'Sun/Moon' })).toHaveAttribute('href', '/admin/datasources/sunmoon/new');
  await expect(page.getByRole('link', { name: 'Jellyfin' })).toHaveAttribute('href', '/admin/datasources/jellyfin/new');
});

test.describe('new-form render per type', () => {
  const formChecks: Array<{ endpoint: string; field: string }> = [
    { endpoint: 'transit', field: 'name="token"' },
    { endpoint: 'pihole', field: 'name="token"' },
    { endpoint: 'github', field: 'name="token"' },
    { endpoint: 'sports', field: 'name="token"' },
    { endpoint: 'sunmoon', field: 'name="token"' },
    { endpoint: 'jellyfin', field: 'name="token"' },
    { endpoint: 'uptime', field: 'name="config"' },
  ];

  for (const { endpoint, field } of formChecks) {
    test(`${endpoint} new form returns 200 and contains ${field}`, async ({ request }) => {
      const res = await request.get(`/admin/datasources/${endpoint}/new`);
      expect(res.status()).toBe(200);
      const html = await res.text();
      expect(html).toContain(field);
      // Also ensure form action points to new endpoint
      expect(html).toContain(`/admin/datasources/${endpoint}/new`);
    });
  }
});
