import { expect, test } from "@playwright/test";

test("incomplete search refinement copies settings without execution and creates a separate run", async ({ page, context }) => {
  const commands: { operation_id: string; name: string; limit: number }[] = [];
  const ids: string[] = [];
  page.on("request", req => { if (req.method() === "POST" && req.url().endsWith("/api/screening-search")) commands.push(req.postDataJSON()); });
  page.on("response", async response => { if (response.request().method() === "POST" && response.url().endsWith("/api/screening-search")) ids.push((await response.json()).id); });
  await page.goto("/screening-search");
  await page.getByLabel("Name", { exact: true }).fill("Partial");
  await page.getByText("Additional search fields", { exact: true }).click();
  await page.getByLabel("Birth date").fill("1980");
  await page.getByLabel("Nationality").fill("GB");
  await page.getByLabel("Dataset IDs").fill("us_ofac_sdn");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(page.getByText("No candidates returned in this incomplete response.")).toBeVisible();
  await page.getByLabel("Name", { exact: true }).fill("Unrelated draft");
  await page.getByRole("button", { name: "Refine as new search" }).click();
  await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Partial");
  await expect(page.getByLabel("Name", { exact: true })).toBeFocused();
  await expect(page.getByLabel("Birth date")).toHaveValue("1980");
  await expect(page.getByLabel("Nationality")).toHaveValue("GB");
  expect(commands).toHaveLength(1);
  await page.getByLabel("Name", { exact: true }).fill("Refined synthetic");
  await page.getByLabel("Result limit").selectOption("50");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(page.getByRole("heading", { name: "2 potential matches" })).toBeVisible();
  expect(commands).toHaveLength(2);
  expect(commands[1].operation_id).not.toBe(commands[0].operation_id);
  expect(ids[1]).not.toBe(ids[0]);
  const original = await context.request.get(`/api/screening-search/${ids[0]}`);
  expect(await original.json()).toMatchObject({ partial: true, query: { name: "Partial", limit: 10 }, matches: [] });
});

