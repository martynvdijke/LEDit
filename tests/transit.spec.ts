import { test, expect } from './fixtures';
import * as http from 'node:http';

function pwName(p: string) {
  return `PW-${test.info().parallelIndex}-${Math.random().toString(36).slice(2, 6)}-${p}`;
}

// Minimal departures endpoint returning the common ("custom") JSON shape with
// future times so route filtering and walk-time keep at least one row.
function startMockDepartures(): Promise<{ url: string; close: () => Promise<void>; hits: () => number }> {
  let hits = 0;
  const server = http.createServer((_req, res) => {
    hits += 1;
    const iso = (mins: number) => new Date(Date.now() + mins * 60_000).toISOString();
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(
      JSON.stringify({
        departures: [
          { line: 'S7', destination: 'Potsdam', time: iso(10) },
          { line: 'U2', destination: 'Ruhleben', time: iso(12) },
          { line: 'U1', destination: 'Warschauer', time: iso(15) },
        ],
      }),
    );
  });
  return new Promise((resolve) => {
    server.listen(0, '127.0.0.1', () => {
      const addr = server.address();
      const port = typeof addr === 'object' && addr ? addr.port : 0;
      resolve({
        url: `http://127.0.0.1:${port}`,
        close: () => new Promise((r) => server.close(() => r())),
        hits: () => hits,
      });
    });
  });
}

test.describe('Transit datasource E2E', () => {
  test('admin creates transit source, list/feed render, filter+walk persisted', async ({ page, request }) => {
    const mock = await startMockDepartures();
    const stop = pwName('STOP');
    try {
      await page.goto('/admin/datasources/transit/new');
      await expect(page.locator('h1')).toContainText('Transit');

      await page.fill('#token', stop);
      await page.fill('#url', `${mock.url}/%s`);
      await page.selectOption('#provider', 'custom');
      await page.fill('#max_departures', '2');
      await page.fill('#route_filter', 'S7, U1');
      await page.fill('#walk_time_min', '3');
      await page.fill('#timezone', 'UTC');
      await page.selectOption('#time_mode', 'minutes');
      await page.click('button[type="submit"]');
      await expect(page).toHaveURL(/\/admin\/$/);

      // Appears on the dashboard sources list.
      const row = page.locator(`tr:has-text("${stop}")`);
      await expect(row).toBeVisible({ timeout: 5000 });
      await expect(row).toContainText('Transit');

      // Look up the created id and confirm the configuration persisted.
      const listRes = await request.get('/admin/api/transit');
      expect(listRes.ok()).toBeTruthy();
      const rows = (await listRes.json()) as Array<Record<string, unknown>>;
      const created = rows.find((r) => r.token === stop);
      expect(created).toBeTruthy();
      const id = Number(created?.id ?? created?.ID);
      expect(id).toBeGreaterThan(0);
      expect(created?.route_filter).toBe('S7, U1');
      expect(created?.walk_time_min).toBe(3);
      expect(created?.provider).toBe('custom');

      // The datasource renders a PNG (fetches the mock departures endpoint).
      const preview = await request.get(`/admin/preview?type=transit&id=${id}&w=64&h=64`);
      expect(preview.status()).toBe(200);
      expect(preview.headers()['content-type']).toContain('image/png');
      expect(mock.hits()).toBeGreaterThan(0);

      // Cleanup.
      await request.delete(`/admin/api/transit/${id}`).catch(() => {});
    } finally {
      await mock.close();
    }
  });
});
