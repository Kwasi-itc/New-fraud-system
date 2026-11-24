import { expect, test } from "@playwright/test";
const clientID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", inboxID = "22222222-2222-4222-8222-222222222222";
test.beforeEach(async ({ context, request }) => { await request.get("http://127.0.0.1:3199/reset"); await context.addCookies([{ name: "case_test_session", value: "fixture-admin", domain: "127.0.0.1", path: "/" }]); });

test("full-page custom role editor copies allowed capabilities and persists versioned edits", async ({ page }) => {
  const definitions = [{ name: "cases:read", human: true }, { name: "cases:review", human: true }, { name: "roles:manage", human: true, reserved: true }, { name: "roles:assign", human: true, reserved: true }, { name: "tenants:provision", human: false }];
  const roles = [{ id: "tenant_admin", name: "Tenant Administrator", builtin: true, capabilities: ["cases:read", "cases:review", "roles:manage", "roles:assign"], version: 1 }];
  const writes: unknown[] = [];
  await page.route("**/api/identity/**", async route => {
    const request = route.request(), path = new URL(request.url()).pathname;
    if (path.endsWith("/capabilities")) return route.fulfill({ json: { capabilities: definitions } });
    if (request.method() === "POST") { const input = request.postDataJSON(); writes.push(input); const role = { ...input, id: "role_" + clientID, builtin: false, version: 1 }; roles.push(role); return route.fulfill({ json: role }); }
    if (request.method() === "PUT") { const input = request.postDataJSON(); writes.push(input); const index = roles.findIndex(role => role.id === path.split("/").at(-1)); roles[index] = { ...roles[index], ...input, version: input.version + 1 }; return route.fulfill({ json: roles[index] }); }
    if (path.endsWith("/roles")) return route.fulfill({ json: { roles } });
    return route.fulfill({ json: roles.find(role => role.id === path.split("/").at(-1)) });
  });
  await page.goto("/administration/roles"); await page.getByRole("link", { name: "Create custom role" }).click(); await expect(page).toHaveURL(/\/roles\/new$/);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByRole("checkbox", { name: "roles:manage", exact: true })).toBeDisabled(); await expect(page.getByRole("checkbox", { name: "roles:assign", exact: true })).toBeDisabled();
  await expect(page.getByRole("checkbox", { name: "tenants:provision", exact: true })).toHaveCount(0);
  await page.getByLabel("Role name", { exact: true }).fill("Risk Analyst"); await page.getByLabel("Add capabilities from a role", { exact: true }).selectOption("tenant_admin"); await page.getByRole("button", { name: "Add capabilities", exact: true }).click();
  await expect(page.getByRole("checkbox", { name: "cases:read", exact: true })).toBeChecked(); await expect(page.getByRole("checkbox", { name: "roles:manage", exact: true })).not.toBeChecked();
  await page.getByRole("button", { name: "Create role", exact: true }).first().click(); await expect(page.getByRole("heading", { name: "Risk Analyst", exact: true })).toBeVisible();
  expect(writes[0]).toEqual({ name: "Risk Analyst", capabilities: ["cases:read", "cases:review"], version: 0 });
  await page.getByRole("link", { name: "Edit role", exact: true }).click(); await expect(page).toHaveURL(new RegExp(`/roles/role_${clientID}/edit$`));
  await page.getByRole("checkbox", { name: "cases:review", exact: true }).uncheck(); await page.getByRole("button", { name: "Save changes", exact: true }).first().click();
  await expect(page.getByRole("heading", { name: "Risk Analyst", exact: true })).toBeVisible(); expect(writes[1]).toEqual({ name: "Risk Analyst", capabilities: ["cases:read"], version: 1 });
});

