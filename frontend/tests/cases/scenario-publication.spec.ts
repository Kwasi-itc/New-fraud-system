import { expect, test, type Page } from "@playwright/test";

const tenant = "11111111-1111-4111-8111-111111111111";
const scenario = "88888888-8888-4888-8888-888888888888";
const iteration = "99999999-9999-4999-8999-999999999999";

function preparation(required: boolean, started = false) {
  return {
    scenario_id: scenario,
    iteration_id: iteration,
    preparation_required: required,
    preparation_started: started,
    preparation_finished: !required,
    pending_items: required ? 1 : 0,
  };
}

async function setup(page: Page) {
  await page.route("**/api/data-model/**", route => route.fulfill({ json: { data_model: { tables: {} } } }));
  await page.route("**/api/decisions/**", route => {
    const path = new URL(route.request().url()).pathname;
    let json: unknown = {};
    if (path.endsWith(`/scenarios/${scenario}`)) {
      json = { scenario: { id: scenario, tenant_id: tenant, name: "Payments", trigger_object_type: "transactions", live_iteration_id: null } };
    } else if (path.endsWith("/iterations")) {
      json = { iterations: [{ id: iteration, scenario_id: scenario, version: 1, status: "committed", trigger_formula: { constant: true } }] };
    } else if (path.endsWith("/rules")) json = { rules: [] };
    else if (path.endsWith("/rule-functions")) json = { rule_functions: [] };
    else if (path.endsWith("/editor-identifiers")) json = { payload_accessors: [], database_accessors: [], derived_accessors: [] };
    else if (path.endsWith("/custom-lists")) json = { custom_lists: [] };
    return route.fulfill({ json });
  });
}

test.beforeEach(async ({ context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-workflow", domain: "127.0.0.1", path: "/" }]);
});

test("prepares indexes before allowing the user to publish", async ({ page }) => {
  await setup(page);
  let started = false;
  let ready = false;
  let runningChecks = 0;
  let publications = 0;
  await page.route(`**/api/decisions/tenants/${tenant}/scenarios/${scenario}/publications**`, route => {
    const request = route.request();
    if (new URL(request.url()).pathname.endsWith("/preparation")) {
      if (request.method() === "POST") {
        expect(request.postDataJSON()).toEqual({ iteration_id: iteration });
        started = true;
      } else {
        expect(new URL(request.url()).searchParams.get("iteration_id")).toBe(iteration);
        if (started && !ready) runningChecks++;
      }
      return route.fulfill({ json: { preparation: preparation(!ready, started && !ready) } });
    }
    publications++;
    expect(request.postDataJSON()).toEqual({ action: "publish", iteration_id: iteration });
    return route.fulfill({ json: { publications: [] } });
  });
  await page.goto(`/detection/${scenario}/edit`);
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(page.getByText(/Index preparation is needed/)).toBeVisible();
  const publish = page.getByRole("button", { name: "Publish", exact: true }).last();
  await expect(publish).toBeDisabled();
  await page.getByRole("button", { name: "Start or retry preparation" }).click();
  await expect(page.getByText(/Preparing indexes/)).toBeVisible();
  await expect.poll(() => runningChecks).toBeGreaterThanOrEqual(2);
  expect(publications).toBe(0);
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  ready = true;
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(page.getByText("Index preparation is complete.")).toBeVisible();
  await expect(publish).toBeEnabled();
  await publish.click();
  await expect.poll(() => publications).toBe(1);
});

test("does not publish when preparation status cannot be checked", async ({ page }) => {
  await setup(page);
  let unavailable = true;
  let publications = 0;
  await page.route(`**/api/decisions/tenants/${tenant}/scenarios/${scenario}/publications**`, route => {
    if (new URL(route.request().url()).pathname.endsWith("/preparation")) {
      return unavailable
        ? route.fulfill({ status: 503, json: { error: "unavailable" } })
        : route.fulfill({ json: { preparation: preparation(false) } });
    }
    publications++;
    return route.fulfill({ json: { publications: [] } });
  });
  await page.goto(`/detection/${scenario}/edit`);
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(page.getByText("Could not check index preparation. Retry before publishing.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Publish", exact: true }).last()).toBeDisabled();
  expect(publications).toBe(0);
  unavailable = false;
  await page.getByRole("button", { name: "Retry status check" }).click();
  await expect(page.getByText("Index preparation is complete.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Publish", exact: true }).last()).toBeEnabled();
});

test("allows publication without starting an unnecessary job", async ({ page }) => {
  await setup(page);
  let preparationPosts = 0;
  let publications = 0;
  await page.route(`**/api/decisions/tenants/${tenant}/scenarios/${scenario}/publications**`, route => {
    if (new URL(route.request().url()).pathname.endsWith("/preparation")) {
      if (route.request().method() === "POST") preparationPosts++;
      return route.fulfill({ json: { preparation: preparation(false) } });
    }
    publications++;
    return route.fulfill({ json: { publications: [] } });
  });
  await page.goto(`/detection/${scenario}/edit`);
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(page.getByText("Index preparation is complete.")).toBeVisible();
  await page.getByRole("button", { name: "Publish", exact: true }).last().click();
  await expect.poll(() => publications).toBe(1);
  expect(preparationPosts).toBe(0);
});

test("blocks publication when preparation fails and allows retry", async ({ page }) => {
  await setup(page);
  let fail = true;
  let ready = false;
  let attempts = 0;
  let publications = 0;
  await page.route(`**/api/decisions/tenants/${tenant}/scenarios/${scenario}/publications**`, route => {
    if (new URL(route.request().url()).pathname.endsWith("/preparation")) {
      if (route.request().method() === "POST") {
        attempts++;
        if (fail) return route.fulfill({ status: 503, json: { error: "preparation unavailable" } });
        ready = true;
      }
      return route.fulfill({ json: { preparation: preparation(!ready) } });
    }
    publications++;
    return route.fulfill({ json: { publications: [] } });
  });
  await page.goto(`/detection/${scenario}/edit`);
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await page.getByRole("button", { name: "Start or retry preparation" }).click();
  await expect(page.getByText("Failed to start index preparation")).toBeVisible();
  await expect(page.getByRole("button", { name: "Publish", exact: true }).last()).toBeDisabled();
  expect(publications).toBe(0);
  fail = false;
  await page.getByRole("button", { name: "Start or retry preparation" }).click();
  await expect(page.getByText("Index preparation is complete.")).toBeVisible();
  expect(attempts).toBe(2);
  expect(publications).toBe(0);
  await page.getByRole("button", { name: "Publish", exact: true }).last().click();
  await expect.poll(() => publications).toBe(1);
});
