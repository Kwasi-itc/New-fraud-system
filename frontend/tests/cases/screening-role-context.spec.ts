import { expect, test } from "@playwright/test";

test("MMDCE context shows the source position and municipality, retaining former roles and raw evidence", async ({ page, context, request }) => {
  await request.get("http://127.0.0.1:3199/reset");
  await context.addCookies([{ name: "case_test_session", value: "fixture-analyst", domain: "127.0.0.1", path: "/" }]);
  const generation = "11111111-1111-4111-8111-111111111111";
  const provider = { id: "internal-watchlists", name: "Internal watchlists", configured: true, health: "available", fields: { Person: ["name"] }, profile_fields: { "name-fuzzy-v1": { Person: ["name"] } }, entity_types: ["Person"], profiles: ["name-fuzzy-v1"], threshold_min: 50, threshold_max: 100, limit_max: 50, dataset_selection: true, limitations: [], datasets: [{ ref: { scope: "global", id: "gh-pep-local-gov" }, name: "Ghana local government PEPs", category: "pep_evidence", generation }] };
  await page.route("**/api/screening-search/capabilities", route => route.fulfill({ json: { ...provider, providers: [provider] } }));
  const id = "22222222-2222-4222-8222-222222222222";
  const classification = "Sub-National Domestic PEP (MMDCE / Regional Minister)";
  const candidate = (name: string, programs: string[], sourceStatus: string[]) => ({ id: name, entity_id: name, name, score: 1, schema: "Person", datasets: ["gh-pep-local-gov"], properties: { programs, sourceStatus, source: ["Authored browser fixture"], datasetVersion: [generation] } });
  const matches = [
    candidate("Fictional Ashaiman Official", ["Politically Exposed Person (PEP)", classification, "Active Office Holder", "Outdated fallback role"], ["Ghana Sub-National PEP; Region: Greater Accra; Role: Municipal Chief Executive, Ashaiman Municipal Assembly; Status: active"]),
    candidate("Fictional Former Official", ["Politically Exposed Person (PEP)", classification, "Cooling-off Period (Former Office Holder)"], ["Ghana Sub-National PEP; Role: Former Municipal Chief Executive, Ashaiman Municipal Assembly; Status: cooling_off"]),
    candidate("Fictional Legacy Official", ["Politically Exposed Person (PEP)", classification, "Active Office Holder", "District Chief Executive, Tolon District"], []),
    candidate("Fictional Unspecified Official", ["Politically Exposed Person (PEP)", classification, "Active Office Holder"], []),
  ];
  await page.route(`**/api/screening-search/${id}`, route => route.fulfill({ json: { id, status: "awaiting_review", partial: false, matches, query: { name: "Fictional", entity_type: "Person", provider: "internal-watchlists", profile: "name-fuzzy-v1", threshold: 70, limit: 10, dataset_selection: [{ scope: "global", id: "gh-pep-local-gov" }] } } }));
  await page.goto(`/screening-search?id=${id}`);
  const roleCell = (name: string) => page.getByRole("row").filter({ hasText: name }).getByRole("cell").nth(3);
  await expect(roleCell("Fictional Ashaiman Official")).toHaveText("Municipal Chief Executive, Ashaiman Municipal Assembly");
  await expect(roleCell("Fictional Former Official")).toHaveText("Former Municipal Chief Executive, Ashaiman Municipal Assembly");
  await expect(roleCell("Fictional Legacy Official")).toHaveText("District Chief Executive, Tolon District");
  await expect(roleCell("Fictional Unspecified Official")).toHaveText("Role not provided by source");
  await expect(roleCell("Fictional Ashaiman Official").locator("div")).not.toHaveClass(/truncate/);
  await expect(page.getByRole("row").filter({ hasText: "Fictional Former Official" }).getByRole("cell").nth(2)).toContainText("Cooling-Off");
  await page.getByRole("row").filter({ hasText: "Fictional Ashaiman Official" }).getByRole("button", { name: "Expand row" }).click();
  await expect(page.getByText(classification, { exact: true })).toBeVisible();
  await expect(page.getByText(generation, { exact: true })).toBeVisible();
});
