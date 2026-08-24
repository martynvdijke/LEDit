import { test, expect } from '@playwright/test';

// viewer vs admin role E2E (task 5.3)
// Requires auth enabled; in CI/local dev the webServer starts with LEDIT_AUTH_DISABLE=true,
// so we flip it via test-only hook POST /api/test/enable-auth.

function randSuffix() { return Math.random().toString(36).slice(2, 7); }

test.describe('User roles E2E', () => {
  test.setTimeout(60000);

  const viewerUser = `viewer_${randSuffix()}`;
  const viewerPass = 'ViewerPass123';
  const adminUser = 'admin';
  const adminPass = 'ledit';

  // helper to login via UI and return after redirect
  async function login(page: any, username: string, password: string) {
    await page.goto('/login');
    await page.fill('input[name="username"]', username);
    await page.fill('input[name="password"]', password);
    await page.click('button[type="submit"]');
    await page.waitForURL(/\/admin\//, { timeout: 10000 }).catch(() => {});
  }

  test('viewer can view dashboard/analytics/logs, cannot mutate; admin can mutate; viewer token read 200 mutation 403; viewer cannot access /admin/users', async ({ playwright }) => {
    // Use a fresh API context without cookies for setup
    const apiCtx = await playwright.request.newContext({ baseURL: process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:8080' });
    // Enable auth if server started with LEDIT_AUTH_DISABLE
    await apiCtx.post('/api/test/enable-auth').catch(() => {});
    // Small delay for seed
    await new Promise(r => setTimeout(r, 300));

    // Create isolated browser contexts for admin and viewer
    const browser = await playwright.chromium.launch();
    const adminCtx = await browser.newContext({ baseURL: process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:8080' });
    const viewerCtx = await browser.newContext({ baseURL: process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:8080' });
    const adminPage = await adminCtx.newPage();
    const viewerPage = await viewerCtx.newPage();
    const adminReq = adminCtx.request;
    const viewerReq = viewerCtx.request;

    // 1. Admin login
    await login(adminPage, adminUser, adminPass);
    // verify admin landed on dashboard
    await expect(adminPage).toHaveURL(/\/admin\//, { timeout: 8000 });

    // Create viewer user via admin API (JSON)
    const createResp = await adminReq.post('/admin/api/users', { data: { username: viewerUser, password: viewerPass, role: 'viewer' }});
    // If 409 duplicate (retries), fetch existing
    if (createResp.status() !== 201) {
      const txt = await createResp.text();
      // maybe already exists from prior run
      expect([201, 409]).toContain(createResp.status());
    }
    // Ensure viewer appears in list
    const list = await adminReq.get('/admin/api/users');
    expect(list.ok()).toBeTruthy();
    const users = await list.json() as any[];
    expect(users.find((u: any) => u.username === viewerUser)).toBeTruthy();

    // 2. Viewer login → GET /admin dashboard 200, analytics 200, logs 200
    await login(viewerPage, viewerUser, viewerPass);
    await expect(viewerPage).toHaveURL(/\/admin\//, { timeout: 8000 });

    for (const path of ['/admin/', '/admin/analytics', '/admin/logs']) {
      const resp = await viewerReq.get(path);
      expect(resp.status(), `viewer GET ${path}`).toBe(200);
    }

    // 3. Viewer cannot access /admin/users (403 or redirect)
    // Page redirect is followed by APIRequest, so check via page navigation
    await viewerPage.goto('/admin/users');
    await expect(viewerPage).toHaveURL(/\/admin\//, { timeout: 5000 });
    expect(viewerPage.url()).not.toContain('/admin/users');
    // API should be forbidden (403)
    const apiUsersViewer = await viewerReq.get('/admin/api/users');
    expect(apiUsersViewer.status()).toBe(403);

    // 4. Viewer attempts playlist creation → 403 and no row created
    const beforeList = await adminReq.get('/admin/playlists');
    const beforeHtml = await beforeList.text();
    const beforeCount = (beforeHtml.match(/\/admin\/playlists\/\d+\/edit/g) || []).length;

    const plName = `PW-role-${randSuffix()}`;
    const createPlViewer = await viewerReq.post('/admin/playlists/new', {
      form: { name: plName, items: '[]', schedule_windows: '[]', enabled: 'on' }
    });
    expect(createPlViewer.status()).toBe(403);
    // verify via body contains insufficient_role if JSON else 403 page
    // Verify list unchanged (admin view)
    const afterList = await adminReq.get('/admin/playlists');
    const afterHtml = await afterList.text();
    expect(afterHtml).not.toContain(plName);
    const afterCount = (afterHtml.match(/\/admin\/playlists\/\d+\/edit/g) || []).length;
    expect(afterCount).toBe(beforeCount);

    // 5. Admin can create playlist successfully
    const adminPlName = `PW-admin-${randSuffix()}`;
    const adminCreate = await adminReq.post('/admin/playlists/new', {
      form: { name: adminPlName, items: '[]', schedule_windows: '[]', enabled: 'on' }
    });
    // should redirect 302 to /admin/playlists
    expect([302, 303, 200]).toContain(adminCreate.status());
    const adminList2 = await adminReq.get('/admin/playlists');
    const adminHtml2 = await adminList2.text();
    expect(adminHtml2).toContain(adminPlName);
    // cleanup: extract id and delete
    let createdId = '';
    const m = adminHtml2.indexOf(adminPlName);
    if (m >= 0) {
      const snippet = adminHtml2.slice(Math.max(0, m - 2000), m + 2000);
      const mm = snippet.match(/\/admin\/playlists\/(\d+)\/edit/);
      if (mm) createdId = mm[1];
      // also try searching around
      if (!createdId) {
        const mm2 = adminHtml2.match(new RegExp(`/admin/playlists/(\\d+)/edit[^>]*>[\\s\\S]*?${adminPlName}`));
        if (mm2) createdId = mm2[1];
      }
    }
    // fallback: search all ids and find by fetching? just delete by id via POST if found
    // Do edit/delete test: admin can edit
    if (createdId) {
      const editResp = await adminReq.post(`/admin/playlists/${createdId}/edit`, {
        form: { name: adminPlName + '_edited', items: '[]', schedule_windows: '[]', enabled: 'on' }
      });
      expect([302, 303, 200]).toContain(editResp.status());
      // delete
      const delResp = await adminReq.post(`/admin/playlists/${createdId}/delete`);
      expect([302, 303, 200]).toContain(delResp.status());
      const finalList = await adminReq.get('/admin/playlists');
      const finalHtml = await finalList.text();
      expect(finalHtml).not.toContain(adminPlName + '_edited');
    }

    // 6. Admin can pause feed via POST /api/feed/pause with bearer token
    // create admin token
    const adminTokenResp = await adminReq.post('/admin/api-tokens', { form: { name: 'e2e-admin-token', role: 'admin' } });
    expect(adminTokenResp.status()).toBe(201);
    const adminTokenBody = await adminTokenResp.json() as any;
    const adminToken = adminTokenBody.secret as string;
    expect(adminToken).toBeTruthy();
    // use raw request with bearer
    const bare = await playwright.request.newContext({ baseURL: process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:8080' });
    const pauseResp = await bare.post('/api/feed/pause', { headers: { Authorization: `Bearer ${adminToken}` } });
    expect(pauseResp.status()).toBe(200);
    // resume to clean
    await bare.post('/api/feed/resume', { headers: { Authorization: `Bearer ${adminToken}` } });
    // revoke admin token
    await adminReq.post(`/admin/api-tokens/${adminTokenBody.id}/revoke`).catch(()=>{});

    // 7. Viewer-scoped API token: create via admin, GET /api/feed/current succeeds, POST /api/feed/next 403
    const viewerTokenResp = await adminReq.post('/admin/api-tokens', { form: { name: 'e2e-viewer-token', role: 'viewer' } });
    expect(viewerTokenResp.status()).toBe(201);
    const viewerTokenBody = await viewerTokenResp.json() as any;
    const viewerToken = viewerTokenBody.secret as string;
    expect(viewerToken).toBeTruthy();

    const vGet = await bare.get('/api/feed/current', { headers: { Authorization: `Bearer ${viewerToken}` } });
    expect(vGet.status()).toBe(200);

    const vPost = await bare.post('/api/feed/next', { headers: { Authorization: `Bearer ${viewerToken}` } });
    expect(vPost.status()).toBe(403);
    const vBody = await vPost.json().catch(async () => ({ text: await vPost.text() }));
    // should contain insufficient_role
    const bodyStr = JSON.stringify(vBody);
    expect(bodyStr).toContain('insufficient_role');

    // viewer token also cannot pause
    const vPause = await bare.post('/api/feed/pause', { headers: { Authorization: `Bearer ${viewerToken}` } });
    expect(vPause.status()).toBe(403);

    // cleanup tokens and user
    await adminReq.post(`/admin/api-tokens/${viewerTokenBody.id}/revoke`).catch(()=>{});
    // delete viewer user (need id)
    const users2 = await (await adminReq.get('/admin/api/users')).json() as any[];
    const vu = users2.find((u: any) => u.username === viewerUser);
    if (vu) await adminReq.delete(`/admin/api/users/${vu.id}`).catch(()=>{});

    await bare.dispose();
    await adminCtx.close();
    await viewerCtx.close();
    await browser.close();
    await apiCtx.dispose();
  });
});
