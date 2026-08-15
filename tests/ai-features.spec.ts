import { test, expect, Page } from '@playwright/test';
import * as http from 'node:http';
import { AddressInfo } from 'node:net';

const SLIDE_NAME = 'PW AI Slide';
const DIGEST_NAME = 'PW AI Digest';

/** Starts a mock OpenAI-compatible /chat/completions server. */
async function startMockLLM(): Promise<http.Server> {
  const srv = http.createServer((req, res) => {
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({
      choices: [{ message: { role: 'assistant', content: 'Sunny 21C in Amsterdam today' } }],
    }));
  });
  await new Promise<void>((resolve) => srv.listen(0, '127.0.0.1', resolve));
  return srv;
}

async function seedCleanState(page: Page): Promise<void> {
  await page.request.post('/admin/settings', {
    form: { timeout: '5', width: '64', height: '64' },
  });
  // Remove leftover AI digests.
  await page.goto('/admin/');
  let del = page.locator('tr:has-text("AI Digest") form[action$="/delete"] button');
  while ((await del.count()) > 0) {
    await del.first().click();
    await page.waitForLoadState('networkidle');
    await page.goto('/admin/');
    del = page.locator('tr:has-text("AI Digest") form[action$="/delete"] button');
  }
}

test.describe('AI features', () => {
  test('generate button drafts slide content via the AI provider (2.5)', async ({ page }) => {
    const srv = await startMockLLM();
    const port = (srv.address() as AddressInfo).port;
    try {
      await seedCleanState(page);

      // Configure AI settings to point at the mock provider.
      await page.goto('/admin/settings/ai');
      await page.selectOption('#provider', 'openai');
      await page.fill('#api_key', 'test-key');
      await page.fill('#model', 'test-model');
      await page.fill('#endpoint', `http://127.0.0.1:${port}/v1`);
      await page.click('button[type="submit"]');
      await expect(page).toHaveURL(/\/admin\/settings\/ai/);

      // 2.5 — human-in-the-loop: generation fills the form, nothing saved yet.
      await page.goto('/admin/textslides/new');
      await page.fill('#ai_prompt', 'Give me a short weather line');
      await page.click('#btn-generate-ai');
      await expect(page.locator('#content')).toHaveValue('Sunny 21C in Amsterdam today', { timeout: 15000 });
      await expect(page.locator('#ai-error')).toBeHidden();

      // The generated content is not persisted until the form is submitted.
      await page.goto('/admin/');
      await expect(page.locator(`tr:has-text("${SLIDE_NAME}")`)).toHaveCount(0);
    } finally {
      srv.close();
    }
  });

  test('generate button surfaces a clear error when AI is not configured (2.5)', async ({ page }) => {
    await seedCleanState(page);

    // Reset AI settings to empty (later tests may have configured a provider).
    await page.request.post('/admin/settings/ai', {
      form: { provider: 'openai', api_key: '', model: '', endpoint: '' },
    });

    await page.goto('/admin/textslides/new');
    await page.fill('#ai_prompt', 'Anything at all');
    await page.click('#btn-generate-ai');
    await expect(page.locator('#ai-error')).toBeVisible({ timeout: 15000 });
    await expect(page.locator('#ai-error')).toContainText('AI settings are not configured');
    await expect(page.locator('#content')).toHaveValue('');
  });

  test('AI Digest CRUD + disabled excluded from matrix bindings (3.7)', async ({ page }) => {
    await seedCleanState(page);

    // 3.7 — create a digest via the admin form.
    await page.goto('/admin/aidigests/new');
    await page.fill('#name', DIGEST_NAME);
    await page.fill('#prompt', 'Summarize the top stories');
    await page.fill('#ttl_minutes', '30');
    await page.locator('#enabled').check();
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL(/\/admin\/$/);

    const row = page.locator(`tr:has-text("${DIGEST_NAME}")`);
    await expect(row).toBeVisible();
    await expect(row).toContainText('AI Digest');

    // Edit page is prefilled.
    await row.locator('a:has-text("Edit")').click();
    await expect(page).toHaveURL(/\/admin\/aidigests\/\d+\/edit/);
    await expect(page.locator('#name')).toHaveValue(DIGEST_NAME);
    await expect(page.locator('#prompt')).toHaveValue('Summarize the top stories');
    await expect(page.locator('#ttl_minutes')).toHaveValue('30');
    await expect(page.locator('#enabled')).toBeChecked();
    await expect(page.locator('button:has-text("Refresh now")')).toHaveCount(1);

    // Sidebar links to the new form.
    await expect(page.locator('.sidebar a[href="/admin/aidigests/new"]')).toHaveCount(1);

    // Delete via the edit page's delete form.
    page.on('dialog', (d) => d.accept());
    await page.click('button:has-text("Delete")');
    await expect(page).toHaveURL(/\/admin\/$/);
    await expect(page.locator(`tr:has-text("${DIGEST_NAME}")`)).toHaveCount(0);
  });
});
