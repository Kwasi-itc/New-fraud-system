import type { IdentityCapability } from "@/lib/identity-api";

export const capabilityAreas = [
  { id: "all", name: "All capabilities", prefixes: [] as string[] },
  { id: "cases", name: "Cases & inboxes", prefixes: ["cases", "inboxes", "reports"] },
  { id: "detection", name: "Detection", prefixes: ["scenarios", "decisions", "workflows"] },
  { id: "data", name: "Data & ingestion", prefixes: ["models", "records", "ingest"] },
  { id: "screening", name: "Screening", prefixes: ["screening"] },
  { id: "sources", name: "Lists & sources", prefixes: ["lists", "sources", "private-lists", "source-notices"] },
  { id: "administration", name: "Account administration", prefixes: ["roles", "members", "invitations", "sessions", "audit", "clients"] },
  { id: "operations", name: "Operations", prefixes: ["executions", "metrics", "services"] },
];
export function capabilityArea(name: string) {
  return capabilityAreas.slice(1).find(area => area.prefixes.includes(name.split(":")[0])) ?? capabilityAreas[capabilityAreas.length - 1];
}
const subjects: Record<string, string> = { roles: "roles", members: "tenant members", invitations: "invitations", sessions: "application sessions", audit: "audit history", clients: "API clients", models: "the data model", records: "tenant records", ingest: "ingestion", scenarios: "scenarios", decisions: "decisions", workflows: "workflows", screening: "screening", inboxes: "inboxes", cases: "cases", reports: "reports", lists: "lists", sources: "sources", executions: "executions", metrics: "metrics", services: "service information", "private-lists": "private lists", "source-notices": "source notices" };
const actions: Record<string, string> = { read: "View", write: "Create and update", manage: "Manage", publish: "Publish", export: "Export", search: "Search", review: "Review", assign: "Assign", import: "Import", approve: "Approve", admin: "Administer" };
const titles: Record<string, string> = { "roles:manage": "Create and manage roles", "clients:approve-sensitive": "Approve sensitive client scopes", "models:index-write": "Manage data model indexes", "ingest:write": "Ingest tenant records", "decisions:evaluate": "Request decision evaluations", "cases:access-all": "Access all tenant cases", "cases:read": "View permitted cases", "cases:review": "Review case evidence", "inboxes:read": "View permitted inboxes" };
export function capabilityTitle(capability: Pick<IdentityCapability, "name">) {
  if (titles[capability.name]) return titles[capability.name];
  const [subject, action] = capability.name.split(":");
  return subjects[subject] && actions[action] ? `${actions[action]} ${subjects[subject]}` : capability.name;
}
