import type { Page, APIRequestContext } from '@playwright/test';

// Seed GeneralSettings via POST /admin/settings form.
// Uses page.request so it works even without navigation.
export async function seedGeneralSettings(
  request: APIRequestContext,
  opts: { timeout?: string; width?: string; height?: string; random?: boolean } = {},
) {
  const timeout = opts.timeout ?? '1';
  const width = opts.width ?? '64';
  const height = opts.height ?? '64';
  const form: Record<string, string> = { timeout, width, height };
  if (opts.random) form.random = 'on';
  await request.post('/admin/settings', { form });
}

// Delete test-named devices via admin UI delete endpoints discovered from /admin/devices.
// Removes any device whose name starts with prefix (default PW-).
export async function cleanupTestDevices(request: APIRequestContext, prefix = 'PW-') {
  // We need to scrape device ids by fetching the page HTML via request
  const res = await request.get('/admin/devices');
  if (!res.ok()) return;
  const html = await res.text();
  // Extract device ids and names from delete form actions and table rows
  // Delete form: /admin/devices/:id/delete, name visible nearby
  // Simple heuristic: find all delete form actions
  const re = /\/admin\/devices\/(\d+)\/delete/g;
  let m: RegExpExecArray | null;
  const ids: string[] = [];
  while ((m = re.exec(html)) !== null) ids.push(m[1]);

  // Only delete those whose row contains prefix - we check each page row by fetching? Easier: delete any with prefix by checking html snippet around id
  for (const id of ids) {
    // Cheap check: if prefix appears near the id in html, delete; otherwise if no prefix check still delete PW- only?
    // To avoid deleting non-test data, only delete if prefix found in surrounding 500 chars or html contains prefix at all when prefix filtering
    const idx = html.indexOf(`/admin/devices/${id}/delete`);
    const snippet = html.slice(Math.max(0, idx - 800), idx + 800);
    if (prefix && !snippet.includes(prefix)) continue;
    await request.post(`/admin/devices/${id}/delete`).catch(() => {});
  }
}

// Ensure GeneralSettings seeded and clean test devices.
export async function ensureCleanState(request: APIRequestContext) {
  await seedGeneralSettings(request);
  await cleanupTestDevices(request, 'PW-');
}
