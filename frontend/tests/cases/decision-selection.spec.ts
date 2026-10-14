import { expect, test } from "@playwright/test";

const tenant = "11111111-1111-4111-8111-111111111111";
const inbox = "22222222-2222-4222-8222-222222222222";
const scenario = "88888888-8888-4888-8888-888888888888";
const linked = "44444444-4444-4444-8444-444444444444";
const first = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const second = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const privateID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee";

test.beforeEach(async ({ page, context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  // Detection owner reads are controlled; case writes use the real Next proxy.
  await page.route("**/v1/tenants/**", async route => {
    const url = new URL(route.request().url());
    expect(url.pathname).toContain(tenant);
    if (url.pathname.endsWith("/scenarios")) return route.fulfill({ json: { scenarios: [{ id: scenario, name: "Payment checks", trigger_object_type: "payment", created_at: "2026-01-01T00:00:00Z" }] } });
    if (url.pathname.endsWith("/decisions/count")) return route.fulfill({ json: { count: 4 } });
    if (url.pathname.endsWith("/decisions")) return route.fulfill({ json: { decisions: [first, second, linked, privateID].map((id, i) => ({ id, tenant_id: tenant, scenario_id: scenario, object_type: "payment", object_id: `payment-${i}`, score: 50, outcome: "review", created_at: "2026-01-01T00:00:00Z" })), pagination: { has_more: false } } });
    return route.fulfill({ json: { lists: [], entries: [] } });
  });
  await page.goto("/detection?tab=Decisions");
  await expect(page.getByLabel(`Select decision ${first}`, { exact: true })).toBeEnabled();
});

test("select decisions, show actual membership, create one case and navigate", async ({ page }) => {
  const add = page.getByRole("button", { name: /^Add to Case/ });
  await expect(add).toBeDisabled();
  await expect(page.getByLabel(`Select decision ${linked}`, { exact: true })).toBeDisabled();
  await expect(page.getByLabel(`Select decision ${privateID}`, { exact: true })).toBeDisabled();
  await expect(page.getByRole("link", { name: "Payment investigation · investigating" })).toBeVisible();
  await page.getByLabel("Select eligible decisions on this page").check();
  await expect(add).toHaveText("Add to Case (2)");
  await add.click();
  const modal = page.getByRole("dialog", { name: "Create case" });
  await expect(modal.getByText("2 selected decisions")).toBeVisible();
  await expect(modal.getByLabel("Case name")).toBeFocused();
  await expect(modal.getByRole("button", { name: "Create case", exact: true })).toBeDisabled();
  await modal.getByLabel("Case name").fill("Selected payment investigation");
  await modal.getByRole("combobox", { name: "Inbox", exact: true }).selectOption(inbox);
  await page.screenshot({ path: "test-results/decision-selection-modal.png" });
  const sent = page.waitForRequest(r => r.method() === "POST" && r.url().endsWith(`/tenants/${tenant}/cases`));
  await modal.getByRole("button", { name: "Create case", exact: true }).click();
  expect((await sent).postDataJSON().decision_ids).toEqual([first, second]);
  await expect(page).toHaveURL(/\/cases\/33333333-3333-4333-8333-333333333333/);
  await expect(page.getByRole("heading", { name: "Selected payment investigation", exact: true })).toBeVisible();
});

test("selection clears on filter/page-size changes and modal supports remove/cancel", async ({ page }) => {
  await page.getByLabel(`Select decision ${first}`, { exact: true }).check();
  await expect(page.getByLabel("Select eligible decisions on this page")).toHaveJSProperty("indeterminate", true);
  await page.getByRole("button", { name: /^Add to Case/ }).click();
  await page.getByRole("dialog").getByLabel(`Remove decision ${first}`).click();
  await expect(page.getByRole("dialog").getByText("0 selected decisions")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^Add to Case/ })).toBeFocused();
  await page.getByLabel(`Select decision ${first}`, { exact: true }).check();
  await page.getByRole("combobox", { name: "Rows per page" }).selectOption("10");
  await expect(page.getByRole("button", { name: /^Add to Case/ })).toBeDisabled();
  await page.getByLabel(`Select decision ${first}`, { exact: true }).check();
  await page.getByPlaceholder("Search by decision, object, or scenario").fill("payment");
  await expect(page.getByRole("button", { name: /^Add to Case/ })).toBeDisabled();
  await page.getByPlaceholder("Search by decision, object, or scenario").clear();
  await expect(page.getByLabel(`Select decision ${first}`, { exact: true })).not.toBeChecked();
});

test("lost creation response retries identical operation without duplicate history", async ({ page, request }) => {
  await request.get("http://127.0.0.1:3199/fixture/decision-intake?lost_ack");
  await page.getByLabel(`Select decision ${first}`, { exact: true }).check();
  await page.getByRole("button", { name: /^Add to Case/ }).click();
  const modal = page.getByRole("dialog");
  await modal.getByLabel("Case name").fill("Recovered selection");
  await modal.getByRole("combobox", { name: "Inbox", exact: true }).selectOption(inbox);
  const payloads: unknown[] = [];
  page.on("request", r => { if (r.method() === "POST" && r.url().endsWith(`/tenants/${tenant}/cases`)) payloads.push(r.postDataJSON()); });
  await modal.getByRole("button", { name: "Create case", exact: true }).click();
  await expect(modal.getByRole("alert")).toContainText("Retry the same request");
  await expect(modal.getByLabel("Case name")).toBeDisabled();
  await modal.getByRole("button", { name: "Retry creation" }).click();
  await expect(page).toHaveURL(/\/cases\/33333333/);
  expect(payloads).toHaveLength(2); expect(payloads[1]).toEqual(payloads[0]);
  const events = await page.request.get(`/api/cases/tenants/${tenant}/cases/33333333-3333-4333-8333-333333333333/links/events`);
  expect((await events.json()).items.filter((e: { event_type: string }) => e.event_type === "case_created")).toHaveLength(1);
});

test("revoked inbox access keeps the modal and displays the failure", async ({ page, request }) => {
  await page.getByLabel(`Select decision ${first}`, { exact: true }).check();
  await page.getByRole("button", { name: /^Add to Case/ }).click();
  const modal = page.getByRole("dialog");
  await modal.getByLabel("Case name").fill("Denied selection");
  await modal.getByRole("combobox", { name: "Inbox", exact: true }).selectOption(inbox);
  await request.get("http://127.0.0.1:3199/fixture/decision-intake?denied");
  await modal.getByRole("button", { name: "Create case", exact: true }).click();
  await expect(modal.getByRole("alert")).toContainText("Inbox access was revoked");
  await expect(modal.getByLabel("Case name")).toHaveValue("Denied selection");
  await expect(modal.getByRole("button", { name: "Cancel" })).toBeEnabled();
});
