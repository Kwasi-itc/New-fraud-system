import { expect, test } from "@playwright/test";
import { aggregateFilters } from "../../src/components/detection/rule-execution-evidence";
import type { RuleEvaluationNode } from "../../src/lib/decision-engine-api";

const tenant = "11111111-1111-4111-8111-111111111111";
const decision = "44444444-4444-4444-8444-444444444444";
const scenario = "88888888-8888-4888-8888-888888888888";
const constant = (value: string | number | boolean) => ({ constant: value, return_value: value });
const reference = { function: "field_ref", return_value: "2026-11-26T13:42:18Z", named_children: { field: constant("initiated_at") } };
const earlier = { function: "TimeAdd", return_value: "2026-11-25T13:42:18Z", named_children: { timestampField: reference, duration: constant("24h"), sign: constant("-") } };
const filter = (field: string, operator: string, value: RuleEvaluationNode) => ({ function: "Filter", named_children: { fieldName: constant(field), operator: constant(operator), value } });
const aggregate = {
  function: "Aggregator", return_value: 3,
  named_children: {
    tableName: constant("transactions"), fieldName: constant("user_id"), aggregator: constant("COUNT_DISTINCT"),
    filters: { function: "List", children: [filter("initiated_at", ">", earlier), filter("initiated_at", "<=", reference), filter("leg", "=", constant("credit"))] },
  },
};
const comparison = { function: "gte", return_value: true, children: [aggregate, constant(3)] };
const skipped = { function: "eq", skipped: true, children: [{ function: "field_ref", named_children: { field: constant("state") } }, constant("settled")] };
const execution = (id: string, outcome: string, evaluation?: object) => ({
  id, decision_id: decision, rule_id: id, rule_name: id === "rule-1" ? "Many senders" : "Risk signal",
  outcome, score_modifier: id === "rule-1" ? 50 : 20, evaluation, created_at: "2026-11-26T13:42:18Z",
});

