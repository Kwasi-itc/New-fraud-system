// Optional browser check; uses the frontend's existing Playwright installation.
const assert = require('node:assert/strict');
const {chromium} = require('../../frontend/node_modules/@playwright/test');

(async () => {
  const port = Number(process.env.POSTGRES_GATEWAY_PORT || 8088);
  assert(Number.isInteger(port) && port >= 1024 && port <= 65535);
  const base = `http://127.0.0.1:${port}`;
  const browser = await chromium.launch({headless: true});
  try {
    for (const name of ['data-model', 'ingestion', 'decisions', 'screening']) {
      const page = await browser.newPage();
      page.on('pageerror', error => console.error(`${name}: ${error.message}`));
      page.setDefaultTimeout(20000);
      await page.goto(`${base}/services/${name}/docs`);
      await page.locator('.swagger-ui .opblock').first().waitFor({timeout: 45000});
      const operation = page.locator('.opblock').filter({has: page.locator('.opblock-summary-path[data-path="/healthz"]')});
      await operation.locator('.opblock-summary').click();
      await operation.getByRole('button', {name: 'Try it out', exact: true}).click();
      const response = page.waitForResponse(result => result.url() === `${base}/services/${name}/healthz`);
      await operation.getByRole('button', {name: 'Execute', exact: true}).click();
      const result = await response;
      assert(result.ok(), `${name}: health status ${result.status()}`);
      console.log(`PASS browser ${name}: Swagger execution through gateway (${result.status()})`);
      await page.close();
    }
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error.message); process.exitCode = 1; });
