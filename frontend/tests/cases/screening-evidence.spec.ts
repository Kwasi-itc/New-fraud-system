import { expect, test } from "@playwright/test";
import { screeningEvidence } from "../../src/lib/server/screening-evidence";

test("screening evidence allowlist removes owner secrets and raw payloads", () => {
  const projected = screeningEvidence({ screening: { ID: "run", Provider: "opensanctions", Status: "awaiting_review", LastError: "secret-error", ResponseJSON: { api_key: "secret-response" }, RequestJSON: { queries: [{ name: "Synthetic", password: "secret-query" }], provider_config: { threshold: 70, api_key: "secret-config" } } }, matches: [{ ID: "match", Name: "Synthetic candidate", Payload: { datasets: ["test"], properties: { country: ["gb"], api_key: ["secret-payload"] }, private: "secret-payload" } }] }, "run");
  expect(JSON.stringify(projected)).not.toContain("secret");
  expect(projected.screening.threshold).toBe(70);
  expect(projected.screening.queries).toEqual([{ name: "Synthetic" }]);
  expect(projected.matches[0].properties).toEqual({ country: ["gb"] });
});

test("screening evidence rejects an unexpected parent", () => {
  expect(() => screeningEvidence({ screening: { ID: "foreign" } }, "requested")).toThrow();
});

test("evidence gateway denies signed-out and foreign-tenant requests", async ({ context, request }) => {
  await request.post("http://127.0.0.1:3199/reset");
  const suffix = "/33333333-3333-4333-8333-333333333333/screenings/66666666-6666-4666-8666-666666666666";
  expect((await context.request.get("/api/case-evidence/11111111-1111-4111-8111-111111111111" + suffix)).status()).toBe(401);
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  const response = await context.request.get("/api/case-evidence/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" + suffix);
  expect(response.status()).toBe(403);
  expect(await response.text()).not.toContain("fixture-screening-owner-private-token");
});

test("authorized case evidence is projected and rendered as stored candidate cards", async ({ context, page, request }) => {
  await request.post("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  await request.post("http://127.0.0.1:3199/fixture/screening?match=1");
  const response = await context.request.get("/api/case-evidence/11111111-1111-4111-8111-111111111111/33333333-3333-4333-8333-333333333333/screenings/66666666-6666-4666-8666-666666666666");
  expect(response.status()).toBe(200);
  expect(await response.text()).not.toContain("fixture-secret");
  await page.goto("/cases/33333333-3333-4333-8333-333333333333");
  await page.getByRole("tab", { name: "Screening Matches", exact: true }).click();
  await page.getByRole("button", { name: "Inspect evidence", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Stored screening evidence" })).toBeVisible();
  await expect(page.locator("summary").filter({ hasText: "Candidate" })).toBeVisible();
});
