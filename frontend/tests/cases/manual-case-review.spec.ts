import { expect, test } from "@playwright/test";

test.beforeEach(async ({ context, request }) => {
  await request.post("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  await request.post("http://127.0.0.1:3199/fixture/screening?match=1&manual=1");
});

test("continuous candidate can be excluded and the exclusion removed through explicit reviewed actions", async ({ page, request }) => {
  await request.post("http://127.0.0.1:3199/fixture/screening?match=1");
  await page.goto("/cases/33333333-3333-4333-8333-333333333333");
  await page.getByRole("tab", { name: "Screening Matches", exact: true }).click();
  await page.getByRole("button", { name: "Inspect evidence", exact: true }).click();
  await page.getByRole("button", { name: "Review match", exact: true }).click();
  await page.getByLabel("Review action", { exact: true }).selectOption("dismiss_and_exclude");
  await expect(page.getByText("Suppressed candidates remain stored.", { exact: false })).toBeVisible();
  await page.getByLabel("Review findings").fill("Different synthetic identity; exclude unchanged query only");
  await page.getByRole("button", { name: "Check review", exact: true }).click();
  await page.getByRole("button", { name: "Confirm review", exact: true }).click();
  await expect(page.getByText("Review status: no hit", { exact: true })).toBeVisible();
  await page.getByLabel("Review action", { exact: true }).selectOption("remove_exclusion");
  await expect(page.getByText("removing an exclusion does not trigger a new search.", { exact: false })).toBeVisible();
  await page.getByLabel("Review findings").fill("Reassess future results");
  await page.getByRole("button", { name: "Check review", exact: true }).click();
  await page.getByRole("button", { name: "Confirm review", exact: true }).click();
  await expect(page.getByLabel("Review action", { exact: true })).toHaveCount(0);
});

test("manual freeform review offers no exclusion control", async ({ page }) => {
  await page.goto("/cases/33333333-3333-4333-8333-333333333333");
  await page.getByRole("tab", { name: "Screening Matches", exact: true }).click();
  await page.getByRole("button", { name: "Inspect evidence", exact: true }).click();
  await page.getByRole("button", { name: "Review match", exact: true }).click();
  await expect(page.getByLabel("Review action", { exact: true })).toBeVisible();
  await expect(page.locator('option[value="dismiss_and_exclude"]')).toHaveCount(0);
});

test("archived-source exclusion removal is explicit and retries the same operation", async ({ page, request }) => {
  await request.post("http://127.0.0.1:3199/fixture/screening?match=1&archived_exclusion=1");
  await request.post("http://127.0.0.1:3199/fixture/review?lost_ack=1");
  const commands: unknown[] = [];
  page.on("request", req => {
    if (req.url().includes("/api/case-review/") && req.method() === "POST") commands.push(req.postDataJSON());
  });
  await page.goto("/cases/33333333-3333-4333-8333-333333333333");
  await page.getByRole("tab", { name: "Screening Matches", exact: true }).click();
  await page.getByRole("button", { name: "Inspect evidence", exact: true }).click();
  await page.getByRole("button", { name: "Review match", exact: true }).click();
  await page.getByLabel("Review action", { exact: true }).selectOption("remove_exclusion");
  await expect(page.getByText("including archived screenings.", { exact: false })).toBeVisible();
  await expect(page.locator('option[value="confirmed_hit"]')).toHaveCount(0);
  await page.getByLabel("Review findings").fill("Reassess future results without changing archived evidence");
  await page.getByRole("button", { name: "Check review", exact: true }).click();
  await expect(page.getByText("Confirm removal for future screenings. Historical screening results will not change.")).toBeVisible();
  await page.getByRole("button", { name: "Confirm review", exact: true }).click();
  await expect(page.getByText("Review response unavailable. Retry the same operation.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Confirm review", exact: true }).click();
  await expect(page.getByLabel("Review action", { exact: true })).toHaveCount(0);
  expect(commands).toHaveLength(2);
  expect(commands[0]).toEqual(commands[1]);
  await expect(page.getByText("Review status: no hit", { exact: true })).toBeVisible();
});

test("exclusion findings reserve audit-prefix space before submitting", async ({ page, request }) => {
  await request.post("http://127.0.0.1:3199/fixture/screening?match=1&archived_exclusion=1");
  await page.goto("/cases/33333333-3333-4333-8333-333333333333");
  await page.getByRole("tab", { name: "Screening Matches", exact: true }).click();
  await page.getByRole("button", { name: "Inspect evidence", exact: true }).click();
  await page.getByRole("button", { name: "Review match", exact: true }).click();
  await page.getByLabel("Review action", { exact: true }).selectOption("remove_exclusion");
  await page.getByLabel("Review findings").fill("é".repeat(4951));
  await page.getByRole("button", { name: "Check review", exact: true }).click();
  await expect(page.getByText("Choose an action and enter findings of 1–9900 UTF-8 bytes.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Confirm review", exact: true })).toHaveCount(0);
});

for (const action of ["confirmed_hit", "no_hit"]) {
  test(`manual investigation records ${action} with a retry-safe command`, async ({ page, request }) => {
    await request.post("http://127.0.0.1:3199/fixture/review?lost_ack=1");
    const commands: unknown[] = [];
    page.on("request", req => {
      if (req.url().includes("/api/case-review/") && req.method() === "POST") commands.push(req.postDataJSON());
    });
    await page.goto("/cases/33333333-3333-4333-8333-333333333333");
    await page.getByRole("tab", { name: "Screening Matches", exact: true }).click();
    await page.getByRole("button", { name: "Inspect evidence", exact: true }).click();
  await page.getByRole("button", { name: "Review match", exact: true }).click();
    await page.getByLabel("Review action", { exact: true }).selectOption(action);
    await page.getByLabel("Review findings").fill("Synthetic identity investigation");
    await page.getByRole("button", { name: "Check review", exact: true }).click();
    await page.getByRole("button", { name: "Confirm review", exact: true }).click();
    await expect(page.getByText("Review response unavailable. Retry the same operation.", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Confirm review", exact: true }).click();
    await expect(page.getByText("Review recorded with case history.")).toBeVisible();
    await expect(page.getByText(`Review status: ${action.replaceAll("_", " ")}`, { exact: true })).toBeVisible();
    expect(commands).toHaveLength(2);
    expect(commands[0]).toEqual(commands[1]);
    await expect(page.getByRole("button", { name: "Reopen investigation", exact: true })).toHaveCount(0);
  });
}
