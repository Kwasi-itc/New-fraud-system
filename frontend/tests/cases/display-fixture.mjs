const tenant = "11111111-1111-4111-8111-111111111111";
const table = "12121212-1212-4212-8212-121212121212";
const reference = "13131313-1313-4313-8313-131313131313";
const customer = "14141414-1414-4414-8414-141414141414";
const amount = "15151515-1515-4515-8515-151515151515";
const link = "16161616-1616-4616-8616-161616161616";
let enabled = false, deny = false, settings;
export function resetDisplay() { enabled = false; deny = false; settings = { table_id: table, name: "payment", version: 0, caption_field: "", displayed_fields: [], field_order: [reference, customer, amount], field_display: {}, fields: [{ id: reference, name: "reference", data_type: "string", enum_labels: {} }, { id: customer, name: "customer_id", data_type: "string", enum_labels: {} }, { id: amount, name: "amount", data_type: "float", enum_labels: {} }], links: [{ id: link, field_id: customer, name: "customer", target_table: "customer", target_field: "object_id", caption_field: "full_name" }] }; }
resetDisplay();
export async function handleDisplay(req, url, send) {
  if (url.pathname === "/fixture/display") { enabled = true; deny = url.searchParams.has("deny"); send(200, {}); return true; }
  if (!enabled || !url.pathname.startsWith("/v1/")) return false;
  const tenantPath = `/v1/tenants/${tenant}`;
  const settingsPath = `/v1/tables/${table}/display-settings`;
  if (![settingsPath, tenantPath, `${tenantPath}/tables`, `${tenantPath}/data-model`, `${tenantPath}/pivots`, `${tenantPath}/schema-change-log`, `${tenantPath}/index-jobs`, `${tenantPath}/record-displays`].includes(url.pathname)) return false;
  if (!["Bearer fixture-admin", "Bearer fixture-analyst"].includes(req.headers.authorization)) { send(403, { error: { message: "Current platform identity required" } }); return true; }
  if (url.pathname === settingsPath) {
    if (req.method === "PUT") {
      if (deny || req.headers.authorization !== "Bearer fixture-admin") { send(403, { error: { message: "Platform administrator required" } }); return true; }
      let raw = ""; for await (const chunk of req) raw += chunk; const body = JSON.parse(raw);
      if (body.version !== settings.version) { send(409, { error: { message: "Settings changed; reload" } }); return true; }
      settings = { ...settings, ...body, version: settings.version + 1 };
    }
    send(200, settings); return true;
  }
  if (url.pathname.endsWith("/record-displays")) {
    let raw = ""; for await (const chunk of req) raw += chunk; const { objects } = JSON.parse(raw);
    const selected = settings.field_order.filter(id => settings.displayed_fields.includes(id));
    send(200, { columns: { payment: selected.map(id => ({ id, label: settings.field_display[id]?.label || settings.fields.find(f => f.id === id).name, data_type: settings.fields.find(f => f.id === id).data_type })) }, records: objects.map(o => ({ ...o, caption: settings.caption_field ? `PAY-${o.object_id}` : o.object_id, found: true, values: Object.fromEntries(selected.map(id => [id, id === customer ? (settings.field_display[id]?.link_id ? "Ada Customer" : "c1") : id === amount ? 12.5 : "PAY-001"])) })) }); return true;
  }
  const modelTable = { id: table, name: "payment", alias: "Payments", caption_field: settings.caption_field, description: "", archived: false, fields: Object.fromEntries(settings.fields.map(f => [f.name, { ...f, nullable: false, is_enum: false, is_unique: false, archived: false, enum_values: [] }])), links_to_single: {}, navigation_options: [], options: null };
  if (url.pathname === tenantPath) send(200, { tenant: { id: tenant, name: "Test", status: "active" } });
  else if (url.pathname.endsWith("/tables")) send(200, { tables: [modelTable] });
  else if (url.pathname.endsWith("/data-model")) send(200, { data_model: { revision_id: String(settings.version), ingestion_contract: { writable: true, managed_system_fields: ["object_id", "updated_at"] }, tables: { payment: modelTable }, pivots: [] } });
  else send(200, { pivots: [], schema_changes: [], index_jobs: [] });
  return true;
}
