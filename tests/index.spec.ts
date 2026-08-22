import { test, expect } from './fixtures';

test.describe('Index / Live Feed', () => {
  test('should load the index page', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('h1')).toContainText('Live feed');
  });

  test('should have WebSocket status indicator', async ({ page }) => {
    await page.goto('/');
    const status = page.locator('#status-text');
    await expect(status).toBeVisible();
    const text = await status.textContent() ?? '';
    expect(text.length).toBeGreaterThan(0);
  });

  test('should have media display elements in DOM', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#media-container')).toBeAttached();
    await expect(page.locator('#media-display')).toBeAttached();
    await expect(page.locator('#video-display')).toBeAttached();
  });

  test('should show LEDit branding', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'LEDit live feed' })).toBeVisible();
  });

  test('should have sidebar with navigation links', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'Live feed', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Dashboard' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Settings', exact: true })).toBeVisible();
  });

  test('should show source and next labels', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#source-label')).toBeVisible();
    await expect(page.locator('#next-label')).toBeVisible();
  });

  test('should have feed control buttons', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#btn-pause')).toBeVisible();
    await expect(page.locator('#btn-skip')).toBeVisible();
    await expect(page.locator('#btn-fullscreen')).toBeVisible();
    await expect(page.locator('#btn-pause')).toHaveText('Pause');
    await expect(page.locator('#btn-skip')).toHaveText('Skip to next');
    await expect(page.locator('#btn-fullscreen')).toHaveText('Fullscreen');
  });

  test('should have matrix overlay canvas', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#matrix-overlay')).toBeAttached();
  });

  test('should load owned assets and remain usable on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/');
    await expect(page.locator('link[href="/static/assets/styles.css"]')).toHaveCount(1);
    await expect(page.getByRole('button', { name: /Open navigation/ })).toBeVisible();
    await page.getByRole('button', { name: /Open navigation/ }).click();
    await expect(page.locator('[data-app-shell]')).toHaveClass(/nav-open/);
    await expect(page.locator('body')).not.toHaveCSS('overflow-x', 'scroll');
  });

  test('should expose PWA branding metadata', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('link[rel="manifest"]')).toHaveAttribute('href', '/static/pwa/manifest.json');
    await expect(page.locator('link[rel="icon"]')).toHaveAttribute('href', '/static/pwa/favicon.png');
  });

  test('should hide feed controls on public landing when unauthenticated', async ({ page }) => {
    await page.goto('/');
    const publicVisible = await page.locator('.public-landing').isVisible().catch(() => false);
    if (publicVisible) {
      await expect(page.locator('#source-label')).toBeHidden();
      await expect(page.locator('#next-label')).toBeHidden();
      await expect(page.locator('#media-display')).toBeHidden();
      await expect(page.locator('#btn-pause')).toBeHidden();
      await expect(page.getByText('Login to view feed')).toBeVisible();
      await expect(page.getByText(/Detailed feed state/)).toBeVisible();
    } else {
      // Auth disabled (webServer uses LEDIT_AUTH_DISABLE=true) — feed should be visible
      await expect(page.locator('#source-label')).toBeVisible();
    }
  });

  test('wsFeed helper receives PNG frame', async ({ page, wsFeed }) => {
    await page.goto('/');
    const feed = await wsFeed('/ws/feed');
    const frame = await feed.nextFrame(8000);
    expect(frame.format).toBe('PNG');
    expect(typeof frame.image).toBe('string');
  });
});
