import { defineConfig, devices } from '@playwright/test';

// Each project runs its own isolated server (own port + own SQLite database)
// so state written by one browser project cannot leak into the other.
export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'html',
  use: {
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium-desktop',
      use: { ...devices['Desktop Chrome'], baseURL: 'http://127.0.0.1:8080' },
      webServer: {
        command: 'rm -rf data && LEDIT_AUTH_DISABLE=true ./ledit',
        url: 'http://127.0.0.1:8080',
        reuseExistingServer: !process.env.CI,
      },
    },
    {
      name: 'firefox-desktop',
      use: { ...devices['Desktop Firefox'], baseURL: 'http://127.0.0.1:8081' },
      webServer: {
        command: 'rm -rf data-e2e && LEDIT_DB_DIR=data-e2e LEDIT_PORT=8081 LEDIT_AUTH_DISABLE=true ./ledit',
        url: 'http://127.0.0.1:8081',
        reuseExistingServer: !process.env.CI,
      },
    },
  ],
});
