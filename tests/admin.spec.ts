import { test, expect } from '@playwright/test';

test.describe('Admin Dashboard', () => {
  test('should load the admin dashboard', async ({ page }) => {
    await page.goto('/admin/');
    await expect(page.locator('h1')).toContainText('Admin Dashboard');
  });

  test('should show no settings message when unconfigured', async ({ page }) => {
    await page.goto('/admin/');
    await expect(page.getByText('No settings configured yet')).toBeVisible();
  });

  test('should have settings link in the page', async ({ page }) => {
    await page.goto('/admin/');
    await expect(page.getByRole('link', { name: 'Configure settings' })).toBeVisible();
  });
});

test.describe('Admin Settings', () => {
  test('should load the settings page with form fields', async ({ page }) => {
    await page.goto('/admin/settings');
    await expect(page.locator('h1')).toContainText('General Settings');
    await expect(page.locator('#timeout')).toBeAttached();
    await expect(page.locator('#random')).toBeAttached();
    await expect(page.locator('#width')).toBeAttached();
    await expect(page.locator('#height')).toBeAttached();
    await expect(page.locator('label[for="random"]')).toHaveText('Random order');
  });

  test('should submit the settings form', async ({ page }) => {
    await page.goto('/admin/settings');
    await page.locator('#timeout').fill('3');
    await page.locator('#random').check();
    await page.locator('#width').fill('64');
    await page.locator('#height').fill('64');
    await page.getByRole('button', { name: 'Save' }).click();
    await page.waitForURL('/admin/');
    await expect(page.locator('h1')).toContainText('Admin Dashboard');
  });
});

test.describe('Sidebar Navigation', () => {
  test('sidebar navigation links should be present', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'Home' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Dashboard', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Add Sonarr' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Add Radarr' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Add F1' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Add Weather' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Add HA' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Add Untappd' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Add Image' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Add Video' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Add Crypto' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Notifications' })).toBeVisible();
  });

  test('should navigate to settings via sidebar', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: 'Settings' }).click();
    await expect(page).toHaveURL('/admin/settings');
  });

  test('should navigate to admin via sidebar', async ({ page }) => {
    await page.goto('/admin/settings');
    await page.getByRole('link', { name: 'Dashboard', exact: true }).click();
    await expect(page).toHaveURL('/admin/');
  });
});

test.describe('Datasource Forms', () => {
  const datasources = [
    { link: 'Add Sonarr', path: 'sonarr', title: 'New Sonarr Source' },
    { link: 'Add Radarr', path: 'radarr', title: 'New Radarr Source' },
    { link: 'Add F1', path: 'f1', title: 'New F1 Source' },
    { link: 'Add Weather', path: 'weather', title: 'New Weather Source' },
    { link: 'Add HA', path: 'homeassistant', title: 'New HomeAssistant Source' },
    { link: 'Add Untappd', path: 'untappd', title: 'New Untappd Source' },
    { link: 'Add Image', path: 'images', title: 'New Image Source' },
    { link: 'Add Video', path: 'videos', title: 'New Video Source' },
    { link: 'Add Crypto', path: 'crypto', title: 'New Crypto Source' },
  ];

  for (const ds of datasources) {
    test(`${ds.title} form should load`, async ({ page }) => {
      await page.goto('/');
      await page.getByRole('link', { name: ds.link }).click();
      await expect(page.locator('h1')).toContainText(ds.title);
      await expect(page.getByRole('button', { name: 'Create' })).toBeVisible();
    });
  }
});

test.describe('Admin Dashboard Datasource Table', () => {
  test('should show datasources table after configuration', async ({ page }) => {
    await page.goto('/admin/settings');
    await page.locator('#timeout').fill('1');
    await page.locator('#width').fill('64');
    await page.locator('#height').fill('64');
    await page.getByRole('button', { name: 'Save' }).click();
    await page.waitForURL('/admin/');

    await expect(page.locator('h2')).toContainText('Datasources');
    await expect(page.getByText('No datasources configured')).toBeVisible();
  });
});
