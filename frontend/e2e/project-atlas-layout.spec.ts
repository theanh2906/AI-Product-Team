import { expect, test } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';

function resolveCompiledStyles(): string {
  const browserDist = path.resolve('dist/frontend/browser');
  const stablePath = path.join(browserDist, 'styles.css');
  if (fs.existsSync(stablePath)) return stablePath;
  const fingerprinted = fs.readdirSync(browserDist).find((file) => /^styles-[A-Z0-9]+\.css$/i.test(file));
  if (!fingerprinted) throw new Error(`Could not find compiled styles in ${browserDist}`);
  return path.join(browserDist, fingerprinted);
}

test('Project Atlas styles ship in the initial bundle and render its desktop layout', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.setContent(`
    <style>
      :root {
        --bg: #0d1412;
        --surface: #131c19;
        --surface-soft: #19241f;
        --panel: #101816;
        --text: #edf4f1;
        --muted: #9baaa5;
        --line: #293b35;
        --line-strong: #3b534b;
        --brand: #43c4b7;
        --brand-dark: #62d8cd;
        --brand-soft: #173c37;
        --shadow: 0 8px 30px rgba(0, 0, 0, .2);
      }
      html, body { width: 100%; height: 100%; margin: 0; }
    </style>
    <app-project-atlas-page><main class="atlas-page">
      <header class="atlas-header">
        <div><span class="atlas-eyebrow">PROJECT INTELLIGENCE</span><h1>Project Atlas</h1><p>Study the repository once, then explore its architecture and flows.</p></div>
        <div class="atlas-header-actions"><button class="atlas-primary">Study project</button></div>
      </header>
      <section class="atlas-project-strip">
        <span class="atlas-project-icon">A</span>
        <div class="atlas-project-copy"><span>Active project</span><strong>vltk-auto</strong><small>E:/Projects/vltk-auto</small></div>
        <span class="atlas-source-badge">local</span>
        <div class="atlas-project-stat"><span>Knowledge status</span><strong>Not studied</strong></div>
      </section>
      <section class="atlas-empty hero-empty">
        <span class="atlas-eyebrow">LOCAL-FIRST PROJECT STUDY</span>
        <h2>Turn this repository into navigable knowledge</h2>
        <p>Index source structure and persist the result inside the project.</p>
        <button class="atlas-primary">Start project study</button>
      </section>
      <div class="atlas-workspace">
        <section class="atlas-canvas">
          <header>
            <div><span class="atlas-eyebrow">workflows</span><h2>sample system map</h2><p>Angular UI connected to a Go local service.</p></div>
            <div class="atlas-canvas-tools">
              <span class="atlas-confidence"><i></i>Current snapshot</span>
              <button type="button"><span class="material-symbols-rounded">remove</span></button>
              <strong>100%</strong>
              <button type="button"><span class="material-symbols-rounded">add</span></button>
              <button type="button"><span class="material-symbols-rounded">open_in_full</span><span>Fullscreen</span></button>
            </div>
          </header>
          <div class="atlas-diagram-frame" style="--diagram-scale: 1;">
            <div class="atlas-diagram-zoom-layer">
              <div class="flow-layout">
                <aside><button class="selected"><strong>Request to board</strong><small>2 steps</small></button></aside>
                <div class="sequence-diagram" style="--sequence-lanes: 3;">
                  <div class="sequence-lanes" style="grid-template-columns: repeat(3, minmax(120px, 1fr));">
                    <div class="sequence-lane"><span>AN</span><strong>Angular UI</strong></div>
                    <div class="sequence-lane"><span>GO</span><strong>Go API</strong></div>
                    <div class="sequence-lane"><span>BO</span><strong>Board Store</strong></div>
                  </div>
                  <div class="sequence-steps" style="grid-template-columns: repeat(3, minmax(120px, 1fr));">
                    <div class="sequence-step" data-align="left" style="grid-column: 1 / 3; grid-row: 1;"><b>1</b><i></i><span><small>Angular UI -> Go API</small><strong>Submit request</strong></span></div>
                    <div class="sequence-step" data-align="left" style="grid-column: 2 / 4; grid-row: 2;"><b>2</b><i></i><span><small>Go API -> Board Store</small><strong>Persist backlog card</strong></span></div>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div class="atlas-diagram-frame atlas-diagram-frame-fullscreen" style="--diagram-scale: 1;">
            <div class="atlas-diagram-zoom-layer">
              <div class="flow-layout">
                <aside><button class="selected"><strong>Delivery flow</strong><small>4 stages</small></button></aside>
                <f-flow class="workflow-graph workflow-flow">
                  <f-canvas>
                    <article class="workflow-node workflow-node-flow" style="left: 36px; top: 34px;"><b>1</b><span>Start</span><strong>Capture request</strong><i class="workflow-connector workflow-connector-source"></i></article>
                    <article class="workflow-node workflow-node-flow" style="left: 390px; top: 34px;"><b>2</b><span>Stage</span><strong>Approve plan</strong><i class="workflow-connector workflow-connector-target"></i><i class="workflow-connector workflow-connector-source"></i></article>
                    <article class="workflow-node workflow-node-flow" style="left: 390px; top: 204px;"><b>3</b><span>Stage</span><strong>Run implementation</strong><i class="workflow-connector workflow-connector-target"></i><i class="workflow-connector workflow-connector-source"></i></article>
                    <article class="workflow-node workflow-node-flow" style="left: 36px; top: 204px;"><b>4</b><span>Finish</span><strong>Verify QA</strong><i class="workflow-connector workflow-connector-target"></i></article>
                  </f-canvas>
                </f-flow>
                <div class="workflow-minimap"><span style="left: 6px; top: 6px;"></span><span style="left: 65px; top: 6px;"></span><span style="left: 65px; top: 34px;"></span><span style="left: 6px; top: 34px;"></span></div>
              </div>
            </div>
          </div>
        </section>
        <aside class="atlas-inspector"></aside>
      </div>
      <div class="atlas-fullscreen-backdrop">
        <section class="atlas-fullscreen-modal"><header><div><span class="atlas-eyebrow">INTERACTIVE DIAGRAM</span><h2>sample · workflows</h2><p>Fullscreen diagram modal.</p></div><div class="atlas-canvas-tools"><button>Close</button></div></header><div class="atlas-fullscreen-body"></div></section>
      </div>
    </main></app-project-atlas-page>
  `);
  await page.addStyleTag({ path: resolveCompiledStyles() });
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'dark'));

  const pageLayout = page.locator('.atlas-page');
  const projectStrip = page.locator('.atlas-project-strip');
  const emptyState = page.locator('.atlas-empty');

  await expect(pageLayout).toHaveCSS('padding-top', '22px');
  await expect(page.locator('.atlas-header')).toHaveCSS('display', 'flex');
  await expect(projectStrip).toHaveCSS('display', 'flex');
  await expect(projectStrip).toHaveCSS('border-top-left-radius', '10px');
  await expect(emptyState).toHaveCSS('display', 'grid');
  await expect(emptyState).toHaveCSS('text-align', 'center');
  await expect(page.locator('app-project-atlas-page')).toHaveCSS('display', 'block');
  await expect(page.locator('.atlas-canvas-tools').first()).toHaveCSS('display', 'flex');
  await expect(page.locator('.atlas-diagram-frame').first()).toHaveCSS('overflow-x', 'auto');
  await expect(page.locator('.atlas-fullscreen-backdrop')).toHaveCSS('position', 'fixed');
  await expect(page.locator('.atlas-fullscreen-modal')).toHaveCSS('display', 'grid');
  await expect(page.locator('.sequence-diagram')).toHaveCSS('display', 'grid');
  await expect(page.locator('f-flow.workflow-flow')).toHaveCSS('display', 'block');
  await expect(page.locator('f-flow.workflow-flow')).toHaveCSS('overflow', 'hidden');
  await expect(page.locator('.workflow-node-flow').first()).toHaveCSS('cursor', 'grab');
  await expect(page.locator('.workflow-minimap')).toHaveCSS('position', 'absolute');
  const overflow = await page.locator('.atlas-workspace').evaluate((element) => element.scrollWidth - element.clientWidth);
  expect(overflow).toBeLessThanOrEqual(1);
  await page.locator('.atlas-fullscreen-backdrop').evaluate((element) => {
    (element as HTMLElement).hidden = true;
  });
  await page.screenshot({ path: testInfo.outputPath('project-atlas-global-style.png'), fullPage: true });
});
