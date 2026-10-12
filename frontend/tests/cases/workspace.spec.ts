import { expect, test } from "@playwright/test";
const tenant = "11111111-1111-4111-8111-111111111111";
const id = "33333333-3333-4333-8333-333333333333";

test("empty tenant queue handles nullable collections without a page crash", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.route(`**/api/cases/tenants/${tenant}/inboxes`, route => route.fulfill({ json: { inboxes: null } }));
  await page.route(`**/api/cases/tenants/${tenant}/tags?*`, route => route.fulfill({ json: { tags: null } }));
  await page.route(`**/api/cases/tenants/${tenant}/case-queue?*`, route => route.fulfill({ json: { cases: null, counts: null } }));
  await page.goto(`/cases?tenant=${tenant}`);
  await expect(page.getByText("No cases match these filters.")).toBeVisible();
  await page.getByRole("button", { name: "Create case", exact: true }).click();
  await expect(page.getByRole("combobox", { name: "Inbox", exact: true }).last()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Create investigation" })).toBeVisible();
  expect(errors).toEqual([]);
});

test("decision lookup retains closed membership and creates from an unlinked decision", async ({ page, request }) => {
  const decision = "44444444-4444-4444-8444-444444444444";
  await page.goto(`/cases/decisions/${decision}?tenant=${tenant}`);
  await expect(page.getByRole("link", { name: "Payment investigation", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Create a case", exact: true })).toHaveCount(0);
  await page.request.post(`/api/cases/tenants/${tenant}/cases/${id}/close`, { data: { outcome: "false_positive", comment: "Reviewed" }, headers: { Origin: "http://127.0.0.1:3100" } });
  await page.reload();
  await expect(page.getByText("· closed")).toBeVisible();
  await request.get("http://127.0.0.1:3199/reset");
  await request.get("http://127.0.0.1:3199/fixture/decision-intake");
  await page.reload();
  await page.getByRole("button", { name: "Create a case", exact: true }).click();
  await page.getByLabel("Case name", { exact: true }).fill("Decision-led investigation");
  await page.getByRole("combobox", { name: "Inbox", exact: true }).selectOption("22222222-2222-4222-8222-222222222222");
  await page.getByRole("button", { name: "Create investigation", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/cases/${id}\\?tenant=`));
  await expect(page.getByRole("heading", { name: "Decision-led investigation", exact: true })).toBeVisible();
  await page.getByRole("link", { name: `Decision ${decision}`, exact: true }).click();
  await page.getByRole("button", { name: "Inspect evidence", exact: true }).click();
  await expect(page.getByText("Velocity", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Decision-led investigation", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/cases/${id}\\?tenant=`));
});

test("decision attachment confirms the case and handles revoked access without losing selection", async ({ page, request }) => {
  await request.get("http://127.0.0.1:3199/fixture/decision-intake?denied");
  await page.goto(`/cases/decisions/44444444-4444-4444-8444-444444444444?tenant=${tenant}`);
  await page.getByRole("button", { name: "Add to existing case", exact: true }).click();
  await page.getByRole("combobox", { name: "Destination case", exact: true }).selectOption(id);
  await page.getByRole("button", { name: "Review attachment", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("Its outcome and snooze time are preserved");
  await page.getByRole("button", { name: "Confirm attachment", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Inbox access was revoked" })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Destination case", exact: true })).toHaveValue(id);
  await request.get("http://127.0.0.1:3199/fixture/decision-intake");
  await page.getByRole("button", { name: "Confirm attachment", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/cases/${id}\\?tenant=`));
});

test("uncertain creation discovers committed membership and prevents another create", async ({ page, request }) => {
  await request.get("http://127.0.0.1:3199/fixture/decision-intake?lost_ack");
  await page.goto(`/cases/decisions/44444444-4444-4444-8444-444444444444?tenant=${tenant}`);
  await page.getByRole("button", { name: "Create a case", exact: true }).click();
  await page.getByLabel("Case name", { exact: true }).fill("Recovered investigation");
  await page.getByRole("combobox", { name: "Inbox", exact: true }).selectOption("22222222-2222-4222-8222-222222222222");
  await page.getByRole("button", { name: "Create investigation", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Creation response unavailable" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Recovered investigation", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Create investigation", exact: true })).toHaveCount(0);
});

test("queue filters survive reload, history navigation and return from a case", async ({ page }) => {
  await page.goto(`/cases?tenant=${tenant}`);
  await expect(page.getByRole("button", { name: "Switch tenant" })).toHaveCount(0);
  await expect(page.getByLabel("Tenant", { exact: true })).toHaveCount(0);
  await expect(page.getByLabel("Search", { exact: true })).toBeHidden();
  if (await page.getByRole("button", { name: "Filters", exact: true }).getAttribute("aria-expanded") === "false") await page.getByRole("button", { name: "Filters", exact: true }).click();
  await page.getByLabel("Search", { exact: true }).fill("Payment");
  await page.getByRole("button", { name: "Filters", exact: true }).click();
  await expect(page.getByLabel("Search", { exact: true })).toBeHidden();
  await page.getByRole("button", { name: "Filters", exact: true }).click();
  await expect(page.getByLabel("Search", { exact: true })).toHaveValue("Payment");
  await page.getByRole("button", { name: "Apply filters", exact: true }).click();
  await expect(page).toHaveURL(/queue=/);
  await page.reload();
  await expect(page.getByLabel("Search", { exact: true })).toHaveValue("Payment");
  await page.getByRole("link", { name: "Payment investigation", exact: true }).click();
  // The queue also has a "Case queue" navigation link. Wait for the detail
  // navigation to commit before targeting the return link on that page.
  await expect(page).toHaveURL(url => url.pathname === `/cases/${id}`);
  await page.getByRole("link", { name: "Case queue", exact: true }).click();
  await expect(page).toHaveURL(url => url.pathname === "/cases" && new URLSearchParams(url.searchParams.get("queue") ?? "").get("name") === "Payment");
  await expect(page.getByLabel("Search", { exact: true })).toHaveValue("Payment");
  if (await page.getByRole("button", { name: "Filters", exact: true }).getAttribute("aria-expanded") === "false") await page.getByRole("button", { name: "Filters", exact: true }).click();
  await page.getByLabel("Search", { exact: true }).fill("No match");
  await page.getByRole("button", { name: "Apply filters", exact: true }).click();
  await expect(page).toHaveURL(url => new URLSearchParams(url.searchParams.get("queue") ?? "").get("name") === "No match");
  await expect(page.getByText("No cases match these filters.")).toBeVisible();
  await page.goBack();
  await expect(page.getByLabel("Search", { exact: true })).toHaveValue("Payment");
  await expect(page.getByRole("link", { name: "Payment investigation", exact: true })).toBeVisible();
  await page.goForward();
  await expect(page.getByLabel("Search", { exact: true })).toHaveValue("No match");
  await page.getByRole("button", { name: "Remove name filter", exact: true }).click();
  await expect(page.getByLabel("Search", { exact: true })).toHaveValue("");
  await expect(page.getByRole("link", { name: "Payment investigation", exact: true })).toBeVisible();
});

test("queue restores UTC and boolean filters and scopes assigned-to-me to the session", async ({ page }) => {
  const queue = new URLSearchParams({ include_snoozed: "true", created_from: "2026-01-01T09:00:00.000Z", cursor: "untrusted", limit: "999999" });
  const calls: string[] = [];
  page.on("request", req => { if (req.url().includes("/case-queue?")) calls.push(req.url()); });
  await page.goto(`/cases?tenant=${tenant}&queue=${encodeURIComponent(queue.toString())}`);
  await expect(page.getByLabel("Include snoozed", { exact: true })).toBeChecked();
  await expect(page.getByLabel("Created from (UTC)", { exact: true })).toHaveValue("2026-01-01T09:00");
  await page.getByRole("button", { name: "Assigned to me", exact: true }).click();
  await expect(page.getByLabel("Assignee ID", { exact: true })).toHaveValue("analyst");
  await page.reload();
  await expect(page.getByLabel("Assignee ID", { exact: true })).toHaveValue("analyst");
  expect(calls.some(url => url.includes("untrusted") || url.includes("999999"))).toBe(false);
  await page.getByRole("button", { name: "Reset filters", exact: true }).click();
  await expect(page.getByLabel("Assignee ID", { exact: true })).toHaveValue("");
  await expect(page.getByLabel("Include snoozed", { exact: true })).not.toBeChecked();
});

for (const action of ["approve", "decline", "confirmed_hit", "no_hit"]) {
  test(`investigator records ${action} with history and no automatic case closure`, async ({ page, request }) => {
    const match = action.includes("hit");
    if (match) await request.post("http://127.0.0.1:3199/fixture/screening?match=1");
    await page.goto(`/cases?tenant=${tenant}`);
    await page.getByRole("link", { name: match ? "Screening review" : "Payment investigation", exact: true }).click();
    await page.getByRole("button", { name: match ? "Review match" : "Review decision", exact: true }).click();
    await page.getByLabel("Review action", { exact: true }).selectOption(action);
    await page.getByLabel("Review findings").fill(`Finding for ${action}`);
    await page.getByRole("button", { name: "Check review", exact: true }).click();
    await expect(page.getByText(`Finding for ${action}`, { exact: true })).toHaveCount(1);
    await page.getByRole("button", { name: "Confirm review", exact: true }).click();
    await expect(page.getByText("Review recorded with case history.")).toBeVisible();
    await expect(page.getByText(`Review status: ${action.replaceAll("_", " ")}`, { exact: true })).toBeVisible();
    await page.reload();
    await expect(page.getByText(`Finding for ${action}`, { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Reopen investigation", exact: true })).toHaveCount(0);
  });
}

test("uncertain review retries the identical operation without duplicate history", async ({ page, request }) => {
  await request.post("http://127.0.0.1:3199/fixture/review?lost_ack=1");
  const commands: unknown[] = [];
  page.on("request", req => { if (req.url().includes("/api/case-review/") && req.method() === "POST") commands.push(req.postDataJSON()); });
  await page.goto(`/cases/${id}?tenant=${tenant}`);
  await page.getByRole("button", { name: "Review decision", exact: true }).click();
  await page.getByLabel("Review action", { exact: true }).selectOption("approve");
  await page.getByLabel("Review findings").fill("Verified identity");
  await page.getByRole("button", { name: "Check review", exact: true }).click();
  await page.getByRole("button", { name: "Confirm review", exact: true }).click();
  await expect(page.getByText("Review response unavailable. Retry the same operation.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Confirm review", exact: true }).click();
  await expect(page.getByText("Review recorded with case history.")).toBeVisible();
  expect(commands).toHaveLength(2); expect(commands[0]).toEqual(commands[1]);
  await expect(page.getByText("Verified identity", { exact: true })).toHaveCount(1);
});

test("review access revocation preserves findings and rejects cross-origin commands", async ({ page, request }) => {
  await page.goto(`/cases/${id}?tenant=${tenant}`);
  await page.getByRole("button", { name: "Review decision", exact: true }).click();
  await page.getByLabel("Review action", { exact: true }).selectOption("decline");
  await page.getByLabel("Review findings").fill("Preserve this finding");
  await request.post("http://127.0.0.1:3199/fixture/review?denied=1");
  await page.getByRole("button", { name: "Check review", exact: true }).click();
  await page.getByRole("button", { name: "Confirm review", exact: true }).click();
  await expect(page.getByText("Current case/resource access is required.", { exact: true })).toBeVisible();
  await expect(page.getByLabel("Review findings")).toHaveValue("Preserve this finding");
  const response = await page.request.post(`/api/case-review/${tenant}/${id}/decisions`, { headers: { Origin: "https://untrusted.invalid" }, data: {} });
  expect(response.status()).toBe(403);
});

for (const withMatch of [false, true]) {
  test(`decision screening arrives with its decision (${withMatch ? "match" : "partial empty"})`, async ({ page, request }) => {
    await request.post(`http://127.0.0.1:3199/fixture/screening?decision=1${withMatch ? "&match=1" : ""}`);
    await page.goto(`/cases?tenant=${tenant}`);
    await page.getByRole("link", { name: "Screening review", exact: true }).click();
    await expect(page.getByRole("button", { name: "Transfer decision", exact: true })).toBeVisible();
    await expect(page.getByText("Screening 66666666-6666-4666-8666-666666666666", { exact: true })).toBeVisible();
    await expect(page.getByText("Incomplete screening results.", { exact: false })).toHaveCount(withMatch ? 0 : 1);
    await page.reload();
    await expect(page.getByRole("button", { name: "Transfer decision", exact: true })).toBeVisible();
  });
}

for (const withMatch of [false, true]) {
  test(`screening arrival is visible before review (${withMatch ? "pending match" : "partial zero matches"})`, async ({ page, request }) => {
    await request.post(`http://127.0.0.1:3199/fixture/screening${withMatch ? "?match=1" : ""}`);
    await page.goto(`/cases?tenant=${tenant}`);
    await page.getByRole("link", { name: "Screening review", exact: true }).click();
    await expect(page.getByText("Screening 66666666-6666-4666-8666-666666666666", { exact: true })).toBeVisible();
    await expect(page.getByText(withMatch ? "Match pending-match-1 · pending" : "Screening result · awaiting review", { exact: true })).toBeVisible();
    await expect(page.getByText("Incomplete screening results.", { exact: false })).toHaveCount(withMatch ? 0 : 1);
    await page.getByRole("button", { name: "Inspect evidence", exact: true }).click();
    await expect(page.getByText("Test screening provider", { exact: true })).toBeVisible();
    await page.reload();
    await expect(page.getByText("Screening 66666666-6666-4666-8666-666666666666", { exact: true })).toBeVisible();
  });
}

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
  if (await page.getByRole("button", { name: "Filters", exact: true }).getAttribute("aria-expanded") === "false") await page.getByRole("button", { name: "Filters", exact: true }).click();
  await page.getByLabel("Search", { exact: true }).fill("no matching case");
  await page.getByRole("button", { name: "Apply filters" }).click();
  await expect(page.getByText("No cases match these filters.")).toBeVisible();
  await page.goto("/cases?tenant=99999999-9999-4999-8999-999999999999");
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

test("decision transfer confirms destination and safely retries a lost acknowledgment", async ({ page, request }) => {
  await request.post("http://127.0.0.1:3199/fixture/transfers?lost_ack=1");
  const operations: string[] = [];
  page.on("request", req => { if (req.method() === "POST" && req.url().endsWith("/decision-transfers")) operations.push(req.postDataJSON().operation_id); });
  await page.goto(`/cases/${id}?tenant=${tenant}`);
  await page.getByRole("button", { name: "Transfer decision", exact: true }).click();
  await page.getByLabel("Destination case", { exact: true }).selectOption("77777777-7777-4777-8777-777777777777");
  await page.getByLabel("Transfer reason", { exact: true }).fill("   ");
  await page.getByRole("button", { name: "Review transfer", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Enter a transfer reason" })).toBeVisible();
  expect(operations).toHaveLength(0);
  await page.getByLabel("Transfer reason", { exact: true }).fill("Correct investigation grouping");
  await page.getByRole("button", { name: "Review transfer", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Confirm transfer to Correct investigation" })).toBeVisible();
  expect(operations).toHaveLength(0);
  await page.getByRole("button", { name: "Confirm transfer", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Acknowledgment unavailable" })).toBeVisible();
  await expect(page.getByLabel("Transfer reason", { exact: true })).toHaveValue("Correct investigation grouping");
  await page.getByRole("button", { name: "Confirm transfer", exact: true }).click();
  await expect(page.getByText("No linked decisions.", { exact: true })).toBeVisible();
  await expect(page.getByText("Correct investigation grouping", { exact: true })).toBeVisible();
  expect(operations).toHaveLength(2); expect(operations[0]).toBe(operations[1]);
  await page.reload();
  await expect(page.getByText("No linked decisions.", { exact: true })).toBeVisible();
});

test("single and bulk closure validate whitespace and UTF-8 findings size", async ({ page }) => {
  let closeRequests = 0;
  page.on("request", request => { if (request.method() === "POST" && (request.url().endsWith("/close") || request.url().endsWith("/case-bulk"))) closeRequests++; });
  await page.goto(`/cases/${id}?tenant=${tenant}`);
  await page.getByRole("combobox", { name: "Outcome", exact: true }).selectOption("confirmed_risk");
  const findings = page.getByLabel("Closing findings", { exact: true });
  await findings.fill("   ");
  await page.getByRole("button", { name: "Close case", exact: true }).click();
  await expect(findings).toHaveJSProperty("validationMessage", "Enter closing findings, not just spaces.");
  await findings.fill("é".repeat(5001));
  await page.getByRole("button", { name: "Close case", exact: true }).click();
  expect(await findings.evaluate(node => (node as HTMLTextAreaElement).validity.valid)).toBe(false);
  expect(closeRequests).toBe(0);
  await findings.fill("é".repeat(5000));
  expect(await findings.evaluate(node => (node as HTMLTextAreaElement).validity.valid)).toBe(true);
  await page.goto(`/cases?tenant=${tenant}`);
  await page.getByLabel("Selected cases").selectOption(id);
  await page.getByRole("combobox", { name: "Bulk action", exact: true }).selectOption("close");
  const bulkFindings = page.getByLabel("Bulk closing findings", { exact: true });
  await bulkFindings.fill(" \n ");
  await page.getByRole("button", { name: "Apply bulk action" }).click();
  expect(await bulkFindings.evaluate(node => (node as HTMLTextAreaElement).validity.valid)).toBe(false);
  await bulkFindings.fill("é".repeat(5001));
  await page.getByRole("button", { name: "Apply bulk action" }).click();
  expect(await bulkFindings.evaluate(node => (node as HTMLTextAreaElement).validity.valid)).toBe(false);
  expect(closeRequests).toBe(0);
});

test("stale closure refreshes state and reopening handles capacity errors", async ({ page, request }) => {
  await page.goto(`/cases/${id}?tenant=${tenant}`);
  await page.getByRole("combobox", { name: "Outcome", exact: true }).selectOption("confirmed_risk");
  await page.getByLabel("Closing findings", { exact: true }).fill("My findings");
  // Another investigator resolves the case after this page has loaded.
  await request.post(`http://127.0.0.1:3199/v1/tenants/${tenant}/cases/${id}/close`, { headers: { Authorization: "Bearer fixture-analyst" }, data: { outcome: "false_positive", comment: "Other investigator findings" } });
  await page.getByRole("button", { name: "Close case", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "conflicts with current state" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Reopen investigation" })).toBeVisible();
  await expect(page.getByLabel("Evidence file")).toHaveCount(0);
  await expect(page.getByText("Reopen the case to upload additional evidence.", { exact: true })).toBeVisible();
  await expect(page.getByText("Other investigator findings", { exact: true })).toBeVisible();
  const endpoint = `**/api/cases/tenants/${tenant}/cases/${id}`;
  await page.route(endpoint, async route => {
    if (route.request().method() === "PATCH") return route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ error: "assignee has reached inbox capacity", code: "capacity_reached" }) });
    await route.continue();
  });
  await page.getByRole("button", { name: "Reopen investigation" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "assignee has reached inbox capacity" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Reopen investigation" })).toBeEnabled();
  await page.unroute(endpoint);
  let releaseRequest!: () => void;
  const pendingRequest = new Promise<void>(resolve => { releaseRequest = resolve; });
  await page.route(endpoint, async route => { if (route.request().method() === "PATCH") await pendingRequest; await route.continue(); });
  const reopenRequest = page.waitForRequest(request => request.method() === "PATCH" && request.url().endsWith(`/cases/${id}`));
  await page.getByRole("button", { name: "Reopen investigation" }).click();
  expect((await reopenRequest).postDataJSON()).toEqual({ status: "investigating" });
  try { await expect(page.getByRole("button", { name: "Reopen investigation" })).toBeDisabled(); } finally { releaseRequest(); }
  await expect(page.getByRole("button", { name: "Close case", exact: true })).toBeVisible();
  await expect(page.getByLabel("Evidence file")).toBeVisible();
  await page.reload();
  const overview = await page.request.get(`/api/cases/tenants/${tenant}/cases/${id}/overview`);
  expect((await overview.json()).case).toMatchObject({ status: "investigating", outcome: "unset", assigned_to: "analyst" });
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
