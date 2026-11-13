import { expect, test, type Page } from "@playwright/test";

const tenant = "11111111-1111-4111-8111-111111111111";
const scenario = "88888888-8888-4888-8888-888888888888";
const iteration = "99999999-9999-4999-8999-999999999999";
const scoreID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const personID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const companyID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc";
const editorURL = `/detection/${scenario}/edit?tab=Rules`;

async function authoring(page: Page, empty = false) {
  await page.route("**/api/data-model/**", route => route.fulfill({ json: { data_model: { tables: {} } } }));
  await page.route("**/api/decisions/**", route => {
    const path = new URL(route.request().url()).pathname;
    let json: unknown = {};
    if (path.endsWith(`/scenarios/${scenario}`)) json = { scenario: { id: scenario, tenant_id: tenant, name: "Payments", trigger_object_type: "transactions", live_iteration_id: iteration } };
    else if (path.endsWith("/iterations")) json = { iterations: [{ id: iteration, scenario_id: scenario, version: 1, status: "committed", trigger_formula: { constant: true } }] };
    else if (path.endsWith("/rules")) json = { rules: empty ? [] : [{ id: scoreID, iteration_id: iteration, name: "High amount", description: "amount >= 10000", rule_group: "Value checks", score_modifier: 40, formula: { constant: true } }] };
    else if (path.endsWith("/validate")) json = { validation: { valid: true, errors: [], trigger_errors: [], rule_results: [] } };
    else if (path.endsWith("/rule-functions")) json = { rule_functions: [] };
    else if (path.endsWith("/editor-identifiers")) json = { payload_accessors: [], database_accessors: [], derived_accessors: [] };
    else if (path.endsWith("/custom-lists")) json = { custom_lists: [] };
    return route.fulfill({ json });
  });
}

test.beforeEach(async ({ context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-workflow", domain: "127.0.0.1", path: "/" }]);
  for (const [id, name, entity_type, name_field, enabled, forced_outcome, threshold] of [
    [personID, "Beneficiary person screening", "Person", "beneficiary_name", true, "block_and_review", 85],
    [companyID, "Company follow-up screening", "Organization", "company_name", false, undefined, 0],
  ] as const) {
    const response = await context.request.put(`/api/scenario-screening/scenarios/${scenario}/configs/${id}`, {
      headers: { Origin: "http://127.0.0.1:3100" },
      data: { id, name, entity_type, name_field, enabled, forced_outcome, threshold, outcomes: ["review"], inbox_id: "22222222-2222-4222-8222-222222222222", updated_at: null },
    });
    expect(response.status()).toBe(200);
  }
});

test("lists score and screening rules together with their real behavior and enabled state", async ({ page }) => {
  await authoring(page);
  await page.goto(editorURL);
  const table = page.getByRole("table", { name: "Scenario rules" });
  await expect(table.getByRole("row")).toHaveCount(4);
  const person = table.getByRole("row").filter({ hasText: "Beneficiary person screening" });
  await expect(person).toContainText("Screening");
  await expect(person).toContainText("Enabled");
  await expect(person).toContainText("85% similarity");
  await expect(person).toContainText("No score change");
  await expect(person).toContainText("At least Block and review");
  const company = table.getByRole("row").filter({ hasText: "Company follow-up screening" });
  await expect(company).toContainText("Disabled");
  await expect(company).toContainText("0% similarity");
  await expect(company).toContainText("After decision");
  const score = table.getByRole("row").filter({ hasText: "High amount" });
  await expect(score).toContainText("+40");
  await expect(score).toContainText("Score based");
  await expect(page.getByText("Screening rules use live scenario settings.", { exact: false })).toBeVisible();
});

test("search and type filters include screening rules, including a screening-only scenario", async ({ page }) => {
  await authoring(page, true);
  await page.goto(editorURL);
  const table = page.getByRole("table", { name: "Scenario rules" });
  await expect(table.getByRole("row")).toHaveCount(3);
  await page.getByPlaceholder("Search", { exact: true }).fill("company_name");
  await expect(table.getByRole("row")).toHaveCount(2);
  await expect(table).toContainText("Company follow-up screening");
  await page.getByPlaceholder("Search", { exact: true }).fill("");
  await page.getByLabel("Rule type").selectOption("rule");
  await expect(page.getByText("No rules match the current filters.")).toBeVisible();
  await page.getByLabel("Rule type").selectOption("screening");
  await expect(table.getByRole("row")).toHaveCount(3);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByLabel("Rule type")).toBeVisible();
  const container = await table.locator("..").boundingBox();
  expect(container!.width).toBeLessThanOrEqual(390);
});

test("opens the selected configuration directly and refreshes its row after saving", async ({ page }) => {
  await authoring(page);
  await page.goto(editorURL);
  const row = page.getByRole("table", { name: "Scenario rules" }).getByRole("row").filter({ hasText: "Company follow-up screening" });
  await row.getByRole("link", { name: "Edit", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`screening\\?configId=${companyID}`));
  const form = page.getByRole("form", { name: "Screening configuration" });
  await expect(form.getByLabel("Rule name", { exact: true })).toHaveValue("Company follow-up screening");
  await expect(form.getByLabel("Screen for")).toHaveValue("Organization");
  await form.getByLabel("Rule name", { exact: true }).fill("Renamed company screening");
  await form.getByRole("button", { name: "Save screening" }).click();
  await expect(form).toBeHidden();
  await page.getByRole("link", { name: "Back to scenario", exact: false }).click();
  const table = page.getByRole("table", { name: "Scenario rules" });
  await expect(table).toContainText("Renamed company screening");
  await expect(table).not.toContainText("Company follow-up screening");
});

test("a read-only member can view the selected screening without writable controls", async ({ context, page }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-platform", domain: "127.0.0.1", path: "/" }]);
  await authoring(page);
  await page.goto(editorURL);
  const row = page.getByRole("table", { name: "Scenario rules" }).getByRole("row").filter({ hasText: "Beneficiary person screening" });
  await expect(row.getByRole("link", { name: "Edit", exact: true })).toHaveCount(0);
  await row.getByRole("link", { name: "View", exact: true }).click();
  const form = page.getByRole("form", { name: "Screening configuration" });
  await expect(form.getByLabel("Rule name", { exact: true })).toBeDisabled();
  await expect(form.getByRole("button", { name: "Save screening" })).toBeDisabled();
});

test("a screening failure is visible without hiding score rules and can recover", async ({ page }) => {
  await authoring(page);
  let failed = true;
  await page.route("**/api/scenario-screening/scenarios/*", route => failed ? route.fulfill({ status: 503, json: { error: "private-owner-error" } }) : route.continue());
  await page.goto(editorURL);
  const table = page.getByRole("table", { name: "Scenario rules" });
  await expect(table).toContainText("High amount");
  await expect(page.getByRole("alert").filter({ hasText: "Screening rules could not be loaded" })).toBeVisible();
  await expect(page.locator("body")).not.toContainText("private-owner-error");
  failed = false;
  await page.getByRole("button", { name: "Retry screening rules" }).click();
  await expect(table).toContainText("Beneficiary person screening");
  await expect(page.getByRole("button", { name: "Retry screening rules" })).toHaveCount(0);
});
