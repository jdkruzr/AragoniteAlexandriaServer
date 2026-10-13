// Screenshot and sanity-check rendered UI previews (see docs/web-ui.md).
//
//   ALEXANDRIA_UI_PREVIEW_DIR=/tmp/ui go test -count=1 -run 'TestWritePreviews|TestBOOXViewsRender' ./internal/alexandria/web ./internal/boox
//   NODE_PATH=$(npm root -g) node tools/ui-preview/screenshots.cjs /tmp/ui [page ...]
//
// Needs Playwright (npm i -g playwright) and a Chromium; set CHROME to use an
// installed browser. Writes <page>-{desktop,mobile,dark}.png beside the HTML
// and fails on horizontal page overflow or a help tip that does not open.
const fs = require('fs'), path = require('path');
const { chromium } = require('playwright');
const dir = path.resolve(process.argv[2] || '');
if (!process.argv[2] || !fs.existsSync(dir)) { console.error('usage: screenshots.cjs DIR [page ...]'); process.exit(2); }
const only = process.argv.slice(3);
const pages = fs.readdirSync(dir).filter(f => f.endsWith('.html')).map(f => f.slice(0, -5)).filter(p => !only.length || only.includes(p));
(async () => {
  const browser = await chromium.launch({ executablePath: process.env.CHROME || undefined, headless: true });
  let failed = 0;
  for (const name of pages) for (const [variant, width, colorScheme] of [['desktop', 1360, 'light'], ['mobile', 390, 'light'], ['dark', 1360, 'dark']]) {
    const page = await browser.newPage({ viewport: { width, height: 900 }, colorScheme });
    const problems = [];
    await page.goto('file://' + path.join(dir, name + '.html'));
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)) problems.push('horizontal page overflow');
    const tip = page.locator('button.tip').first();
    if (variant === 'desktop' && await tip.count()) {
      await tip.click();
      if (!await page.locator('.tip-pop:popover-open').count()) problems.push('help tip did not open');
      await page.keyboard.press('Escape');
    }
    await page.screenshot({ path: path.join(dir, `${name}-${variant}.png`), fullPage: true });
    console.log(JSON.stringify({ page: name, variant, passed: !problems.length, problems }));
    failed += problems.length > 0;
    await page.close();
  }
  await browser.close();
  process.exit(failed ? 1 : 0);
})();
