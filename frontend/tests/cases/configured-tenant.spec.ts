import { expect, test } from "@playwright/test";
const tenant = "11111111-1111-4111-8111-111111111111";
const foreign = "99999999-9999-4999-8999-999999999999";
test.beforeEach(async ({ context,request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-workflow", domain: "127.0.0.1", path: "/" }]);
});
test("Detection uses the authenticated tenant", async ({ page }) => {
  const paths: string[] = [];
  await page.route("**/api/decisions/tenants/**", async route => {
    paths.push(new URL(route.request().url()).pathname);
    await route.fulfill({ json: { decisions: [], scenarios: [], lists: [], entries: [], count: 0 } });
  });
  await page.goto("/detection?tab=Decisions");
  await expect.poll(() => paths.some(path => path.endsWith("/decisions"))).toBe(true);
  expect(paths.every(path => path.startsWith("/api/decisions/tenants/"+tenant+"/"))).toBe(true);
});
test("Cases defaults to the session tenant and rejects foreign tenant links", async ({ page }) => {
  const access = page.waitForResponse(r => r.url().includes("/api/cases/tenants/"+tenant+"/case-session"));
  await page.goto("/cases");
  expect((await access).status()).toBe(200);
  const denied = page.waitForResponse(r => r.url().includes("/api/cases/tenants/"+foreign+"/case-session"));
  await page.goto("/cases?tenant="+foreign);
  expect((await denied).status()).toBe(403);
});
