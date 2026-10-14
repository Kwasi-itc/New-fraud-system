import { expect, test } from "@playwright/test";

const tenant = "11111111-1111-4111-8111-111111111111";
const decision = "44444444-4444-4444-8444-444444444444";
const caseId = "33333333-3333-4333-8333-333333333333";
const scenario = "88888888-8888-4888-8888-888888888888";
const membershipPath = `**/api/cases/tenants/${tenant}/decision-case-memberships`;

test.beforeEach(async ({ page, context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  await page.route("**/v1/tenants/**", async route => {
    const path = new URL(route.request().url()).pathname;
    expect(path).toContain(tenant);
    if (path.endsWith(`/decisions/${decision}`)) return route.fulfill({ json: {
      decision: { id: decision, tenant_id: tenant, scenario_id: scenario, scenario_iteration_id: "iteration-1", object_id: "payment-1", object_type: "payment", outcome: "review", score: 35, created_at: "2026-01-01T00:00:00Z" }, rule_executions: [],
    } });
    if (path.endsWith(`/scenarios/${scenario}`)) return route.fulfill({ json: { scenario: { id: scenario, name: "Payment checks" } } });
    return route.fulfill({ json: { iterations: [], rules: [], workflow_executions: [], screening_executions: [], scoring_requests: [] } });
  });
});

test("individual decision shows the case name and links directly to case details", async ({ page }) => {
  await page.goto(`/detection/decisions/${decision}?tenant=${tenant}`);
  const link = page.getByRole("link", { name: "Payment investigation", exact: true });
  await expect(link).toHaveAttribute("href", `/cases/${caseId}?tenant=${tenant}`);
  await link.click();
  await expect(page).toHaveURL(new RegExp(`/cases/${caseId}\\?tenant=${tenant}`));
  await expect(page.getByRole("heading", { name: "Payment investigation", exact: true })).toBeVisible();
});

for (const state of ["unlinked", "unavailable"] as const) {
  test(`individual decision distinguishes ${state} membership`, async ({ page }) => {
    await page.route(membershipPath, route => route.fulfill({ json: { decisions: [{ decision_id: decision, state }] } }));
    await page.goto(`/detection/decisions/${decision}?tenant=${tenant}`);
    await expect(page.getByText(state === "unlinked" ? "No case" : "Case unavailable", { exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Payment investigation", exact: true })).toHaveCount(0);
  });
}

test("case lookup failure stays visible and can be retried", async ({ page }) => {
  let failed = true;
  await page.route(membershipPath, route => failed
    ? route.fulfill({ status: 503, json: { error: "Unavailable" } })
    : route.continue());
  await page.goto(`/detection/decisions/${decision}?tenant=${tenant}`);
  await expect(page.getByRole("alert").filter({ hasText: "Could not load case" })).toBeVisible();
  await expect(page.getByText("No case", { exact: true })).toHaveCount(0);
  failed = false;
  await page.getByRole("button", { name: "Retry case lookup" }).click();
  await expect(page.getByRole("link", { name: "Payment investigation", exact: true })).toBeVisible();
});
