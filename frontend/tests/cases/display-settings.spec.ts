import { expect, test } from "@playwright/test";
const first = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const tenant = "11111111-1111-4111-8111-111111111111";
test.beforeEach(async ({ context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await request.get("http://127.0.0.1:3199/fixture/display");
  await context.addCookies([{ name: "case_test_session", value: "fixture-admin", domain: "127.0.0.1", path: "/" }]);
});

test("configure captions, linked labels and column order, then view them in Decisions", async ({ page }) => {
  await page.goto("/your-data");
  await page.getByRole("button", { name: "Display settings for payment" }).click();
  const modal = page.getByRole("dialog", { name: "Display settings" });
  await modal.getByLabel("Record caption").selectOption("reference");
  await modal.getByLabel("Show customer_id", { exact: true }).check();
  await modal.getByLabel("Show amount", { exact: true }).check();
  await modal.getByLabel("Label for customer_id").fill("Customer");
  await modal.getByLabel("Label for amount").fill("Amount");
  await modal.getByLabel("Display value for customer_id").selectOption({ label: "customer.full_name" });
  await modal.getByLabel("Move amount up", { exact: true }).click();
  await page.screenshot({ path: "test-results/display-settings.png" });
  await modal.getByRole("button", { name: "Save display settings" }).click();
  await expect(modal).toHaveCount(0);
  await page.getByRole("button", { name: "Display settings for payment" }).click();
  await expect(modal.getByLabel("Label for customer_id")).toHaveValue("Customer");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("button", { name: "Display settings for payment" })).toBeFocused();
  await page.route("**/v1/tenants/**", async route => {
    const path = new URL(route.request().url()).pathname;
    expect(path).toContain(tenant);
    const scenarios = [{ id: "scenario", name: "Payment checks", trigger_object_type: "payment", created_at: "2026-01-01T00:00:00Z" }];
    await route.fulfill({ json: path.endsWith("/decisions/count") ? { count: 1 } : path.endsWith("/decisions") ? { decisions: [{ id: first, scenario_id: "scenario", object_type: "payment", object_id: "p1", outcome: "review", score: 5, created_at: "2026-01-01T00:00:00Z" }], pagination: { has_more: false } } : { scenarios, lists: [], entries: [] } });
  });
  await page.goto("/detection?tab=Decisions");
  await expect(page.getByRole("cell", { name: "PAY-p1", exact: true })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Customer", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Filters", exact: true }).click();
  await page.getByRole("button", { name: "Trigger object", exact: true }).click();
  await page.getByRole("button", { name: /Trigger object Any/ }).click();
  await page.getByLabel("Trigger object filter value").fill("payment");
  await expect(page.getByRole("cell", { name: "Ada Customer", exact: true })).toBeVisible();
  const headings = await page.getByRole("columnheader").allTextContents();
  expect(headings.indexOf("Amount")).toBeLessThan(headings.indexOf("Customer"));
  await page.screenshot({ path: "test-results/decision-displays-columns.png" });
  await page.getByLabel(`Select decision ${first}`, { exact: true }).check();
  await expect(page.getByRole("button", { name: "Add to Case (1)" })).toBeEnabled();
  await page.getByRole("button", { name: "Clear filters", exact: true }).click();
  await expect(page.getByRole("columnheader", { name: "Customer", exact: true })).toHaveCount(0);
  await page.screenshot({ path: "test-results/decision-displays.png" });
});

test("permission failure keeps the editor and unsaved changes visible", async ({ page, request }) => {
  await page.goto("/your-data");
  await page.getByRole("button", { name: "Display settings for payment" }).click();
  const modal = page.getByRole("dialog");
  await modal.getByLabel("Label for amount").fill("Total amount");
  await request.get("http://127.0.0.1:3199/fixture/display?deny");
  await modal.getByRole("button", { name: "Save display settings" }).click();
  await expect(modal.getByRole("alert")).toContainText("Platform administrator required");
  await expect(modal.getByLabel("Label for amount")).toHaveValue("Total amount");
});
