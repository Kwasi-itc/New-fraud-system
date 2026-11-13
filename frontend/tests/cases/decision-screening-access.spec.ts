import { expect, test } from "@playwright/test";

const path = "/api/decision-screening/44444444-4444-4444-8444-444444444444";
test.beforeEach(async ({ request }) => {
  await request.get("http://127.0.0.1:3199/reset");
});
test("gateway requires a session and ignores supplied actor and tenant", async ({ request }) => {
  expect((await request.get(path)).status()).toBe(401);
  const response = await request.get(`${path}?tenant=foreign&user=foreign`, {
    headers: { Cookie: "case_test_session=fixture-analyst", "X-Screening-User": "foreign", "X-Screening-Version": "900" },
  });
  expect(response.status()).toBe(200);
  expect(response.headers()["cache-control"]).toContain("no-store");
  expect(await response.json()).toEqual({ screening_executions: [] });
});
test("foreign decision is denied without leaking backend errors", async ({ request }) => {
  const response = await request.get("/api/decision-screening/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", {
    headers: { Cookie: "case_test_session=fixture-analyst" },
  });
  expect(response.status()).toBe(403);
  expect(await response.text()).not.toContain("private-ownership-error");
  expect((await request.get("/api/decision-screening/invalid", { headers: { Cookie: "case_test_session=fixture-analyst" } })).status()).toBe(400);
});
