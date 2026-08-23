import { test, expect, type Page } from './fixtures';

// Matrix dashboard: new datasource forms, matrix layout editor, live previews
// and the on-demand preview endpoint. Tests clean up after themselves so the
// shared test database stays pristine for other spec files.

async function openAddDatasource(page: Page) {
  await page.goto('/');
  await page.locator('summary', { hasText: 'Add datasource' }).click();
}

async function deleteSourceRow(page: Page, type: string) {
  const row = page.getByRole('row', { name: new RegExp(type) }).first();
  await row.getByRole('button', { name: 'Delete' }).click();
  await expect(page.getByRole('row', { name: new RegExp(type) })).toHaveCount(0);
}

async function createGoogleCalendar(page: Page, url: string) {
  await openAddDatasource(page);
  await page.getByRole('link', { name: 'Google Cal' }).click();
  await page.locator('#name').fill('Family');
  await page.locator('#url').fill(url);
  await page.getByRole('button', { name: 'Create' }).click();
  await page.waitForURL('/admin/');
  await expect(page.getByRole('row', { name: /Google Calendar/ })).toHaveCount(1);
}

test.describe('New datasource forms', () => {
  const forms = [
    { link: 'Google Cal', title: 'New Google Calendar Source' },
    { link: 'News', title: 'New News Source' },
    { link: 'Custom API', title: 'New Custom API Source' },
  ];

  for (const ds of forms) {
    test(`${ds.link} form should load`, async ({ page }) => {
      await openAddDatasource(page);
      await page.getByRole('link', { name: ds.link }).click();
      await expect(page.locator('h1')).toContainText(ds.title);
      await expect(page.getByRole('button', { name: 'Create' })).toBeVisible();
      // Live preview block is present on datasource forms
      await expect(page.locator('[data-live-preview-img]')).toBeAttached();
    });
  }

  test('Custom API form shows config editor and Test button', async ({ page }) => {
    await openAddDatasource(page);
    await page.getByRole('link', { name: 'Custom API' }).click();
    await expect(page.locator('#config')).toBeAttached();
    await expect(page.getByRole('button', { name: 'Test API' })).toBeVisible();
  });
});

test.describe('Google Calendar create/edit/delete', () => {
  test('create, preview, edit and delete a Google Calendar source', async ({ page, request }) => {
    await createGoogleCalendar(page, 'https://calendar.google.com/calendar/ical/example/private-abc/basic.ics');

    // On-demand preview endpoint serves a PNG for the saved source
    const row = page.getByRole('row', { name: /Google Calendar/ }).first();
    const id = await row.locator('td').first().textContent();
    const res = await request.get(`/admin/preview?type=googlecalendar&id=${id?.trim()}&w=64&h=64`);
    expect(res.status()).toBe(200);
    expect(res.headers()['content-type']).toContain('image/png');

    // Edit page loads with values
    await row.getByRole('link', { name: 'Edit' }).click();
    await expect(page.locator('h1')).toContainText('Edit Google Calendar Source');
    await expect(page.locator('#name')).toHaveValue('Family');

    await page.goto('/admin/');
    await deleteSourceRow(page, 'Google Calendar');
  });

  test('deleted source id is rejected by the preview endpoint', async ({ page, request }) => {
    const res = await request.get('/admin/preview?type=googlecalendar&id=9999&w=64&h=64');
    expect(res.status()).toBe(404);
  });
});

test.describe('News datasource', () => {
  test('create and delete a News source', async ({ page }) => {
    await openAddDatasource(page);
    await page.getByRole('link', { name: 'News' }).click();
    await page.locator('#name').fill('Tech');
    await page.locator('#url').fill('https://example.com/rss,https://example.org/feed');
    await page.getByRole('button', { name: 'Create' }).click();
    await page.waitForURL('/admin/');
    await expect(page.getByRole('row', { name: /News/ })).toHaveCount(1);
    await deleteSourceRow(page, 'News');
  });
});

