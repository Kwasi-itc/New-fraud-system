"use client";

import { useState } from "react";
import { ChevronDown, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export type WorkflowCondition = {
  kind: "all" | "any" | "always" | "never" | "outcome_in" | "payload";
  children?: WorkflowCondition[];
  outcomes?: string[];
  field?: string;
  operator?: string;
  value?: string | number | boolean | null;
};
export const outcomeLabels: Record<string, string> = {
  approve: "Approve", review: "Review", block_and_review: "Block and review", decline: "Decline",
};
const templates = [
  { kind: "always", label: "Always applies", description: "The condition always matches" },
  { kind: "never", label: "Never applies", description: "The condition never matches" },
  { kind: "outcome_in", label: "Outcome is in", description: "Match one or more decision outcomes" },
  { kind: "payload", label: "Payload field condition", description: "Compare a field on the evaluated object" },
] as const;
const control = "min-h-9 min-w-0 max-w-full rounded-xl border border-slate-200 bg-white px-3 py-1.5 text-sm text-slate-900";

export function conditionCount(node: WorkflowCondition): number {
  return 1 + (node.children ?? []).reduce((sum, child) => sum + conditionCount(child), 0);
}
export function validCondition(node: WorkflowCondition, depth = 1): boolean {
  if (depth > 4 || conditionCount(node) > 64) return false;
  if (node.kind === "all" || node.kind === "any") return !!node.children?.length && node.children.every(child => validCondition(child, depth + 1));
  if (node.kind === "outcome_in") return !!node.outcomes?.length;
  if (node.kind === "payload") return !!node.field?.trim() && (node.operator === "exists" || node.operator === "not_exists" || node.value !== undefined && (typeof node.value !== "number" || Number.isFinite(node.value)));
  return true;
}

function seed(kind: WorkflowCondition["kind"]): WorkflowCondition {
  if (kind === "outcome_in") return { kind, outcomes: [] };
  if (kind === "payload") return { kind, field: "", operator: "eq", value: "" };
  if (kind === "all" || kind === "any") return { kind, children: [] };
  return { kind };
}

export function WorkflowConditionEditor({ value, onChange, depth = 1, total, onRemove }: {
  value: WorkflowCondition; onChange: (value: WorkflowCondition) => void;
  depth?: number; total: number; onRemove?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const group = value.kind === "all" || value.kind === "any";
  const children = value.children ?? [];
  const remove = onRemove && <button type="button" aria-label={group ? "Remove condition group" : "Remove condition"} onClick={onRemove} className="ml-auto rounded-lg p-2 text-slate-500 hover:bg-slate-100"><Trash2 className="size-4" /></button>;
  if (group) return <section aria-label="Condition group" className="min-w-0 space-y-3 rounded-xl border border-slate-200 bg-slate-50/50 p-2 sm:p-4">
    <div className="flex flex-wrap items-center gap-2 text-sm"><span className="font-semibold">{depth === 1 ? "If" : "Group"}</span>
      <select aria-label="Group matching" value={value.kind} onChange={event => onChange({ ...value, kind: event.target.value as "all" | "any" })} className={control}><option value="all">All conditions (AND)</option><option value="any">Any condition (OR)</option></select>{remove}
    </div>
    {children.length === 0 && <p className="text-sm text-slate-500">Add a condition to choose which decisions match this rule.</p>}
    <div className="space-y-3 border-l-2 border-blue-100 pl-2 sm:pl-4">{children.map((child, index) => <div key={index} className="space-y-2">
      {index > 0 && <span className="text-xs font-semibold uppercase text-slate-500">{value.kind === "all" ? "And" : "Or"}</span>}
      <WorkflowConditionEditor value={child} depth={depth + 1} total={total} onChange={next => onChange({ ...value, children: children.map((item, i) => i === index ? next : item) })} onRemove={() => onChange({ ...value, children: children.filter((_, i) => i !== index) })} />
    </div>)}</div>
    <div className="flex flex-wrap gap-2">
      <div className="relative"><Button type="button" variant="outline" aria-expanded={open} disabled={depth >= 4 || total >= 64} onClick={() => setOpen(!open)} className="h-auto min-h-9 max-w-full flex-wrap whitespace-normal rounded-xl"><Plus className="size-4" />Add Condition<ChevronDown className="size-3" /></Button>
        {open && <div role="group" aria-label="Condition options" onKeyDown={event => { if (event.key === "Escape") setOpen(false); }} className="absolute left-0 top-full z-30 mt-2 w-64 max-w-[calc(100vw-7rem)] rounded-2xl border border-slate-200 bg-white p-2 shadow-xl">
          {templates.map(item => <button key={item.kind} type="button" onClick={() => { onChange({ ...value, children: [...children, seed(item.kind)] }); setOpen(false); }} className="block w-full rounded-xl p-3 text-left hover:bg-slate-50"><span className="block text-sm font-medium">{item.label}</span><span className="block text-xs text-slate-500">{item.description}</span></button>)}
        </div>}
      </div>
      <Button type="button" variant="outline" disabled={depth >= 3 || total >= 63} onClick={() => onChange({ ...value, children: [...children, seed("all")] })} className="h-auto min-h-9 max-w-full flex-wrap whitespace-normal rounded-xl"><Plus className="size-4" />Add group</Button>
    </div>
  </section>;

  const existence = value.operator === "exists" || value.operator === "not_exists";
  const numeric = ["gt", "gte", "lt", "lte"].includes(value.operator ?? "");
  const valueType = value.value === null ? "null" : typeof value.value === "number" ? "number" : typeof value.value === "boolean" ? "boolean" : "string";
  return <div className="min-w-0 space-y-3 rounded-xl border border-slate-200 bg-white p-3">
    <div className="flex items-center gap-2"><select aria-label="Condition type" className={`${control} flex-1`} value={value.kind} onChange={event => onChange(seed(event.target.value as WorkflowCondition["kind"]))}>{templates.map(item => <option key={item.kind} value={item.kind}>{item.label}</option>)}</select>{remove}</div>
    {value.kind === "outcome_in" && <div role="group" aria-label="Decision outcomes" className="flex flex-wrap gap-2">{Object.entries(outcomeLabels).map(([key, label]) => <label key={key} className="flex items-center gap-2 rounded-full border border-slate-200 px-3 py-1.5 text-sm"><input type="checkbox" className="accent-[#1f4f96]" checked={value.outcomes?.includes(key) ?? false} onChange={event => onChange({ ...value, outcomes: event.target.checked ? [...(value.outcomes ?? []), key] : value.outcomes?.filter(item => item !== key) })} />{label}</label>)}</div>}
    {value.kind === "payload" && <div className="space-y-2">
      <Input aria-label="Payload field" placeholder="Field name, e.g. amount" maxLength={200} value={value.field ?? ""} onChange={event => onChange({ ...value, field: event.target.value })} />
      <div className="flex flex-wrap gap-2"><select aria-label="Payload operator" className={control} value={value.operator} onChange={event => {
        const operator = event.target.value;
        const next = { ...value, operator };
        if (operator === "exists" || operator === "not_exists") delete next.value;
        else if (["gt", "gte", "lt", "lte"].includes(operator)) next.value = typeof value.value === "number" ? value.value : 0;
        else if (operator === "contains") next.value = typeof value.value === "string" ? value.value : "";
        else next.value = value.value ?? "";
        onChange(next);
      }}>{Object.entries({ eq: "Equals", neq: "Does not equal", gt: "Greater than", gte: "At least", lt: "Less than", lte: "At most", contains: "Contains text", exists: "Exists", not_exists: "Does not exist" }).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select>
      {!existence && <select aria-label="Value type" className={control} disabled={numeric || value.operator === "contains"} value={valueType} onChange={event => onChange({ ...value, value: ({ string: "", number: 0, boolean: true, null: null } as const)[event.target.value as "string" | "number" | "boolean" | "null"] })}><option value="string">Text</option><option value="number">Number</option><option value="boolean">Boolean</option><option value="null">Null</option></select>}</div>
      {!existence && valueType !== "null" && (valueType === "boolean" ? <select aria-label="Payload value" className={control} value={String(value.value)} onChange={event => onChange({ ...value, value: event.target.value === "true" })}><option value="true">True</option><option value="false">False</option></select> : <Input aria-label="Payload value" type={valueType === "number" ? "number" : "text"} step="any" maxLength={1000} value={typeof value.value === "number" && !Number.isFinite(value.value) ? "" : String(value.value ?? "")} onChange={event => onChange({ ...value, value: valueType === "number" ? event.target.valueAsNumber : event.target.value })} />)}
      <p className="text-xs text-slate-500">Use the exact payload field name. Missing fields only match “Does not exist”.</p>
    </div>}
  </div>;
}
