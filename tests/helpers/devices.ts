import type { Page } from '@playwright/test';

/** Row on /admin/devices for a device name created by the test fixtures. */
export function deviceRow(page: Page, name: string) {
  return page.locator('tbody tr').filter({ hasText: name }).first();
}

/**
 * Resolve the device id from its own table row. Avoids scanning a fixed-size
 * HTML window around the name, which can pick up a neighbouring device's
 * preview/delete link (and therefore the wrong dimensions or token).
 */
export async function getDeviceId(page: Page, name: string): Promise<string> {
  const row = deviceRow(page, name);
  if ((await row.count()) === 0) return '';
  const href = await row.locator('a[href*="/preview"], a[href*="/delete"]').first().getAttribute('href');
  const match = href?.match(/\/admin\/devices\/(\d+)\//);
  return match ? match[1] : '';
}
