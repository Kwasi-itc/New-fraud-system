import { expect, test } from "@playwright/test";

const scenario = "88888888-8888-4888-8888-888888888888";
test.beforeEach(async ({ context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-workflow", domain: "127.0.0.1", path: "/" }]);
});

test("configure synchronous screening with ordered name parts and local matching overrides", async ({ page, request }) => {
  await request.get("http://127.0.0.1:3199/fixture/scenario-local-provider");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/detection/${scenario}/screening?create=1`);
  const form = page.getByRole("form", { name: "Screening configuration" });
  await expect(page.getByText(/synthetic names only; no sanctions coverage/)).toBeVisible();
  await form.getByLabel("Name", { exact: true }).fill("Beneficiary match blocks for review");
  await form.getByLabel("Screening timing").selectOption("during");
  await form.getByLabel("Outcome when a possible match is found").selectOption("block_and_review");
  await expect(form.getByText("Screen after these decision outcomes")).toHaveCount(0);
  await expect(form.getByText(/Provider failure stops evaluation/)).toBeVisible();
  await form.getByLabel("Name field", { exact: true }).selectOption("first_name");
  await form.getByRole("button", { name: "Add name field", exact: true }).click();
  await expect(form.getByRole("button", { name: "Save configuration" })).toBeDisabled();
  await form.getByLabel("Additional name field 1").selectOption("last_name");
  await expect(form.getByText(/80% similarity, 50 results/)).toBeVisible();
  await expect(form.getByLabel("Dataset IDs (comma separated)")).toHaveCount(0);
  await form.getByLabel("Match threshold (0-100)").fill("85");
  await form.getByLabel("Result limit (1-50)").fill("10");
  await form.getByLabel("Review inbox").selectOption("22222222-2222-4222-8222-222222222222");
  await form.getByRole("button", { name: "Save configuration" }).click();
  await expect(page.getByRole("status")).toContainText("saved");
  await page.goto(`/detection/${scenario}/screening`);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(form.getByLabel("Screening timing")).toHaveValue("during");
  await expect(form.getByLabel("Outcome when a possible match is found")).toHaveValue("block_and_review");
  await expect(form.getByLabel("Additional name field 1")).toHaveValue("last_name");
  await expect(form.getByLabel("Match threshold (0-100)")).toHaveValue("85");
  await expect(form.getByLabel("Result limit (1-50)")).toHaveValue("10");
  await form.getByLabel("Name field", { exact: true }).selectOption("last_name");
  await expect(form.getByRole("alert").filter({ hasText: "different available field" })).toBeVisible();
  await expect(form.getByRole("button", { name: "Save configuration" })).toBeDisabled();
  await form.getByRole("button", { name: "Remove name field 1" }).click();
  await form.getByLabel("Screening timing").selectOption("after");
  await expect(form.getByText("Screen after these decision outcomes")).toBeVisible();
  await expect(form.getByRole("button", { name: "Save configuration" })).toBeEnabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
});
