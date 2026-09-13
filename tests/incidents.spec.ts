import { test, expect } from './fixtures';
import type { WsFeed } from './helpers/ws';

const TITLE = 'PW INCIDENT';
const MESSAGE = 'playwright outage';

// Scan a bounded number of frames until a frame with the given source arrives.
async function waitForSource(feed: WsFeed, source: string, tries = 8): Promise<boolean> {
  for (let i = 0; i < tries; i++) {
    await feed.pollBrowserBuffer();
    let frame;
    try {
      frame = await feed.nextFrame(3000);
    } catch {
      continue;
    }
    if (frame.source === source) return true;
  }
  return false;
}

test.describe('Incident mode', () => {
  test.afterEach(async ({ request }) => {
    // Resolve by fingerprint so no incident leaks into the next spec.
    await request
      .post('/api/incident', { data: { title: TITLE, message: MESSAGE, status: 'resolved' } })
      .catch(() => {});
    await request.post('/admin/incidents/resolve-all').catch(() => {});
  });

  test('webhook incident takes over the feed until resolved', async ({ page, request, wsFeed }) => {
    await page.goto('/');
    const feed = await wsFeed('/ws/feed');

    // Raise via the generic ingress shape (webhook auth is a no-op without a key).
    const raise = await request.post('/api/incident', {
      data: { title: TITLE, message: MESSAGE, severity: 'critical', source: 'e2e' },
    });
    expect(raise.ok()).toBeTruthy();
    expect((await raise.json()).active_count).toBe(1);

    expect(await waitForSource(feed, 'INCIDENT'), 'no INCIDENT frame on the feed').toBe(true);

    // Resolving releases the wall back to normal rotation.
    const resolve = await request.post('/api/incident', {
      data: { title: TITLE, message: MESSAGE, status: 'resolved' },
    });
    expect(resolve.ok()).toBeTruthy();

    let released = false;
    for (let i = 0; i < 8 && !released; i++) {
      await feed.pollBrowserBuffer();
      let frame;
      try {
        frame = await feed.nextFrame(3000);
      } catch {
        continue;
      }
      if (frame.source && frame.source !== 'INCIDENT') released = true;
    }
    expect(released, 'feed never returned to rotation after resolve').toBe(true);
  });
});
