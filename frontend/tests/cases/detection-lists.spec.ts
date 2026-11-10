import {expect,test} from "@playwright/test";

const tenant="11111111-1111-4111-8111-111111111111";
test("Lists renders owner counts without fetching every entry",async({context,page})=>{
  await context.addCookies([{name:"case_test_session",value:"fixture-workflow",domain:"127.0.0.1",path:"/"}]);
  const entryRequests:string[]=[];
  await page.route("**/api/decisions/**/platform/custom-list-entries**",route=>{
    entryRequests.push(route.request().url());
    return route.fulfill({status:400,json:{error:"invalid_execution_request"}});
  });
  await page.route(`**/api/decisions/tenants/${tenant}/platform/custom-lists`,route=>route.fulfill({json:{custom_lists:[
    {id:"empty",tenant_id:tenant,name:"Empty list",kind:"generic_text",description:"Empty",entry_count:0},
    {id:"one",tenant_id:tenant,name:"Single entry",kind:"generic_text",description:"One",entry_count:1},
    {id:"large",tenant_id:tenant,name:"Large list",kind:"generic_text",description:"Large",entry_count:2501},
  ]}}));
  await page.goto("/detection?tab=Lists");
  await expect(page.getByText("Empty list",{exact:true})).toBeVisible();
  await expect(page.getByText("0 values",{exact:true})).toBeVisible();
  await expect(page.getByText("1 value",{exact:true})).toBeVisible();
  await expect(page.getByText("2501 values",{exact:true})).toBeVisible();
  await expect(page.getByText("invalid_execution_request",{exact:true})).toHaveCount(0);
  expect(entryRequests).toEqual([]);
});
