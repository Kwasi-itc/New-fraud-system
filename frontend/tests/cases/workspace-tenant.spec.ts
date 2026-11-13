import { expect, test, type Page } from "@playwright/test";

const tenant = "11111111-1111-4111-8111-111111111111";
const foreignTenant = "99999999-9999-4999-8999-999999999999";
const scenario = "88888888-8888-4888-8888-888888888888";
const list = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const liveIteration = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";

test.beforeEach(async ({ context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-workflow", domain: "127.0.0.1", path: "/" }]);
});

async function workspace(page: Page, live = false) {
  const paths: string[] = [];
  await page.route("**/api/data-model/**", route => {
    const path = new URL(route.request().url()).pathname;
    paths.push(path);
    expect(path).toContain(`/tenants/${tenant}/`);
    return route.fulfill({ json: { data_model: { tables: {} } } });
  });
  await page.route("**/api/decisions/**", route => {
    const url = new URL(route.request().url());
    paths.push(url.pathname);
    if (!url.pathname.endsWith("/rule-functions")) expect(url.pathname).toContain(`/tenants/${tenant}/`);
    const item = { id: scenario, tenant_id: tenant, name: "Session tenant checks", trigger_object_type: "payment", created_at: "2026-01-01T00:00:00Z", ...(live ? { live_iteration_id: liveIteration } : {}) };
    const json = url.pathname.endsWith("/scenarios") ? { scenarios: live ? [item] : [] }
      : url.pathname.endsWith(`/scenarios/${scenario}`) ? { scenario: item }
      : url.pathname.endsWith("/decisions/count") ? { count: url.searchParams.has("outcome") ? 0 : 42 }
      : url.pathname.endsWith(`/custom-lists/${list}`) ? { custom_list: { id: list, tenant_id: tenant, name: "Tenant list", list_type: "string" } }
      : { decisions: [], iterations: [], publications: [], rules: [], custom_lists: [], entries: [], count: 0, pagination: { has_more: false } };
    return route.fulfill({ json });
  });
  return paths;
}

test("dashboard loads the signed-in workspace without a configured tenant", async ({ page }) => {
  const paths = await workspace(page);
  await page.goto(`/dashboard?tenant=${foreignTenant}`);
  await expect(page.getByRole("heading", { name: "Getting started", exact: true })).toBeVisible();
  await expect(page.getByText("Dashboard data is unavailable")).toHaveCount(0);
  expect(paths.some(path => path.endsWith("/scenarios"))).toBe(true);
  expect(paths.every(path => !path.includes(foreignTenant))).toBe(true);
});

test("operations dashboard counts decisions for the same session tenant", async ({ page }) => {
  const paths = await workspace(page, true);
  await page.goto("/dashboard");
  await expect(page.getByRole("heading", { name: "Dashboard", exact: true })).toBeVisible();
  await expect(page.getByText("42", { exact: true }).first()).toBeVisible();
  await expect.poll(() => paths.filter(path => path.endsWith("/decisions/count")).length).toBe(5);
  await expect(page.getByText("Dashboard data is unavailable")).toHaveCount(0);
});

test("Detection uses the session tenant even with a foreign tenant link", async ({ page }) => {
  const paths = await workspace(page);
  await page.goto(`/detection?tab=Decisions&tenant=${foreignTenant}`);
  await expect.poll(() => paths.some(path => path.endsWith("/decisions"))).toBe(true);
  expect(paths.every(path => !path.includes(foreignTenant))).toBe(true);
});

test("Cases stays in the session workspace and rejects direct foreign access", async ({ page }) => {
  const access = page.waitForResponse(response => response.url().includes(`/api/cases/tenants/${tenant}/case-session`));
  await page.goto(`/cases?tenant=${foreignTenant}`);
  expect((await access).status()).toBe(200);
  const denied = await page.request.get(`/api/cases/tenants/${foreignTenant}/case-session`);
  expect(denied.status()).toBe(403);
});

for (const [label, route, endpoint] of [
  ["scenario", `/detection/${scenario}`, `/scenarios/${scenario}`],
  ["execution", `/detection/${scenario}/live`, `/scenarios/${scenario}`],
  ["test", `/detection/${scenario}/tests`, `/scenarios/${scenario}`],
  ["list", `/detection/lists/${list}`, `/custom-lists/${list}`],
] as const) {
  test(`${label} page requests the signed-in tenant without public configuration`, async ({ page }) => {
    const paths = await workspace(page);
    await page.goto(route);
    await expect.poll(() => paths.some(path => path.endsWith(endpoint))).toBe(true);
    await expect(page.getByText(/NEXT_PUBLIC_DATA_MODEL_TENANT_ID/)).toHaveCount(0);
  });
}
