import { test, expect } from '@playwright/test';
import { seedGeneralSettings } from './helpers/db';

// E2E runs with LEDIT_AUTH_DISABLE=true so admin routes are reachable without a
// login. Bundles are built by exporting a real playlist, then re-imported.

async function createPlaylist(request: any, name: string): Promise<void> {
  await seedGeneralSettings(request);
  const res = await request.post('/admin/playlists/new', {
    form: { name, enabled: 'on', items: '[]', schedule_windows: '[]' },
    maxRedirects: 0,
  });
  expect([200, 302]).toContain(res.status());
}

test('share page lists artifacts and the import form', async ({ page }) => {
  await page.goto('/admin/artifacts');
  await expect(page.locator('h1')).toContainText('Share Artifacts');
  await expect(page.locator('#artifact-import-form')).toBeVisible();
  await expect(page.locator('a[href="/admin/artifacts/catalog"]').first()).toBeVisible();
});

test('playlist bundle export and import round-trip', async ({ page, request }) => {
  const name = `PW-artifact-${Date.now()}`;
  await createPlaylist(request, name);

  await page.goto('/admin/playlists');
  const exportLink = page.locator(`a[href^="/admin/api/artifacts/export/playlist/"]`).last();
  await expect(exportLink).toBeVisible();

  const href = await exportLink.getAttribute('href');
  const exportRes = await request.get(href!);
  expect(exportRes.status()).toBe(200);
  expect(exportRes.headers()['content-type']).toContain('application/zip');
  const bundle = await exportRes.body();
  expect(bundle.length).toBeGreaterThan(0);

  // Re-import under a new name (conflict strategy rename).
  const copyName = `${name}-copy`;
  const importRes = await request.post('/admin/api/artifacts/import', {
    multipart: {
      file: { name: 'bundle.zip', mimeType: 'application/zip', buffer: bundle },
      name: copyName,
      on_conflict: 'rename',
    },
  });
  expect(importRes.status()).toBe(200);
  const body = await importRes.json();
  expect(body.imported).toBe(true);
  expect(body.kind).toBe('playlist');
  expect(body.name).toBe(copyName);
  expect(body.id).toBeGreaterThan(0);

  await page.goto('/admin/artifacts');
  await expect(page.locator('.card', { hasText: 'Shareable artifacts' })).toBeVisible();
});

test('conflicting name returns a stable name_conflict error', async ({ request }) => {
  const name = `PW-conflict-${Date.now()}`;
  await createPlaylist(request, name);

  await request.get('/admin/playlists');
  const listRes = await request.get('/admin/playlists');
  expect(listRes.status()).toBe(200);

  // Find the export link for our playlist by scanning the page.
  const html = await listRes.text();
  const matches = [...html.matchAll(/\/admin\/api\/artifacts\/export\/playlist\/(\d+)/g)].map((m) => m[1]);
  expect(matches.length).toBeGreaterThan(0);
  const exportRes = await request.get(`/admin/api/artifacts/export/playlist/${matches[matches.length - 1]}`);
  expect(exportRes.status()).toBe(200);
  const bundle = await exportRes.body();

  const conflict = await request.post('/admin/api/artifacts/import', {
    multipart: { file: { name: 'bundle.zip', mimeType: 'application/zip', buffer: bundle } },
  });
  expect(conflict.status()).toBe(409);
  const conflictBody = await conflict.json();
  expect(conflictBody.error).toBe('name_conflict');
  expect(conflictBody.details.kind).toBe('playlist');

  const skipped = await request.post('/admin/api/artifacts/import', {
    multipart: {
      file: { name: 'bundle.zip', mimeType: 'application/zip', buffer: bundle },
      on_conflict: 'skip',
    },
  });
  expect(skipped.status()).toBe(200);
  const skippedBody = await skipped.json();
  expect(skippedBody.skipped).toBe(true);
  expect(skippedBody.imported).toBe(false);
});

test('dry run validates without writing', async ({ request }) => {
  const source = `PW-dryrun-src-${Date.now()}`;
  await createPlaylist(request, source);
  const listRes = await request.get('/admin/playlists');
  const html = await listRes.text();
  const ids = [...html.matchAll(/\/admin\/api\/artifacts\/export\/playlist\/(\d+)/g)].map((m) => m[1]);
  const exportRes = await request.get(`/admin/api/artifacts/export/playlist/${ids[ids.length - 1]}`);
  const bundle = await exportRes.body();

  const target = `PW-dryrun-${Date.now()}`;
  const dry = await request.post('/admin/api/artifacts/import', {
    multipart: {
      file: { name: 'bundle.zip', mimeType: 'application/zip', buffer: bundle },
      name: target,
      on_conflict: 'rename',
      dry_run: '1',
    },
  });
  expect(dry.status()).toBe(200);
  const dryBody = await dry.json();
  expect(dryBody.imported).toBe(false);
  expect(dryBody.kind).toBe('playlist');

  const after = await (await request.get('/admin/playlists')).text();
  expect(after).not.toContain(target);
});

test('invalid bundle is rejected without a write', async ({ request }) => {
  const res = await request.post('/admin/api/artifacts/import', {
    multipart: {
      file: { name: 'bundle.zip', mimeType: 'application/zip', buffer: Buffer.from('not a zip') },
    },
  });
  expect(res.status()).toBe(400);
  const body = await res.json();
  expect(body.error).toBe('invalid_zip');
});

test('catalog page renders guidance when unconfigured', async ({ page }) => {
  await page.goto('/admin/artifacts/catalog');
  await expect(page.locator('h1')).toContainText('Artifact Catalog');
  await expect(page.locator('body')).toContainText('ARTIFACT_CATALOG_URL');
});

test('deep link prefills the form without fetching', async ({ page }) => {
  const url = 'https://example.test/ledit-theme.zip';
  await page.goto(`/admin/artifacts?url=${encodeURIComponent(url)}&sha256=abc123&name=Prefilled&on_conflict=rename`);
  await expect(page.locator('#artifact-url')).toHaveValue(url);
  await expect(page.locator('#artifact-sha')).toHaveValue('abc123');
  await expect(page.locator('#artifact-name')).toHaveValue('Prefilled');
  await expect(page.locator('#artifact-conflict')).toHaveValue('rename');
});
