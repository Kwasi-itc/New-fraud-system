import { expect, test } from "@playwright/test";
test.beforeEach(async ({ request }) => { await request.get("http://127.0.0.1:3199/reset"); });
test("administrator invites with one role, sees delivery, revokes and reads audit", async ({ page, context }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-admin", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  await page.goto("/my-account"); await page.getByRole("navigation", { name: "Account pages" }).getByRole("link", { name: "Users & invitations", exact: true }).click();
  await page.getByRole("button", { name: "Invite member", exact: true }).click(); await page.getByLabel("Email address", { exact: true }).fill("new@example.test");
  await expect(page.getByLabel("Role", { exact: true })).not.toHaveAttribute("multiple"); await page.getByLabel("Role", { exact: true }).selectOption("case_investigator");
  const post = page.waitForRequest(request => request.url().endsWith("/api/auth/invitations") && request.method() === "POST");
  await page.getByRole("dialog").getByRole("button", { name: "Invite member", exact: true }).click(); expect((await post).postDataJSON().roles).toEqual(["case_investigator"]);
  await expect(page.getByRole("status")).toContainText("queued for email delivery"); const row = page.getByRole("row").filter({ hasText: "new@example.test" }); await expect(row).toContainText("pending"); await expect(row).toContainText("queued");
  await row.getByRole("button", { name: "Revoke invitation", exact: true }).click(); await page.getByRole("dialog").getByRole("button", { name: "Revoke invitation", exact: true }).click(); await expect(row).toContainText("cancelled");
  await page.getByText("Access audit trail", { exact: true }).click(); await expect(page.getByText("invitation.created — 2026-10-09", { exact: true })).toBeVisible();
  expect((await page.request.post("/api/auth/invitations", { headers: { Origin: "https://evil.test" }, data: { action: "create" } })).status()).toBe(403);
});
test("non administrators cannot view invitations or audit", async ({ page, context }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  await page.goto("/administration/users"); await expect(page.getByRole("alert").filter({ hasText: "Tenant Administrator" })).toBeVisible();
  expect((await page.request.get("/api/auth/invitations")).status()).toBe(403); expect((await page.request.get("/api/auth/audit")).status()).toBe(403);
});
test("invitation fragment is removed and posted only on explicit acceptance", async ({ page }) => {
  const requests: string[] = []; page.on("request", request => requests.push(request.url())); await page.goto(`/invite#${"a".repeat(43)}`); await expect(page).toHaveURL(/\/invite$/);
  expect(requests.some(url => url.includes("a".repeat(43)))).toBe(false);
  const post = page.waitForRequest(request => request.url().endsWith("/api/auth/invite-start") && request.method() === "POST"); await page.getByRole("button", { name: "Continue to sign in and accept" }).click();
  expect((await post).postData()).toContain(`token=${"a".repeat(43)}`); await expect(page).toHaveURL("http://127.0.0.1:3199/provider-login");
});
