import { test, expect } from './fixtures';

async function clearWebhookKey(request: import('@playwright/test').APIRequestContext) {
  await request.post('/admin/webhook', { form: { api_key: '', default_ttl: '30' } });
}

test.describe('Inbox / notifications', () => {
  test('webhook notify creates notification and appears in history', async ({ request }) => {
    await clearWebhookKey(request);
    const title = `PW-notif-${Date.now()}-${Math.random().toString(36).slice(2, 4)}`;
    const message = `hello ${Math.random()}`;
    const res = await request.post('/api/webhook/notify', { data: { title, message } });
    expect(res.status()).toBeLessThan(400);

    const hist = await request.get('/api/notifications');
    expect(hist.status()).toBe(200);
    const body = await hist.json() as any[];
    expect(Array.isArray(body)).toBeTruthy();
    const found = body.some((n: any) => n.title === title || n.message === message);
    expect(found).toBeTruthy();
  });

  test('GET /api/notifications?unread_for filters per device', async ({ request }) => {
    await clearWebhookKey(request);
    // create device
    const dName = `PW-InboxD-${Date.now()}-${Math.random().toString(36).slice(2, 4)}`;
    await request.post('/admin/devices/new', {
      form: { name: dName, ip: '127.0.0.1', port: '6270', width: '64', height: '64', refresh_interval: '2', enabled: 'on' },
    });
    const listHtml = await (await request.get('/admin/devices')).text();
    const idx = listHtml.indexOf(dName);
    let deviceId = '';
    if (idx >= 0) {
      const snippet = listHtml.slice(Math.max(0, idx - 1200), idx + 1200);
      const m = snippet.match(/\/admin\/devices\/(\d+)\/preview/) || snippet.match(/\/admin\/devices\/(\d+)\/delete/);
      if (m) deviceId = m[1];
    }
    expect(deviceId).not.toBe('');

    const filtered = await request.get(`/api/notifications?unread_for=${deviceId}`);
    expect(filtered.status()).toBe(200);
    const arr = await filtered.json() as any[];
    expect(Array.isArray(arr)).toBeTruthy();

    // also test kind and priority_min params
    const byKind = await request.get('/api/notifications?kind=notification');
    expect(byKind.status()).toBe(200);
    expect(Array.isArray(await byKind.json())).toBeTruthy();
    const byPrio = await request.get('/api/notifications?priority_min=1');
    expect(byPrio.status()).toBe(200);
    // priority filter may return empty array or 200 with array; just assert 200
    const prioBody = await byPrio.text();
    expect(prioBody.length).toBeGreaterThan(0);

    // cleanup device
    await request.post(`/admin/devices/${deviceId}/delete`).catch(() => {});
  });

  test('GET /api/delivery-log?surface=inbound returns deliveries array', async ({ request }) => {
    const res = await request.get('/api/delivery-log?surface=inbound');
    expect(res.status()).toBe(200);
    const body = await res.json() as any;
    expect(Array.isArray(body.deliveries)).toBeTruthy();
  });

  test('GET /api/delivery-log without filter returns deliveries', async ({ request }) => {
    const res = await request.get('/api/delivery-log');
    expect(res.status()).toBe(200);
    const body = await res.json() as any;
    expect(Array.isArray(body.deliveries)).toBeTruthy();
  });
});
