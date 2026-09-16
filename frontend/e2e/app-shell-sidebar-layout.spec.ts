import { expect, test } from '@playwright/test';
import path from 'node:path';

const compiledStyles = path.resolve('dist/frontend/browser/styles.css');

const theme = `<style>:root{--bg:#09120f;--surface:#101c18;--surface-soft:#14231e;--control:#162720;--panel:#13201b;--text:#eef5f2;--muted:#98aaa4;--line:#294039;--line-strong:#3c554c;--brand:#48c4b8;--brand-dark:#65ddd1;--brand-soft:#173c37;--success:#36c88a;--amber:#ffb23e;--amber-soft:#452f12;--red:#ff8179;--red-soft:#46201e}*{box-sizing:border-box}html,body{width:100%;height:100%;margin:0;background:var(--bg);font-family:Arial,sans-serif}</style>`;

test('collapsing the app sidebar gives the work area more room without document overflow', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.setContent(`${theme}<div class="app-shell">
    <aside class="sidebar" aria-label="Primary navigation">
      <div class="brand-row">
        <a class="brand"><img class="brand-mark" src="/assets/brand/product-team-logo-64.png" alt="" width="28" height="28"><span class="brand-copy"><strong>ProductCrew</strong><small>Development build</small></span></a>
        <button class="sidebar-collapse-toggle"><span class="material-symbols-rounded">keyboard_double_arrow_left</span></button>
      </div>
      <label class="global-project-switcher" title="Active project: AI-Product-Team"><span>Active project</span><div><span class="material-symbols-rounded">folder_open</span><select><option>AI-Product-Team</option></select><span class="material-symbols-rounded chevron">expand_more</span></div></label>
      <nav class="nav-list">
        ${[
          ['layers', 'Features'],
          ['view_kanban', 'Work board'],
          ['folder_open', 'Projects'],
          ['account_tree', 'Project Atlas'],
          ['bug_report', 'Bug Scanner'],
          ['radar', 'Feature Radar'],
          ['monitoring', 'Observability'],
          ['settings', 'Settings'],
        ].map(([icon, label]) => `<a title="${label}"><span class="material-symbols-rounded">${icon}</span><span class="nav-copy">${label}</span></a>`).join('')}
      </nav>
      <div class="notification-control"><button class="notification-toggle"><span class="material-symbols-rounded">notifications</span><span>Notifications</span><b>35</b></button></div>
      <button class="theme-toggle"><span class="material-symbols-rounded">dark_mode</span><span>Dark mode</span><span class="theme-switch"><i></i></span></button>
      <div class="local-status"><span class="status-dot"></span><div><strong>Local runtime ready</strong><small>Claude Code connected</small></div></div>
    </aside>
    <main class="main-content">
      <section class="board-page"><header class="board-header"><div><span class="eyebrow">Project orchestration</span><h1>Agent Work Board</h1></div><div class="board-actions"><button class="new-request-button board-action-card">New request</button></div></header></section>
    </main>
  </div>`);
  await page.addStyleTag({ path: compiledStyles });

  const expanded = await page.locator('.main-content').boundingBox();
  const expandedSidebar = await page.locator('.sidebar').boundingBox();
  expect(Math.round(expandedSidebar!.width)).toBe(224);
  const brandOverlap = await page.locator('.brand-row').evaluate((row) => {
    const copy = row.querySelector<HTMLElement>('.brand-copy');
    const toggle = row.querySelector<HTMLElement>('.sidebar-collapse-toggle');
    if (!copy || !toggle) return 0;
    return copy.getBoundingClientRect().right - toggle.getBoundingClientRect().left;
  });
  expect(brandOverlap).toBeLessThanOrEqual(-6);

  await page.locator('.app-shell').evaluate((element) => element.classList.add('sidebar-collapsed'));
  await page.waitForTimeout(250);
  const collapsed = await page.locator('.main-content').boundingBox();
  const sidebar = await page.locator('.sidebar').boundingBox();

  expect(Math.round(sidebar!.width)).toBe(64);
  expect(collapsed!.x).toBeLessThan(expanded!.x);
  expect(collapsed!.width - expanded!.width).toBeGreaterThan(120);

  const horizontalOverflow = await page.locator('html').evaluate((element) => element.scrollWidth - element.clientWidth);
  expect(horizontalOverflow).toBeLessThanOrEqual(1);

  const railOverflow = await page.locator('.sidebar').evaluate((sidebarElement) => {
    const sidebarBox = sidebarElement.getBoundingClientRect();
    const children = Array.from(sidebarElement.querySelectorAll<HTMLElement>('.brand, .sidebar-collapse-toggle, .global-project-switcher > div, .nav-list a, .notification-toggle, .theme-toggle, .local-status'));
    return Math.max(...children.map((child) => child.getBoundingClientRect().right - sidebarBox.right));
  });
  expect(railOverflow).toBeLessThanOrEqual(1);
});
