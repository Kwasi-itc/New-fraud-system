import { expect, test } from "@playwright/test";

test("decision details do not load screening or workflow panels", async ({ page, context, request }) => {
  const tenant = "11111111-1111-4111-8111-111111111111";
  const decision = "44444444-4444-4444-8444-444444444444";
  const scenario = "88888888-8888-4888-8888-888888888888";
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  const removedRequests: string[] = [];
  page.on("request", (req) => {
    if (/decision-screening|workflow-executions|scoring-requests/.test(req.url())) removedRequests.push(req.url());
  });
  await page.route("**/api/decisions/tenants/**", async route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith(`/decisions/${decision}`)) return route.fulfill({ json: {
      decision: { id: decision, tenant_id: tenant, scenario_id: scenario, scenario_iteration_id: "iteration-1", object_id: "payment-1", object_type: "payment", outcome: "review", score: 35, triggered: true, created_at: "2026-01-01T00:00:00Z" },
      rule_executions: [],
    } });
    if (path.endsWith(`/scenarios/${scenario}`)) return route.fulfill({ json: { scenario: { id: scenario, name: "Payment checks" } } });
    return route.fulfill({ json: { iterations: [], rules: [] } });
  });
  await page.goto(`/detection/decisions/${decision}?tenant=${tenant}`);
  await expect(page.getByRole("heading", { name: "Decision details" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Rule execution breakdown" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Decision screening" })).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "Workflow Executions" })).toHaveCount(0);
  expect(removedRequests).toEqual([]);
});
