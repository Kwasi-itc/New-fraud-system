import { expect, test } from "@playwright/test";

test.beforeEach(async ({ request, context }) => {
  await request.post("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  await request.post("http://127.0.0.1:3199/fixture/screening?match=1&archived_exclusion=1");
});

test("case exclusion history paginates, exposes scoped applications and supports removal", async ({ page, request }) => {
  await request.post("http://127.0.0.1:3199/fixture/exclusions?mode=filled");
  await page.goto("/cases/33333333-3333-4333-8333-333333333333");
  await page.getByRole("tab", { name: "Screening Matches", exact: true }).click();
  const panel = page.getByRole("region", { name: "Exclusion management", exact: true });
  await panel.getByRole("button", { name: "Manage exclusions" }).click();
  await expect(panel.getByText("Customer: synthetic-customer")).toBeVisible();
  await expect(panel.getByText("Different identity <script>never execute</script>")).toBeVisible();
  await expect(panel.locator("script")).toHaveCount(0);
  await panel.getByRole("button", { name: "Next", exact: true }).click();
  await expect(panel.getByText("Customer: older-customer")).toBeVisible();
  await panel.getByRole("button", { name: "Previous", exact: true }).click();
  await expect(panel.getByText("Customer: synthetic-customer")).toBeVisible();
  await panel.getByRole("button", { name: "View suppression history" }).click();
  await expect(panel.getByRole("region", { name: "Suppression history", exact: true }).getByText("Candidate: suppressed-match")).toBeVisible();
  await panel.getByRole("button", { name: "Review match", exact: true }).click();
  await panel.getByLabel("Review action", { exact: true }).selectOption("remove_exclusion");
  await panel.getByLabel("Review findings").fill("Remove exclusion for future evaluation");
  await panel.getByRole("button", { name: "Check review" }).click();
  await panel.getByRole("button", { name: "Confirm review" }).click();
  await expect(panel.getByText("Currently included", { exact: true })).toBeVisible();
  await expect(panel.getByRole("button", { name: "Review match" })).toHaveCount(0);
});

test("history has empty, denied and retry states", async ({ page, request }) => {
  await request.post("http://127.0.0.1:3199/fixture/exclusions?mode=failed");
  await page.goto("/cases/33333333-3333-4333-8333-333333333333");
  await page.getByRole("tab", { name: "Screening Matches", exact: true }).click();
  const panel = page.getByRole("region", { name: "Exclusion management", exact: true });
  await panel.getByRole("button", { name: "Manage exclusions" }).click();
  await expect(panel.getByText("Exclusion history is temporarily unavailable.")).toBeVisible();
  await request.post("http://127.0.0.1:3199/fixture/exclusions?mode=denied");
  await panel.getByRole("button", { name: /retry/i }).click();
  await expect(panel.getByText("Current case/resource access is required.")).toBeVisible();
  await expect(panel.getByText("synthetic-customer", { exact: false })).toHaveCount(0);
  await request.post("http://127.0.0.1:3199/fixture/exclusions?mode=empty");
  await panel.getByRole("button", { name: /retry/i }).click();
  await expect(panel.getByText("No exclusion actions recorded in this case.")).toBeVisible();
});
