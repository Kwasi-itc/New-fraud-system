import { expect, test } from "@playwright/test";

// Run against a build made with NEXT_PUBLIC_DATA_MODEL_TENANT_ID set.
const configuredTenant = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID;
const sessionTenant = "11111111-1111-4111-8111-111111111111";

test.beforeEach(async ({ context }) => {
  test.skip(!configuredTenant || configuredTenant === sessionTenant, "Requires a configured tenant different from the fixture session.");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
});

test("Detection requests the configured tenant without replacing the session credential", async ({ page }) => {
  const paths: string[] = [];
  await page.route("**/v1/tenants/**", async route => {
    paths.push(new URL(route.request().url()).pathname);
    await route.fulfill({ json: { decisions: [], scenarios: [{ id: "88888888-8888-4888-8888-888888888888", name: "Tenant regression scenario", trigger_object_type: "payment", created_at: "2026-01-01T00:00:00Z" }], lists: [], entries: [], count: 0 } });
  });
  const caseAccess = page.waitForResponse(response => response.url().includes(`/api/cases/tenants/${configuredTenant}/case-session`));
  await page.goto("/detection?tab=Decisions");
  expect((await caseAccess).status()).toBe(403);
  await expect.poll(() => paths.some(path => path.endsWith("/decisions"))).toBe(true);
  expect(paths.every(path => path.startsWith(`/v1/tenants/${configuredTenant}/`))).toBe(true);
  await expect(page.getByRole("button", { name: /^Add to Case/ })).toBeDisabled();
});

test("Cases defaults to configured tenant and preserves explicit tenant links", async ({ page }) => {
  const configuredAccess = page.waitForResponse(response => response.url().includes(`/api/cases/tenants/${configuredTenant}/case-session`));
  await page.goto("/cases");
  expect((await configuredAccess).status()).toBe(403);
  const explicitAccess = page.waitForResponse(response => response.url().includes(`/api/cases/tenants/${sessionTenant}/case-session`));
  await page.goto(`/cases?tenant=${sessionTenant}`);
  expect((await explicitAccess).status()).toBe(200);
});

test("My Account opens the configured default case workspace", async ({ page }) => {
  await page.goto("/my-account");
  await expect(page.getByRole("link", { name: "Open Case Manager" })).toHaveAttribute("href", "/cases");
});
