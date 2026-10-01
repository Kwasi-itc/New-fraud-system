import { expect, test } from "@playwright/test";
const tenant = "11111111-1111-4111-8111-111111111111";
const id = "33333333-3333-4333-8333-333333333333";

test.beforeEach(async ({ context, request }) => {
  await request.post("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
});

test("investigator inspects evidence, comments, closes, reloads, and reopens", async ({ page }) => {
  await page.goto(`/cases?tenant=${tenant}`);
  await expect(page.getByRole("link", { name: "Payment investigation" })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("case-queue.png"), fullPage: true });
  await page.getByRole("link", { name: "Payment investigation" }).click();
  await expect(page.getByRole("heading", { name: "Payment investigation" })).toBeVisible();
  await page.getByRole("button", { name: "Inspect evidence" }).click();
  await expect(page.getByText("Velocity", { exact: true })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("case-detail.png"), fullPage: true });
  await page.getByLabel("Investigation note").fill("Customer contacted");
  await page.getByRole("button", { name: "Add comment", exact: true }).click();
  await expect(page.getByText("Customer contacted", { exact: true })).toBeVisible();
  await page.getByRole("combobox", { name: "Outcome", exact: true }).selectOption("confirmed_risk");
  await page.getByLabel("Closing findings").fill("Confirmed with customer");
  await page.getByRole("button", { name: "Close case", exact: true }).click();
  await expect(page.getByRole("button", { name: "Reopen investigation" })).toBeVisible();
  await page.reload();
  await expect(page.getByText("Confirmed with customer", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Reopen investigation" }).click();
  await expect(page.getByRole("button", { name: "Close case", exact: true })).toBeVisible();
});

test("empty queues and tenant switches never show another tenant's cached cases", async ({ page }) => {
  await page.goto(`/cases?tenant=${tenant}`);
  await expect(page.getByRole("link", { name: "Payment investigation" })).toBeVisible();
  await page.getByLabel("Search", { exact: true }).fill("no matching case");
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(page.getByText("No cases match these filters.")).toBeVisible();
  await page.getByLabel("Tenant", { exact: true }).fill("99999999-9999-4999-8999-999999999999");
  await page.getByRole("button", { name: "Switch tenant" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "cannot access this tenant" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Payment investigation" })).toHaveCount(0);
});

test("unauthorized inboxes and failed writes remain actionable", async ({ page }) => {
  await page.goto(`/cases/99999999-9999-4999-8999-999999999999?tenant=${tenant}`);
  await expect(page.getByRole("alert").filter({ hasText: "cannot access this inbox" })).toBeVisible();
  await page.goto(`/cases/${id}?tenant=${tenant}`);
  await page.getByLabel("Investigation note").fill("fail write");
  await page.getByRole("button", { name: "Add comment", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "could not be saved" })).toBeVisible();
  await expect(page.getByLabel("Investigation note")).toHaveValue("fail write");
});

test("proxy requires a session and rejects cross-origin writes", async ({ context, request }) => {
  const unsafe = await request.post(`/api/cases/tenants/${tenant}/cases/${id}/comments`, { headers: { origin: "https://attacker.invalid" }, data: { comment: "forged" } });
  expect(unsafe.status()).toBe(403);
  await context.clearCookies();
  const signedOut = await context.request.get(`/api/cases/tenants/${tenant}/case-session`);
  expect(signedOut.status()).toBe(401);
});

test("evidence uploads finalize, survive reload, and download through the private proxy", async ({ page, context }) => {
  await page.goto(`/cases/${id}?tenant=${tenant}`);
  await page.getByLabel("Evidence file", { exact: true }).setInputFiles({ name: "note.txt", mimeType: "text/plain", buffer: Buffer.from("Retained evidence\n") });
  await page.getByRole("button", { name: "Upload evidence", exact: true }).click();
  await expect(page.getByText("Evidence attached.", { exact: true })).toBeVisible();
  await page.reload();
  const link = page.getByRole("link", { name: "note.txt", exact: true });
  await expect(link).toBeVisible();
  const target = (await link.getAttribute("href"))!;
  const response = await context.request.get(target);
  expect(response.status()).toBe(200);
  expect(await response.text()).toBe("Retained evidence\n");
  expect(response.headers()["cache-control"]).toContain("no-store");
  expect(response.headers()["content-disposition"]).toContain("attachment");
  const unsafe = await context.request.put(`/api/cases/tenants/${tenant}/cases/${id}/uploads/55555555-5555-4555-8555-555555555555/content`, { headers: { origin: "https://attacker.invalid", "content-type": "application/octet-stream" }, data: "forged" });
  expect(unsafe.status()).toBe(403);
  await context.clearCookies();
  expect((await context.request.get(target)).status()).toBe(401);
});

test("bulk close reports results and retains the operation ID for retry", async ({ page }) => {
  await page.goto(`/cases?tenant=${tenant}`);
  await page.getByRole("listbox", { name: "Selected cases" }).selectOption(id);
  await page.getByRole("combobox", { name: "Bulk action", exact: true }).selectOption("close");
  await page.getByLabel("Bulk closing findings", { exact: true }).fill("Batch reviewed");
  await page.getByRole("button", { name: "Apply bulk action", exact: true }).click();
  await expect(page.getByText(/Completed$/)).toBeVisible();
  await page.getByRole("button", { name: "Retry same operation" }).click();
  await expect(page.getByText(/Already completed$/)).toBeVisible();
});

test("administrators create, update, and archive tags", async ({ page, context }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-admin", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  await page.goto(`/cases/tags?tenant=${tenant}`);
  await page.getByLabel("Name", { exact: true }).fill("Review");
  await page.getByRole("button", { name: "Create tag", exact: true }).click();
  await page.getByLabel("Name for Review", { exact: true }).fill("Follow up");
  await page.getByRole("button", { name: "Save tag", exact: true }).click();
  await page.getByRole("button", { name: "Archive Follow up", exact: true }).click();
  await expect(page.getByText("No active tags.", { exact: true })).toBeVisible();
});

test("report drafts retain findings, require completion fields, and export privately", async ({ page, context }) => {
  await page.goto(`/cases/${id}?tenant=${tenant}`);
  await page.getByRole("button", { name: "New report", exact: true }).click();
  await page.getByLabel("New report title", { exact: true }).fill("Suspicious transfers");
  await page.getByRole("button", { name: "Create draft", exact: true }).click();
  await page.locator("summary").filter({ hasText: "Suspicious transfers" }).click();
  await page.getByRole("button", { name: "Complete report", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Completion requires" })).toBeVisible();
  await page.getByLabel("Report subject", { exact: true }).fill("Account A-1");
  await page.getByLabel("Report narrative", { exact: true }).fill("Investigator reviewed transaction evidence.");
  await page.getByLabel("Activity start (UTC)", { exact: true }).fill("2026-01-01T10:00");
  await page.getByLabel("Activity end (UTC)", { exact: true }).fill("2026-01-02T10:00");
  await expect(page.getByRole("button", { name: "Complete report", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Save report draft", exact: true }).click();
  await expect(page.getByRole("button", { name: "Complete report", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Complete report", exact: true }).click();
  await expect(page.locator("summary").filter({ hasText: "Suspicious transfers" })).toContainText("completed");
  await page.reload();
  await page.locator("summary").filter({ hasText: "Suspicious transfers" }).click();
  await expect(page.getByRole("textbox", { name: "Report narrative", exact: true })).toBeDisabled();
  await expect(page.getByRole("textbox", { name: "Report narrative", exact: true })).toHaveValue("Investigator reviewed transaction evidence.");
  const path = (await page.getByRole("link", { name: "Download report JSON", exact: true }).getAttribute("href"))!;
  const result = await context.request.get(path);
  expect(result.status()).toBe(200);
  expect((await result.json()).status).toBe("completed");
  expect(result.headers()["content-disposition"]).toContain("attachment");
  expect(result.headers()["cache-control"]).toContain("no-store");
  expect((await context.request.get(path.replace(id, "99999999-9999-4999-8999-999999999999"))).status()).toBe(403);
  await context.clearCookies();
  expect((await context.request.get(path)).status()).toBe(401);
});

test("analytics filters reconcile visible cases and hide data after access failure", async ({ page, context }) => {
  await page.goto(`/cases/analytics?tenant=${tenant}`);
  await page.getByLabel("Created on or after (UTC)", { exact: true }).fill("2026-01-01");
  await page.getByLabel("Created before (UTC)", { exact: true }).fill("2026-02-01");
  await page.getByRole("button", { name: "Apply analytics filters", exact: true }).click();
  await expect(page.getByRole("table", { name: "Status by inbox", exact: true })).toContainText("Fraud review");
  await expect(page.getByRole("table", { name: "Assignment workload", exact: true })).toContainText("analyst");
  const result = await context.request.get(`/api/cases/tenants/${tenant}/case-analytics?from=2026-01-01T00:00:00Z&to=2026-02-01T00:00:00Z`);
  expect(result.status()).toBe(200);
  expect(result.headers()["cache-control"]).toContain("no-store");
  expect((await result.json()).totals).toMatchObject({ total: 1, investigating: 1, closed: 0 });
  await page.getByLabel("Created before (UTC)", { exact: true }).fill("2028-01-01");
  await page.getByRole("button", { name: "Apply analytics filters", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "366 days" })).toBeVisible();
  await page.getByLabel("Created on or after (UTC)", { exact: true }).fill("2026-02-01");
  await page.getByLabel("Created before (UTC)", { exact: true }).fill("2026-03-01");
  await page.getByRole("button", { name: "Apply analytics filters", exact: true }).click();
  await expect(page.getByText("No permitted cases were created in this range.", { exact: true })).toBeVisible();
  await expect(page.getByRole("table", { name: "Status by inbox", exact: true })).toHaveCount(0);
  await page.getByLabel("Created on or after (UTC)", { exact: true }).fill("2026-01-01");
  await page.getByRole("button", { name: "Apply analytics filters", exact: true }).click();
  await expect(page.getByRole("table", { name: "Status by inbox", exact: true })).toBeVisible();
  await context.clearCookies();
  await page.getByRole("button", { name: "Refresh analytics", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Sign in with your investigator account" })).toBeVisible();
  await expect(page.getByRole("table", { name: "Status by inbox", exact: true })).toHaveCount(0);
});