test.beforeEach(async ({ page, context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  await page.route("**/api/decisions/tenants/**", async route => {
    const path = new URL(route.request().url()).pathname;
    expect(path).toContain(tenant);
    if (path.endsWith(`/decisions/${decision}`)) return route.fulfill({ json: {
      decision: { id: decision, tenant_id: tenant, scenario_id: scenario, scenario_iteration_id: "iteration-1", object_id: "payment-1", object_type: "transactions", outcome: "review", score: 50, triggered: true, created_at: "2026-11-26T13:42:18Z", request_body: { fields: { amount: 100 } } },
      rule_executions: [execution("rule-1", "hit", comparison), execution("rule-2", "no_hit", { function: "and", return_value: false, children: [{ function: "eq", return_value: false, children: [constant("failed"), constant("settled")] }, skipped] }), execution("rule-3", "snoozed")],
    } });
    if (path.endsWith(`/scenarios/${scenario}`)) return route.fulfill({ json: { scenario: { id: scenario, name: "Payment checks" } } });
    if (path.endsWith("/iterations")) return route.fulfill({ json: { iterations: [{ id: "iteration-1", version: 3, score_review_threshold: 40, score_block_and_review_threshold: 70, score_decline_threshold: 90 }] } });
    if (path.endsWith("/rules")) return route.fulfill({ json: { rules: [{ id: "rule-1", formula: { function: "gte", children: [{ constant: 3 }, { constant: 3 }] } }] } });
    return route.fulfill({ json: { workflow_executions: [], scoring_requests: [] } });
  });
});

test("decision inspector displays recorded comparisons, independent filters, and time windows", async ({ page }) => {
  await page.goto(`/detection/decisions/${decision}?tenant=${tenant}`);
  await expect(page.getByRole("heading", { name: "Rule execution breakdown" })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Product navigation" }).getByRole("link", { name: "Decisions" })).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("heading", { name: "Evaluated request" })).toBeVisible();
  await expect(page.getByText("100", { exact: true })).toBeVisible();
  await page.getByText("View raw request JSON").click();
  await expect(page.locator("pre").filter({ hasText: "\"amount\": 100" })).toBeVisible();
  await expect(page.getByLabel("Score 50. Review threshold 40, block and review 70, decline 90.")).toBeVisible();
  const rule = page.getByRole("button", { name: /Many senders.*Matched/ });
  await expect(rule).toHaveCSS("background-color", "rgb(255, 251, 243)");
  await expect(rule).toContainText("Aggregate");
  await rule.click();
  const dialog = page.getByRole("dialog", { name: "Rule execution" });
  await expect(dialog).toContainText("Distinct count of user id (transactions) ≥ 3");
  await expect(dialog).toContainText("Observed: 3 ≥ 3");
  await expect(dialog).toContainText("Within the 1-day window before the evaluated record's initiated at");
  await expect(dialog).toContainText("Start excluded · End included");
  await expect(dialog).toContainText("leg = credit");
  await dialog.getByText("View configured time filters").click();
  await expect(dialog).toContainText("initiated at > initiated at − 24h");
  await expect(dialog.getByText(/contributing transactions/i)).toHaveCount(0);
  await dialog.getByRole("combobox", { name: "Rule execution" }).selectOption("rule-2");
  await expect(dialog).toContainText("Not evaluated");
  await expect(dialog).toContainText("Observed: failed = settled");
  await dialog.getByRole("combobox", { name: "Rule execution" }).selectOption("rule-3");
  await expect(dialog).toContainText("No evaluation trace was recorded");
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  await rule.click();
  await expect(dialog).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
});

test("decline highlights matched rules in red and does not invent score thresholds", async ({ page }) => {
  await page.route(`**/api/decisions/tenants/${tenant}/decisions/${decision}`, route => route.fulfill({ json: {
    decision: { id: decision, tenant_id: tenant, scenario_id: scenario, scenario_iteration_id: "missing-iteration", object_id: "payment-1", object_type: "transactions", outcome: "decline", score: 120, triggered: true, created_at: "2026-11-26T13:42:18Z" },
    rule_executions: [execution("rule-1", "hit", comparison)],
  } }));
  await page.goto(`/detection/decisions/${decision}?tenant=${tenant}`);
  await expect(page.getByRole("button", { name: /Many senders.*Matched/ })).toHaveCSS("background-color", "rgb(255, 247, 246)");
  await expect(page.getByRole("button", { name: /Many senders.*Matched/ })).toHaveCSS("box-shadow", /rgb\(217, 106, 96\)/);
  await expect(page.getByText("Score thresholds are unavailable for this iteration.")).toBeVisible();
  await expect(page.getByRole("img", { name: /threshold/ })).toHaveCount(0);
  await expect(page.getByText("No request fields were recorded.")).toBeVisible();
  await expect(page.getByRole("table", { name: "Evaluated request fields" })).toHaveCount(0);
});

test("evaluated request maps recorded fields and metadata to their values on desktop and mobile", async ({ page }) => {
  const longValue = "long-identifier-".repeat(12);
  await page.route(`**/api/decisions/tenants/${tenant}/decisions/${decision}`, route => route.fulfill({ json: {
    decision: {
      id: decision, tenant_id: tenant, scenario_id: scenario, scenario_iteration_id: "iteration-1",
      object_id: "payment-1", object_type: "transactions", outcome: "review", score: 50, triggered: true,
      created_at: "2026-11-26T13:42:18Z",
      request_body: { source: "api", duplicate: "metadata", fields: { duplicate: "field", amount: 0, initiated_at: longValue, enabled: false, extra: { nested: [1, 2] }, median: null } },
    },
    rule_executions: [],
  } }));
  await page.goto(`/detection/decisions/${decision}?tenant=${tenant}`);
  const table = page.getByRole("table", { name: "Evaluated request fields" });
  await expect(table.getByRole("columnheader", { name: "Field" })).toBeVisible();
  await expect(table.getByRole("columnheader", { name: "Value" })).toBeVisible();
  for (const [field, value] of [["amount", "0"], ["enabled", "false"], ["median", "null"], ["extra", '{"nested":[1,2]}'], ["initiated_at", longValue], ["fields.duplicate", "field"], ["request.duplicate", "metadata"], ["source", "api"]]) {
    const row = table.getByRole("row").filter({ has: page.getByRole("rowheader", { name: field, exact: true }) });
    await expect(row.getByRole("cell")).toHaveText(value);
  }
  await page.getByText("View raw request JSON").click();
  await expect(page.locator("pre").filter({ hasText: "\"duplicate\": \"metadata\"" })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(table).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
});

test("decision overview retains the linked case", async ({ page }) => {
  const caseId = "33333333-3333-4333-8333-333333333333";
  await page.route(`**/api/cases/tenants/${tenant}/decision-case-memberships`, route => route.fulfill({ json: {
    decisions: [{ decision_id: decision, state: "linked", case_id: caseId, case_name: "Payment investigation" }],
  } }));
  await page.goto(`/detection/decisions/${decision}?tenant=${tenant}`);
  await expect(page.getByRole("link", { name: "Payment investigation", exact: true })).toHaveAttribute("href", `/cases/${caseId}?tenant=${tenant}`);
});

test("full-page navigation preserves decision context and filters do not erase other outcomes", async ({ page }) => {
  await page.goto(`/detection/decisions/${decision}?tenant=${tenant}&fromTab=Decisions`);
  await page.getByRole("button", { name: "Other", exact: true }).click();
  await expect(page.getByRole("button", { name: /Many senders/ })).toHaveCount(0);
  await expect(page.getByRole("button", { name: /Risk signal.*Snoozed/ })).toBeVisible();
  await page.getByRole("button", { name: /Risk signal.*Not matched/ }).click();
  await page.getByRole("link", { name: "Open full page" }).click();
  await expect(page).toHaveURL(/view=rule/);
  await expect(page.getByRole("heading", { name: "Rule execution", exact: true })).toBeVisible();
  await page.getByRole("combobox", { name: "Rule execution" }).selectOption("rule-1");
  await expect(page).toHaveURL(/ruleExecution=rule-1/);
  await page.getByRole("link", { name: "Back to decision" }).click();
  await expect(page).toHaveURL(new RegExp(`tenant=${tenant}.*fromTab=Decisions`));
  await expect(page.getByRole("heading", { name: "Decision details" })).toBeVisible();
  await expect(page.getByRole("dialog", { name: "Rule execution" })).toHaveCount(0);
});

test("window inference preserves relative anchors, strict bounds, and unsupported filters", () => {
  const shifted = (duration: string, sign: string, value: string) => ({
    function: "TimeAdd", return_value: value,
    named_children: { timestampField: reference, duration: constant(duration), sign: constant(sign) },
  });
  const start = shifted("P30D", "-", "2026-10-27T13:42:18Z");
  const future = shifted("P14D", "+", "2026-12-10T13:42:18Z");
  const timeAggregate = (children: RuleEvaluationNode[]) => ({ function: "Aggregator", named_children: { filters: { function: "List", children } } });
  const historical = aggregateFilters(timeAggregate([filter("initiated_at", ">", start), filter("initiated_at", "<=", earlier)]));
  expect(historical.windows[0].summary).toBe("Between 30 days and 1 day before the evaluated record's initiated at");
  expect(historical.windows[0].boundary).toBe("Start excluded · End included");
  const spanning = aggregateFilters(timeAggregate([filter("initiated_at", ">=", earlier), filter("initiated_at", "<", future)]));
  expect(spanning.windows[0].summary).toBe("Between 1 day before and 14 days after the evaluated record's initiated at");
  const unbounded = aggregateFilters(timeAggregate([filter("initiated_at", "<", earlier)]));
  expect(unbounded.windows[0].summary).toBe("Earlier than 1 day before the evaluated record's initiated at");
  const strict = aggregateFilters(timeAggregate([filter("initiated_at", ">", earlier), filter("initiated_at", "<=", reference), filter("initiated_at", "<", reference)]));
  expect(strict.windows[0].boundary).toBe("Start excluded · End excluded");
  const ambiguous = aggregateFilters(timeAggregate([filter("initiated_at", "<", reference)]));
  expect(ambiguous.windows).toHaveLength(0);
  expect(ambiguous.filters).toHaveLength(1);
  const complex = aggregateFilters(timeAggregate([{ function: "or", children: [filter("state", "=", constant("failed"))] }]));
  expect(complex).toEqual({ windows: [], filters: [], unsupported: true });
});
