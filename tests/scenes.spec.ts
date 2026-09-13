import { test, expect, type Page } from './fixtures';

async function clearScenes(page: Page): Promise<void> {
  await page.goto('/admin/scenes');
  let buttons = page.locator('table form[action$="/delete"] button');
  while ((await buttons.count()) > 0) {
    await buttons.first().click();
    await page.waitForLoadState('networkidle');
    await page.goto('/admin/scenes');
    buttons = page.locator('table form[action$="/delete"] button');
  }
}

test.describe('Ambient scenes admin', () => {
  test.setTimeout(90_000);

  test('create with compound trigger, list badges, preview takes over the wall', async ({ page, wsFeed }) => {
    await clearScenes(page);
    const name = `PW-Scene-${Math.random().toString(36).slice(2, 6)}`;

    await page.goto('/admin/scenes/new');
    await page.fill('#name', name);
    await page.fill('#priority', '10');
    await page.fill('#ttl_seconds', '300');

    // Compound all-of trigger: temperature > 28 AND motion == on.
    const first = page.locator('.trigger-group .condition-row').nth(0);
    await first.locator('.cond-entity').fill('sensor.temp');
    await first.locator('.cond-op').selectOption('>');
    await first.locator('.cond-value').fill('28');
    await page.locator('.trigger-group button:has-text("Add condition")').click();
    const second = page.locator('.trigger-group .condition-row').nth(1);
    await second.locator('.cond-entity').fill('binary_sensor.motion');
    await second.locator('.cond-op').selectOption('==');
    await second.locator('.cond-value').fill('on');

    // Actions: pin Clock, brightness 40, overlay text.
    await page.selectOption('#group-select', 'clock');
    await page.selectOption('#source-select', '0');
    await page.fill('#brightness_level', '40');
    await page.fill('#overlay_text', 'Cool down');
    await page.click('#scene-form button[type="submit"]');

    await expect(page).toHaveURL(/\/admin\/scenes$/);
    await expect(page.locator('table')).toContainText(name);
    // Per-condition evaluation badges (HA unconfigured -> unknown).
    await expect(page.locator('table')).toContainText('sensor.temp > 28');
    await expect(page.locator('table')).toContainText('binary_sensor.motion == on');
    await expect(page.locator('table')).toContainText('unknown');

    // Edit form renders and round-trips saved values.
    const html = await page.content();
    const idMatch = html.match(/\/admin\/scenes\/(\d+)\/edit/);
    expect(idMatch).not.toBeNull();
    const editRes = await page.request.get(`/admin/scenes/${idMatch![1]}/edit`);
    expect(editRes.status()).toBe(200);
    const editHtml = await editRes.text();
    expect(editHtml).toContain('value="Cool down"');
    expect(editHtml).toContain('sensor.temp');

    // Preview applies the scene transiently (200) and the wall takes the source.
    const feed = await wsFeed('/ws/feed');
    const row = page.locator('tr', { hasText: name });
    const respPromise = page.waitForResponse((r) => r.url().includes('/api/scenes/') && r.url().includes('/preview'));
    await row.locator('.preview-btn').click();
    const resp = await respPromise;
    expect(resp.status()).toBe(200);

    let sawSceneSource = false;
    for (let i = 0; i < 6 && !sawSceneSource; i++) {
      const frame = await feed.nextFrame(10_000);
      if (frame.source === 'Clock') sawSceneSource = true;
    }
    expect(sawSceneSource).toBe(true);

    await clearScenes(page);
  });

  test('validation rejects missing name with 400', async ({ request }) => {
    const res = await request.post('/admin/scenes/new', { form: { name: '', enabled: 'on', triggers: '[]' } });
    expect(res.status()).toBe(400);
  });
});
