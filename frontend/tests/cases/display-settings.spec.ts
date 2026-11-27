import { expect, test } from "@playwright/test";
import { createUuid } from "../../src/lib/create-uuid";
const first = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const tenant = "11111111-1111-4111-8111-111111111111";
test.beforeEach(async ({ context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await request.get("http://127.0.0.1:3199/fixture/display");
  await context.addCookies([{ name: "case_test_session", value: "fixture-admin", domain: "127.0.0.1", path: "/" }]);
});

test("UUID generation uses secure bytes and sets the version and variant bits", () => {
  const crypto = globalThis.crypto;
  const previous = Object.getOwnPropertyDescriptor(crypto, "getRandomValues");
  Object.defineProperty(crypto, "getRandomValues", {
    configurable: true,
    value: (bytes: Uint8Array) => { bytes.forEach((_, index) => { bytes[index] = index; }); return bytes; },
  });
  try {
    expect(createUuid()).toBe("00010203-0405-4607-8809-0a0b0c0d0e0f");
  } finally {
    if (previous) Object.defineProperty(crypto, "getRandomValues", previous);
    else Reflect.deleteProperty(crypto, "getRandomValues");
  }
});

test("configure captions, linked labels and column order, then view them in Decisions", async ({ page }) => {
  await page.goto("/your-data");
  await page.getByRole("button", { name: "Display settings for payment" }).click();
  const modal = page.getByRole("dialog", { name: "Display settings" });
  await modal.getByLabel("Primary record caption field").selectOption("reference");
  await modal.getByLabel("Add display column").selectOption({ label: "customer.full_name" });
  await modal.getByLabel("Add display column").selectOption({ label: "amount" });
  await modal.getByLabel("Label for customer.full_name").fill("Customer");
  await modal.getByLabel("Label for amount").fill("Amount");
  await modal.getByRole("row").filter({ has: page.getByLabel("Label for amount") }).getByLabel("Move column up", { exact: true }).click();
  await page.screenshot({ path: "test-results/display-settings.png" });
  await modal.getByRole("button", { name: "Save", exact: true }).click();
  await expect(modal).toHaveCount(0);
  await page.getByRole("button", { name: "Display settings for payment" }).click();
  await expect(modal.getByLabel("Label for customer.full_name")).toHaveValue("Customer");
  await expect(modal.getByRole("row").filter({ has: page.getByRole("textbox") }).first().getByLabel("Label for amount")).toHaveValue("Amount");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("button", { name: "Display settings for payment" })).toBeFocused();
  await page.route("**/api/decisions/tenants/**", async route => {
    const path = new URL(route.request().url()).pathname;
    expect(path).toContain(tenant);
    const scenarios = [{ id: "scenario", name: "Payment checks", trigger_object_type: "payment", created_at: "2026-01-01T00:00:00Z" }];
    await route.fulfill({ json: path.endsWith("/decisions/count") ? { count: 1 } : path.endsWith("/decisions") ? { decisions: [{ id: first, scenario_id: "scenario", object_type: "payment", object_id: "p1", outcome: "review", score: 5, created_at: "2026-01-01T00:00:00Z" }], pagination: { has_more: false } } : { scenarios, lists: [], entries: [] } });
  });
  await page.goto("/detection?tab=Decisions");
  await expect(page.getByRole("columnheader", { name: "Customer", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Fetch record fields", exact: true }).click();
  await expect(page.getByRole("cell", { name: "PAY-p1", exact: true })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Customer", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Filters", exact: true }).click();
  await page.getByRole("button", { name: "Trigger object", exact: true }).click();
  await expect(page.getByRole("button", { name: "Trigger object: payment", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Apply", exact: true }).click();
  await expect(page.getByRole("cell", { name: "Ada Customer", exact: true })).toBeVisible();
  const headings = await page.getByRole("columnheader").allTextContents();
  expect(headings.indexOf("Amount")).toBeLessThan(headings.indexOf("Customer"));
  await page.screenshot({ path: "test-results/decision-displays-columns.png" });
  await page.getByLabel(`Select decision ${first}`, { exact: true }).check();
  await expect(page.getByRole("button", { name: "Add to Case", exact: true })).toBeEnabled();
  await expect(page.getByLabel(`Select decision ${first}`, { exact: true })).toBeChecked();
  await page.getByRole("button", { name: "Clear all", exact: true }).click();
  await expect(page.getByLabel(`Select decision ${first}`, { exact: true })).not.toBeChecked();
  await expect(page.getByRole("columnheader", { name: "Customer", exact: true })).toBeVisible();
  await page.getByRole("button", { name: /^Columns/ }).click();
  await page.getByRole("checkbox", { name: "Customer", exact: true }).uncheck();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await expect(page.getByRole("columnheader", { name: "Customer", exact: true })).toHaveCount(0);
  await page.screenshot({ path: "test-results/decision-displays.png" });
});

test("permission failure keeps the editor and unsaved changes visible", async ({ page, request }) => {
  await page.goto("/your-data");
  await page.getByRole("button", { name: "Display settings for payment" }).click();
  const modal = page.getByRole("dialog");
  await modal.getByLabel("Add display column").selectOption({ label: "amount" });
  await modal.getByLabel("Label for amount").fill("Total amount");
  await request.get("http://127.0.0.1:3199/fixture/display?deny");
  await modal.getByRole("button", { name: "Save", exact: true }).click();
  await expect(modal.getByRole("alert")).toContainText("Platform administrator required");
  await expect(modal.getByLabel("Label for amount")).toHaveValue("Total amount");
});

test("direct and related columns save when randomUUID is unavailable", async ({ page }) => {
  await page.addInitScript(() => { Object.defineProperty(Crypto.prototype, "randomUUID", { value: undefined }); });
  await page.goto("/your-data");
  await page.getByRole("button", { name: "Display settings for payment" }).click();
  const modal = page.getByRole("dialog", { name: "Display settings" });
  await modal.getByLabel("Add display column").selectOption({ label: "amount" });
  await modal.getByLabel("Add display column").selectOption({ label: "customer.full_name" });
  const save = page.waitForRequest(request => request.method() === "PUT" && request.url().endsWith("/display-settings"));
  await modal.getByRole("button", { name: "Save", exact: true }).click();
  const columns = (await save).postDataJSON().display_columns;
  expect(columns.map((column: { type: string }) => column.type)).toEqual(["direct", "relation"]);
  const ids = columns.map((column: { id: string }) => column.id);
  expect(ids[0]).toMatch(/^col_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  expect(ids[1]).toMatch(/^col_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  expect(ids[0]).not.toBe(ids[1]);
  await expect(modal).toHaveCount(0);
  await page.getByRole("button", { name: "Display settings for payment" }).click();
  await expect(modal.getByLabel("Label for amount")).toBeVisible();
  await expect(modal.getByLabel("Label for customer.full_name")).toBeVisible();
});

test("display settings reports when secure randomness is unavailable", async ({ page }) => {
  await page.goto("/your-data");
  await page.getByRole("button", { name: "Display settings for payment" }).click();
  const modal = page.getByRole("dialog", { name: "Display settings" });
  await page.evaluate(() => { Object.defineProperty(Crypto.prototype, "getRandomValues", { value: undefined }); });
  await modal.getByLabel("Add display column").selectOption({ label: "amount" });
  await expect(modal.getByRole("alert")).toContainText("Secure random number generation is unavailable");
  await expect(modal.getByLabel("Label for amount")).toHaveCount(0);
});
