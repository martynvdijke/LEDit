import { test, expect } from './fixtures';

test.describe('Telegram settings', () => {
  test('save + reload round-trip', async ({ page }) => {
    await page.goto('/admin/telegram');
    await expect(page.locator('h1')).toContainText('Telegram Settings');

    await page.locator('#enabled').check();
    await page.fill('#bot_token', '123456:ABC-test-token');
    await page.fill('#allowed_chat_id', '123456');
    await page.getByRole('button', { name: 'Save Settings' }).click();

    await expect(page).toHaveURL(/\/admin\/telegram$/);

    await page.reload();
    await expect(page.locator('#enabled')).toBeChecked();
    await expect(page.locator('#bot_token')).toHaveValue('123456:ABC-test-token');
    await expect(page.locator('#allowed_chat_id')).toHaveValue('123456');
  });

  test('token required when enabled', async ({ page }) => {
    await page.goto('/admin/telegram');

    await page.locator('#enabled').check();
    await page.fill('#bot_token', '');
    await page.getByRole('button', { name: 'Save Settings' }).click();

    await expect(page).toHaveURL(/\/admin\/telegram/);
    await expect(page.getByText('Bot token is required')).toBeVisible();
  });

  test('test message without chat id shows error', async ({ page }) => {
    await page.goto('/admin/telegram');

    await page.locator('#enabled').check();
    await page.fill('#bot_token', '123456:ABC-test-token');
    await page.fill('#allowed_chat_id', '');
    await page.getByRole('button', { name: 'Save Settings' }).click();
    await expect(page).toHaveURL(/\/admin\/telegram/);

    await page.click('#test-telegram');
    await expect(page.getByText('Set an allowed chat ID first')).toBeVisible();
  });
});
