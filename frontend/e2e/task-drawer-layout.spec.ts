import { expect, test, type Page } from '@playwright/test';
import path from 'node:path';

const workItemStyles = path.resolve('src/app/features/work-items/work-item-page.css');

const longEvidence =
  'Automated and source-level checks passed, including an-unbroken-evidence-token-that-must-never-expand-the-fixed-width-task-drawer-beyond-its-visible-boundary.';

async function renderTaskDrawer(page: Page) {
  await page.setContent(`
    <style>
      :root {
        --text: #f2f6f4;
        --muted: #9dadA7;
        --surface: #10201a;
        --surface-soft: #15261f;
        --line: #294038;
        --brand: #48c4b8;
        --brand-soft: #153d38;
        --red: #ff8179;
        --red-soft: #46201e;
      }
      html, body { width: 100%; height: 100%; margin: 0; }
    </style>
    <div class="drawer-backdrop">
      <aside class="task-drawer" aria-label="Task details">
        <header class="drawer-header"><div><strong>QA</strong><span>QA-010</span></div></header>
        <div class="drawer-scroll">
          <section class="drawer-title">
            <h2>Validate smoke-test tracking end to end</h2>
            <p>${longEvidence}</p>
          </section>
          <section class="drawer-section execution-alert">
            <span>!</span><div><strong>QA needs attention</strong><p>${longEvidence}</p></div>
          </section>
          <section class="drawer-section delivery-report">
            <div class="delivery-heading"><span>✓</span><div><strong>QA delivery report</strong></div></div>
            <p>${longEvidence}</p>
            <h4>Verification</h4>
            <ul>
              <li>${longEvidence}</li>
              <li><code>C:/workspace/.productcrew/verification/${longEvidence}</code></li>
            </ul>
            <h4>Remaining risks</h4>
            <ul><li>${longEvidence}</li></ul>
          </section>
        </div>
      </aside>
    </div>
  `);
  await page.addStyleTag({ path: workItemStyles });
}

for (const viewport of [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'compact desktop pane', width: 520, height: 720 },
]) {
  test(`task delivery report stays inside the drawer at ${viewport.name} width`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await renderTaskDrawer(page);

    const overflow = await page.locator('.task-drawer').evaluate((drawer) => {
      const inspected = [drawer, ...drawer.querySelectorAll('.drawer-scroll, .drawer-section, .delivery-report, .delivery-report ul, .delivery-report li')];
      return inspected
        .map((element) => ({
          selector: element.className || element.tagName.toLowerCase(),
          clientWidth: element.clientWidth,
          scrollWidth: element.scrollWidth,
        }))
        .filter((measurement) => measurement.scrollWidth > measurement.clientWidth);
    });

    await expect(page.locator('.drawer-scroll')).toHaveJSProperty('scrollLeft', 0);
    expect(overflow).toEqual([]);
  });
}
