# Playwright E2E

Shared fixtures in `fixtures.ts` (re-exports `@playwright/test`).

- `seedCleanState` auto-fixture: seeds GeneralSettings (timeout/width/height) and deletes `PW-*` test devices after each test.
- `adminApi` helper: `seedSettings`, `createDevice`, `deleteDevice`, `cleanupDevices`.
- `wsFeed` helper: `wsFeed(url)` returns `WsFeed` with `nextFrame(timeout)` and `drain()`.

Usage:
```ts
import { test, expect } from './fixtures';

test('my case', async ({ page, adminApi, wsFeed }) => {
  const feed = await wsFeed('/ws/feed');
  const frame = await feed.nextFrame(8000);
  expect(frame.format).toBe('PNG');
});
```

Run:
- `npm run test:e2e` or `npx playwright test --project=chromium-desktop`
- CI shard: `npm run test:e2e:ci`
