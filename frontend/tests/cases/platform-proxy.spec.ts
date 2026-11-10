import { expect, test } from "@playwright/test";
const tenant = "11111111-1111-4111-8111-111111111111";
const foreign = "99999999-9999-4999-8999-999999999999";
test.beforeEach(async ({request}) => { await request.get("http://127.0.0.1:3199/reset"); });
for (const service of ["decisions","ingestion"]) {
  test(`${service} gateway enforces session, role, tenant, origin and revocation`,async({context,page})=>{
    const own=`/api/${service}/tenants/${tenant}/integration-fixture`;
    expect((await page.request.get(own)).status()).toBe(401);
    await context.addCookies([{name:"case_test_session",value:"fixture-analyst",domain:"127.0.0.1",path:"/"}]);
    expect((await page.request.get(own)).status()).toBe(403);
    await context.addCookies([{name:"case_test_session",value:"fixture-workflow",domain:"127.0.0.1",path:"/"}]);
    const allowed=await page.request.get(own,{headers:{Authorization:"Bearer forged-browser-token"}});
    expect(allowed.status()).toBe(200);
    expect(allowed.headers()["cache-control"]).toContain("no-store");
    expect(await allowed.json()).toEqual({verified:true});
    expect((await page.request.get(`/api/${service}/tenants/${foreign}/integration-fixture`)).status()).toBe(403);
    expect((await page.request.post(own,{headers:{Origin:"https://foreign.example"},data:{}})).status()).toBe(403);
    expect((await page.request.get(`/api/${service}/tenants/${tenant}/screening-executions`)).status()).toBe(403);
    expect((await page.request.get(`/api/${service}/tenants%2Fforeign/anything`)).status()).toBe(400);
    await page.request.post("/api/auth/logout",{headers:{Origin:"http://127.0.0.1:3100"}});
    expect((await page.request.get(own)).status()).toBe(401);
  });
}
