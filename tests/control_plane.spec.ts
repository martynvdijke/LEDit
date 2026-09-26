import { test, expect } from './fixtures';

function extractDeleteId(html: string, endpoint: string): string | null {
  const re = new RegExp(`/admin/datasources/${endpoint}/(\\d+)/delete`, 'g');
  let m: RegExpExecArray | null;
  let last: string | null = null;
  while ((m = re.exec(html)) !== null) last = m[1];
  return last;
}

test('GET /api/control/actions returns array', async ({ request }) => {
  const res = await request.get('/api/control/actions');
  expect(res.status()).toBe(200);
  const body = await res.json();
  expect(Array.isArray(body.actions ?? body)).toBeTruthy();
});

test('GET /api/control/actions with source_type returns actions array', async ({ request }) => {
  const res = await request.get('/api/control/actions?source_type=homeassistant');
  expect(res.status()).toBe(200);
  const body = await res.json() as any;
  expect(body.source_type).toBe('homeassistant');
  expect(Array.isArray(body.actions)).toBeTruthy();
  expect(body.actions).toContain('call_service');
});

test('POST /api/control/execute unknown action returns 400', async ({ request }) => {
  const res = await request.post('/api/control/execute', {
    data: { source_type: 'homeassistant', source_id: 1, action: 'unknown_action_xyz', params: {} },
  });
  expect(res.status()).toBe(400);
  const body = await res.json() as any;
  expect(body.error).toMatch(/unknown_action/i);
});

test('POST /api/control/execute unconfigured source returns 400/501/502', async ({ request }) => {
  // Use a valid action but non-existent source id
  const res = await request.post('/api/control/execute', {
    data: { source_type: 'homeassistant', source_id: 999999, action: 'call_service', params: { service: 'light.turn_on' } },
  });
  expect([400, 501, 502]).toContain(res.status());
});

test('create homeassistant datasource then control actions includes it', async ({ request }) => {
  const token = `ha-ctrl-${Date.now()}`;
  const createRes = await request.post('/admin/datasources/homeassistant/new', {
    form: { token, url: 'http://127.0.0.1:9' },
  });
  expect([200, 302]).toContain(createRes.status());

  const actionsRes = await request.get('/api/control/actions?source_type=homeassistant');
  expect(actionsRes.status()).toBe(200);
  const body = await actionsRes.json() as any;
  expect(body.actions).toContain('call_service');

  // Cleanup
  const html = await (await request.get('/admin/')).text();
  const id = extractDeleteId(html, 'homeassistant');
  // Find id matching our token (last created)
  if (id) {
    // Delete all matching and verify token gone - delete first
    const re = new RegExp(`/admin/datasources/homeassistant/(\\d+)/delete`, 'g');
    let m: RegExpExecArray | null;
    let ids: string[] = [];
    while ((m = re.exec(html)) !== null) ids.push(m[1]);
    // delete in reverse (newest first) until token gone
    for (const delId of ids.reverse()) {
      const beforeHtml = await (await request.get('/admin/')).text();
      if (!beforeHtml.includes(token)) break;
      await request.post(`/admin/datasources/homeassistant/${delId}/delete`);
      const afterHtml = await (await request.get('/admin/')).text();
      if (!afterHtml.includes(token)) break;
    }
  }
});

test('POST /api/eventrules/:id/simulate non-existent returns 404', async ({ request }) => {
  const res = await request.post('/api/eventrules/999999/simulate');
  expect(res.status()).toBe(404);
});
