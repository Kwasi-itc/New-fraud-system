"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { ArrowLeft, CircleSlash2, Info, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { requestCase, type Inbox, type Tag } from "@/lib/case-manager-api";
import { cn } from "@/lib/utils";

import { WorkflowConditionEditor, conditionCount, validCondition } from "./workflow-condition-editor";

import { readDrafts, writeDrafts, previewTitle, titleExamples, type CaseRule } from "./workflow-drafts";

type WorkflowPage = {
  tenant_id: string;
  user_id: string;
  can_edit: boolean;
  scenario_name: string;
  rules: CaseRule[];
  other_rules: number;
  legacy_workflows: boolean;
};
const actions = [
  { value: "do_nothing", label: "Do nothing", description: "Stop at this matching rule without taking an action" },
  { value: "create_case", label: "Create case", description: "Create a new case in the specified inbox" },
  { value: "add_to_case_if_possible", label: "Add to case if possible", description: "Add to an eligible case for the same object, or create a new one" },
] as const;
const primaryButton = "h-10 rounded-xl bg-[#1f4f96] px-5 text-[14px] text-white shadow-none hover:bg-[#163f79]";
const outlineButton = "h-9 rounded-xl border-slate-200 px-3.5 text-[13px] shadow-none";

async function workflowRequest<T>(scenario: string, rule?: CaseRule, method = rule ? "PUT" : "GET"): Promise<T> {
  const response = await fetch(`/api/workflows/scenarios/${encodeURIComponent(scenario)}${rule ? `/rules/${rule.id}` : ""}`, {
    method,
    body: rule ? JSON.stringify(rule) : undefined,
    headers: { "Content-Type": "application/json" },
    cache: "no-store", credentials: "same-origin",
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error ?? "Workflow request failed.");
  return data;
}

function WorkflowRuleCard({ rule, saved, inboxes, tags, disabled, dirty, saving, canSave, onChange, onSave, onCancel, onDelete }: {
  rule: CaseRule;
  saved?: CaseRule;
  inboxes: Inbox[];
  tags: Tag[];
  disabled: boolean;
  dirty: boolean;
  saving: boolean;
  canSave: boolean;
  onChange: (rule: CaseRule) => void;
  onSave: () => void;
  onCancel: () => void;
  onDelete: () => void;
}) {
  const [menu, setMenu] = useState<"action" | null>(null);
  const [tagSearch, setTagSearch] = useState("");
  const [tagsOpen, setTagsOpen] = useState(false);
  const root = useRef<HTMLFormElement>(null);
  useEffect(() => {
    function dismiss(event: PointerEvent) {
      if (!root.current?.contains(event.target as Node)) setMenu(null);
    }
    document.addEventListener("pointerdown", dismiss);
    return () => document.removeEventListener("pointerdown", dismiss);
  }, []);
  const condition = rule.condition ?? { kind: "all" as const, children: rule.outcomes.length ? [{ kind: "outcome_in" as const, outcomes: rule.outcomes }] : [] };
  const activeInboxes = inboxes.filter(inbox => inbox.status === "active");
  const titlePreview = previewTitle(rule.title);
  const ActionIcon = rule.action_type === "do_nothing" ? CircleSlash2 : Plus;
  const action = actions.find(item => item.value === rule.action_type)!;
  const filteredTags = tags.filter(tag => tag.name.toLowerCase().includes(tagSearch.toLowerCase()));
  const selectedTags = rule.tag_ids.map(id => ({ id, name: tags.find(tag => tag.id === id)?.name ?? "Unavailable tag" }));

  return (
    <form ref={root} aria-label={`Workflow rule ${saved?.name ?? "New Rule"}`} onKeyDown={event => {
      if (event.key === "Escape") setMenu(null);
    }} onSubmit={event => { event.preventDefault(); if (canSave && !disabled) onSave(); }}>
      <fieldset disabled={disabled} className="min-w-0 space-y-4">
        <legend className="sr-only">Workflow rule</legend>
        <article className="grid gap-3 xl:grid-cols-[minmax(0,1fr)_240px] xl:items-start">
          <Card className="min-w-0 rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="space-y-4 p-5">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h2 className="sr-only">{saved?.name ?? rule.name}</h2>
                <Input aria-label="Rule name" required maxLength={200} value={rule.name}
                  onChange={event => onChange({ ...rule, name: event.target.value })}
                  className="h-10 max-w-[280px] rounded-xl border-slate-200 text-[15px] font-semibold text-slate-950 shadow-none" />
                <Button type="button" variant="outline" className={outlineButton} onClick={onDelete}><Trash2 className="size-4" />Delete</Button>
                {saved && <span className="text-xs font-medium text-slate-500">{saved.enabled ? "Enabled" : "Disabled"}</span>}
              </div>
              <WorkflowConditionEditor value={condition} total={conditionCount(condition)} onChange={condition => onChange({ ...rule, outcomes: [], condition })} />
              <label className="flex items-center gap-2 text-[13px] text-slate-600"><input type="checkbox" className="size-4 accent-[#1f4f96]" checked={rule.enabled} onChange={event => onChange({ ...rule, enabled: event.target.checked })} />Enable for future decisions</label>
            </CardContent>
          </Card>
          <div className="space-y-3 xl:pt-4">
            <div className="flex items-center justify-center gap-2 xl:justify-start">
              <span className="rounded-lg bg-slate-100 px-3 py-2 text-[11px] font-semibold uppercase tracking-[0.16em] text-slate-500">Then</span>
              <div className="relative min-w-0">
                <button type="button" aria-label="Choose action" aria-expanded={menu === "action"} onClick={() => setMenu(menu === "action" ? null : "action")}
                  className="inline-flex min-h-12 items-center gap-2 rounded-2xl border border-slate-200 bg-white px-4 py-3 text-left text-[14px] font-medium text-slate-950 hover:border-slate-300"><ActionIcon className="size-4 shrink-0 text-slate-500" />{action.label}</button>
                {menu === "action" && <div role="group" aria-label="Case actions" className="absolute right-0 top-full z-20 mt-2 w-[340px] max-w-[calc(100vw-4rem)] rounded-2xl border border-slate-200 bg-white p-2 shadow-[0_20px_48px_rgba(15,23,42,0.14)]">
                  {actions.map(item => <button key={item.value} type="button" onClick={() => { onChange(item.value === "do_nothing" ? { ...rule, action_type: item.value, condition, outcomes: [], inbox_id: "", title: "", title_mode: "", tag_ids: [] } : { ...rule, action_type: item.value, title: rule.title || "Case %object_id%", title_mode: rule.title ? rule.title_mode : "template" }); setMenu(null); }} className="flex w-full flex-col rounded-xl px-3.5 py-3 text-left hover:bg-slate-50">
                    <span className="flex items-center gap-2 text-[15px] font-medium text-slate-950">{item.value === "do_nothing" ? <CircleSlash2 className="size-4 text-slate-500" /> : <Plus className="size-4 text-slate-500" />}{item.label}</span><span className="mt-1 text-[13px] leading-5 text-slate-500">{item.description}</span>
                  </button>)}
                </div>}
              </div>
            </div>
            {rule.action_type !== "do_nothing" && <Card className="min-w-0 rounded-2xl border border-slate-200 shadow-none">
              <CardContent className="space-y-4 p-5">
                <div className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2 text-[14px] font-medium text-slate-950"><ActionIcon className="size-4 shrink-0" />{action.label}</div>
                <div className="space-y-3 text-[14px] text-slate-950">
                  <label className="flex items-center gap-3"><span className="shrink-0 font-semibold">In</span>
                    <select aria-label="Destination inbox" required value={rule.inbox_id} onChange={event => onChange({ ...rule, inbox_id: event.target.value })} className="h-10 min-w-0 flex-1 rounded-xl border border-slate-200 bg-white px-3 outline-none">
                      <option value="">Select an inbox</option>
                      {saved && rule.inbox_id && !activeInboxes.some(item => item.id === rule.inbox_id) && <option value={rule.inbox_id}>Existing destination (not active)</option>}
                      {activeInboxes.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
                    </select>
                  </label>
                  <label className="flex items-center gap-3"><span className="shrink-0 font-semibold">With title</span><Input aria-label="Case title" required maxLength={200} value={rule.title} onChange={event => onChange({ ...rule, title: event.target.value })} className="h-10 min-w-0 rounded-xl border-slate-200 text-[14px] shadow-none" /></label>
                                    <label className="flex items-center gap-2 text-xs text-slate-600"><input type="checkbox" aria-label="Use title placeholders" checked={rule.title_mode === "template"} onChange={event => onChange({ ...rule, title_mode: event.target.checked ? "template" : "" })} />Use placeholders</label>
                  <select aria-label="Insert title placeholder" value="" className="w-full min-w-0 rounded-xl border border-slate-200 bg-white p-2 text-xs" onChange={event => { if (event.target.value) onChange({ ...rule, title_mode: "template", title: `${rule.title}%${event.target.value}%` }); }}>
                    <option value="">Insert placeholder…</option>{Object.keys(titleExamples).map(key => <option key={key} value={key}>{`%${key}%`}</option>)}
                  </select>
                  {rule.title_mode === "template" && <div className="space-y-1 text-xs text-slate-500">
                    {titlePreview.error ? <p role="alert" className="text-red-700">{titlePreview.error}</p> : <p aria-label="Example case title" className="break-words">Example: {titlePreview.text}</p>}
                    <p>Preview uses example values. Titles longer than 200 bytes are shortened. Use %% for a percent sign.</p>
                  </div>}
                  {rule.action_type === "add_to_case_if_possible" && <p className="text-xs text-slate-500">This title is used only when a new case is created. Existing cases keep their names.</p>}
                  <div className="space-y-2">
                    <div className="flex items-center justify-between gap-3"><span className="font-semibold">With tags</span><Button type="button" variant="outline" className={outlineButton} aria-expanded={tagsOpen} onClick={() => setTagsOpen(!tagsOpen)}><Plus className="size-4" />Add</Button></div>
                    {tagsOpen && <>
                      <Input aria-label="Search tags" placeholder="Search tags..." value={tagSearch} onChange={event => setTagSearch(event.target.value)} className="h-10 rounded-xl border-slate-200 text-[14px] shadow-none" />
                      <div className="flex flex-wrap gap-2">{filteredTags.map(tag => <button key={tag.id} type="button" aria-pressed={rule.tag_ids.includes(tag.id)} onClick={() => onChange({ ...rule, tag_ids: rule.tag_ids.includes(tag.id) ? rule.tag_ids.filter(id => id !== tag.id) : [...rule.tag_ids, tag.id] })} className={cn("rounded-full border px-3 py-1.5 text-[14px]", rule.tag_ids.includes(tag.id) ? "border-[#1f4f96] bg-blue-50 text-[#1f4f96]" : "border-slate-300 text-slate-700")}>{tag.name}</button>)}</div>
                      {filteredTags.length === 0 && <p className="text-xs text-slate-500">No matching tags.</p>}
                    </>}
                    {!tagsOpen && <div className="flex flex-wrap gap-2">{selectedTags.map(tag => <span key={tag.id} className="rounded-full border border-[#1f4f96] bg-blue-50 px-3 py-1.5 text-[13px] text-[#1f4f96]">{tag.name}</span>)}</div>}
                  </div>
                </div>
              </CardContent>
            </Card>}
          </div>
        </article>
        {dirty && <div className="flex justify-end gap-3"><Button type="button" variant="outline" className="h-10 rounded-xl border-slate-900 px-4 text-[14px] shadow-none" onClick={onCancel}>Cancel</Button><Button type="submit" className={primaryButton} disabled={!canSave}>{saving ? "Saving..." : "Save"}</Button></div>}
      </fieldset>
    </form>
  );
}

export function ScenarioWorkflowPage({ scenarioId }: { scenarioId: string }) {
  const page = useQuery({ queryKey: ["case-workflows", scenarioId], queryFn: () => workflowRequest<WorkflowPage>(scenarioId) });
  if (!page.data) return <main className="p-6">{page.error ? <p role="alert">{page.error.message}</p> : <p>Loading workflows...</p>}</main>;
  return <WorkflowEditor key={`${page.data.tenant_id}:${page.data.user_id}:${scenarioId}`} scenarioId={scenarioId} page={page} />;
}

function WorkflowEditor({ scenarioId, page }: { scenarioId: string; page: UseQueryResult<WorkflowPage, Error> }) {
  const client = useQueryClient();
  const key = ["case-workflows", scenarioId];
  const tenant = page.data!.tenant_id;
  const storageKey = `workflow-drafts:v1:${tenant}:${page.data!.user_id}:${scenarioId}`;
  const inboxes = useQuery({ queryKey: ["cases", tenant, "inboxes"], queryFn: () => requestCase<{ inboxes: Inbox[] }>(tenant, "inboxes") });
  const tags = useQuery({ queryKey: ["cases", tenant, "tags"], queryFn: () => requestCase<{ tags: Tag[] }>(tenant, "tags") });
  const [initial] = useState(() => {
    try { return { rules: page.data?.can_edit ? readDrafts(storageKey) : [], error: "" }; }
    catch (error) { return { rules: [], error: `Could not restore browser drafts: ${error instanceof Error ? error.message : "Storage unavailable."}` }; }
  });
  const [drafts, setDrafts] = useState<CaseRule[]>(initial.rules);
  const [storageError, setStorageError] = useState(initial.error);
  const [notice, setNotice] = useState(initial.rules.length ? "Saved browser drafts restored. Review each rule before saving it to the workflow." : "");
  function forgetDraft(id: string) {
    setDrafts(current => current.filter(rule => rule.id !== id));
    try { writeDrafts(storageKey, readDrafts(storageKey).filter(rule => rule.id !== id)); setStorageError(""); }
    catch { setStorageError("The browser draft could not be removed. Discard saved drafts before leaving to prevent it reappearing."); }
  }
  const save = useMutation({
    mutationFn: (rule: CaseRule) => workflowRequest<CaseRule>(scenarioId, rule),
    onSuccess: async (_, rule) => { forgetDraft(rule.id); setNotice("Workflow saved. It applies to future decisions; already queued actions are unchanged."); await client.invalidateQueries({ queryKey: key }); },
    onError: async () => { setNotice(""); await client.invalidateQueries({ queryKey: key }); },
  });
  const remove = useMutation({
    mutationFn: (rule: CaseRule) => workflowRequest<{ deleted: string }>(scenarioId, rule, "DELETE"),
    onSuccess: async (_, rule) => { forgetDraft(rule.id); setNotice("Rule deleted. Past executions and already queued actions are preserved."); await client.invalidateQueries({ queryKey: key }); },
    onError: async () => { setNotice(""); await client.invalidateQueries({ queryKey: key }); },
  });
  const activeInboxes = (inboxes.data?.inboxes ?? []).filter(item => item.status === "active");
  const availableTags = (tags.data?.tags ?? []).filter(tag => !tag.deleted_at);
  const editable = !page.isError && !!page.data?.can_edit && !page.data.legacy_workflows;
  const busy = save.isPending || remove.isPending;
  function valid(rule: CaseRule) {
    return !!rule.name.trim() && (rule.condition ? validCondition(rule.condition) : rule.outcomes.length > 0) &&
      (rule.action_type === "do_nothing" || !!rule.title.trim() && (rule.title_mode !== "template" || !previewTitle(rule.title).error) &&
        !inboxes.isPending && !tags.isPending && !inboxes.isError && !tags.isError &&
        (activeInboxes.some(item => item.id === rule.inbox_id) || (!rule.enabled && !!rule.updated_at && !!rule.inbox_id)) &&
        (!rule.enabled || rule.tag_ids.every(id => availableTags.some(tag => tag.id === id))));
  }
  const savedRules = page.data?.rules ?? [];
  const savedById = new Map(savedRules.map(rule => [rule.id, rule]));
  const draftById = new Map(drafts.map(rule => [rule.id, rule]));
  const rules = [...savedRules.map(rule => draftById.get(rule.id) ?? rule), ...drafts.filter(rule => !savedById.has(rule.id))];
  function change(rule: CaseRule) {
    setNotice("");
    setDrafts(current => current.some(item => item.id === rule.id) ? current.map(item => item.id === rule.id ? rule : item) : [...current, rule]);
  }
  function createRule() {
    save.reset(); remove.reset();
    change({ id: crypto.randomUUID(), name: "New Rule", enabled: false, outcomes: [], condition: { kind: "all", children: [] }, action_type: "do_nothing", inbox_id: "", title: "", tag_ids: [], updated_at: null });
  }
  function submit(rule: CaseRule) {
    if (!valid(rule) || !editable || busy) return;
    if (!window.confirm(rule.enabled ? "Save and enable this workflow for future decisions?" : "Save this workflow as disabled? Already queued actions will still run.")) return;
    remove.reset(); save.mutate(rule);
  }
  function deleteRule(rule: CaseRule) {
    const saved = savedById.get(rule.id);
    if (!saved) { forgetDraft(rule.id); return; }
    if (!window.confirm(`Delete “${saved.name}” from future evaluations? Past executions and queued actions are preserved. Any draft for this rule will be discarded.`)) return;
    save.reset(); remove.mutate(saved);
  }
  function saveDrafts() {
    setNotice("");
    try { writeDrafts(storageKey, drafts); setStorageError(""); setNotice("Drafts saved in this browser for your account. Live workflows are unchanged."); }
    catch (error) { setStorageError(`Drafts were not saved: ${error instanceof Error ? error.message : "Storage unavailable."}`); }
  }
  function discardDrafts() {
    if (!window.confirm("Discard all workflow drafts for this scenario in this browser? Saved workflow rules are unchanged.")) return;
    setNotice("");
    try { localStorage.removeItem(storageKey); setDrafts([]); setStorageError(""); setNotice("Browser drafts discarded."); }
    catch { setStorageError("Browser drafts could not be discarded. Storage is unavailable."); }
  }
  const mutationError = save.error ?? remove.error;
  return (
    <main className="mx-auto w-full max-w-[1320px] space-y-5 px-4 sm:px-6 xl:px-8">
      <nav aria-label="Breadcrumb" className="flex flex-wrap items-center gap-3 border-b border-slate-200 pb-4">
        <Link href={`/detection/${scenarioId}`} aria-label="Back to scenario" className="inline-flex size-10 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"><ArrowLeft className="size-4" /></Link>
        <div className="flex flex-wrap items-center gap-2 text-[15px] text-slate-500"><Link href="/detection" className="font-medium text-[#1f4f96]">Detection</Link><span>/</span><span className="font-medium text-slate-700">{page.data?.scenario_name ?? "Scenario"}</span><span>/</span><span aria-current="page" className="font-semibold text-slate-950">Workflow</span></div>
      </nav>
      <header className="flex flex-wrap items-center justify-between gap-4"><div className="space-y-2"><h1 className="text-[1.7rem] font-semibold tracking-tight text-slate-950">Workflow</h1><p className="flex items-center gap-2 text-[14px] text-slate-600"><Info className="size-4 shrink-0 text-slate-400" />Define what should happen after a decision outcome is produced.</p></div>
        <div className="flex flex-wrap gap-2"><Button variant="outline" className={outlineButton} disabled={!editable || busy || !drafts.length} onClick={saveDrafts}>Save draft</Button>
          {(drafts.length > 0 || storageError) && <Button variant="outline" className={outlineButton} disabled={!editable || busy} onClick={discardDrafts}>Discard drafts</Button>}</div>
      </header>
      {page.error && <p role="alert" className="text-red-700">{page.error.message}</p>}
      {notice && <p role="status" className="text-sm text-green-800">{notice}</p>}
      {storageError && <p role="alert" className="text-sm text-red-700">{storageError}</p>}
      {mutationError && <div role="alert" className="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700"><p>{mutationError.message} The saved list has been refreshed. Compare it before retrying; your drafts are retained.</p><details className="mt-2"><summary>Compare saved rules</summary>{savedRules.map(rule => <div key={rule.id} className="mt-2"><p className="font-medium">{rule.name}</p><p>{rule.title} · {rule.enabled ? "Enabled" : "Disabled"}</p><pre className="whitespace-pre-wrap break-words text-xs">{JSON.stringify(rule.condition ?? rule.outcomes, null, 2)}</pre></div>)}</details></div>}
      {page.data && !page.isError && <>
        <p className="text-[13px] text-slate-500">Rules run in order. The first matching rule stops further rules, including Do nothing. Save each rule to apply it to future decisions; new rules append when saved. Save draft keeps unfinished edits only in this browser.</p>
        {!page.data.can_edit && <p>Viewing only. A platform administrator with destination inbox access must save changes.</p>}
        {page.data.legacy_workflows && <p role="alert">This scenario has active legacy workflows. Migrate those workflows before creating rules here; creating a structured rule would change which workflow system runs.</p>}
        {page.data.other_rules > 0 && <p role="status">This scenario also has {page.data.other_rules} advanced or older rules. They retain their order and configuration and cannot be edited here.</p>}
        {(inboxes.error || tags.error) && <p role="alert">{inboxes.error?.message ?? tags.error?.message} Case actions cannot be saved until inboxes and tags load.</p>}
        <div className="space-y-4">{rules.map((rule, index) => <div key={rule.id} className="space-y-4">
          {index > 0 && page.data.other_rules === 0 && <div className="flex justify-center"><div className="rounded-lg bg-slate-100 px-3 py-1.5 text-[11px] font-semibold uppercase tracking-[0.16em] text-slate-500">Else</div></div>}
          <WorkflowRuleCard rule={rule} saved={savedById.get(rule.id)} inboxes={inboxes.data?.inboxes ?? []} tags={availableTags}
            disabled={!editable || busy} dirty={draftById.has(rule.id)} saving={save.isPending && save.variables?.id === rule.id}
            canSave={valid(rule)} onChange={change} onSave={() => submit(rule)} onDelete={() => deleteRule(rule)}
            onCancel={() => { forgetDraft(rule.id); save.reset(); remove.reset(); }} />
        </div>)}</div>
        {rules.length === 0 && <p className="text-sm text-slate-500">No case workflows yet. Create a rule to get started.</p>}
        <div className="flex justify-center pb-2"><Button className={primaryButton} disabled={!editable || busy || rules.length >= 500} onClick={createRule}><Plus className="size-4" />Create rule</Button></div>
      </>}
    </main>
  );
}
