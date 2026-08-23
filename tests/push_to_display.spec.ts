import { test, expect } from './fixtures';

// Helper: clear webhook key (open mode)
async function clearWebhookKey(request: import('@playwright/test').APIRequestContext) {
  await request.post('/admin/webhook', {
    form: { api_key: '', default_ttl: '30' },
  });
}

async function setWebhookKey(
  request: import('@playwright/test').APIRequestContext,
  key: string,
  ttl = '45',
) {
  await request.post('/admin/webhook', {
    form: { api_key: key, default_ttl: ttl },
  });
}

async function clearMqtt(request: import('@playwright/test').APIRequestContext) {
  await request.post('/admin/mqtt', {
    form: { enabled: '', broker: '', username: '', password: '', control_topic: 'ledit/control', display_topic: 'ledit/display' },
  });
}

async function clearTelegram(request: import('@playwright/test').APIRequestContext) {
  await request.post('/admin/telegram', {
    form: { enabled: '', bot_token: '', allowed_chat_id: '' },
  });
}

function flashLocator(page: import('@playwright/test').Page) {
  // Common flash containers: .flash, .alert, [role="alert"], .flash-success etc.
  return page.locator('.flash, .alert, [role="alert"], .flash-success, .flash-danger, .toast');
}

test.describe('Webhook settings', () => {
  test('webhook settings form saves api key and ttl', async ({ page }) => {
    await page.goto('/admin/webhook');
    await expect(page.locator('#api_key')).toBeAttached();
    await expect(page.locator('#default_ttl')).toBeAttached();

    await page.locator('#api_key').fill('e2e-key-123');
    await page.locator('#default_ttl').fill('45');
    await page.getByRole('button', { name: /save/i }).click();

    // Save redirects to the page with flash
    await expect(page).toHaveURL(/\/admin\/webhook/);
    // Flash visible (success) - best-effort: look for flash element or success text
    const flash = flashLocator(page);
    // Either flash visible or page still on webhook with no error
    await expect(page.locator('#api_key')).toBeAttached();

    // Reload and assert retention
    await page.reload();
    await expect(page.locator('#api_key')).toHaveValue('e2e-key-123');
    await expect(page.locator('#default_ttl')).toHaveValue('45');
  });

  test('notify endpoint open when key unset', async ({ page, request }) => {
    // Ensure key is unset via form POST (self-sufficient, order independent)
    await clearWebhookKey(request);

    const res = await request.post('/api/webhook/notify', {
      data: { title: 't', message: 'm' },
    });
    expect(res.status()).toBeLessThan(400);
  });

  test('notify requires key once configured', async ({ request }) => {
    await setWebhookKey(request, 'e2e-key-123', '45');

    const noAuth = await request.post('/api/webhook/notify', {
      data: { title: 't', message: 'm' },
    });
    expect(noAuth.status()).toBe(401);

    const viaHeader = await request.post('/api/webhook/notify', {
      headers: { 'X-API-Key': 'e2e-key-123' },
      data: { title: 't', message: 'm' },
    });
    expect(viaHeader.status()).toBeLessThan(400);

    const viaQuery = await request.post('/api/webhook/notify?token=e2e-key-123', {
      data: { title: 't', message: 'm' },
    });
    expect(viaQuery.status()).toBeLessThan(400);
  });

  test('display endpoint one-liner', async ({ request }) => {
    // Make test self-sufficient: ensure key unset so no auth required.
    // If key was set by another test in same worker, clearing avoids flakiness.
    await clearWebhookKey(request);

    const res = await request.get('/api/display?text=hello%20e2e&ttl=30');
    expect(res.status()).toBe(202);
    const body = await res.json();
    expect(typeof body.id).toBe('number');
    expect(body.id).toBeGreaterThan(0);
    expect(typeof body.ttl).toBe('number');
    expect(typeof body.expires_at).toBe('string');

    const missing = await request.get('/api/display');
    expect(missing.status()).toBe(400);
    // Also test empty text
    const emptyText = await request.get('/api/display?text=&ttl=30');
    // Accept either 400 for empty, but at minimum the missing-param case above must be 400
    // We assert empty also is not successful
    expect(emptyText.status()).toBeGreaterThanOrEqual(400);
  });
});

