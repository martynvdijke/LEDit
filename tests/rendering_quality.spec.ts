import { test, expect } from './fixtures';

test.describe('Rendering quality', () => {
  test('theme font: GET /admin/themes/new/edit contains PixelifySans.ttf', async ({ request }) => {
    const res = await request.get('/admin/themes/new/edit');
    expect(res.status()).toBe(200);
    const html = await res.text();
    expect(html).toContain('PixelifySans.ttf');
    expect(html).toContain('name="font_name"');
  });

  test('theme create with PixelifySans.ttf succeeds', async ({ request }) => {
    const name = `PW-Theme-${Date.now()}-${Math.random().toString(36).slice(2, 4)}`;
    const res = await request.post('/admin/themes/new/edit', {
      form: {
        name,
        bg_color: '#282a36',
        accent_color: '#50fa7b',
        text_color: '#8be9fd',
        title: 'CUSTOM',
        font_size: '24',
        font_name: 'PixelifySans.ttf',
      },
    });
    // Success redirects (302) to /admin/themes
    expect([200, 302]).toContain(res.status());
    const location = res.headers()['location'] ?? '';
    // Playwright follows redirects; check via list page
    const list = await request.get('/admin/themes');
    expect(list.status()).toBe(200);
    const html = await list.text();
    expect(html).toContain(name);

    // cleanup: find id and delete
    const m = html.match(new RegExp(`/admin/themes/(\\d+)/edit[^>]*>[^<]*${name}|${name}[\\s\\S]{0,500}/admin/themes/(\\d+)/edit`, 'i'));
    // alternative: scan for id near name
    let themeId = '';
    const idx = html.indexOf(name);
    if (idx >= 0) {
      const snippet = html.slice(Math.max(0, idx - 1500), idx + 1500);
      const mm = snippet.match(/\/admin\/themes\/(\d+)\/edit/);
      if (mm) themeId = mm[1];
      const mm2 = snippet.match(/\/admin\/themes\/(\d+)\/delete/);
      if (!themeId && mm2) themeId = mm2[1];
    }
    if (themeId) {
      await request.post(`/admin/themes/${themeId}/delete`).catch(() => {});
    }
    void location;
  });

  test('theme create with invalid font ../x.ttf is rejected (no redirect)', async ({ request }) => {
    const res = await request.post('/admin/themes/new/edit', {
      form: {
        name: `PW-BadFont-${Date.now()}`,
        bg_color: '#282a36',
        accent_color: '#50fa7b',
        text_color: '#8be9fd',
        title: 'CUSTOM',
        font_size: '24',
        font_name: '../x.ttf',
      },
    });
    // Validation failure re-renders with 200 and no Location header
    expect(res.status()).toBe(200);
    expect(res.headers()['location'] ?? '').toBe('');
  });

  test('device create with output_bilinear and panel JSON succeeds', async ({ request }) => {
    const dName = `PW-RenderOK-${Date.now()}-${Math.random().toString(36).slice(2, 4)}`;
    const res = await request.post('/admin/devices/new', {
      form: {
        name: dName,
        ip: '127.0.0.1',
        port: '6270',
        width: '64',
        height: '64',
        refresh_interval: '2',
        enabled: 'on',
        panel_cols: '2',
        panel_gap: '0',
        output_bilinear: 'on',
        output_panel_gammas: '[1,2.2]',
        output_panel_color_orders: '["RGB","GRB"]',
      },
    });
    expect([200, 302]).toContain(res.status());
    const html = await (await request.get('/admin/devices')).text();
    expect(html).toContain(dName);

    // Verify device actually stored output fields via html? At least name present means success
    // Cleanup
    const idx = html.indexOf(dName);
    if (idx >= 0) {
      const snippet = html.slice(Math.max(0, idx - 1200), idx + 1200);
      const m = snippet.match(/\/admin\/devices\/(\d+)\/delete/);
      if (m) await request.post(`/admin/devices/${m[1]}/delete`).catch(() => {});
    }
  });

  test('device create with invalid gamma [9] is rejected (redirect with flash)', async ({ request }) => {
    const dName = `PW-RenderBad-${Date.now()}-${Math.random().toString(36).slice(2, 4)}`;
    await request.post('/admin/devices/new', {
      form: {
        name: dName,
        ip: '127.0.0.1',
        port: '6270',
        width: '64',
        height: '64',
        refresh_interval: '2',
        enabled: 'on',
        panel_cols: '2',
        output_panel_gammas: '[9]',
        output_panel_color_orders: '[]',
      },
    });
    const html = await (await request.get('/admin/devices')).text();
    // Invalid gamma should NOT create device (validation redirects back without creating)
    expect(html).not.toContain(dName);
  });
});
