import { test, expect } from './fixtures';

// Visual layout editor: template, drag, source assignment, live preview,
// persistence round-trip, keyboard editing and reduced-motion operability.

type Region = { id: string; x: number; y: number; w: number; h: number; source_type?: string; source_id?: number };

declare global {
  interface Window {
    __layoutDebug?: { convert(deltaPx: number, scale: number): number; getRegions(): Region[]; setScale(s: number): void };
  }
}

test.describe('Visual layout editor', () => {
  test('header/footer template, drag, source, preview, save and keyboard edit', async ({ page }) => {
    page.on('dialog', (d) => d.accept());

    await page.goto('/admin/layouts/new');
    await page.locator('[data-field="name-visible"]').fill('PW-Layout');
    await page.locator('[data-template="header-footer"]').click();
    await expect(page.locator('[data-region][data-region-id]')).toHaveCount(3);

    // Live preview renders through the compositor after the debounce.
    await expect.poll(
      () => page.locator('[data-live-preview-img]').getAttribute('src'),
      { timeout: 8000 },
    ).toMatch(/^blob:/);

    const before = await page.evaluate(() => window.__layoutDebug!.getRegions());

    // Drag the middle region down by a snapped amount.
    const mid = page.locator('[data-region][data-region-id="r2"]');
    await mid.click();
    const box = (await mid.boundingBox())!;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2 + 8, { steps: 6 });
    await page.mouse.up();

    const afterDrag = await page.evaluate(() => window.__layoutDebug!.getRegions());
    const r2After = afterDrag.find((r) => r.id === 'r2')!;
    const r2Before = before.find((r) => r.id === 'r2')!;
    expect(r2After.y).toBeGreaterThan(r2Before.y);

    // Keyboard nudge moves one snap step (gap default 2). Nudge up: the drag
    // above lands the region on the bottom edge, where ArrowDown is a clamped no-op.
    await page.locator('[data-region][data-region-id="r2"]').click();
    await expect(page.locator('[data-region][data-region-id="r2"]')).toBeFocused();
    await page.keyboard.press('ArrowUp');
    const afterKey = await page.evaluate(() => window.__layoutDebug!.getRegions());
    expect(afterKey.find((r) => r.id === 'r2')!.y).toBe(r2After.y - 2);

    // Assign a source via the inspector.
    await page.locator('[data-region][data-region-id="r1"]').click();
    await page.locator('[data-field="source"]').selectOption('clock:0');
    const withSource = await page.evaluate(() => window.__layoutDebug!.getRegions());
    expect(withSource.find((r) => r.id === 'r1')!.source_type).toBe('clock');

    // Save and confirm the persisted config round-trips.
    await page.locator('[data-save]').click();
    await page.waitForURL(/\/admin\/layouts\/\d+\/edit/);

    await page.reload();
    const reloaded = await page.evaluate(() => window.__layoutDebug!.getRegions());
    expect(reloaded).toHaveLength(3);
    expect(reloaded.find((r) => r.id === 'r1')!.source_type).toBe('clock');
    expect(reloaded.find((r) => r.id === 'r2')!.y).toBe(afterKey.find((r) => r.id === 'r2')!.y);

    // Cleanup.
    await page.goto('/admin/layouts');
    await page.getByRole('row', { name: /PW-Layout/ }).getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByRole('row', { name: /PW-Layout/ })).toHaveCount(0);
  });

  test('viewport to logical conversion is scale-aware', async ({ page }) => {
    await page.goto('/admin/layouts/new');
    const conv = await page.evaluate(() => ({
      s1: window.__layoutDebug!.convert(10, 1),
      s2: window.__layoutDebug!.convert(10, 2),
    }));
    expect(conv.s1).toBe(10);
    expect(conv.s2).toBe(5);
  });

  test('reduced motion keeps controls operable', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    page.on('dialog', (d) => d.accept());
    await page.goto('/admin/layouts/new');
    for (const sel of ['[data-undo]', '[data-redo]', '[data-add-region]', '[data-template="blank"]', '[data-save]']) {
      await expect(page.locator(sel)).toBeVisible();
    }
    await page.locator('[data-add-region]').click();
    await expect(page.locator('[data-region][data-region-id]')).toHaveCount(1);
  });

  test('preview rejects an out-of-bounds rectangle', async ({ request }) => {
    const res = await request.post('/admin/preview/layout', {
      form: {
        name: 'TooBig',
        mode: 'absolute',
        rows: '1',
        cols: '1',
        gap: '0',
        padding: '0',
        background: '#282a36',
        canvas_w: '64',
        canvas_h: '64',
        w: '64',
        h: '64',
        regions: JSON.stringify([{ id: 'a', x: 40, y: 0, w: 40, h: 64 }]),
      },
    });
    expect(res.status()).toBe(400);
  });
});
