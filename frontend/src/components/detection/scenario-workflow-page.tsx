"use client";

import Link from "next/link";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { requestCase, type Inbox, type Tag } from "@/lib/case-manager-api";

type CaseRule = { id: string; name: string; enabled: boolean; outcomes: string[]; action_type: "create_case" | "add_to_case_if_possible"; inbox_id: string; title: string; tag_ids: string[]; updated_at: string | null };
type WorkflowPage = { tenant_id: string; can_edit: boolean; scenario_name: string; rules: CaseRule[]; other_rules: number; legacy_workflows: boolean };
const outcomes = ["approve", "review", "block_and_review", "decline"];
async function workflowRequest<T>(scenario: string, rule?: CaseRule): Promise<T> {
  const response = await fetch(`/api/workflows/scenarios/${encodeURIComponent(scenario)}${rule ? `/rules/${rule.id}` : ""}`, {
    method: rule ? "PUT" : "GET", body: rule ? JSON.stringify(rule) : undefined,
    headers: { "Content-Type": "application/json" }, cache: "no-store", credentials: "same-origin",
  });
  const data = await response.json(); if (!response.ok) throw new Error(data.error ?? "Workflow request failed."); return data;
}

export function ScenarioWorkflowPage({ scenarioId }: { scenarioId: string }) {
  const client = useQueryClient();
  const key = ["case-workflows", scenarioId];
  const page = useQuery({ queryKey: key, queryFn: () => workflowRequest<WorkflowPage>(scenarioId) });
  const tenant = page.data?.tenant_id ?? "";
  const inboxes = useQuery({ queryKey: ["cases", tenant, "inboxes"], queryFn: () => requestCase<{ inboxes: Inbox[] }>(tenant, "inboxes"), enabled: !!tenant });
  const tags = useQuery({ queryKey: ["cases", tenant, "tags"], queryFn: () => requestCase<{ tags: Tag[] }>(tenant, "tags"), enabled: !!tenant });
  const [draft, setDraft] = useState<CaseRule | null>(null);
  const [notice, setNotice] = useState("");
  const save = useMutation({
    mutationFn: (rule: CaseRule) => workflowRequest<CaseRule>(scenarioId, rule),
    onSuccess: async () => { setDraft(null); setNotice("Workflow saved. It applies to future decisions; already queued actions are unchanged."); await client.invalidateQueries({ queryKey: key }); },
    onError: async () => { setNotice(""); await client.invalidateQueries({ queryKey: key }); },
  });
  const activeInboxes = (inboxes.data?.inboxes ?? []).filter(i => i.status === "active");
  const availableTags = (tags.data?.tags ?? []).filter(t => !t.deleted_at);
  const editable = !page.isError && !!page.data?.can_edit && !page.data.legacy_workflows;
  const valid = !!draft?.name.trim() && !!draft.title.trim() && draft.outcomes.length > 0 && (activeInboxes.some(i => i.id === draft.inbox_id) || (!draft.enabled && !!draft.updated_at && !!draft.inbox_id)) && (!draft.enabled || draft.tag_ids.every(id => availableTags.some(t => t.id === id)));
  function edit(rule: CaseRule) { save.reset(); setNotice(""); setDraft({ ...rule, tag_ids: rule.tag_ids ?? [] }); }
  return <main className="mx-auto max-w-5xl space-y-6 p-6">
    <Link href={`/detection/${scenarioId}`} className="text-sm text-indigo-700">← Back to scenario</Link>
    <header><h1 className="text-2xl font-semibold">Case workflows</h1><p className="mt-2 text-slate-600">{page.data?.scenario_name} · Route matching decisions into an investigation case.</p></header>
    {page.isPending && <p>Loading workflows…</p>}
    {page.error && <p role="alert" className="text-red-700">{page.error.message}</p>}
    {page.data && !page.isError && <>
      <p className="text-sm text-slate-600">Rules run in order. The first matching case rule stops further rules. Saving an enabled rule changes routing for future decisions immediately.</p>
      {!page.data.can_edit && <p>Viewing only. A platform administrator with destination inbox access must save changes.</p>}
      {page.data.legacy_workflows && <p role="alert">This scenario has active legacy workflows. Migrate those workflows before creating rules here; creating a structured rule would change which workflow system runs.</p>}
      {page.data.other_rules > 0 && <p role="status">This scenario also has {page.data.other_rules} advanced or older rules. They retain their order and configuration and cannot be edited here.</p>}
      {(inboxes.error || tags.error) && <p role="alert">{inboxes.error?.message ?? tags.error?.message} Saving is unavailable until inboxes and tags load.</p>}
      <div className="space-y-3">{page.data.rules.map(rule => <article key={rule.id} className="rounded-xl border bg-white p-4">
        <div className="flex items-center justify-between gap-4"><h2 className="font-semibold">{rule.name}</h2><Button variant="outline" disabled={!editable || save.isPending} onClick={() => edit(rule)}>Edit {rule.name}</Button></div>
        <p>{rule.enabled ? "Enabled" : "Disabled"} · {rule.outcomes.join(", ")} → {rule.action_type === "create_case" ? "Create case" : "Add to case if possible"}</p>
        <p className="text-sm text-slate-600">{inboxes.data?.inboxes.find(i => i.id === rule.inbox_id)?.name ?? "Destination inbox unavailable"} · {rule.title}</p>
      </article>)}</div>
      {page.data.rules.length === 0 && <p>No visible case workflows have been saved with this editor.</p>}
      <Button disabled={!editable || save.isPending || !!draft} onClick={() => edit({ id: crypto.randomUUID(), name: "", enabled: false, outcomes: ["review", "block_and_review"], action_type: "create_case", inbox_id: "", title: "", tag_ids: [], updated_at: null })}>Add case workflow</Button>
    </>}
    {notice && <p role="status" className="text-green-800">{notice}</p>}
    {save.error && <p role="alert" className="text-red-700">{save.error.message} The saved list has been refreshed. Compare it before retrying; your draft is retained.</p>}
    {draft && <form className="space-y-5 rounded-xl border bg-white p-5" onSubmit={e => {
      e.preventDefault(); if (!valid || !editable) return;
      if (!window.confirm(draft.enabled ? "Save and enable this workflow for future decisions?" : "Save this workflow as disabled? Already queued actions will still run.")) return;
      save.mutate(draft);
    }}>
      <fieldset disabled={save.isPending || !editable} className="space-y-5">
        <legend className="font-semibold">{draft.updated_at ? "Edit workflow" : "New workflow"}</legend>
        <label className="block">Rule name<Input required maxLength={200} value={draft.name} onChange={e => setDraft({ ...draft, name: e.target.value })} /></label>
        <fieldset><legend>Decision outcomes</legend><div className="flex flex-wrap gap-4">{outcomes.map(outcome => <label key={outcome}><input type="checkbox" checked={draft.outcomes.includes(outcome)} onChange={e => setDraft({ ...draft, outcomes: e.target.checked ? [...draft.outcomes, outcome] : draft.outcomes.filter(v => v !== outcome) })} /> {outcome}</label>)}</div></fieldset>
        <label className="block">Action<select className="ml-3 rounded border p-2" value={draft.action_type} onChange={e => setDraft({ ...draft, action_type: e.target.value as CaseRule["action_type"] })}><option value="create_case">Create case</option><option value="add_to_case_if_possible">Add to case if possible</option></select></label>
        <p className="text-sm text-slate-600">“Add to case if possible” reuses an eligible case for the same object, or creates one. It does not group every connected record automatically.</p>
        <label className="block">Destination inbox<select required className="ml-3 rounded border p-2" value={draft.inbox_id} onChange={e => setDraft({ ...draft, inbox_id: e.target.value })}><option value="">Select an inbox</option>{draft.updated_at && draft.inbox_id && !activeInboxes.some(i => i.id === draft.inbox_id) && <option value={draft.inbox_id}>Existing destination (not active)</option>}{activeInboxes.map(i => <option value={i.id} key={i.id}>{i.name}</option>)}</select></label>
        <label className="block">Case title<Input required maxLength={200} value={draft.title} onChange={e => setDraft({ ...draft, title: e.target.value })} /></label>
        <p className="text-sm text-slate-600">The title is literal text. Template placeholders are not expanded.</p>
        <fieldset><legend>Case tags</legend>{availableTags.map(tag => <label key={tag.id} className="mr-4"><input type="checkbox" checked={draft.tag_ids.includes(tag.id)} onChange={e => setDraft({ ...draft, tag_ids: e.target.checked ? [...draft.tag_ids, tag.id] : draft.tag_ids.filter(v => v !== tag.id) })} /> {tag.name}</label>)}</fieldset>
        <label className="block"><input type="checkbox" checked={draft.enabled} onChange={e => setDraft({ ...draft, enabled: e.target.checked })} /> Enable for future decisions</label>
        <div className="flex gap-3"><Button type="submit" disabled={!valid || !!inboxes.error || !!tags.error || inboxes.isPending || tags.isPending}>{save.isPending ? "Saving…" : "Save workflow"}</Button><Button type="button" variant="outline" onClick={() => { setDraft(null); save.reset(); }}>Cancel</Button></div>
      </fieldset>
    </form>}
  </main>;
}
