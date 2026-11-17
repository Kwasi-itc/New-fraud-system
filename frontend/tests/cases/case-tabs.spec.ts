import { expect, test } from "@playwright/test";

test("investigator navigates the case sections by keyboard with named active panels", async ({page,context,request}) => {
  await request.post("http://127.0.0.1:3199/reset");
  await context.addCookies([{name:"case_test_session",value:"fixture-analyst",domain:"127.0.0.1",path:"/"}]);
  await page.goto("/cases/33333333-3333-4333-8333-333333333333");
  const overview=page.getByRole("tab",{name:"Overview & Decisions",exact:true});
  await expect(overview).toHaveAttribute("aria-selected","true");
  await expect(page.getByRole("tabpanel")).toHaveCount(1);
  await overview.focus();
  await page.keyboard.press("ArrowLeft");
  const audit=page.getByRole("tab",{name:"Activity & Audit",exact:true});
  await expect(audit).toBeFocused();
  await expect(audit).toHaveAttribute("aria-selected","true");
  await expect(page.getByRole("tabpanel",{name:"Activity & Audit",exact:true})).toBeVisible();
  await expect(overview).toHaveAttribute("tabindex","-1");
  await page.keyboard.press("Home");
  await expect(overview).toBeFocused();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("tabpanel",{name:"Investigation & Connections",exact:true})).toBeVisible();
  await page.keyboard.press("End");
  await expect(audit).toBeFocused();
});