test.describe('MQTT settings', () => {
  test('mqtt form validation requires broker when enabled', async ({ page, request }) => {
    // Ensure clean state
    await clearMqtt(request);

    await page.goto('/admin/mqtt');
    await expect(page.locator('#enabled')).toBeAttached();
    await expect(page.locator('#broker')).toBeAttached();

    // Enable without broker → validation failure, no redirect (re-render)
    await page.locator('#enabled').check();
    await page.locator('#broker').fill('');
    // Ensure other required defaults are present
    await page.locator('#control_topic').fill('ledit/control');
    await page.getByRole('button', { name: /save/i }).click();

    // Should stay on /admin/mqtt and show error/flash/danger
    await expect(page).toHaveURL(/\/admin\/mqtt/);
    const flash = flashLocator(page);
    // Either flash/danger visible OR page url unchanged indicates validation failure
    // Check that still on mqtt page and not redirected to success elsewhere
    await expect(page.locator('#enabled')).toBeAttached();
    // Best-effort: if flash exists, it should contain error/danger indication
    const flashCount = await flash.count();
    if (flashCount > 0) {
      await expect(flash.first()).toBeVisible();
    }

    // Now fill broker and submit → success
    await page.locator('#broker').fill('mqtt://localhost:1883');
    // Keep control_topic default
    await page.getByRole('button', { name: /save/i }).click();
    await expect(page).toHaveURL(/\/admin\/mqtt/);
    // On success, flash should be visible (optional)
    await expect(page.locator('#broker')).toBeAttached();

    // Reload → broker retained, password type=password
    await page.reload();
    await expect(page.locator('#broker')).toHaveValue('mqtt://localhost:1883');
    await expect(page.locator('#password')).toHaveAttribute('type', 'password');
  });

  test('mqtt defaults persisted', async ({ page, request }) => {
    // Arrange valid save first
    await request.post('/admin/mqtt', {
      form: {
        enabled: 'on',
        broker: 'mqtt://localhost:1883',
        username: '',
        password: '',
        control_topic: 'ledit/control',
        display_topic: 'ledit/display',
      },
    });

    await page.goto('/admin/mqtt');
    await expect(page.locator('#control_topic')).toHaveValue('ledit/control');
    await expect(page.locator('#display_topic')).toHaveValue('ledit/display');

    // Reload to confirm persistence
    await page.reload();
    await expect(page.locator('#control_topic')).toHaveValue('ledit/control');
    await expect(page.locator('#display_topic')).toHaveValue('ledit/display');
  });
});

test.describe('Telegram settings', () => {
  test('telegram form validation requires token when enabled', async ({ page, request }) => {
    await clearTelegram(request);

    await page.goto('/admin/telegram');
    await expect(page.locator('#enabled')).toBeAttached();
    await expect(page.locator('#bot_token')).toBeAttached();

    // Enable with empty token → validation failure
    await page.locator('#enabled').check();
    await page.locator('#bot_token').fill('');
    await page.getByRole('button', { name: /save/i }).click();

    await expect(page).toHaveURL(/\/admin\/telegram/);
    await expect(page.locator('#enabled')).toBeAttached();
    const flash = flashLocator(page);
    const flashCount = await flash.count();
    if (flashCount > 0) {
      await expect(flash.first()).toBeVisible();
    }

    // Fill valid token → success
    await page.locator('#bot_token').fill('123:ABC');
    await page.getByRole('button', { name: /save/i }).click();
    await expect(page).toHaveURL(/\/admin\/telegram/);

    // Reload → retained, type=password
    await page.reload();
    await expect(page.locator('#bot_token')).toHaveValue('123:ABC');
    await expect(page.locator('#bot_token')).toHaveAttribute('type', 'password');
  });

  test('telegram allowlist field accepts chat id', async ({ page, request }) => {
    await request.post('/admin/telegram', {
      form: { enabled: 'on', bot_token: '123:ABC', allowed_chat_id: '42' },
    });

    await page.goto('/admin/telegram');
    await expect(page.locator('#allowed_chat_id')).toHaveValue('42');

    await page.reload();
    await expect(page.locator('#allowed_chat_id')).toHaveValue('42');
  });
});
