import { test, expect, type Page } from './fixtures';

async function clearAlarms(page: Page): Promise<void> {
  await page.goto('/admin/alarms');
  let buttons = page.locator('table form[action$="/delete"] button');
  while ((await buttons.count()) > 0) {
    await buttons.first().click();
    await page.waitForLoadState('networkidle');
    await page.goto('/admin/alarms');
    buttons = page.locator('table form[action$="/delete"] button');
  }
}

async function selectWakeSource(page: Page, type: string, id: string): Promise<void> {
  await page.selectOption('#group-select', type);
  await page.selectOption('#source-select', id);
}

test.describe('Wake alarm admin', () => {
  // The evaluator reloads definitions on a ~30s cadence and the active-alarm
  // assertion allows 45s, so the default 30s test timeout is too short.
  test.setTimeout(90_000);

  test('create, list, active indicator, dismiss, and validation', async ({ page }) => {
    await clearAlarms(page);
    const name = `PW-Wake-${Math.random().toString(36).slice(2, 6)}`;

    // Create a weekday alarm Mon-Fri 06:30-07:00.
    await page.goto('/admin/alarms/new');
    await page.fill('#name', name);
    for (const d of ['1', '2', '3', '4', '5']) {
      await page.locator(`#day-${d}`).check();
    }
    await page.fill('#start', '06:30');
    await page.fill('#end', '07:00');
    await selectWakeSource(page, 'systemstats', '0');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/alarms$/);
    await expect(page.locator('table')).toContainText(name);

    // A window covering "now" activates the alarm, but the evaluator reloads
    // definitions on its ~30s cadence. The admin page is server-rendered and
    // does not poll, so reload it until the active banner appears.
    const activeName = `${name}-active`;
    await page.goto('/admin/alarms/new');
    await page.fill('#name', activeName);
    for (const d of ['0', '1', '2', '3', '4', '5', '6']) {
      await page.locator(`#day-${d}`).check();
    }
    await page.fill('#start', '00:00');
    await page.fill('#end', '23:59');
    await selectWakeSource(page, 'systemstats', '0');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/alarms$/);

    await expect
      .poll(async () => {
        await page.goto('/admin/alarms');
        return page.locator('.alert-warning').count();
      }, { timeout: 60_000, intervals: [2000, 3000, 5000] })
      .toBeGreaterThan(0);
    await expect(page.locator('.alert-warning')).toContainText(activeName);

    // Dismiss releases the active alarm and clears the indicator.
    await page.request.post('/api/feed/alarm/dismiss');
    await page.goto('/admin/alarms');
    await expect(page.locator('.alert-warning')).toHaveCount(0);

    // Server-side validation: empty days and invalid time both reject with 400.
    const emptyDays = await page.request.post('/admin/alarms/new', {
      form: { name: 'bad', start: '06:00', end: '07:00', wake_source_type: 'systemstats', wake_source_id: '0' },
    });
    expect(emptyDays.status()).toBe(400);

    const invalidTime = await page.request.post('/admin/alarms/new', {
      form: { name: 'bad', days: '1', start: '25:00', end: '07:00', wake_source_type: 'systemstats', wake_source_id: '0' },
    });
    expect(invalidTime.status()).toBe(400);

    await clearAlarms(page);
  });
});