test.describe('Custom API test action', () => {
  test('Test API shows an error for an unreachable URL', async ({ page }) => {
    await openAddDatasource(page);
    await page.getByRole('link', { name: 'Custom API' }).click();
    await page.locator('#url').fill('http://127.0.0.1:9/nowhere');
    await page.getByRole('button', { name: 'Test API' }).click();
    await expect(page.locator('[data-test-result]')).toContainText(/failed|error/i, { timeout: 5000 });
  });

  test('create and delete a Custom API source', async ({ page }) => {
    await openAddDatasource(page);
    await page.getByRole('link', { name: 'Custom API' }).click();
    await page.locator('#token').fill('');
    await page.locator('#url').fill('https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd');
    await page.locator('#config').fill('{"title":"BTC","rows":[{"label":"Price","path":"bitcoin.usd"}]}');
    await page.getByRole('button', { name: 'Create' }).click();
    await page.waitForURL('/admin/');
    await expect(page.getByRole('row', { name: /Custom API/ })).toHaveCount(1);
    await deleteSourceRow(page, 'Custom API');
  });
});

test.describe('Matrix layout editor', () => {
  test('create with a bound cell, live preview, template and delete', async ({ page, request }) => {
    await createGoogleCalendar(page, 'https://calendar.google.com/calendar/ical/example/private-abc/basic.ics');

    await page.goto('/admin/matrixlayouts/new');
    await expect(page.locator('h1')).toContainText('New Matrix Layout');
    await page.locator('#name').fill('Wall');
    await page.locator('#rows').fill('2');
    await page.locator('#cols').fill('2');

    // Bind the first cell to the Google Calendar source
    const firstSelect = page.locator('[data-matrix-grid] select').first();
    await firstSelect.selectOption({ label: 'Google Calendar: Family' });

    // Live composite preview updates after the debounce
    await expect.poll(
      () => page.locator('[data-live-preview-img]').getAttribute('src'),
      { timeout: 8000 },
    ).toMatch(/^blob:/);

    await page.getByRole('button', { name: 'Create' }).click();
    await page.waitForURL('/admin/matrixlayouts');
    const row = page.getByRole('row', { name: /Wall/ });
    await expect(row).toHaveCount(1);
    await expect(row.getByText('2 × 2')).toBeVisible();

    // Template export serves a PNG
    const id = await row.locator('td').first().textContent();
    const tpl = await request.get(`/admin/preview?type=matrix&id=${id?.trim()}&w=128&h=128&template=1`);
    expect(tpl.status()).toBe(200);
    expect(tpl.headers()['content-type']).toContain('image/png');

    // Edit page shows the bound select
    await row.getByRole('link', { name: 'Edit' }).click();
    await expect(page.locator('h1')).toContainText('Edit Matrix Layout');
    await expect(page.locator('[data-matrix-grid] select').first()).toHaveValue(/googlecalendar:/);

    await page.goto('/admin/matrixlayouts');
    await page.getByRole('row', { name: /Wall/ }).getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByRole('row', { name: /Wall/ })).toHaveCount(0);

    await page.goto('/admin/');
    await deleteSourceRow(page, 'Google Calendar');
  });

  test('invalid dimensions show a warning and freeze the preview', async ({ page }) => {
    await page.goto('/admin/matrixlayouts/new');
    const warning = page.locator('[data-matrix-warning]');
    await expect(warning).toBeHidden();

    await page.locator('#rows').fill('0');
    await expect(warning).toBeVisible();
    await expect(warning).toContainText('between 1 and 8');

    await page.locator('#rows').fill('9');
    await expect(warning).toBeVisible();

    await page.locator('#rows').fill('3');
    await expect(warning).toBeHidden();
  });

  test('submitting an invalid layout is rejected', async ({ page }) => {
    await page.goto('/admin/matrixlayouts/new');
    await page.locator('#name').fill('Broken');
    await page.locator('#rows').fill('0');
    await page.getByRole('button', { name: 'Create' }).click();
    await expect(page).toHaveURL(/\/admin\/matrixlayouts\/new/);
    await expect(page.locator('[role="alert"]')).toContainText(/Rows|must be/i);
  });
});

test.describe('Live previews on datasource forms', () => {
  test('typing a URL updates the preview image', async ({ page }) => {
    await openAddDatasource(page);
    await page.getByRole('link', { name: 'Google Cal' }).click();
    const img = page.locator('[data-live-preview-img]');

    await page.locator('#url').fill('https://calendar.google.com/calendar/ical/example/private-abc/basic.ics');
    await expect.poll(() => img.getAttribute('src'), { timeout: 8000 }).toMatch(/^blob:/);
  });
});
