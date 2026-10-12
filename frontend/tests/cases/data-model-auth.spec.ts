import { expect, test } from "@playwright/test";
const tenant = "11111111-1111-4111-8111-111111111111";
test.beforeEach(async ({ request }) => { await request.get("http://127.0.0.1:3199/reset"); });

test("Data Model proxy requires a session and rejects foreign-origin writes", async ({ request }) => {
  expect((await request.get(`/api/data-model/tenants/${tenant}/tables`)).status()).toBe(401);
  expect((await request.post(`/api/data-model/tenants/${tenant}/tables`, { headers: { Origin: "https://foreign.example" }, data: {} })).status()).toBe(403);
});

test("Data Model proxy forwards original identity and relays owner denials", async ({ context, page }) => {
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  expect((await page.request.get(`/api/data-model/tenants/${tenant}/tables`)).status()).toBe(403);
  await context.addCookies([{ name: "case_test_session", value: "fixture-platform", domain: "127.0.0.1", path: "/", httpOnly: true, sameSite: "Lax" }]);
  const response = await page.request.get(`/api/data-model/tenants/${tenant}/tables`, { headers: { Authorization: "Bearer caller-supplied-machine-token" } });
  expect(response.status()).toBe(200);
  expect(response.headers()["cache-control"]).toContain("no-store");
  expect(await response.json()).toEqual({ tables: [] });
  expect((await page.request.get("/api/data-model/tenants/22222222-2222-4222-8222-222222222222/tables")).status()).toBe(403);
  expect((await page.request.get("/api/data-model/tenants%2Fforeign/tables")).status()).toBe(400);
  await page.request.post("/api/auth/logout", { headers: { Origin: "http://127.0.0.1:3100" } });
  expect((await page.request.get(`/api/data-model/tenants/${tenant}/tables`)).status()).toBe(401);
});
