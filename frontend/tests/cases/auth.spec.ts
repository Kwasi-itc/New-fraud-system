import { expect, test } from "@playwright/test";

test.beforeEach(async ({ request }) => { await request.get("http://127.0.0.1:3199/reset"); });
const login = async (context: import("@playwright/test").BrowserContext, value = "fixture-analyst") => context.addCookies([{ name: "case_test_session", value, domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);

test("successful sign-in and invitation completion redirect to the dashboard", async ({ page, context }) => {
  for (const action of ["start", "invite-start"]) {
    await context.clearCookies();
    const start = await page.request.post(`/api/auth/${action}`, {
      headers: { Origin: "http://127.0.0.1:3100" },
      ...(action === "invite-start" ? { form: { token: "a".repeat(43) } } : {}),
      maxRedirects: 0,
    });
    expect(start.status()).toBe(303);
    expect((await context.cookies()).some(cookie => cookie.name === "fraud_login")).toBe(true);
    const callback = await page.request.get("/api/auth/callback?state=fixture-state&code=fixture-code", { maxRedirects: 0 });
    expect(callback.status()).toBe(303);
    expect(callback.headers().location).toBe("http://127.0.0.1:3100/dashboard");
    expect(callback.headers()["cache-control"]).toContain("no-store");
    const cookies = await context.cookies();
    expect(cookies.find(cookie => cookie.name === "case_test_session")?.value).toBe("fixture-analyst");
    expect(cookies.some(cookie => cookie.name === "fraud_login")).toBe(false);
  }
  await context.clearCookies();
  const denied = await page.request.get("/api/auth/callback?state=fixture-state&code=fixture-code", { maxRedirects: 0 });
  expect(denied.headers().location).toBe("http://127.0.0.1:3100/login?error=denied");
  expect((await context.cookies()).some(cookie => cookie.name === "case_test_session")).toBe(false);
  await page.request.post("/api/auth/start", { headers: { Origin: "http://127.0.0.1:3100" }, maxRedirects: 0 });
  const invalid = await page.request.get("/api/auth/callback?state=wrong&code=fixture-code", { maxRedirects: 0 });
  expect(invalid.headers().location).toBe("http://127.0.0.1:3100/login?error=denied");
  expect((await context.cookies()).some(cookie => cookie.name === "case_test_session" || cookie.name === "fraud_login")).toBe(false);
});

test("product navigation shows workspace links and keeps other routes accessible", async ({ page, context }) => {
  await login(context);
  await page.goto("/dashboard");
  const sidebar = page.locator("aside").first();
  for (const label of ["Dashboard", "Your Data Model", "Scenarios", "Lists", "Decisions", "Case Management", "Screening", "My Account"]) {
    await expect(sidebar.getByRole("link", { name: label, exact: true })).toBeVisible();
  }
  for (const label of ["Customer hub", "Settings"]) {
    await expect(sidebar.getByRole("link", { name: label, exact: true })).toHaveCount(0);
  }
  await expect(page.locator("main")).toHaveClass(/product-workspace/);
  await expect(page.getByRole("button", { name: "Expand sidebar" })).toBeHidden();
  await page.getByRole("button", { name: "Collapse sidebar" }).click();
  await expect(page.getByRole("button", { name: "Collapse sidebar" })).toBeHidden();
  await expect(sidebar.getByRole("link", { name: "Decisions", exact: true })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(sidebar.getByText("Scenarios", { exact: true })).toBeVisible();
  await expect(sidebar.getByText("My Account", { exact: true })).toBeVisible();
  await page.setViewportSize({ width: 1280, height: 844 });
  await page.getByRole("button", { name: "Collapse sidebar" }).click();
  await page.getByRole("button", { name: "Expand sidebar" }).click();
  for (const path of ["/my-account", "/administration/users", "/screening-search", "/cases", "/investigator", "/settings"]) {
    const response = await page.goto(path);
    expect(response?.status()).toBe(200);
    await expect(page.locator("main")).not.toHaveClass(/product-workspace/);
  }
});

test("sidebar aligns detection links and places the data model above account", async ({ page, context }) => {
  await login(context);
  await page.goto("/dashboard");
  const sidebar = page.locator("aside").first();
  const navigation = sidebar.getByRole("navigation", { name: "Product navigation" });
  const workspace = navigation.getByText("Workspace", { exact: true });
  const detection = navigation.getByText("Detection", { exact: true });
  await expect(detection.locator("svg")).toHaveCount(0);
  await expect(detection).toHaveCSS("font-size", await workspace.evaluate(el => getComputedStyle(el).fontSize));

  for (const width of [1280, 390]) {
    await page.setViewportSize({ width, height: 844 });
    const links = sidebar.locator('a[href]:not(:has(img))');
    await expect(links).toHaveText(["Dashboard", "Scenarios", "Lists", "Decisions", "Case Management", "Screening", "Your Data Model", "My Account"]);
    const iconPositions = await Promise.all(["Dashboard", "Scenarios", "Lists", "Decisions", "Case Management", "Screening", "Your Data Model", "My Account"].map(async name => {
      const icon = sidebar.getByRole("link", { name, exact: true }).locator("svg");
      return (await icon.boundingBox())!.x;
    }));
    expect(new Set(iconPositions).size).toBe(1);
  }

  await page.setViewportSize({ width: 1280, height: 844 });
  await page.getByRole("button", { name: "Collapse sidebar" }).click();
  await expect(detection).toHaveClass(/sr-only/);
  await expect(sidebar.getByRole("link", { name: "Your Data Model", exact: true })).toBeVisible();
  await expect(sidebar.getByRole("link", { name: "My Account", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Expand sidebar" }).click();
  await sidebar.getByRole("link", { name: "Your Data Model", exact: true }).click();
  await expect(page).toHaveURL(/\/your-data$/);
  await expect(sidebar.getByRole("link", { name: "Your Data Model", exact: true })).toHaveAttribute("aria-current", "page");
});

test("case management and screening navigation open their pages and remain active on subpages", async ({ page, context }) => {
  await login(context);
  await page.goto("/dashboard");
  const navigation = page.getByRole("navigation", { name: "Product navigation" });
  for (const [label, path, subpage] of [
    ["Case Management", "/cases", "/cases/inboxes"],
    ["Screening", "/screening-search", "/screening-search/history"],
  ] as const) {
    const link = navigation.getByRole("link", { name: label, exact: true });
    await expect(link).toHaveAttribute("href", path);
    await link.click();
    await expect(page).toHaveURL(new RegExp(`${path}$`));
    await expect(link).toHaveAttribute("aria-current", "page");
    await page.goto(subpage);
    await expect(link).toHaveAttribute("aria-current", "page");
  }
});

test("signed-out pages require login and account writes reject foreign origins", async ({ page }) => {
  await page.goto("/my-account"); await expect(page).toHaveURL(/\/login$/);
  for (const action of ["logout", "profile", "manage-sign-in"]) expect((await page.request.post(`/api/auth/${action}`, { headers: { Origin: "https://untrusted.example" }, data: { name: "Forged", version: 1 } })).status()).toBe(403);
});
test("one role tag and compact account controls preserve tenant access", async ({ page, context }) => {
  await login(context); await page.goto("/my-account");
  await expect(page.getByRole("heading", { name: "Test investigator", exact: true })).toBeVisible();
  const navigation = page.getByRole("navigation", { name: "Account pages" });
  await expect(navigation.getByRole("link")).toHaveCount(1);
  await expect(page.getByRole("heading", { name: "My access", exact: true })).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "Sign-in & security", exact: true })).toHaveCount(0);
  expect((await page.request.get("/api/auth/members")).status()).toBe(403);
  await page.getByRole("button", { name: "Sign out", exact: true }).first().click(); await expect(page).toHaveURL(/\/login$/);
  await page.goto("/my-account"); await expect(page).toHaveURL(/\/login$/);
});
test("profile edits persist through owner APIs and caller fields cannot replace the session", async ({ page, context }) => {
  await login(context); await page.goto("/my-account");
  await page.getByRole("button", { name: "Edit profile" }).click(); await page.getByLabel("Display name", { exact: true }).fill("Updated investigator");
  await page.getByRole("dialog").getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("heading", { name: "Updated investigator", exact: true })).toBeVisible();
  await page.reload(); await expect(page.getByRole("heading", { name: "Updated investigator", exact: true })).toBeVisible();
  const response = await page.request.post("/api/auth/profile", { headers: { Origin: "http://127.0.0.1:3100" }, data: { session: "fixture-admin", user_id: "other-user", name: "Own update", version: 2 } });
  expect(response.status()).toBe(204); expect((await (await page.request.get("/api/auth/profile")).json()).name).toBe("Own update");
  expect((await page.request.post("/api/auth/profile", { headers: { Origin: "http://127.0.0.1:3100" }, data: { name: "Stale", version: 1 } })).status()).toBe(409);
});
test("manage sign-in uses a configured provider destination and requires a session", async ({ page, context }) => {
  await login(context); await page.goto("/my-account");
  const response = await page.request.post("/api/auth/manage-sign-in", { headers: { Origin: "http://127.0.0.1:3100" }, maxRedirects: 0 });
  expect(response.status()).toBe(303); expect(response.headers().location).toBe("http://127.0.0.1:3199/provider-account");
  await context.clearCookies(); expect((await page.request.post("/api/auth/manage-sign-in", { headers: { Origin: "http://127.0.0.1:3100" }, maxRedirects: 0 })).status()).toBe(401);
});
test("administrator member editor is single-choice and retains conflict errors", async ({ page, context }) => {
  await login(context, "fixture-admin"); await page.goto("/administration/users");
  await page.getByRole("button", { name: "Manage", exact: true }).click();
  await expect(page.getByLabel("Assigned role")).not.toHaveAttribute("multiple");
  await page.getByLabel("Assigned role").selectOption("case_manager_admin"); await page.getByRole("button", { name: "Save member", exact: true }).click();
  await expect(page.getByRole("dialog").getByRole("alert")).toContainText("Account changed");
});
test("another own session is revoked only after confirmation", async ({ page, context }) => {
  await login(context); await page.goto("/my-account"); await expect(page.getByRole("button", { name: "Revoke", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Revoke", exact: true }).click(); await page.getByRole("dialog").getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByRole("button", { name: "Revoke", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Revoke", exact: true }).click(); await page.getByRole("dialog").getByRole("button", { name: "Revoke session", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Session revoked."); await expect(page.getByRole("button", { name: "Revoke", exact: true })).toHaveCount(0);
  expect((await page.request.post("/api/auth/sessions", { headers: { Origin: "https://untrusted.example" }, data: { all: true } })).status()).toBe(403);
});
test("revoke-all clears the cookie and requires login", async ({ page, context }) => {
  await login(context); await page.goto("/my-account"); await page.getByRole("button", { name: "Sign out all sessions", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Sign out all sessions", exact: true }).click(); await expect(page).toHaveURL(/\/login$/);
  expect((await context.cookies()).some(cookie => cookie.name === "case_test_session")).toBe(false);
});
test("session revocation failure retains a retryable confirmation without reporting success", async ({ page, context, request }) => {
  await login(context); await request.get("http://127.0.0.1:3199/fixture/session-error"); await page.goto("/my-account");
  await page.getByRole("button", { name: "Revoke", exact: true }).click(); await page.getByRole("dialog").getByRole("button", { name: "Revoke session", exact: true }).click();
  await expect(page.getByRole("dialog").getByRole("alert")).toContainText("Unavailable"); await expect(page.getByRole("dialog").getByRole("button", { name: "Revoke session", exact: true })).toBeEnabled();
  await expect(page.getByRole("status")).toHaveCount(0); await expect(page).toHaveURL(/\/my-account$/);
});