test("refinement requires explicit removal of unsupported stored settings", async ({ page }) => {
  await page.goto("/screening-search");
  await page.getByLabel("Name", { exact: true }).fill("Stored advanced query");
  await page.getByText("Additional search fields", { exact: true }).click();
  await page.getByLabel("Address").fill("Synthetic address");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(page.getByRole("heading", { name: "2 potential matches" })).toBeVisible();
  await page.route("**/api/screening-search/capabilities", route => route.fulfill({ json: { name: "OpenSanctions", configured: true, dataset_selection: false, entity_types: ["Person", "Organization"], threshold_min: 0, threshold_max: 100, limit_max: 50, fields: { Person: ["name"], Organization: ["name"] }, limitations: [] } }));
  await page.reload();
  await page.getByRole("button", { name: /Stored advanced query/ }).click();
  await page.getByRole("button", { name: "Refine as new search" }).click();
  await expect(page.getByRole("button", { name: "Search", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Clear unsupported settings" }).click();
  const sent = page.waitForRequest(req => req.method() === "POST" && req.url().endsWith("/api/screening-search"));
  await page.getByRole("button", { name: "Search", exact: true }).click();
  expect((await sent).postDataJSON()).not.toHaveProperty("address");
});

test.beforeEach(async ({ context, request }) => {
  await request.post("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
});

test("candidate investigation selects an authorized inbox and opens the case", async ({ page }) => {
  await page.goto("/screening-search");
  await page.getByLabel("Name", { exact: true }).fill("Synthetic investigation");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.getByRole("button", { name: "Open investigation", exact: true }).first().click();
  await expect(page.getByRole("button", { name: "Continue to investigation" })).toBeDisabled();
  await page.getByLabel("Investigation inbox").selectOption("22222222-2222-4222-8222-222222222222");
  const sent = page.waitForRequest(req => req.method() === "POST" && req.url().endsWith("/manual-screening-investigations"));
  await page.getByRole("button", { name: "Continue to investigation" }).click();
  expect((await sent).postDataJSON()).toMatchObject({ candidate_id: "m1", inbox_id: "22222222-2222-4222-8222-222222222222" });
  await expect(page).toHaveURL(/\/cases\/33333333-3333-4333-8333-333333333333$/);
});

test("investigation failure remains visible and retry reuses the same search", async ({ page }) => {
  await page.goto("/screening-search");
  await page.getByLabel("Name", { exact: true }).fill("Synthetic retry");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.getByRole("button", { name: "Open investigation", exact: true }).first().click();
  await page.getByLabel("Investigation inbox").selectOption("22222222-2222-4222-8222-222222222222");
  const inputs: unknown[] = [];
  await page.route("**/manual-screening-investigations", async route => { inputs.push(route.request().postDataJSON()); if (inputs.length === 1) await route.fulfill({ status: 503, json: { error: "Response lost. Retry the same request." } }); else await route.continue(); });
  await page.getByRole("button", { name: "Continue to investigation" }).click();
  await expect(page.getByText("Response lost. Retry the same request.")).toBeVisible();
  await page.getByRole("button", { name: "Continue to investigation" }).click();
  await expect(page).toHaveURL(/\/cases\/33333333-3333-4333-8333-333333333333$/);
  expect(inputs[0]).toEqual(inputs[1]);
});

test("generic capability hides unsupported fields without blocking history", async ({ page, request }) => {
  await request.post("http://127.0.0.1:3199/fixture/manual-history");
  await page.route("**/api/screening-search/capabilities", route => route.fulfill({ json: { id: "opensanctions", name: "OpenSanctions", configured: true, health: "unknown", dataset_selection: false, entity_types: ["Person", "Organization"], threshold_min: 0, threshold_max: 100, limit_max: 50, fields: { Person: ["name"], Organization: ["name"] }, limitations: [] } }));
  await page.goto("/screening-search");
  await expect(page.getByText("Additional search fields", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Additional identity fields and dataset selection are unavailable", { exact: false })).toBeVisible();
  await page.getByLabel("Name", { exact: true }).fill("Synthetic");
  const submission = page.waitForRequest(req => req.method() === "POST" && req.url().endsWith("/api/screening-search"));
  await page.getByRole("button", { name: "Search", exact: true }).click();
  const body = (await submission).postDataJSON();
  expect(body).not.toHaveProperty("birth_date"); expect(body).not.toHaveProperty("datasets");
  await expect(page.getByRole("heading", { name: "2 potential matches" })).toBeVisible();
});

test("unconfigured provider disables new searches but preserves stored history", async ({ page, request }) => {
  await request.post("http://127.0.0.1:3199/fixture/manual-history");
  await page.route("**/api/screening-search/capabilities", route => route.fulfill({ json: { name: "OpenSanctions", configured: false, dataset_selection: false, entity_types: ["Person", "Organization"], threshold_min: 0, threshold_max: 100, limit_max: 50, fields: {}, limitations: [] } }));
  await page.goto("/screening-search");
  await expect(page.getByRole("button", { name: "Search", exact: true })).toBeDisabled();
  await expect(page.getByText("Provider configuration required before searching")).toBeVisible();
  await expect(page.getByText("Historical 24", { exact: true })).toBeVisible();
});

test("capability load failure can be retried", async ({ page }) => {
  let failed = false;
  await page.route("**/api/screening-search/capabilities", async route => {
    if (!failed) { failed = true; await route.fulfill({ status: 503, json: { error: "Unavailable" } }); }
    else await route.continue();
  });
  await page.goto("/screening-search");
  await page.getByRole("button", { name: "Retry provider settings" }).click();
  await expect(page.getByRole("button", { name: "Search", exact: true })).toBeEnabled();
});

test("manual organization search uses owner API and renders candidates", async ({ page }) => {
  await page.goto("/screening-search");
  await expect(page.getByRole("heading", { name: "Screen a person or organization" })).toBeVisible();
  await page.getByLabel("Entity type").selectOption("Organization");
  await page.getByLabel("Name", { exact: true }).fill("Acme");
  await page.getByText("Additional search fields", { exact: true }).click();
  await page.getByLabel("Country", { exact: true }).fill("GH");
  await page.getByLabel("Registration number").fill("ORG-123");
  await page.getByLabel("Matching threshold").selectOption("80");
  await page.getByLabel("Result limit").selectOption("20");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(page.getByRole("heading", { name: "2 potential matches" })).toBeVisible();
  await expect(page.getByText("94% similarity")).toBeVisible();
  await page.getByText("Candidate details", { exact: true }).first().click();
  await expect(page.getByText("1980-01-01", { exact: true })).toBeVisible();
  await expect(page.getByText("Results for")).toContainText("threshold 80%");
  await expect(page.getByRole("region", { name: "Search results" }).getByText("ORG-123", { exact: true })).toBeVisible();
});

for (const name of ["Nobody", "Partial", "Failure", "Denied"]) {
  test(`manual search ${name} state`, async ({ page }) => {
    await page.goto("/screening-search");
    await page.getByLabel("Name", { exact: true }).fill(name);
    await page.getByRole("button", { name: "Search", exact: true }).click();
    if (name === "Nobody") await expect(page.getByText("No matches found for these search settings.")).toBeVisible();
    if (name === "Partial") {
      await expect(page.getByText("Incomplete results.", { exact: false })).toBeVisible();
      await expect(page.getByText("No candidates returned in this incomplete response.")).toBeVisible();
      await expect(page.getByText("No matches found for these search settings.")).toHaveCount(0);
      await page.getByRole("button", { name: "View all history" }).click();
      await page.getByLabel("History status").selectOption("no_hit");
      await expect(page.getByText("No searches match these filters.")).toBeVisible();
      await page.getByLabel("History status").selectOption("awaiting_review");
      await expect(page.getByRole("region", { name: "Search history" }).getByRole("button", { name: /Partial/ })).toBeVisible();
    }
    if (name === "Failure") await expect(page.getByRole("region", { name: "Search results" }).getByRole("alert")).toContainText("provider could not complete");
    if (name === "Denied") await expect(page.getByRole("region", { name: "Search results" }).getByRole("alert")).toContainText("access denied");
  });
}

test("lost create response retries identical operation and original input", async ({ page }) => {
  const submitted: object[] = [];
  page.on("request", req => { if (req.method() === "POST" && req.url().endsWith("/api/screening-search")) submitted.push(req.postDataJSON()); });
  await page.goto("/screening-search");
  await page.getByLabel("Name", { exact: true }).fill("Lost response");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(page.getByRole("region", { name: "Search results" }).getByRole("alert")).toContainText("Response lost");
  await page.getByLabel("Name", { exact: true }).fill("Changed form");
  await page.getByRole("button", { name: "Retry same search" }).click();
  await expect(page.getByRole("heading", { name: "2 potential matches" })).toBeVisible();
  expect(submitted).toHaveLength(2); expect(submitted[0]).toEqual(submitted[1]);
});

test("invalid empty input does not submit and proxy rejects foreign origins", async ({ page, request }) => {
  let calls = 0; page.on("request", req => { if (req.method() === "POST" && req.url().endsWith("/api/screening-search")) calls++; });
  await page.goto("/screening-search");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  expect(calls).toBe(0);
  const response = await request.post("/api/screening-search", { headers: { origin: "https://foreign.invalid" }, data: {} });
  expect(response.status()).toBe(403);
});

test("person identifying fields are sent and stored history reopens without a new search", async ({ page }) => {
  const submitted: Record<string, unknown>[] = [];
  page.on("request", req => { if (req.method() === "POST" && req.url().endsWith("/api/screening-search")) submitted.push(req.postDataJSON()); });
  await page.goto("/screening-search");
  await page.getByLabel("Name", { exact: true }).fill("Historical person");
  await page.getByText("Additional search fields", { exact: true }).click();
  await page.getByLabel("Birth date").fill("1980"); await page.getByLabel("Nationality").fill("GB");
  await page.getByLabel("Address", { exact: true }).fill("London"); await page.getByLabel("Dataset IDs").fill("us_ofac_sdn");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(page.getByRole("heading", { name: "2 potential matches" })).toBeVisible();
  expect(submitted[0]).toMatchObject({ birth_date: "1980", nationality: "GB", address: "London", datasets: ["us_ofac_sdn"] });
  await page.reload();
  await page.getByRole("region", { name: "Search history" }).getByRole("button", { name: /Historical person/ }).click();
  await expect(page.getByText("Stored search results", { exact: false })).toBeVisible();
  await expect(page.getByRole("heading", { name: "2 potential matches" })).toBeVisible();
  expect(submitted).toHaveLength(1);
});

test("history keyset navigation, filters and retry", async ({ page, request }) => {
  await request.post("http://127.0.0.1:3199/fixture/manual-history");
  await request.post("http://127.0.0.1:3199/fixture/manual-history-error");
  await page.goto("/screening-search");
  const history = page.getByRole("region", { name: "Search history" });
  await expect(history.getByRole("alert")).toContainText("History unavailable");
  await history.getByRole("button", { name: "Retry history" }).click();
  await expect(history.getByRole("button", { name: /Historical 24/ })).toBeVisible();
  await history.getByRole("button", { name: "View all history" }).click();
  await history.getByRole("button", { name: "Next searches" }).click();
  await expect(history.getByText("Page 2")).toBeVisible();
  await expect(history.getByRole("button", { name: /Historical 04/ })).toBeVisible();
  await history.getByRole("button", { name: "Previous searches" }).click();
  await expect(history.getByRole("button", { name: /Historical 24/ })).toBeVisible();
  await history.getByLabel("History entity type").selectOption("Organization");
  await expect(history.getByText("Page 1")).toBeVisible();
  await expect(history.getByRole("button", { name: "Next searches" })).toBeDisabled();
  await history.getByLabel("From (UTC)").fill("2026-02-01");
  await expect(history.getByText("No searches match these filters.")).toBeVisible();
  await history.getByRole("button", { name: "Show recent" }).click();
  await expect(history.getByRole("button", { name: /Historical 24/ })).toBeVisible();
});
