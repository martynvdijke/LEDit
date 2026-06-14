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
    await expect(page.getByRole('link', { name: 'Settings', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Schedules' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Devices' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Theme' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Analytics', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Notifications' })).toBeVisible();
    // Datasources are in a dropdown — open it to verify items
    await page.getByRole('button', { name: 'Add Datasource' }).click();
    await expect(page.getByRole('link', { name: 'Sonarr' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Radarr' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'F1' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Weather' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Home Assistant' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Untappd' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Image' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Video' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Crypto' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Stock' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'RSS Feed' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Calendar' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Text Slide' })).toBeVisible();
  });

  test('should navigate to settings via sidebar', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: 'Settings', exact: true }).click();
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
    { link: 'Sonarr', title: 'New Sonarr Source' },
    { link: 'Radarr', title: 'New Radarr Source' },
    { link: 'F1', title: 'New F1 Source' },
    { link: 'Weather', title: 'New Weather Source' },
    { link: 'Home Assistant', title: 'New HomeAssistant Source' },
    { link: 'Untappd', title: 'New Untappd Source' },
    { link: 'Image', title: 'New Image Source' },
    { link: 'Video', title: 'New Video Source' },
    { link: 'Crypto', title: 'New Crypto Source' },
    { link: 'Stock', title: 'New Stock Source' },
    { link: 'RSS Feed', title: 'New RSS Feed Source' },
    { link: 'Calendar', title: 'New Calendar Source' },
    { link: 'Text Slide', title: 'New Text Slide' },
  ];

  for (const ds of datasources) {
    test(`${ds.title} form should load`, async ({ page }) => {
      await page.goto('/');
      await page.getByRole('button', { name: 'Add Datasource' }).click();
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

test.describe('Schedules', () => {
  test('should load schedules page with empty state', async ({ page }) => {
    await page.goto('/admin/schedules');
    await expect(page.locator('h1')).toContainText('Schedules');
    await expect(page.getByText('No schedules configured')).toBeVisible();
  });

  test('should show new schedule form', async ({ page }) => {
    await page.goto('/admin/schedules/new');
    await expect(page.locator('h1')).toContainText('New Schedule');
    await expect(page.locator('#name')).toBeAttached();
    await expect(page.locator('#time_range')).toBeAttached();
    await expect(page.locator('#enabled')).toBeAttached();
    await expect(page.getByRole('button', { name: 'Create' })).toBeVisible();
  });
});

test.describe('Devices', () => {
  test('should load devices page with empty state', async ({ page }) => {
    await page.goto('/admin/devices');
    await expect(page.locator('h1')).toContainText('Devices');
    await expect(page.getByText('No devices configured')).toBeVisible();
  });

  test('should show new device form', async ({ page }) => {
    await page.goto('/admin/devices/new');
    await expect(page.locator('h1')).toContainText('New Device');
    await expect(page.locator('#name')).toBeAttached();
    await expect(page.locator('#ip')).toBeAttached();
    await expect(page.locator('#port')).toBeAttached();
    await expect(page.locator('#width')).toBeAttached();
    await expect(page.locator('#height')).toBeAttached();
    await expect(page.getByRole('button', { name: 'Create' })).toBeVisible();
  });
});

test.describe('Theme Editor', () => {
  test('should load theme editor with color pickers', async ({ page }) => {
    await page.goto('/admin/theme');
    await expect(page.locator('h1')).toContainText('Theme Editor');
    await expect(page.locator('#bg_color')).toBeAttached();
    await expect(page.locator('#accent_color')).toBeAttached();
    await expect(page.locator('#text_color')).toBeAttached();
    await expect(page.locator('#title')).toBeAttached();
    await expect(page.locator('#font_size')).toBeAttached();
    await expect(page.getByRole('button', { name: 'Save Theme' })).toBeVisible();
  });
});

test.describe('Analytics', () => {
  test('should load analytics page', async ({ page }) => {
    await page.goto('/admin/analytics');
    await expect(page.locator('h1')).toContainText('Analytics');
    await expect(page.getByText('Total Displays')).toBeVisible();
    await expect(page.getByText('Server Uptime')).toBeVisible();
  });
});

test.describe('Notifications', () => {
  test('should load notifications page', async ({ page }) => {
    await page.goto('/admin/notifications');
    await expect(page.locator('h1')).toContainText('Notifications');
  });
});

test.describe('Login Page', () => {
  test('should load login page', async ({ page }) => {
    await page.goto('/login');
    await expect(page.locator('h1')).toContainText('LEDit Login');
    await expect(page.locator('#username')).toBeAttached();
    await expect(page.locator('#password')).toBeAttached();
    await expect(page.getByRole('button', { name: 'Login' })).toBeVisible();
  });
});
