import type { WorkflowCondition } from "./workflow-condition-editor";

export type CaseRule = {
  id: string; name: string; enabled: boolean; outcomes: string[];
  condition?: WorkflowCondition;
  action_type: "create_case" | "add_to_case_if_possible" | "do_nothing";
  inbox_id: string; title: string; title_mode?: "template" | "";
  tag_ids: string[]; updated_at: string | null;
};
export const titleExamples: Record<string, string> = {
  object_id: "TX-1042", object_type: "transaction", decision_id: "decision-123",
  scenario_id: "scenario-123", outcome: "review", score: "75",
};
export function previewTitle(title: string): { text: string; error?: string } {
  let text = "";
  while (title.length) {
    const start = title.indexOf("%");
    if (start < 0) { text += title; break; }
    text += title.slice(0, start); title = title.slice(start + 1);
    const end = title.indexOf("%");
    if (end < 0) return { text, error: "Close each placeholder with %. Use %% for a literal percent sign." };
    const key = title.slice(0, end); title = title.slice(end + 1);
    if (key === "") text += "%";
    else if (Object.hasOwn(titleExamples, key)) text += titleExamples[key];
    else return { text, error: `Unknown placeholder: %${key}%` };
  }
  return { text: text.trim() };
}

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
function record(value: unknown): value is Record<string, unknown> { return !!value && typeof value === "object" && !Array.isArray(value); }
function strings(value: unknown, limit: number): value is string[] { return Array.isArray(value) && value.length <= limit && value.every(item => typeof item === "string" && item.length <= 200); }
function draftCondition(value: unknown, depth = 1, budget = { count: 0 }): boolean {
  if (!record(value) || depth > 4 || ++budget.count > 64) return false;
  if (value.kind === "all" || value.kind === "any") return Array.isArray(value.children) && value.children.every(child => draftCondition(child, depth + 1, budget));
  if (value.kind === "always" || value.kind === "never") return true;
  if (value.kind === "outcome_in") return strings(value.outcomes, 4) && value.outcomes.every(item => ["approve", "review", "block_and_review", "decline"].includes(item));
  return value.kind === "payload" && typeof value.field === "string" && value.field.length <= 200 && typeof value.operator === "string" && ["eq", "neq", "gt", "gte", "lt", "lte", "contains", "exists", "not_exists"].includes(value.operator) &&
    (value.value === undefined || value.value === null || typeof value.value === "boolean" || typeof value.value === "number" && Number.isFinite(value.value) || typeof value.value === "string" && value.value.length <= 2000);
}
function draftRule(value: unknown): value is CaseRule {
  return record(value) && typeof value.id === "string" && uuid.test(value.id) && typeof value.name === "string" && value.name.length <= 200 && typeof value.enabled === "boolean" &&
    strings(value.outcomes, 4) && (value.condition === undefined || draftCondition(value.condition)) &&
    ["create_case", "add_to_case_if_possible", "do_nothing"].includes(String(value.action_type)) &&
    typeof value.inbox_id === "string" && (value.inbox_id === "" || uuid.test(value.inbox_id)) &&
    typeof value.title === "string" && value.title.length <= 200 && [undefined, "", "template"].includes(value.title_mode as string | undefined) &&
    strings(value.tag_ids, 25) && value.tag_ids.every(id => uuid.test(id)) &&
    (value.updated_at === null || typeof value.updated_at === "string" && Number.isFinite(Date.parse(value.updated_at)));
}
export function readDrafts(key: string): CaseRule[] {
  const raw = localStorage.getItem(key);
  if (!raw) return [];
  if (raw.length > 2_000_000) throw new Error("Stored drafts are too large.");
  const data: unknown = JSON.parse(raw);
  if (!record(data) || data.version !== 1 || !Array.isArray(data.rules) || data.rules.length > 500 || !data.rules.every(draftRule) || new Set(data.rules.map(rule => rule.id)).size !== data.rules.length) throw new Error("Stored workflow drafts have an invalid format.");
  return data.rules;
}
export function writeDrafts(key: string, rules: CaseRule[]) {
  if (!rules.length) { localStorage.removeItem(key); return; }
  if (!rules.every(draftRule)) throw new Error("Complete or clear invalid draft values before saving drafts.");
  const raw = JSON.stringify({ version: 1, rules });
  if (rules.length > 500 || raw.length > 2_000_000) throw new Error("Draft storage limit reached.");
  localStorage.setItem(key, raw);
}
