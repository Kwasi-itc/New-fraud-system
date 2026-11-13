import { expect, test } from "@playwright/test";

const tenant = "11111111-1111-4111-8111-111111111111";
const caseID = "33333333-3333-4333-8333-333333333333";
const source = "66666666-6666-4666-8666-666666666666";
const preview = "99999999-9999-4999-8999-999999999999";

test.beforeEach(async ({ context, request }) => {
  await request.post("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  await request.post("http://127.0.0.1:3199/fixture/screening?match=1&manual=1");
});

test("decision approval is disabled with the owner's screening explanation", async ({ page, request }) => {
  await request.post("http://127.0.0.1:3199/reset");
  await page.route("**/api/case-review/**/decisions?*", route => route.fulfill({ json: {
    status: "pending", version: "v1", eligible: true, actions: ["decline"],
    disabled_actions: { approve: "Screening matches must be resolved before approving." },
  } }));
  await page.goto(`/cases/${caseID}`);
  await page.getByRole("button", { name: "Review decision", exact: true }).first().click();
  await expect(page.locator('option[value="approve"]')).toBeDisabled();
  await expect(page.getByRole("status").filter({ hasText: "Screening matches must be resolved before approving." })).toBeVisible();
  await page.getByLabel("Review action", { exact: true }).selectOption("decline");
});

test("refinement previews persisted results, then retries an identical audited apply", async ({ page }) => {
  const commands: Record<string, unknown>[] = [];
  await page.route("**/api/case-refinement/**", async route => {
    if (route.request().method() === "GET") return route.fulfill({ json: { source_id: source, version: "current-version", can_refine: true, provider: "opensanctions", queries: [{ name: "Original beneficiary", type: "Organization" }] } });
    commands.push(route.request().postDataJSON());
    return route.fulfill(commands.length === 1 ? { status: 503, json: { error: "Response unavailable. Retry the same operation." } } : { json: { screening_id: preview } });
  });
  await page.route("**/api/screening-search", route => route.fulfill({ status: 202, json: { id: preview, status: "pending" } }));
  await page.route(`**/api/screening-search/${preview}`, route => route.fulfill({ json: { id: preview, status: "no_hit", partial: false, matches: [] } }));
  await page.goto(`/cases/${caseID}?tab=screenings`);
  await page.getByRole("button", { name: "Inspect evidence", exact: true }).click();
  await page.getByRole("button", { name: "Refine search", exact: true }).click();
  await expect(page.getByLabel("Refined entity type")).toHaveValue("Organization");
  await page.getByLabel("Refined name", { exact: true }).fill("Complete beneficiary name");
  await page.getByRole("button", { name: "Preview results", exact: true }).click();
  await expect(page.getByRole("heading", { name: "No possible matches found" })).toBeVisible();
  await page.getByLabel("Refinement findings").fill("Validated registered identity with the customer");
  await page.getByRole("button", { name: "Apply changes", exact: true }).click();
  await expect(page.getByRole("region", { name: "Refine screening search" }).getByRole("alert")).toContainText("Retry the same operation");
  await page.getByRole("button", { name: "Apply changes", exact: true }).click();
  await expect(page.getByText("Refinement applied. Original evidence retained; review the replacement results in this case.")).toBeVisible();
  expect(commands).toHaveLength(2);
  expect(commands[0]).toEqual(commands[1]);
  expect(commands[0]).toMatchObject({ replacement_id: preview, expected_version: "current-version" });
});

test("incomplete previews cannot be applied", async ({ page }) => {
  await page.route("**/api/case-refinement/**", route => route.fulfill({ json: { version: "v1", can_refine: true, queries: [{ name: "Broad name", type: "Person" }] } }));
  await page.route("**/api/screening-search", route => route.fulfill({ status: 202, json: { id: preview, status: "pending" } }));
  await page.route(`**/api/screening-search/${preview}`, route => route.fulfill({ json: { id: preview, status: "awaiting_review", partial: true, matches: [] } }));
  await page.goto(`/cases/${caseID}?tab=screenings`);
  await page.getByRole("button", { name: "Inspect evidence", exact: true }).click();
  await page.getByRole("button", { name: "Refine search", exact: true }).click();
  await page.getByRole("button", { name: "Preview results", exact: true }).click();
  await expect(page.getByText("Results are incomplete. Narrow the search and preview again before applying.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Apply changes", exact: true })).toHaveCount(0);
});
