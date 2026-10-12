import { expect, test } from "@playwright/test";
test.beforeEach(async ({ request }) => { await request.get("http://127.0.0.1:3199/reset"); });
test("administrator invites, sees queued delivery, cancels and reads audit", async ({ page, context }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-admin", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  await page.goto("/my-account"); await page.getByRole("link", { name: "Manage users, invitations and access" }).click();
  await expect(page.getByRole("heading", { name: "Users & access" })).toBeVisible();
  await page.getByLabel("Email address").fill("new@example.test");
  page.once("dialog", d => d.accept()); await page.getByRole("button", { name: "Invite user", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("queued for email delivery");
  await expect(page.getByText("new@example.test — pending")).toBeVisible();
  await expect(page.getByText("Email: queued")).toBeVisible();
  page.once("dialog", d => d.accept()); await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByText("new@example.test — cancelled")).toBeVisible();
  await expect(page.getByText("invitation.created — 2026-10-09")).toBeVisible();
  expect((await page.request.post("/api/auth/invitations", { headers: { Origin: "https://evil.test" }, data: { action: "create" } })).status()).toBe(403);
});
test("non administrators cannot view invitations or audit", async ({ page, context }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  await page.goto("/administration/users"); await expect(page.getByRole("alert").filter({ hasText: "Account administrator access is required" })).toBeVisible();
  expect((await page.request.get("/api/auth/invitations")).status()).toBe(403);
  expect((await page.request.get("/api/auth/audit")).status()).toBe(403);
});
test("invitation fragment is removed and posted only on explicit acceptance", async ({ page }) => {
  const requests: string[] = []; page.on("request", r => requests.push(r.url()));
  await page.goto(`/invite#${"a".repeat(43)}`);
  await expect(page).toHaveURL(/\/invite$/);
  await expect(page.getByRole("button", { name: "Continue to sign in and accept" })).toBeVisible();
  expect(requests.some(u => u.includes("a".repeat(43)))).toBe(false);
  const post = page.waitForRequest(r => r.url().endsWith("/api/auth/invite-start") && r.method() === "POST");
  await page.getByRole("button", { name: "Continue to sign in and accept" }).click();
  expect((await post).postData()).toContain(`token=${"a".repeat(43)}`);
  await expect(page).toHaveURL("http://127.0.0.1:3199/provider-login");
});
