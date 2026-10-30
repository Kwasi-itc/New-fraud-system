import { expect, test } from "@playwright/test";

test.beforeEach(async ({ context, request }) => {
  await request.post("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
});

test("stored report renders provenance and review states without rerunning and prints only report content", async ({ page }) => {
  let submissions = 0;
  page.on("request", req => { if (req.method() === "POST" && req.url().endsWith("/api/screening-search")) submissions++; });
  await page.goto("/screening-search");
  const name = "<img src=x onerror=alert(1)> Synthetic";
  await page.getByLabel("Name", { exact: true }).fill(name);
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.getByRole("link", { name: "View printable report" }).click();
  await expect(page.getByRole("heading", { name: "2 stored candidates" })).toBeVisible();
  await expect(page.getByText("opensanctions", { exact: true })).toBeVisible();
  await expect(page.getByText(name, { exact: false })).toBeVisible();
  await expect(page.getByText(/Review: pending/)).toBeVisible();
  await expect(page.getByText(/Review: no hit/)).toBeVisible();
  expect(await page.locator("img").count()).toBe(0);
  expect(submissions).toBe(1);
  await page.evaluate(() => { window.print = () => { document.documentElement.dataset.printRequested = "true"; }; });
  await page.getByRole("button", { name: "Print report" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-print-requested", "true");
  await page.emulateMedia({ media: "print" });
  await expect(page.getByRole("button", { name: "Print report" })).toBeHidden();
  await expect(page.getByRole("link", { name: "Back to screening" })).toBeHidden();
  await expect(page.getByRole("heading", { name: "Candidate One" })).toBeVisible();
  const pdf = await page.pdf({ format: "A4" });
  expect(pdf.subarray(0, 4).toString()).toBe("%PDF");
  expect(pdf.length).toBeGreaterThan(1000);
});

for (const name of ["Partial", "Nobody"]) {
  test(`report preserves ${name} semantics`, async ({ page }) => {
    await page.goto("/screening-search");
    await page.getByLabel("Name", { exact: true }).fill(name);
    await page.getByRole("button", { name: "Search", exact: true }).click();
    await page.getByRole("link", { name: "View printable report" }).click();
    await expect(page.getByRole("heading", { name: "0 stored candidates" })).toBeVisible();
    await expect(page.getByText(name === "Partial" ? "Incomplete results. Additional candidates may exist; this report does not establish a negative screening." : "No matches returned for these search settings.", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Print report" })).toBeEnabled();
    if (name === "Partial") await expect(page.getByText("awaiting review", { exact: true })).toBeVisible();
  });
}

test("report denies unavailable IDs without leaking response details and supports transient retry", async ({ page }) => {
  const id = "77777777-7777-4777-8777-777777777777";
  let calls = 0;
  await page.route(`**/api/screening-search/${id}`, route => {
    calls++;
    return route.fulfill({ status: calls === 1 ? 503 : 404, json: { error: "secret-provider-address" } });
  });
  await page.goto(`/screening-search/${id}/report`);
  await expect(page.getByText("The report could not be loaded.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Retry report" }).click();
  await expect(page.getByText("This search is unavailable or you do not have access.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Print report" })).toBeDisabled();
  await expect(page.getByText("secret-provider-address")).toHaveCount(0);
});

test("report requires a session", async ({ context, page }) => {
  await context.clearCookies();
  await page.goto("/screening-search/77777777-7777-4777-8777-777777777777/report");
  await expect(page).toHaveURL(/\/login/);
});

for (const status of ["pending", "failed", "archived"]) {
  test(`a ${status} search cannot be printed as a completed results report`, async ({ page }) => {
    const id = "77777777-7777-4777-8777-777777777777";
    await page.route(`**/api/screening-search/${id}`, route => route.fulfill({ json: { id, status, matches: [] } }));
    await page.goto(`/screening-search/${id}/report`);
    await expect(page.getByText("This search is not ready for a results report. Return to screening to check its status.")).toBeVisible();
    await expect(page.getByRole("button", { name: "Print report" })).toBeDisabled();
    await expect(page.getByText("No matches returned for these search settings.")).toHaveCount(0);
  });
}

test("current permission denial prevents report rendering", async ({ page }) => {
  const id = "77777777-7777-4777-8777-777777777777";
  await page.route(`**/api/screening-search/${id}`, route => route.fulfill({ status: 403, json: { error: "private-membership-detail" } }));
  await page.goto(`/screening-search/${id}/report`);
  await expect(page.getByText("This search is unavailable or you do not have access.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Print report" })).toBeDisabled();
  await expect(page.getByText("private-membership-detail")).toHaveCount(0);
});
