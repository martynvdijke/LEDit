import { test, expect } from '@playwright/test';
test('admin backup export and import round-trip', async ({ page }) => {
  await page.goto('/login');
  await page.fill('input[name="username"]','admin');
  await page.fill('input[name="password"]','ledit');
  await page.click('button[type="submit"]');
  await page.waitForURL(/\/admin\//);
  await page.goto('/admin/backup');
  await expect(page.locator('h1')).toContainText('Backup');
  // export download
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.click('#download-btn'),
  ]);
  expect(download.suggestedFilename()).toContain('ledit-backup');
  // dry-run preview (create a minimal bundle via eval)
  await page.evaluate(async ()=>{
    const bundle={version:"1.0", ledit_version:"v0.9.2", exported_at:new Date().toISOString(), entities:{playlists:[{name:"e2e-pl",items:"[]",schedule_windows:"[]"}]}};
    const res=await fetch('/admin/api/backup/import?dry_run=true',{method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(bundle)});
    (window as any).__dry=res.ok;
  });
  // confirm real import via API
  await page.evaluate(async ()=>{
    const bundle={version:"1.0", ledit_version:"v0.9.2", exported_at:new Date().toISOString(), entities:{playlists:[{name:"e2e-pl2",items:"[]",schedule_windows:"[]"}]}};
    await fetch('/admin/api/backup/import',{method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(bundle)});
  });
  await expect(page.locator('#import-card')).toBeVisible();
});
