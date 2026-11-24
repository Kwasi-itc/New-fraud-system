import { expect, test } from "@playwright/test";

test("account pages retain usable navigation and actions on desktop and mobile", async ({ page, context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-admin", domain: "127.0.0.1", path: "/" }]);
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 });
    for (const [name, path] of [["profile", "/my-account"], ["users", "/administration/users"], ["roles", "/administration/roles"], ["role-editor", "/administration/roles/new"], ["clients", "/administration/api-clients"]]) {
      await page.goto(path);
      const navigation = page.getByRole("navigation", { name: "Account pages" });
      await expect(navigation.getByRole("link")).toHaveCount(4);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      if (name === "profile") {
        await expect(page.getByRole("button", { name: "Manage sign-in", exact: true })).toBeVisible();
        await expect(page.getByRole("button", { name: "Revoke", exact: true })).toBeVisible();
      }
      if (name === "role-editor") await expect(page.getByRole("checkbox", { name: "roles:manage", exact: true })).toBeDisabled();
      await page.screenshot({ path: test.info().outputPath(`${name}-${width}.png`), fullPage: true });
    }
  }
});
