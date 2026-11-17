import { expect, test } from "@playwright/test";

test("private list upload, separate review and version restore use the BFF", async ({ context, page, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-private-importer", domain: "127.0.0.1", path: "/" }]);
  await page.goto("/screening-search/lists");
  await page.getByLabel("List name", { exact: true }).fill("Fictional review list");
  await page.getByLabel("Purpose", { exact: true }).fill("Test organization records");
  await page.getByLabel("Data usage authorization", { exact: true }).fill("Fictional data authored for test");
  await page.getByRole("button", { name: "Create list", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Fictional review list" })).toBeVisible();
  await page.getByLabel("CSV file", { exact: true }).setInputFiles({ name: "records.csv", mimeType: "text/csv", buffer: Buffer.from("record_id,name,entity_type,source_reference\n001,Fictional Company,Organization,own fixture\n") });
  await expect(page.getByRole("button", { name: "Submit for review" })).toBeDisabled();
  await page.getByRole("checkbox").check();
  await page.getByRole("button", { name: "Submit for review" }).click();
  await expect(page.getByText("pending review", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Approve and publish" })).toHaveCount(0);
  await page.getByRole("button", { name: "View changes" }).click();
  await expect(page.getByText('"record_id": "001"', { exact: false })).toBeVisible();
  await context.addCookies([{ name: "case_test_session", value: "fixture-private-reviewer", domain: "127.0.0.1", path: "/" }]);
  await page.reload();
  await page.getByRole("button", { name: /Fictional review list/ }).click();
  await page.getByRole("button", { name: "Approve and publish" }).click();
  await expect(page.getByText("published", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Propose restore" })).toBeVisible();
  await page.getByRole("button", { name: "Propose restore" }).click();
  await expect(page.getByText("pending review", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Approve and publish" })).toHaveCount(0);
});

test("private list BFF rejects foreign origins, signed-out access and unsupported paths", async ({ request }) => {
  expect((await request.post("/api/private-screening-lists", { headers: { origin: "https://foreign.invalid" }, data: {} })).status()).toBe(403);
  expect((await request.get("/api/private-screening-lists/capabilities")).status()).toBe(401);
  expect((await request.get("/api/private-screening-lists/global/operator")).status()).toBe(400);
});
