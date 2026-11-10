import "server-only";
import type { AssembledDataModel } from "@/lib/data-model-api";

// Legacy owner URLs use resource IDs without a tenant prefix. Resource tenant
// identities are immutable; only IDs in the current tenant's owner publication
// may be forwarded. Never authorize from caller-supplied metadata.
export function ownsDataModelResource(model: AssembledDataModel, path: string[]) {
  const [kind, id, child] = path;
  const supported = (kind === "tables" && (!child || ["fields", "display-settings"].includes(child))) ||
    (kind === "fields" && (!child || child === "enum-values")) || (kind === "enum-values" && !child);
  if (!supported || path.length > (child ? 3 : 2)) return false;
  for (const table of Object.values(model.tables)) {
    if (kind === "tables" && table.id === id) return true;
    for (const field of Object.values(table.fields)) {
      if (kind === "fields" && field.id === id) return true;
      if (kind === "enum-values" && field.enum_values.some(value => value.id === id)) return true;
    }
  }
  return false;
}
