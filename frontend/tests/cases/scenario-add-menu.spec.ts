import { expect, test, type Page } from "@playwright/test";

const scenario = "88888888-8888-4888-8888-888888888888";
const tenant = "11111111-1111-4111-8111-111111111111";
const iteration = "99999999-9999-4999-8999-999999999999";
const rule = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";

// Decision authoring responses are isolated here; screening saves exercise its real BFF.
async function editor(page: Page, status = "draft") {
  const created: { url: string; body: Record<string, unknown> }[] = [];
  await page.route("**/api/data-model/**", route => route.fulfill({ json: { data_model: { tables: {} } } }));
  await page.route("**/api/decisions/**", async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    let json: unknown;
    if (path.endsWith("/rule-functions")) json = { rule_functions: [] };
    else if (path.endsWith("/editor-identifiers")) json = { payload_accessors: [], database_accessors: [], derived_accessors: [] };
    else if (path.endsWith("/custom-lists")) json = { custom_lists: [] };
    else if (path.endsWith(`/scenarios/${scenario}`)) json = { scenario: { id: scenario, tenant_id: tenant, name: "Payments", description: "Synthetic scenario", trigger_object_type: "transactions", live_iteration_id: null } };
    else if (path.endsWith("/iterations")) json = { iterations: [{ id: iteration, scenario_id: scenario, version: 1, status, trigger_formula: { constant: true } }] };
    else if (path.endsWith("/validate")) json = { validation: { valid: true, errors: [], trigger_errors: [], rule_errors: [] } };
    else if (path.endsWith("/rules") && request.method() === "POST") {
      created.push({ url: request.url(), body: request.postDataJSON() });
      json = { rule: { ...request.postDataJSON(), id: rule, iteration_id: iteration } };
    } else if (path.endsWith("/rules")) json = { rules: created.map(({ body }) => ({ ...body, id: rule, iteration_id: iteration })) };
    else json = {};
    await route.fulfill({ json });
  });
  await page.goto(`/detection/${scenario}/edit?tab=Rules&iterationId=${iteration}`);
  await expect(page.getByRole("button", { name: "Add", exact: true })).toBeVisible();
  await expect(page.getByText("This iteration is valid against the current data model.")).toBeVisible();
  return created;
}