test("client inbox access can be granted and revoked", async ({ page }) => {
  let grants: string[] = []; const mutations: string[] = [];
  const client = { id: clientID, name: "Payments API", scopes: ["cases:read"], active: true, version: 1 };
  await page.route("**/api/identity/**", route => { const path = new URL(route.request().url()).pathname; const data = path.endsWith("/client-scopes") ? { capabilities: [] } : path.includes("scope-approvals") ? { approvals: [] } : path.includes("client-keys") ? { keys: [] } : path.endsWith(`/clients/${clientID}`) ? client : { clients: [client] }; return route.fulfill({ json: data }); });
  await page.route("**/api/cases/**", route => { const request = route.request(), path = new URL(request.url()).pathname; if (request.method() !== "GET") { mutations.push(request.method()); grants = request.method() === "PUT" ? [inboxID] : []; return route.fulfill({ status: 204 }); } return route.fulfill({ json: path.includes(`/clients/${clientID}`) ? { inbox_ids: grants } : { inboxes: [{ id: inboxID, name: "Payments", status: "active" }] } }); });
  await page.goto("/administration/api-clients"); await page.getByRole("button", { name: "Manage Payments API", exact: true }).click();
  const access = page.getByRole("checkbox", { name: "Payments", exact: true }); await access.check(); await expect(access).toBeChecked(); await expect(page.getByRole("status")).toHaveText("Inbox access granted.");
  await access.uncheck(); await expect(access).not.toBeChecked(); await expect(page.getByRole("status")).toHaveText("Inbox access revoked."); expect(mutations).toEqual(["PUT", "DELETE"]);
});

test("role editor supports searching, bulk selection, and discard confirmation", async ({ page }) => {
  await page.goto("/administration/roles/new"); await page.getByLabel("Role name", { exact: true }).fill("Unsaved review role");
  await page.getByRole("searchbox", { name: "Search capabilities", exact: true }).fill("cases:"); await page.getByRole("button", { name: "Select visible", exact: true }).click();
  await expect(page.getByRole("checkbox", { name: "cases:read", exact: true })).toBeChecked(); await page.getByRole("checkbox", { name: "Selected only", exact: true }).check();
  await page.getByRole("button", { name: "Clear visible", exact: true }).click(); await expect(page.getByText("No capabilities match these filters.")).toBeVisible();
  await page.getByRole("navigation", { name: "Account pages" }).getByRole("link", { name: "My account", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Discard role changes?" })).toBeVisible(); await page.getByRole("button", { name: "Keep editing", exact: true }).click();
  await expect(page.getByLabel("Role name", { exact: true })).toHaveValue("Unsaved review role");
});

test("failed inbox grant restores the server state and remains retryable", async ({ page }) => {
  const client = { id: clientID, name: "Payments API", scopes: ["cases:read"], active: true, version: 1 };
  await page.route("**/api/identity/**", route => {
    const path = new URL(route.request().url()).pathname;
    return route.fulfill({ json: path.endsWith("/client-scopes") ? { capabilities: [] } : path.includes("scope-approvals") ? { approvals: [] } : path.includes("client-keys") ? { keys: [] } : path.endsWith(`/clients/${clientID}`) ? client : { clients: [client] } });
  });
  await page.route("**/api/cases/**", route => {
    if (route.request().method() !== "GET") return route.fulfill({ status: 503, json: { error: "Grant unavailable" } });
    return route.fulfill({ json: route.request().url().includes(`/clients/${clientID}`) ? { inbox_ids: [] } : { inboxes: [{ id: inboxID, name: "Payments", status: "active" }] } });
  });
  await page.goto("/administration/api-clients"); await page.getByRole("button", { name: "Manage Payments API", exact: true }).click();
  const access = page.getByRole("checkbox", { name: "Payments", exact: true });
  await access.click();
  await expect(page.getByRole("alert").filter({ hasText: "Grant unavailable" })).toBeVisible();
  await expect(access).not.toBeChecked(); await expect(access).toBeEnabled(); await expect(page.getByRole("status")).toHaveCount(0);
});

test("investigator cannot open role administration or its editor", async ({ page, context }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  for (const path of ["/administration/roles", "/administration/roles/new", `/administration/roles/role_${clientID}/edit`]) { await page.goto(path); await expect(page.getByRole("alert").filter({ hasText: "Tenant Administrator" })).toBeVisible(); await expect(page.getByRole("button", { name: "Create role", exact: true })).toHaveCount(0); }
});
