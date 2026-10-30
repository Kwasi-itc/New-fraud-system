import { expect, test } from "@playwright/test";

const tenant = "11111111-1111-4111-8111-111111111111";
const decision = "44444444-4444-4444-8444-444444444444";
const scenario = "88888888-8888-4888-8888-888888888888";
const path = "**/api/decision-screening/*";
const url = `/detection/decisions/${decision}?tenant=${tenant}`;
function execution(id: string, status: string, response_json: Record<string, unknown> = {}) {
  return { id, tenant_id: tenant, decision_id: decision, scenario_id: scenario, config_id: scenario, status,
    request_json: { provider: "opensanctions" }, response_json, last_error: "private-provider-error", failed_at: "2026-01-01",
    created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" };
}
test.beforeEach(async ({ page, context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  await page.route("**/v1/tenants/**", async route => {
    const p = new URL(route.request().url()).pathname;
    expect(p).toContain(tenant);
    if (p.endsWith(`/decisions/${decision}`)) return route.fulfill({ json: {
      decision: { id: decision, tenant_id: tenant, scenario_id: scenario, scenario_iteration_id: "iteration-1", object_id: "payment-1", object_type: "payment", outcome: "review", score: 35, created_at: "2026-01-01T00:00:00Z" }, rule_executions: [],
    } });
    if (p.endsWith(`/scenarios/${scenario}`)) return route.fulfill({ json: { scenario: { id: scenario, name: "Payment checks" } } });
    return route.fulfill({ json: { iterations: [], rules: [], workflow_executions: [], screening_executions: [], scoring_requests: [] } });
  });
});

test("renders source outcomes without treating execution completion or partial results as clear", async ({ page }) => {
  await page.route(path, route => route.fulfill({ json: { screening_executions: [
    execution("queued", "pending"), execution("processing", "sent"),
    execution("matches", "completed", { status: "awaiting_review", match_count: 2 }),
    execution("empty", "completed", { status: "no_hit", match_count: 0 }),
    execution("partial", "completed", { status: "no_hit", partial: true, match_count: 0 }),
    execution("confirmed", "completed", { status: "confirmed_hit", match_count: 1 }),
    execution("confirmed-partial", "completed", { status: "confirmed_hit", partial: true, match_count: 1 }),
    execution("failed", "failed"), execution("legacy", "completed"),
  ] } }));
  await page.goto(url);
  const panel = page.getByRole("region", { name: "Decision screening" });
  for (const [id, text] of [["queued", "Queued"], ["processing", "Processing"], ["matches", "Needs review"], ["empty", "No outstanding matches"], ["partial", "Incomplete results"], ["confirmed", "Confirmed match"], ["failed", "Screening failed"], ["legacy", "Execution completed"]]) {
    await expect(panel.getByRole("article", { name: `Screening ${id}`, exact: true })).toContainText(text);
  }
  await expect(panel).toContainText("Potential matches are not confirmed hits.");
  await expect(panel.getByRole("article", { name: "Screening confirmed-partial", exact: true })).toContainText("Confirmed match");
  await expect(panel.getByRole("article", { name: "Screening confirmed-partial", exact: true })).toContainText("Provider results are still incomplete.");
  await expect(panel).toContainText("does not change the original score or outcome");
  await expect(panel).not.toContainText("private-provider-error");
  await expect(panel.getByRole("button")).toHaveCount(1);
  await page.setViewportSize({ width: 390, height: 844 });
  const bounds = await panel.boundingBox();
  expect(bounds!.width).toBeLessThanOrEqual(390);
});

test("polls active execution to completion then allows explicit refresh", async ({ page }) => {
  let reads = 0;
  await page.route(path, route => route.fulfill({ json: { screening_executions: [
    ++reads === 1 ? execution("progress", "sent") : execution("progress", "completed", { status: "awaiting_review", match_count: 1 }),
  ] } }));
  await page.goto(url);
  const panel = page.getByRole("region", { name: "Decision screening" });
  await expect(panel.getByText("Processing", { exact: true })).toBeVisible();
  await expect(panel.getByText("Needs review", { exact: true })).toBeVisible({ timeout: 10000 });
  expect(reads).toBe(2);
  await panel.getByRole("button", { name: "Refresh screening results" }).click();
  await expect.poll(() => reads).toBe(3);
});

test("screening denial or service failure does not hide the decision or expose errors", async ({ page }) => {
  let status = 403;
  await page.route(path, route => status === 200
    ? route.fulfill({ json: { screening_executions: [] } })
    : route.fulfill({ status, json: { error: { message: "sensitive internal endpoint" } } }));
  await page.goto(url);
  const panel = page.getByRole("region", { name: "Decision screening" });
  await expect(panel.getByRole("alert")).toHaveText("You do not have access to these screening results.");
  await expect(page.getByText("Score", { exact: true }).first()).toBeVisible();
  await expect(panel).not.toContainText("sensitive internal endpoint");
  status = 503;
  await panel.getByRole("button", { name: "Refresh screening results" }).click();
  await expect(panel.getByRole("alert")).toContainText("Screening results are unavailable");
  status = 200;
  await panel.getByRole("button", { name: "Refresh screening results" }).click();
  await expect(panel).toContainText("No screening requests were created");
});

test("independent loading state leaves the original decision visible", async ({ page }) => {
  let release!: () => void;
  const ready = new Promise<void>(resolve => { release = resolve; });
  await page.route(path, async route => {
    await ready;
    await route.fulfill({ json: { screening_executions: [] } });
  });
  await page.goto(url);
  const panel = page.getByRole("region", { name: "Decision screening" });
  await expect(panel.getByRole("status")).toContainText("Loading screening results");
  await expect(page.getByText("Score", { exact: true }).first()).toBeVisible();
  release();
  await expect(panel).toContainText("No screening requests were created");
});

test("refresh displays recovery performed by the owner without a browser mutation", async ({ page }) => {
  let recovered = false;
  await page.route(path, route => {
    expect(route.request().method()).toBe("GET");
    return route.fulfill({ json: { screening_executions: [
      recovered ? execution("recovered", "completed", { status: "no_hit", match_count: 0 }) : execution("recovered", "failed"),
    ] } });
  });
  await page.goto(url);
  const panel = page.getByRole("region", { name: "Decision screening" });
  await expect(panel.getByText("Screening failed", { exact: true })).toBeVisible();
  recovered = true;
  await panel.getByRole("button", { name: "Refresh screening results" }).click();
  await expect(panel.getByText("No outstanding matches", { exact: true })).toBeVisible();
  await expect(panel.getByText("Screening failed", { exact: true })).toHaveCount(0);
});
