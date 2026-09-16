import { expect, test, type Page } from '@playwright/test';
import path from 'node:path';

const compiledStyles = path.resolve('dist/frontend/browser/styles.css');

const theme = `<style>:root{--bg:#09120f;--surface:#101c18;--surface-soft:#14231e;--control:#162720;--text:#eef5f2;--muted:#98aaa4;--line:#294039;--line-strong:#3c554c;--brand:#48c4b8;--brand-dark:#65ddd1;--brand-soft:#173c37;--success:#36c88a;--amber:#ffb23e;--amber-soft:#452f12;--red:#ff8179;--red-soft:#46201e;--shadow:0 8px 30px rgba(0,0,0,.2)}*{box-sizing:border-box}html,body{width:100%;height:100%;margin:0;background:var(--bg);font-family:Arial,sans-serif}</style>`;

async function addStyles(page: Page) {
  await page.addStyleTag({ path: compiledStyles });
}

test('work board action cards remain one aligned row at compact desktop width', async ({ page }) => {
  await page.setViewportSize({ width: 990, height: 500 });
  await page.setContent(`${theme}<div class="board-page"><header class="board-header"><div class="board-actions">
    <div class="board-queue-control board-action-card"><span class="material-symbols-rounded">move_to_inbox</span><div><small>Queue control</small><strong>Running</strong></div><button class="board-icon-action">Ⅱ</button></div>
    <div class="board-build-control board-action-card"><div><span class="material-symbols-rounded">fact_check</span><span><small>Build verify</small><strong>Configured</strong></span></div><button class="board-icon-action">▶</button></div>
    <div class="board-deploy-control board-action-card" data-state="unconfigured"><div><span class="material-symbols-rounded">rocket_launch</span><span><small>Production deploy</small><strong>Not configured</strong></span></div><a class="board-icon-action">⚙</a></div>
    <button class="new-request-button board-action-card">＋ New request</button>
  </div></header></div>`);
  await addStyles(page);

  const cards = page.locator('.board-action-card');
  const boxes = await cards.evaluateAll((items) => items.map((item) => item.getBoundingClientRect()).map(({ x, y, width, height, right }) => ({ x, y, width, height, right })));
  expect(new Set(boxes.map((box) => Math.round(box.y))).size).toBe(1);
  expect(new Set(boxes.map((box) => Math.round(box.height))).size).toBe(1);
  expect(boxes.every((box) => box.right <= 990)).toBe(true);
  const deployBox = await page.locator('.board-deploy-control').boundingBox();
  const actionBox = await page.locator('.board-deploy-control .board-icon-action').boundingBox();
  expect(actionBox!.y).toBeGreaterThanOrEqual(deployBox!.y);
  expect(actionBox!.y + actionBox!.height).toBeLessThanOrEqual(deployBox!.y + deployBox!.height);
});

test('Git delivery modal matches the approved desktop hierarchy without overflow', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1488, height: 1058 });
  const events = [
    ['10:14:03','Preflight started','Validating repository state, remotes, and protections'],
    ['10:14:15','Preflight completed','Repository state matches the approved snapshot'],
    ['10:14:24','Commit created','SHA a83f19c'],
    ['10:14:26','Push failed','Pre-push hook failed: npm test'],
    ['10:14:27','Developer recovery session running','Analyzing test failures and preparing a fix'],
    ['10:14:34','Verification in progress','28 / 38 tests passing'],
  ].map(([time,title,detail], index) => `<article data-level="${index===3?'warning':'info'}"><time>${time}</time><i></i><div><b>${title}</b><p>${detail}</p></div></article>`).join('');
  await page.setContent(`${theme}<app-git-delivery-modal><div class="delivery-backdrop"><section class="delivery-modal"><header><div class="delivery-title"><span>🚀</span><div><h2>Git delivery</h2><p>Executing delivery to remote repository</p></div></div><div class="delivery-context"><span><i>◫</i><b>AI-Product-Team</b><small>Project</small></span><span><i>⑂</i><b>main</b><small>Branch</small></span><span><i>◇</i><b>DL-028</b><small>Delivery ID</small></span></div><button class="delivery-close">×</button></header><div class="delivery-body"><section class="delivery-main"><div class="delivery-summary"><h3>3 tickets <span>•</span> 12 files</h3><p>Delivering changes from the current workspace to remote</p></div><div class="ticket-table"><div class="ticket-head"><span>Ticket</span><span>Title</span><span>Files</span><span>Status</span></div>${[['DEV-039','Add installer update flow','5'],['DEV-040','Improve error handling','4'],['DEV-041','Update documentation','3']].map(row=>`<div class="ticket-row"><b>${row[0]}</b><span>${row[1]}</span><strong>${row[2]}</strong><em>✓ Ready</em></div>`).join('')}</div><div class="delivery-steps">${['Preflight','Commit','Push','Complete'].map((step,index)=>`<div class="delivery-step ${index<2?'done':index===2?'current':''}"><div class="step-track"><i>${index<2?'✓':index+1}</i>${index<3?'<span></span>':''}</div><b>${step}</b><small>${index<2?'Completed':index===2?'Running':'Pending'}</small></div>`).join('')}</div><div class="integrity-strip">🛡 No broad staging <i>•</i> No force push <i>•</i> Remote SHA is reconciled</div></section><aside class="delivery-activity"><div><h3>Activity &amp; recovery</h3><p>Durable execution events and recovery actions</p></div><div class="timeline">${events}</div></aside></div><details class="delivery-log"><summary><span>▣</span><b>Detailed log</b><small>6 events · Trace ID DL-028</small><span>⌄</span></summary></details><footer><button class="secondary-action">■ Stop after current step</button><button class="pending-action">↻ Delivery in progress…</button></footer></section></div></app-git-delivery-modal>`);
  await addStyles(page);

  await expect(page.locator('app-git-delivery-modal')).toHaveCSS('position', 'fixed');
  const modal = page.locator('.delivery-modal');
  await expect(modal).toBeVisible();
  const overflow = await modal.evaluate((element) => ({ x: element.scrollWidth - element.clientWidth, y: element.scrollHeight - element.clientHeight }));
  expect(overflow.x).toBeLessThanOrEqual(1);
  expect(overflow.y).toBeLessThanOrEqual(1);
  const documentOverflow = await page.locator('html').evaluate((element) => ({ x: element.scrollWidth - element.clientWidth, y: element.scrollHeight - element.clientHeight }));
  expect(documentOverflow.x).toBeLessThanOrEqual(1);
  expect(documentOverflow.y).toBeLessThanOrEqual(1);
  await page.screenshot({ path: testInfo.outputPath('git-delivery-approved.png'), fullPage: true });
});
