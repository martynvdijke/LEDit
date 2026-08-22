import { test as base, expect } from '@playwright/test';
import { seedGeneralSettings, cleanupTestDevices } from './helpers/db';
import { createWsFeed, WsFeed } from './helpers/ws';

type Fixtures = {
  adminApi: {
    seedSettings: (opts?: { timeout?: string; width?: string; height?: string; random?: boolean }) => Promise<void>;
    createDevice: (opts: { name: string; width?: number; height?: number; refreshInterval?: number; ip?: string }) => Promise<{ id: string }>;
    deleteDevice: (id: string) => Promise<void>;
    cleanupDevices: (prefix?: string) => Promise<void>;
  };
  wsFeed: (url: string) => Promise<WsFeed>;
};

export const test = base.extend<Fixtures & { seedCleanState: void }>({
  seedCleanState: [async ({ request }, use) => {
    await seedGeneralSettings(request);
    await cleanupTestDevices(request, 'PW-');
    await use();
    // teardown
    await cleanupTestDevices(request, 'PW-');
  }, { auto: true }],

  adminApi: async ({ request }, use) => {
    const api = {
      seedSettings: (opts?: { timeout?: string; width?: string; height?: string; random?: boolean }) => seedGeneralSettings(request, opts),
      createDevice: async (opts: { name: string; width?: number; height?: number; refreshInterval?: number; ip?: string }) => {
        const width = String(opts.width ?? 64);
        const height = String(opts.height ?? 64);
        const refresh = String(opts.refreshInterval ?? 2);
        const ip = opts.ip ?? '127.0.0.1';
        const res = await request.post('/admin/devices/new', {
          form: { name: opts.name, ip, port: '6270', width, height, refresh_interval: refresh, enabled: 'on' },
        });
        // Try to extract id from devices list
        const list = await request.get('/admin/devices');
        const html = await list.text();
        // Find row containing name and extract device id from preview link /admin/devices/<id>/preview
        const esc = opts.name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        const re = new RegExp(`/admin/devices/(\\d+)/preview[^>]*>[^<]*</a>[\\s\\S]{0,300}${esc}|${esc}[\\s\\S]{0,500}/admin/devices/(\\d+)/preview`, 'i');
        // Fallback: search for name then id
        let id = '';
        const idx = html.indexOf(opts.name);
        if (idx >= 0) {
          const snippet = html.slice(Math.max(0, idx - 1200), idx + 1200);
          const m = snippet.match(/\/admin\/devices\/(\d+)\/preview/);
          if (m) id = m[1];
          if (!id) {
            const m2 = snippet.match(/\/admin\/devices\/(\d+)\/delete/);
            if (m2) id = m2[1];
          }
        }
        if (!id) {
          // try global
          const m = html.match(/\/admin\/devices\/(\d+)\/preview/);
          if (m) id = m[1];
        }
        return { id };
      },
      deleteDevice: async (id: string) => {
        await request.post(`/admin/devices/${id}/delete`).catch(() => {});
      },
      cleanupDevices: (prefix?: string) => cleanupTestDevices(request, prefix ?? 'PW-'),
    };
    await use(api);
  },

  wsFeed: async ({ page }, use) => {
    const feeds: WsFeed[] = [];
    const factory = async (url: string) => {
      const f = await createWsFeed(page, url);
      feeds.push(f);
      return f;
    };
    await use(factory);
    for (const f of feeds) await f.close().catch(() => {});
  },
});

export { expect };
export { createWsFeed, WsFeed } from './helpers/ws';
