// Browser contract fixture; database authorization is tested in Go/PostgreSQL.
let rules = new Map(), conflict = false, addressSupported = true, localProvider = false;
export async function handleScenarioScreening(req, url, send) {
  if (url.pathname === "/reset") { rules = new Map(); conflict = false; addressSupported = true; localProvider = false; return false; }
  if (url.pathname === "/fixture/scenario-screening-conflict") { conflict = true; send(204); return true; }
  if (url.pathname === "/fixture/scenario-address-unavailable") { addressSupported = false; send(204); return true; }
  if (url.pathname === "/fixture/scenario-local-provider") { localProvider = true; addressSupported = false; send(204); return true; }
  if (!url.pathname.startsWith("/internal/screening-authoring/")) return false;
  const reply = (status, data) => { send(status, data); return true; };
  if (req.headers.authorization !== "Bearer fixture-screening-authoring-private-token-32" || req.headers["x-screening-user"] !== "analyst" || req.headers["x-screening-version"] !== "1") return reply(401, { error: "Invalid gateway identity" });
  if (url.pathname.includes("/decisions/")) {
    if (req.headers["x-screening-session"] !== "fixture-analyst") return reply(403, { error: "private-session-error" });
    if (url.pathname !== "/internal/screening-authoring/tenants/11111111-1111-4111-8111-111111111111/decisions/44444444-4444-4444-8444-444444444444/screenings") return reply(403, { error: "private-ownership-error" });
    return reply(200, { screening_executions: [] });
  }
  if (!url.pathname.startsWith("/internal/screening-authoring/tenants/11111111-1111-4111-8111-111111111111/scenarios/")) return reply(403, { error: "Wrong tenant" });
  if (req.method === "GET") return reply(200, { scenario_name: "Payments", inline_supported: true, ...(localProvider ? {provider_name:"Synthetic development catalog",threshold_supported:true,result_limit_supported:true,dataset_selection_supported:false,default_threshold:80,default_result_limit:50} : {}), name_recognition_supported: addressSupported, ignore_lists: [{id:"77777777-7777-4777-8777-777777777777",name:"Company suffixes"}], matching_settings_supported: addressSupported, address_supported: addressSupported, birth_date_supported: addressSupported, nationality_supported: addressSupported, country_supported: addressSupported, registration_number_supported: addressSupported, rules: [...rules.values()], fields: ["first_name", "last_name", "beneficiary_name", "company_name", "postal_address", "birth_date", "nationality", "country", "registration"], other_rules: 0 });
  let raw = ""; for await (const chunk of req) raw += chunk;
  const body = JSON.parse(raw);
  if (!url.pathname.endsWith(`/configs/${body.id}`) || !body.inbox_id || !["Person", "Organization"].includes(body.entity_type) || !["first_name", "last_name", "beneficiary_name", "company_name"].includes(body.name_field) || (!body.outcomes.length && !body.forced_outcome) || (body.address_field && (body.address_field !== "postal_address" || !addressSupported))) return reply(400, { error: "Invalid mapping" });
  if (body.birth_date_field && (body.entity_type !== "Person" || body.birth_date_field !== "birth_date" || !addressSupported)) return reply(422, { error: "Unsupported birth-date mapping" });
  if (body.nationality_field && (body.entity_type !== "Person" || body.nationality_field !== "nationality" || !addressSupported)) return reply(422, { error: "Unsupported nationality mapping" });
  if (body.country_field && (body.entity_type !== "Organization" || body.country_field !== "country" || !addressSupported)) return reply(422, { error: "Unsupported country mapping" });
  if (body.registration_number_field && (body.entity_type !== "Organization" || body.registration_number_field !== "registration" || !addressSupported)) return reply(422, { error: "Unsupported registration mapping" });
  const old = rules.get(body.id);
  if (conflict || (old && old.updated_at !== body.updated_at)) { conflict = false; return reply(409, { error: "Configuration changed. Refresh before saving." }); }
  const saved = { ...body, updated_at: new Date().toISOString() };
  rules.set(body.id, saved);
  return reply(200, saved);
}
