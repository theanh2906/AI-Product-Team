import { expect, test } from '@playwright/test';
import path from 'node:path';

const globalStyles = path.resolve('src/styles.css');

const theme = `<style>:root{--bg:#09120f;--surface:#101c18;--surface-soft:#14231e;--control:#162720;--text:#eef5f2;--muted:#98aaa4;--line:#294039;--line-strong:#3c554c;--brand:#48c4b8;--brand-dark:#65ddd1;--brand-soft:#173c37;--amber:#ffb23e;--amber-soft:#452f12}*{box-sizing:border-box}html,body{width:100%;margin:0;background:var(--bg);font-family:Arial,sans-serif}</style>`;

test('email notification textboxes keep the shared ProductCrew style and row alignment', async ({ page }) => {
  await page.setViewportSize({ width: 1176, height: 560 });
  await page.setContent(`${theme}<section class="settings-card" id="email-notifications-card">
    <div class="settings-card-header">
      <span class="settings-icon">✉</span>
      <div><h2>Email notifications</h2><p>Get an email for high-signal ProductCrew events instead of watching the app.</p></div>
      <span class="credential-status"><i></i>Disabled</span>
    </div>
    <div class="settings-card-body">
      <div class="two-column-fields">
        <label class="field"><span>Destination email</span><input type="email" placeholder="you@example.com"></label>
        <label class="field"><span>Sender name <small>Optional</small></span><input type="text" placeholder="ProductCrew"></label>
      </div>
      <div class="two-column-fields">
        <label class="field"><span>Sender address</span><input type="email" placeholder="productcrew@example.com"></label>
        <label class="field"><span>Security</span><select><option>STARTTLS</option></select></label>
      </div>
      <div class="two-column-fields">
        <label class="field"><span>SMTP host</span><input type="text" placeholder="smtp.example.com"></label>
        <label class="field"><span>SMTP port</span><input type="number" value="587"></label>
      </div>
      <div class="two-column-fields">
        <label class="field"><span>Username <small>Optional</small></span><input type="text" placeholder="SMTP username"></label>
        <label class="field"><span>App base URL <small>Optional</small></span><input type="url" value="http://127.0.0.1:8081"></label>
      </div>
    </div>
  </section>`);
  await page.addStyleTag({ path: globalStyles });
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'dark'));

  const controls = page.locator('#email-notifications-card input, #email-notifications-card select');
  const styles = await controls.evaluateAll((elements) => elements.map((element) => {
    const style = getComputedStyle(element);
    const box = element.getBoundingClientRect();
    return {
      backgroundColor: style.backgroundColor,
      borderColor: style.borderColor,
      borderRadius: style.borderRadius,
      color: style.color,
      height: Math.round(box.height),
    };
  }));

  expect(new Set(styles.map(({ backgroundColor }) => backgroundColor)).size).toBe(1);
  expect(new Set(styles.map(({ borderColor }) => borderColor)).size).toBe(1);
  expect(new Set(styles.map(({ borderRadius }) => borderRadius)).size).toBe(1);
  expect(new Set(styles.map(({ color }) => color)).size).toBe(1);
  expect(new Set(styles.map(({ height }) => height))).toEqual(new Set([42]));

  for (const row of await page.locator('.two-column-fields').all()) {
    const tops = await row.locator('input, select').evaluateAll((elements) => elements.map((element) => element.getBoundingClientRect().y));
    expect(Math.max(...tops) - Math.min(...tops)).toBeLessThanOrEqual(1);
  }

  await page.screenshot({ path: path.resolve('design-output-settings-email-controls.png'), fullPage: true });
});