test.beforeEach(async ({ context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-workflow", domain: "127.0.0.1", path: "/" }]);
});

test("Add opens two choices without creating; keyboard and outside dismissal work", async ({ page }) => {
  const created = await editor(page);
  const add = page.getByRole("button", { name: "Add", exact: true });
  await add.focus();
  await page.keyboard.press("ArrowDown");
  const menu = page.getByRole("menu", { name: "Add to scenario" });
  await expect(menu.getByRole("menuitem")).toHaveCount(2);
  await page.screenshot({ path: test.info().outputPath("add-menu-desktop.png") });
  await expect(menu.getByRole("menuitem", { name: "Add a rule", exact: true })).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(menu.getByRole("menuitem", { name: "Add a screening", exact: true })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(menu).toBeHidden();
  await expect(add).toBeFocused();
  await add.click();
  await page.getByRole("button", { name: "Filters", exact: true }).click();
  await expect(menu).toBeHidden();
  await add.click();
  await expect(page.getByRole("button", { name: "Rule group", exact: true })).toBeHidden();
  await page.keyboard.press("Tab");
  await expect(menu).toBeHidden();
  expect(created).toHaveLength(0);
});

test("Add a rule creates once in the selected draft and opens the rule editor", async ({ page }) => {
  const created = await editor(page);
  await page.getByRole("button", { name: "Add", exact: true }).click();
  await page.getByRole("menuitem", { name: "Add a rule", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/edit/rules/${rule}\\?iterationId=${iteration}`));
  expect(created).toHaveLength(1);
  expect(new URL(created[0].url).pathname).toBe(`/api/decisions/tenants/${tenant}/scenarios/${scenario}/iterations/${iteration}/rules`);
  expect(created[0].body).toMatchObject({ name: "New rule", formula: { constant: true }, display_order: 1, score_modifier: 0 });
});

test("Add a screening opens an unsaved form and persists through the screening gateway", async ({ page }) => {
  const created = await editor(page);
  let saves = 0;
  page.on("request", request => { if (request.url().includes("/api/scenario-screening/") && request.method() === "PUT") saves++; });
  await page.getByRole("button", { name: "Add", exact: true }).click();
  await page.getByRole("menuitem", { name: "Add a screening", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/detection/${scenario}/screening\\?create=1`));
  const form = page.getByRole("form", { name: "Screening configuration" });
  await expect(form).toBeVisible();
  await expect(form.getByLabel("Enable for future decisions")).not.toBeChecked();
  expect(saves).toBe(0);
  expect(created).toHaveLength(0);
  await form.getByLabel("Name", { exact: true }).fill("Beneficiary screening");
  await form.getByLabel("Name field", { exact: true }).selectOption("beneficiary_name");
  await form.getByLabel("Review inbox").selectOption("22222222-2222-4222-8222-222222222222");
  await form.getByRole("button", { name: "Save configuration" }).click();
  await expect(page.getByRole("status")).toContainText("saved");
  await expect(form).toBeHidden();
  expect(saves).toBe(1);
  // A normal visit lists the saved configuration without opening another form.
  await page.goto(`/detection/${scenario}/screening`);
  await expect(page.getByRole("heading", { name: "Beneficiary screening", exact: true })).toBeVisible();
  await expect(form).toBeHidden();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(form.getByLabel("Name field", { exact: true })).toHaveValue("beneficiary_name");
});

test("committed iterations keep screening available and the menu fits a phone viewport", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const created = await editor(page, "committed");
  await page.getByRole("button", { name: "Add", exact: true }).click();
  const menu = page.getByRole("menu", { name: "Add to scenario" });
  await expect(menu.getByRole("menuitem", { name: "Add a rule", exact: true })).toBeDisabled();
  await expect(menu.getByText("Select or create a draft iteration to add a rule")).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: "Add a screening", exact: true })).toBeFocused();
  const bounds = await menu.boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390);
  await page.screenshot({ path: test.info().outputPath("add-menu-mobile.png") });
  await page.getByRole("menuitem", { name: "Add a screening", exact: true }).click();
  const form = page.getByRole("form", { name: "Screening configuration" });
  await expect(form).toBeVisible();
  await form.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(form).toBeHidden();
  await expect(page.getByRole("button", { name: "Create screening configuration" })).toBeEnabled();
  expect(created).toHaveLength(0);
});

test("a read-only account cannot open a writable form using the create URL", async ({ context, page }) => {
  await page.goto(`/detection/${scenario}/screening?create=1`);
  const form = page.getByRole("form", { name: "Screening configuration" });
  await form.getByLabel("Name", { exact: true }).fill("Existing configuration");
  await form.getByLabel("Name field", { exact: true }).selectOption("beneficiary_name");
  await form.getByLabel("Review inbox").selectOption("22222222-2222-4222-8222-222222222222");
  await form.getByRole("button", { name: "Save configuration" }).click();
  await expect(page.getByRole("status")).toContainText("saved");
  await context.addCookies([{ name: "case_test_session", value: "fixture-platform", domain: "127.0.0.1", path: "/" }]);
  await page.goto(`/detection/${scenario}/screening?create=1`);
  await expect(page.getByText(/Read-only\. A platform administrator/)).toBeVisible();
  await expect(page.getByRole("form", { name: "Screening configuration" })).toBeHidden();
  await expect(page.getByRole("button", { name: "Create screening configuration" })).toBeDisabled();
  await page.getByRole("button", { name: "View", exact: true }).click();
  await expect(form.getByLabel("Name", { exact: true })).toHaveValue("Existing configuration");
  await expect(form.getByLabel("Name", { exact: true })).toBeDisabled();
  await expect(form.getByRole("button", { name: "Save configuration" })).toBeDisabled();
});
