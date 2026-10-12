import { expect, test } from "@playwright/test";
test.beforeEach(async ({ request }) => { await request.get("http://127.0.0.1:3199/reset"); });
test("signed-out pages require login and auth writes reject foreign origins", async ({ page }) => {
  await page.goto("/my-account"); await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("button", { name: "Continue to sign in" })).toBeVisible();
  expect((await page.request.post("/api/auth/logout", { headers: { Origin: "https://untrusted.example" } })).status()).toBe(403);
});
test("investigator sees current roles, cannot administer accounts, and can log out", async ({ page, context }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  await page.goto("/my-account"); await expect(page.getByText("Test investigator", { exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Tenant accounts" })).toHaveCount(0);
  expect((await page.request.get("/api/auth/members")).status()).toBe(403);
  await page.getByRole("button", { name: "Sign out", exact: true }).click(); await expect(page).toHaveURL(/\/login$/);
  await page.goto("/my-account"); await expect(page).toHaveURL(/\/login$/);
});
test("administrator access editor confirms writes and displays version conflicts", async ({ page, context }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-admin", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  await page.goto("/my-account"); await expect(page.getByRole("heading", { name: "Tenant accounts" })).toBeVisible();
  await page.getByLabel("Case administrator", { exact: true }).check();
  page.once("dialog", dialog => dialog.accept()); await page.getByRole("button", { name: "Save access" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Account changed" })).toContainText("Account changed; refresh before retrying.");
});

test("investigator can inspect and revoke another own session with confirmation", async ({ page, context }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  await page.goto("/my-account");
  const sessions = page.getByRole("region", { name: "Your application sessions" });
  await expect(sessions.getByText("This browser session", { exact: true })).toBeVisible();
  page.once("dialog", dialog => dialog.dismiss());
  await sessions.getByRole("button", { name: "Revoke session", exact: true }).click();
  await expect(sessions.getByText("Other session", { exact: true })).toBeVisible();
  page.once("dialog", dialog => dialog.accept());
  await sessions.getByRole("button", { name: "Revoke session", exact: true }).click();
  await expect(sessions.getByRole("status")).toHaveText("Session revoked.");
  await expect(sessions.getByText("Other session", { exact: true })).toHaveCount(0);
  await expect(sessions.getByText("This browser session", { exact: true })).toBeVisible();
  expect((await page.request.post("/api/auth/sessions", { headers: { Origin: "https://untrusted.example" }, data: { all: true } })).status()).toBe(403);
});

test("revoke-all clears the session and requires login", async ({ page, context }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  await page.goto("/my-account");
  page.once("dialog", dialog => dialog.accept());
  await page.getByRole("button", { name: "Sign out all sessions in this tenant" }).click();
  await expect(page).toHaveURL(/\/login$/);
  expect((await context.cookies()).some(cookie => cookie.name === "case_test_session")).toBe(false);
  await page.goto("/my-account"); await expect(page).toHaveURL(/\/login$/);
});

test("session revocation failure keeps an actionable error and does not claim success", async ({ page, context, request }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  await request.get("http://127.0.0.1:3199/fixture/session-error");
  await page.goto("/my-account");
  page.once("dialog", dialog => dialog.accept());
  await page.getByRole("button", { name: "Revoke session", exact: true }).click();
  await expect(page.getByRole("region", { name: "Your application sessions" }).getByRole("alert")).toContainText("could not be confirmed");
  await expect(page.getByRole("button", { name: "Refresh sessions" })).toBeEnabled();
  await expect(page).toHaveURL(/\/my-account$/);
});
