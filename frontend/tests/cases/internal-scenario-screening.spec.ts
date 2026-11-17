import { expect, test } from "@playwright/test";

const scenario = "88888888-8888-4888-8888-888888888888";
test("internal provider, profile and scoped dataset survive authoring BFF save and reopen", async ({ context, page, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await request.get("http://127.0.0.1:3199/fixture/scenario-internal-provider");
  await context.addCookies([{ name: "case_test_session", value: "fixture-workflow", domain: "127.0.0.1", path: "/" }]);
  await page.goto(`/detection/${scenario}/screening`);
  await page.getByRole("button", { name: "Add screening" }).click();
  const form = page.getByRole("form", { name: "Screening configuration" });
  await form.getByLabel("Rule name", { exact: true }).fill("Internal beneficiary check");
  await form.getByLabel("Screening provider").selectOption("internal-watchlists");
  await expect(form.getByRole("button", { name: "Save screening" })).toBeDisabled();
  await form.getByLabel("Matching method").selectOption("name-fuzzy-v1");
  await form.getByRole("checkbox", { name: /Private demo/ }).check();
  await form.getByLabel("Name to check", { exact: true }).selectOption("beneficiary_name");
  await form.getByLabel("Review inbox").selectOption("22222222-2222-4222-8222-222222222222");
  await expect(form.getByRole("option", { name: "During the decision", exact: true })).toHaveAttribute("disabled", "");
  await form.getByRole("button", { name: "Save screening" }).click();
  await expect(page.getByRole("status")).toContainText("saved");
  const saved = await (await page.request.get(`/api/scenario-screening/scenarios/${scenario}`)).json();
  expect(saved.rules[0]).toMatchObject({ provider: "internal-watchlists", profile: "name-fuzzy-v1", threshold: 85, dataset_selection: [{ scope: "tenant", id: "private-demo" }] });
  await page.reload();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(form.getByLabel("Screening provider")).toHaveValue("internal-watchlists");
  await expect(form.getByLabel("Matching method")).toHaveValue("name-fuzzy-v1");
  await expect(form.getByRole("checkbox", { name: /Private demo/ })).toBeChecked();
  await form.getByLabel("Matching method").selectOption("exact-name-v1");
  await form.getByText("More identity details", { exact: true }).click();
  await expect(form.getByLabel("Date of birth")).toHaveCount(0);
});

test("case source enrichment BFF rejects foreign origin, signed-out and foreign tenant", async ({ context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  const suffix = "/33333333-3333-4333-8333-333333333333/match-controlled";
  const path = "/api/case-screening-enrichment/11111111-1111-4111-8111-111111111111" + suffix;
  expect((await context.request.post(path, { headers: { origin: "https://foreign.invalid" }, data: {} })).status()).toBe(403);
  expect((await context.request.post(path, { headers: { origin: "http://127.0.0.1:3100" }, data: {} })).status()).toBe(401);
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  const foreign = "/api/case-screening-enrichment/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" + suffix;
  expect((await context.request.post(foreign, { headers: { origin: "http://127.0.0.1:3100" }, data: {} })).status()).toBe(403);
});
