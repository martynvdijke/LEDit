import { test, expect } from './fixtures';

function pwName(p: string) { return `PW-${test.info().parallelIndex}-${Math.random().toString(36).slice(2, 6)}-${p}`; }

test.describe('Screensaver E2E', () => {
  test('playlist with screensaver:starfield + weather animates over WS', async ({ page, request, wsFeed }) => {
    const wRes = await request.post('/admin/datasources/weather/new', {
      form: { token: 'tok', url: 'http://example.com/weather' },
    });
    expect([200, 302].includes(wRes.status())).toBeTruthy();
    let weatherId = 1;
    for (let i = 1; i < 20; i++) {
      const pr = await request.get(`/admin/preview?type=weather&id=${i}&w=64&h=64`);
      if (pr.status() === 200) { weatherId = i; break; }
    }

    const plName = pwName('ScreenPl');
    await page.goto('/admin/playlists/new');
    await page.fill('#name', plName);
    // Set items directly via JS (screensaver:0 + weather)
    const items = JSON.stringify([
      { source_type: 'screensaver', source_id: 0 },
      { source_type: 'weather', source_id: weatherId },
    ]);
    await page.evaluate((v) => {
      const el = document.getElementById('items') as HTMLInputElement;
      if (el) el.value = v;
    }, items);
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/playlists/);

    const plList = await (await request.get('/admin/playlists')).text();
    const all = [...plList.matchAll(/\/admin\/playlists\/(\d+)\/edit/g)].map(x => x[1]);
    const playlistId = all[all.length - 1] || '';
    expect(playlistId).not.toBe('');

    const devName = pwName('ScreenDev');
    const devRes = await request.post('/admin/devices/new', {
      form: {
        name: devName,
        ip: '127.0.0.1',
        port: '6270',
        width: '64',
        height: '64',
        refresh_interval: '1',
        enabled: 'on',
        content_mode: 'playlist',
        playlist_id: playlistId,
      },
    });
    expect([200, 302].includes(devRes.status())).toBeTruthy();

    const devHtml = await (await request.get('/admin/devices')).text();
    const idx = devHtml.indexOf(devName);
    const snippet = devHtml.slice(Math.max(0, idx - 1200), idx + 1200);
    const dm = snippet.match(/\/admin\/devices\/(\d+)\/preview/) || snippet.match(/\/admin\/devices\/(\d+)\/delete/);
    const deviceId = dm ? dm[1] : '';
    expect(deviceId).not.toBe('');

    await page.goto('/');
    const feed = await wsFeed(`/ws/device/${deviceId}/preview`);
    const f1 = await feed.nextFrame(8000);
    expect(f1.format).toBe('PNG');
    const f2 = await feed.nextFrame(8000);
    expect(f2.format).toBe('PNG');
    const images: string[] = [f1.image as string, f2.image as string];
    for (let i = 0; i < 4; i++) {
      try { const f = await feed.nextFrame(3000); if (f.image) images.push(f.image as string); } catch {}
    }
    expect(new Set(images).size).toBeGreaterThanOrEqual(2);

    await request.post(`/admin/devices/${deviceId}/delete`).catch(() => {});
    await request.post(`/admin/playlists/${playlistId}/delete`).catch(() => {});
    for (let i = 1; i < 20; i++) await request.post(`/admin/datasources/weather/${i}/delete`).catch(() => {});
  });
});
