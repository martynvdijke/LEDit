import { test, expect } from './fixtures';

function pwName(p: string) {
  return `PW-${test.info().parallelIndex}-${Math.random().toString(36).slice(2, 6)}-${p}`;
}

test.describe('Now Playing datasource E2E', () => {
  test('admin creates source → list & feed options → unconfigured preview placeholder', async ({ page, request }) => {
    const name = pwName('NowPlaying');

    await page.goto('/admin/nowplaying/new');
    await expect(page.locator('h1')).toContainText('Now Playing');
    await page.fill('#name', name);
    await page.selectOption('#provider', 'jellyfin');
    await page.fill('#url', 'http://127.0.0.1:8096');
    await page.fill('#token', 'tok');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/nowplaying$/);
    await expect(page.locator(`tr:has-text("${name}")`)).toBeVisible({ timeout: 5000 });

    // The source should be selectable in the feed/playlist source options.
    const optionsPage = await request.get('/admin/playlists/new');
    expect(optionsPage.ok()).toBeTruthy();
    expect(await optionsPage.text()).toContain(name);

    // An unconfigured source (Spotify without a token) renders a placeholder
    // PNG with a 200 rather than erroring.
    const unconfiguredName = pwName('Unconfigured');
    const createRes = await request.post('/admin/api/nowplaying', {
      data: { name: unconfiguredName, provider: 'spotify' },
    });
    expect(createRes.status()).toBe(201);
    const created = (await createRes.json()) as any;
    const id = created.ID ?? created.id;
    expect(id).toBeTruthy();

    const preview = await request.get(`/admin/preview?type=nowplaying&id=${id}&w=64&h=64`);
    expect(preview.status()).toBe(200);
    expect(preview.headers()['content-type']).toContain('image/png');

    // Cleanup both rows.
    const listRes = await request.get('/admin/api/nowplaying');
    const list = (await listRes.json()) as any[];
    for (const row of list) {
      const rowName = row.name ?? row.Name;
      if (rowName === name || rowName === unconfiguredName) {
        const rowID = row.ID ?? row.id;
        await request.delete(`/admin/api/nowplaying/${rowID}`).catch(() => {});
      }
    }
  });
});
