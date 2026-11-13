"use client";

import { WorkflowConditionEditor, conditionCount, validCondition, type WorkflowCondition } from "./workflow-condition-editor";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { ArrowLeft, ArrowRight, Building2, Check, ChevronDown, Inbox as InboxIcon, Plus, Save, ScanSearch, ShieldCheck, SlidersHorizontal, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { requestCase, type Inbox } from "@/lib/case-manager-api";
import { useWorkspaceTenant } from "@/components/workspace-session";
import { scenarioScreeningKey } from "@/lib/scenario-screening";

type Rule = { counterparty_id_field?: string; forced_outcome?: string; additional_name_fields?: string[]; use_ner?: boolean; ner_ignore_classification?: boolean; ignore_list_id?: string; minimum_name_length?: number; remove_digits?: boolean; condition?: WorkflowCondition; threshold?: number; result_limit?: number; datasets?: string[]; id: string; name: string; enabled: boolean; entity_type: string; name_field: string; address_field?: string; birth_date_field?: string; nationality_field?: string; country_field?: string; registration_number_field?: string; outcomes: string[]; inbox_id: string; updated_at: string | null };
type ConfigurationPage = { counterparty_supported?: boolean; inline_supported?: boolean; provider_name?: string; default_threshold?: number; default_result_limit?: number; threshold_supported?: boolean; result_limit_supported?: boolean; dataset_selection_supported?: boolean; name_recognition_supported?: boolean; ignore_lists?: { id: string; name: string }[]; matching_settings_supported: boolean; tenant_id: string; can_edit: boolean; scenario_name: string; fields: string[]; address_supported: boolean; birth_date_supported: boolean; nationality_supported: boolean; country_supported: boolean; registration_number_supported: boolean; rules: Rule[]; other_rules: number };
const identityMappings = [{ key: "nationality_field", capability: "nationality_supported", entity: "Person", label: "Nationality" }, { key: "country_field", capability: "country_supported", entity: "Organization", label: "Country" }, { key: "registration_number_field", capability: "registration_number_supported", entity: "Organization", label: "Registration number" }] as const;
const outcomes = [["approve", "Approve"], ["review", "Review"], ["block_and_review", "Block and review"], ["decline", "Decline"]];
async function request(scenario: string, rule?: Rule): Promise<ConfigurationPage | Rule> {
  const response = await fetch(`/api/scenario-screening/scenarios/${encodeURIComponent(scenario)}${rule ? `/configs/${rule.id}` : ""}`, {
    method: rule ? "PUT" : "GET", headers: { "Content-Type": "application/json" }, body: rule ? JSON.stringify(rule) : undefined, cache: "no-store",
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error ?? "Screening configuration unavailable.");
  return data;
}

function emptyScreeningRule(): Rule {
  return { id: crypto.randomUUID(), name: "", enabled: false, entity_type: "Person", name_field: "", address_field: "", birth_date_field: "", nationality_field: "", country_field: "", registration_number_field: "", outcomes: ["review"], inbox_id: "", updated_at: null };
}

function OptionalSettings({ title, description, defaultOpen = false, children }: { title: string; description?: string; defaultOpen?: boolean; children: ReactNode }) {
  const [open, setOpen] = useState(defaultOpen);
  return <details open={open} onToggle={event => setOpen(event.currentTarget.open)} className="group min-w-0 rounded-xl border border-slate-200 bg-white">
    <summary className="flex cursor-pointer list-none items-center gap-3 rounded-xl p-4 text-sm font-medium text-slate-700 outline-none focus-visible:ring-2 focus-visible:ring-blue-600 [&::-webkit-details-marker]:hidden">
      <SlidersHorizontal aria-hidden="true" className="size-4 shrink-0 text-slate-400" /><span className="min-w-0 flex-1"><span>{title}</span>{description && <span className="mt-0.5 block text-xs font-normal text-slate-500">{description}</span>}</span><ChevronDown aria-hidden="true" className="size-4 shrink-0 transition-transform group-open:rotate-180" />
    </summary>
    <div className="space-y-4 border-t border-slate-100 p-4">{children}</div>
  </details>;
}

function FormSection({ step, title, children }: { step: string; title: string; children: ReactNode }) {
  return <section className="min-w-0 space-y-5 p-5 sm:p-7">
    <h3 className="flex items-center gap-3 text-base font-semibold text-slate-900"><span className="flex size-7 items-center justify-center rounded-lg bg-slate-100 text-xs font-semibold text-slate-500">{step}</span>{title}</h3>
    {children}
  </section>;
}

export function ScenarioScreeningPage({ scenarioId, startCreating = false, initialConfigId }: { scenarioId: string; startCreating?: boolean; initialConfigId?: string }) {
  const workspaceTenant = useWorkspaceTenant();
  const [localDraft, setLocalDraft] = useState<Rule | null>(() => startCreating ? emptyScreeningRule() : null);
  const [initialSelectionDismissed, setInitialSelectionDismissed] = useState(false);
  const setDraft = (next: Rule | null) => { setInitialSelectionDismissed(true); setLocalDraft(next); };
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const page = useQuery({ queryKey: scenarioScreeningKey(workspaceTenant, scenarioId), queryFn: async () => await request(scenarioId) as ConfigurationPage, staleTime: 0, retry: false });
  const selectedRule = initialConfigId && !initialSelectionDismissed ? page.data?.rules.find(rule => rule.id === initialConfigId) : undefined;
  const draft = localDraft ?? selectedRule ?? null;
  const tenant = page.data?.tenant_id ?? "";
  const inboxes = useQuery({ queryKey: ["cases", tenant, "inboxes"], queryFn: () => requestCase<{ inboxes: Inbox[] }>(tenant, "inboxes"), enabled: !!tenant });
  const edit = (rule?: Rule) => {
    setError(""); setNotice("");
    setDraft(rule ? { ...rule, outcomes: [...rule.outcomes], additional_name_fields: [...(rule.additional_name_fields ?? [])] } : emptyScreeningRule());
  };
  const save = async () => {
    if (!draft || saving) return;
    setSaving(true); setError(""); setNotice("");
    try {
      await request(scenarioId, draft);
      setDraft(null); setNotice("Screening configuration saved.");
      await page.refetch();
    } catch (e) { setError(e instanceof Error ? e.message : "Save failed. Refresh before retrying."); }
    finally { setSaving(false); }
  };
  const data = page.data;
  const hasVisibleDraft = !!draft && (!!data?.can_edit || !!draft.updated_at);
  const thresholdSupported = data?.threshold_supported || data?.matching_settings_supported;
  const limitSupported = data?.result_limit_supported || data?.matching_settings_supported;
  const datasetsSupported = data?.dataset_selection_supported || data?.matching_settings_supported;
  const unsupportedMatching = !!draft && ((draft.threshold !== undefined && !thresholdSupported) || (draft.result_limit !== undefined && !limitSupported) || (!!draft.datasets?.length && !datasetsSupported));
  const invalidNameFields = !!draft && (draft.additional_name_fields ?? []).some((field, i, fields) => !data?.fields.includes(field) || field === draft.name_field || fields.indexOf(field) !== i);
  const selectClass = "h-11 min-w-0 w-full rounded-xl border border-slate-200 bg-white px-3 text-sm text-slate-900 outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100 disabled:bg-slate-50 disabled:text-slate-500";
  const labelClass = "block space-y-2 text-sm font-medium text-slate-700";
  const inputClass = "h-11 rounded-xl border-slate-200 px-3 focus:border-blue-500 focus:ring-blue-100";
  const invalidCounterparty = !!draft?.counterparty_id_field && (!data?.counterparty_supported || !data.fields.includes(draft.counterparty_id_field));
  const invalidIdentity = !!draft && (identityMappings.some(m => !!draft[m.key] && (draft.entity_type !== m.entity || !data?.[m.capability] || !data.fields.includes(draft[m.key]!))) || (!!draft.birth_date_field && (draft.entity_type !== "Person" || !data?.birth_date_supported || !data.fields.includes(draft.birth_date_field))) || (!!draft.address_field && (!data?.address_supported || !data.fields.includes(draft.address_field))));
  const invalidRecognition = !!draft?.use_ner && (!data?.name_recognition_supported || !data.matching_settings_supported);
  const invalidIgnoreList = !!draft?.ignore_list_id && !data?.ignore_lists?.some(list => list.id === draft.ignore_list_id);
  const saveDisabled = !draft || invalidCounterparty || invalidIdentity || invalidRecognition || invalidIgnoreList || (!!draft.forced_outcome && !data?.inline_supported) || (!!draft.condition && !validCondition(draft.condition)) || unsupportedMatching || invalidNameFields || saving || !data?.can_edit || inboxes.isError || inboxes.isLoading || (!draft.forced_outcome && !draft.outcomes.length) || !data?.fields.includes(draft.name_field);
  const synthetic = data?.provider_name === "Synthetic development catalog";
  const outcomeLabel = (value: string) => outcomes.find(item => item[0] === value)?.[1] ?? value;
  return <div className="mx-auto max-w-6xl space-y-6 px-4 pb-8 sm:px-6">
    <Link href={`/detection/${scenarioId}/edit?tab=Rules`} className="inline-flex items-center gap-2 text-sm font-medium text-slate-500 hover:text-slate-900"><ArrowLeft aria-hidden="true" className="size-4" />Back to scenario</Link>
    <header className="flex flex-wrap items-start justify-between gap-4">
      <div className="flex items-start gap-3">
        <span className="flex size-11 shrink-0 items-center justify-center rounded-2xl border border-blue-100 bg-blue-50 text-blue-700"><ScanSearch aria-hidden="true" className="size-5" /></span>
        <div><p className="mb-1 text-xs font-medium text-slate-500">{data?.scenario_name ?? "Scenario"}</p><h1 className="text-2xl font-semibold tracking-tight text-slate-900">{hasVisibleDraft ? draft?.updated_at ? "Edit screening" : "Create screening" : "Screening"}</h1></div>
      </div>
      {data && !draft && <Button disabled={!data.can_edit || saving} onClick={() => edit()} className="rounded-xl"><Plus aria-hidden="true" className="size-4" />Add screening</Button>}
    </header>
    {page.isLoading && <p role="status" className="text-sm text-slate-500">Loading screening…</p>}
    {page.isError && <div role="alert" className="space-y-2 rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700"><p>{page.error.message}</p><Button variant="outline" onClick={() => page.refetch()}>Try again</Button></div>}
    {notice && <p role="status" className="flex items-center gap-2 rounded-xl bg-emerald-50 p-3 text-sm text-emerald-800"><Check aria-hidden="true" className="size-4" />{notice}</p>}
    {error && <div className="space-y-2 rounded-xl border border-red-200 bg-red-50 p-4"><p role="alert" className="text-sm text-red-700">{error}</p><Button variant="outline" disabled={saving} onClick={() => { setDraft(null); setError(""); void page.refetch(); }}>Discard changes and reload</Button></div>}
    {data && <>
      {initialConfigId && !initialSelectionDismissed && !selectedRule && <p role="alert" className="text-sm text-amber-800">This screening is unavailable. Choose another below.</p>}
      {!data.can_edit && <p className="rounded-xl bg-slate-100 p-3 text-sm text-slate-600">View only. An administrator can make changes.</p>}
      {data.other_rules > 0 && <p className="text-sm text-amber-800">{data.other_rules} screening configuration(s) cannot be edited here.</p>}
      <div className={`flex flex-wrap items-center gap-x-3 gap-y-1 rounded-xl border px-4 py-3 text-xs ${synthetic ? "border-amber-200 bg-amber-50/60 text-amber-900" : "border-slate-200 bg-white text-slate-600"}`}>
        <ShieldCheck aria-hidden="true" className="size-4 shrink-0" /><span className="font-medium">Data source</span><span>{data.provider_name ?? "Provider not reported"}</span>{synthetic && <span className="font-medium sm:ml-auto">Demo data · No live sanctions coverage</span>}
      </div>
      {!draft && <div className="space-y-3">
        {!data.rules.length && <div className="flex flex-col items-center rounded-2xl border border-dashed border-slate-300 bg-white px-5 py-12 text-center"><ScanSearch aria-hidden="true" className="mb-3 size-8 text-slate-300" /><h2 className="font-semibold text-slate-800">No screening rules yet</h2><p className="mt-1 text-sm text-slate-500">Add a screening to check a person or company for possible matches.</p></div>}
        {data.rules.map(rule => <Card key={rule.id} className="rounded-2xl shadow-none"><CardContent className="flex flex-wrap items-center justify-between gap-4 p-5">
          <div className="flex min-w-0 flex-1 items-start gap-3"><span className="rounded-xl bg-slate-50 p-2.5 text-slate-500">{rule.entity_type === "Organization" ? <Building2 aria-hidden="true" className="size-5" /> : <ScanSearch aria-hidden="true" className="size-5" />}</span><div className="min-w-0"><h2 className="break-words font-semibold text-slate-900">{rule.name}</h2><p className="mt-1 break-words text-sm text-slate-500">{rule.entity_type === "Organization" ? "Company" : "Person"} · {[rule.name_field, ...(rule.additional_name_fields ?? [])].join(" + ")}</p><p className="mt-2 text-xs text-slate-500">{rule.forced_outcome ? `During decision · At least ${outcomeLabel(rule.forced_outcome)} on a match` : `After decision · ${rule.outcomes.map(outcomeLabel).join(", ")}`}</p></div></div>
          <div className="flex items-center gap-3"><span className={`rounded-full px-2.5 py-1 text-xs font-medium ${rule.enabled ? "bg-emerald-50 text-emerald-700" : "bg-slate-100 text-slate-500"}`}>{rule.enabled ? "Active" : "Inactive"}</span><Button variant="outline" className="rounded-xl" disabled={saving} onClick={() => edit(rule)}>{data.can_edit ? "Edit" : "View"}<ArrowRight aria-hidden="true" className="size-3.5" /></Button></div>
        </CardContent></Card>)}
      </div>}
      {draft && hasVisibleDraft && <form key={draft.id} aria-label="Screening configuration" onSubmit={event => { event.preventDefault(); void save(); }} className="space-y-4">
        <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_260px]">
          <fieldset disabled={saving || !data.can_edit} className="min-w-0 space-y-4">
            <legend className="sr-only">Screening settings</legend>
            <Card className="divide-y divide-slate-100 rounded-2xl shadow-none">
              <FormSection step="01" title="Who to screen">
                <label className={labelClass}><span>Rule name</span><Input className={inputClass} autoFocus={startCreating} required maxLength={200} placeholder="e.g. Beneficiary company check" value={draft.name} onChange={event => setDraft({ ...draft, name: event.target.value })} /></label>
                <div className="grid gap-4 sm:grid-cols-2">
                  <label className={labelClass}><span>Screen for</span><select aria-label="Screen for" className={selectClass} value={draft.entity_type} onChange={event => setDraft({ ...draft, entity_type: event.target.value })}><option value="Person">A person</option><option value="Organization">A company</option></select></label>
                  <label className={labelClass}><span>Name to check</span><select aria-label="Name to check" required className={selectClass} value={draft.name_field} onChange={event => setDraft({ ...draft, name_field: event.target.value })}><option value="">Choose a name field</option>{draft.name_field && !data.fields.includes(draft.name_field) && <option value={draft.name_field} disabled>Unavailable: {draft.name_field}</option>}{data.fields.map(field => <option key={field}>{field}</option>)}</select></label>
                </div>
                {!data.fields.length && <p role="alert" className="text-sm text-amber-800">Add text fields to this scenario’s data model first.</p>}
                <OptionalSettings title="More identity details" description="Combine names or add identifying information" defaultOpen={!!draft.additional_name_fields?.length || !!draft.address_field || !!draft.birth_date_field || identityMappings.some(mapping => !!draft[mapping.key])}>
                  <div className="space-y-3"><p className="text-xs text-slate-500">Add up to four name fields, joined in the order shown.</p>
                    {(draft.additional_name_fields ?? []).map((field, index) => <div key={index} className="flex min-w-0 items-center gap-2">
                      <select aria-label={`Additional name field ${index + 1}`} required className={selectClass} value={field} onChange={event => setDraft({ ...draft, additional_name_fields: draft.additional_name_fields!.map((value, i) => i === index ? event.target.value : value) })}>
                        <option value="">Choose a name field</option>{field && !data.fields.includes(field) && <option value={field} disabled>Unavailable: {field}</option>}{data.fields.map(value => <option key={value} disabled={value === draft.name_field || (value !== field && !!draft.additional_name_fields?.includes(value))}>{value}</option>)}
                      </select>
                      <Button type="button" variant="ghost" size="icon" className="shrink-0 rounded-xl" aria-label={`Remove name field ${index + 1}`} onClick={() => setDraft({ ...draft, additional_name_fields: draft.additional_name_fields!.filter((_, i) => i !== index) })}><Trash2 aria-hidden="true" className="size-4" /></Button>
                    </div>)}
                    <Button type="button" variant="outline" size="sm" className="rounded-lg" disabled={(draft.additional_name_fields?.length ?? 0) >= 4} onClick={() => setDraft({ ...draft, additional_name_fields: [...(draft.additional_name_fields ?? []), ""] })}><Plus aria-hidden="true" className="size-3.5" />Add name field</Button>
                    {invalidNameFields && <p role="alert" className="text-sm text-amber-800">Choose a different available field for each name part, or remove it.</p>}
                  </div>
                  <div className="grid gap-4 sm:grid-cols-2">
                    {(data.address_supported || draft.address_field) && <label className={labelClass}><span>Address</span><select aria-label="Address" className={selectClass} value={draft.address_field ?? ""} onChange={event => setDraft({ ...draft, address_field: event.target.value })}><option value="">Not included</option>{draft.address_field && (!data.address_supported || !data.fields.includes(draft.address_field)) && <option value={draft.address_field} disabled>Unavailable: {draft.address_field}</option>}{data.address_supported && data.fields.map(field => <option key={field}>{field}</option>)}</select></label>}
                    {((draft.entity_type === "Person" && data.birth_date_supported) || draft.birth_date_field) && <label className={labelClass}><span>Date of birth</span><select aria-label="Date of birth" className={selectClass} value={draft.birth_date_field ?? ""} onChange={event => setDraft({ ...draft, birth_date_field: event.target.value })}><option value="">Not included</option>{draft.birth_date_field && (draft.entity_type !== "Person" || !data.birth_date_supported || !data.fields.includes(draft.birth_date_field)) && <option value={draft.birth_date_field} disabled>Unavailable: {draft.birth_date_field}</option>}{draft.entity_type === "Person" && data.birth_date_supported && data.fields.map(field => <option key={field}>{field}</option>)}</select><span className="block text-xs font-normal text-slate-500">Use a year or YYYY-MM-DD.</span></label>}
                    {identityMappings.map(mapping => {
                      const value = draft[mapping.key] ?? "";
                      const supported = draft.entity_type === mapping.entity && data[mapping.capability];
                      if (!supported && !value) return null;
                      return <label key={mapping.key} className={labelClass}><span>{mapping.label}</span><select aria-label={mapping.label} className={selectClass} value={value} onChange={event => setDraft({ ...draft, [mapping.key]: event.target.value })}><option value="">Not included</option>{value && (!supported || !data.fields.includes(value)) && <option value={value} disabled>Unavailable: {value}</option>}{supported && data.fields.map(field => <option key={field}>{field}</option>)}</select>{mapping.key !== "registration_number_field" && <span className="block text-xs font-normal text-slate-500">Two-letter country code, e.g. GH.</span>}</label>;
                    })}
                  </div>
                  {invalidIdentity && <p role="alert" className="text-sm text-amber-800">An identity field is unavailable for this selection. Clear its mapping or choose a supported entity type before saving.</p>}
                </OptionalSettings>
              </FormSection>
              <FormSection step="02" title="When to run">
                <div className="grid gap-4 sm:grid-cols-2">
                  <label className={labelClass}><span>Run screening</span><select aria-label="Run screening" className={selectClass} value={draft.forced_outcome ? "during" : "after"} onChange={event => setDraft({ ...draft, forced_outcome: event.target.value === "during" ? "review" : undefined })}><option value="after">After the decision</option><option value="during" disabled={!data.inline_supported}>During the decision</option></select></label>
                  {draft.forced_outcome && <label className={labelClass}><span>If a match is found</span><select aria-label="If a match is found" className={selectClass} value={draft.forced_outcome} onChange={event => setDraft({ ...draft, forced_outcome: event.target.value })}><option value="review">Review</option><option value="block_and_review">Block and review</option><option value="decline">Decline</option></select></label>}
                  {(thresholdSupported || draft.threshold !== undefined) && <label className={labelClass}><span>Minimum similarity (%)</span><Input className={inputClass} type="number" min={0} max={100} step={1} placeholder={data.default_threshold !== undefined ? String(data.default_threshold) : "Provider default"} value={draft.threshold ?? ""} onChange={event => setDraft({ ...draft, threshold: event.target.value === "" ? undefined : Number(event.target.value) })} /></label>}
                </div>
                <p className="text-xs leading-relaxed text-slate-500">{draft.forced_outcome ? "A match raises the decision to at least this outcome. The numeric score stays the same." : "Matches are sent for review. The original decision and score stay the same."}</p>
                {!!draft.forced_outcome && !data.inline_supported && <p role="alert" className="text-sm text-amber-800">Screening during the decision is unavailable. Choose “After the decision” or restore the provider.</p>}
                {!draft.forced_outcome && <fieldset className="space-y-3"><legend className="mb-3 text-sm font-medium text-slate-700">Run after these outcomes</legend><div className="flex flex-wrap gap-2">{outcomes.map(([value, label]) => <label key={value} className={`flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-xs font-medium ${draft.outcomes.includes(value) ? "border-blue-200 bg-blue-50 text-blue-800" : "border-slate-200 text-slate-600"}`}><input type="checkbox" className="size-3.5 accent-blue-700" checked={draft.outcomes.includes(value)} onChange={event => setDraft({ ...draft, outcomes: event.target.checked ? [...draft.outcomes, value] : draft.outcomes.filter(item => item !== value) })} />{label}</label>)}</div></fieldset>}
                <OptionalSettings title="Only screen certain records" description={draft.condition ? "Condition added" : "Optional · Screen all eligible records by default"} defaultOpen={!!draft.condition}>
                  <label className="flex items-center gap-2 text-sm text-slate-700"><input type="checkbox" className="size-4 accent-blue-700" checked={!!draft.condition} onChange={event => setDraft({ ...draft, condition: event.target.checked ? { kind: "all", children: [{ kind: "always" }] } : undefined })} />Add a condition</label>
                  {draft.condition && <WorkflowConditionEditor value={draft.condition} total={conditionCount(draft.condition)} onChange={condition => setDraft({ ...draft, condition })} />}
                </OptionalSettings>
              </FormSection>
              <FormSection step="03" title="Where matches go">
                <label className={labelClass}><span>Review inbox</span><select aria-label="Review inbox" required className={selectClass} value={draft.inbox_id} onChange={event => setDraft({ ...draft, inbox_id: event.target.value })}><option value="">Choose an inbox</option>{draft.inbox_id && !inboxes.data?.inboxes.some(inbox => inbox.id === draft.inbox_id) && <option value={draft.inbox_id} disabled>Unavailable inbox</option>}{inboxes.data?.inboxes.filter(inbox => inbox.status === "active" || inbox.id === draft.inbox_id).map(inbox => <option key={inbox.id} value={inbox.id}>{inbox.name}{inbox.status !== "active" ? " (inactive)" : ""}</option>)}</select></label>
                {(data.counterparty_supported || draft.counterparty_id_field) && <OptionalSettings title="Remember cleared matches" description="Optional · Identify the same beneficiary next time" defaultOpen={!!draft.counterparty_id_field}>
                  <label className={labelClass}><span>Beneficiary ID field</span><select aria-label="Beneficiary ID field" className={selectClass} disabled={!data.counterparty_supported && !draft.counterparty_id_field} value={draft.counterparty_id_field ?? ""} onChange={event => setDraft({ ...draft, counterparty_id_field: event.target.value || undefined })}><option value="">Do not remember cleared matches</option>{draft.counterparty_id_field && (!data.counterparty_supported || !data.fields.includes(draft.counterparty_id_field)) && <option value={draft.counterparty_id_field} disabled>Unavailable: {draft.counterparty_id_field}</option>}{data.counterparty_supported && data.fields.map(field => <option key={field}>{field}</option>)}</select></label>
                  <p className="text-xs leading-relaxed text-slate-500">Use a stable ID, such as an account or beneficiary ID. Reviewers can exclude a false match for this beneficiary and matched entity within your organization.</p>
                  {invalidCounterparty && <p role="alert" className="text-sm text-amber-800">This field is unavailable. Clear it before saving.</p>}
                </OptionalSettings>}
                <label className="flex cursor-pointer items-center justify-between gap-4 rounded-xl border border-slate-200 px-4 py-3">
                  <span className="text-sm font-medium text-slate-800">Active<span className="mt-0.5 block text-xs font-normal text-slate-500">Apply to future decisions</span></span>
                  <span className="relative inline-flex shrink-0"><input type="checkbox" role="switch" aria-label="Active" className="peer absolute inset-0 z-10 m-0 h-full w-full cursor-pointer opacity-0" checked={draft.enabled} onChange={event => setDraft({ ...draft, enabled: event.target.checked })} /><span aria-hidden="true" className="h-6 w-11 rounded-full bg-slate-200 transition-colors peer-checked:bg-blue-700 peer-focus-visible:ring-2 peer-focus-visible:ring-blue-500 peer-focus-visible:ring-offset-2 peer-disabled:opacity-50" /><span aria-hidden="true" className="absolute left-1 top-1 size-4 rounded-full bg-white shadow-sm transition-transform peer-checked:translate-x-5" /></span>
                </label>
              </FormSection>
            </Card>
            <OptionalSettings title="Advanced settings" description="Result limits, data sources and name cleanup" defaultOpen={unsupportedMatching || invalidRecognition || invalidIgnoreList}>
              <div className="grid gap-4 sm:grid-cols-2">
                {(limitSupported || draft.result_limit !== undefined) && <label className={labelClass}><span>Maximum matches</span><Input className={inputClass} type="number" min={1} max={50} step={1} placeholder={data.default_result_limit !== undefined ? String(data.default_result_limit) : "Provider default"} value={draft.result_limit ?? ""} onChange={event => setDraft({ ...draft, result_limit: event.target.value === "" ? undefined : Number(event.target.value) })} /></label>}
                {(datasetsSupported || !!draft.datasets?.length) && <label className={labelClass}><span>Source datasets</span><Input className={inputClass} placeholder="e.g. dataset_one, dataset_two" value={draft.datasets?.join(",") ?? ""} onChange={event => setDraft({ ...draft, datasets: event.target.value === "" ? undefined : event.target.value.split(",") })} onBlur={() => setDraft({ ...draft, datasets: draft.datasets?.map(value => value.trim()) })} /><span className="block text-xs font-normal text-slate-500">Dataset IDs, separated by commas.</span></label>}
              </div>
              {unsupportedMatching && <p role="alert" className="text-sm text-amber-800">Some saved matching settings are unavailable. Clear unsupported values or restore the provider before saving.</p>}
              <div className="space-y-4 border-t border-slate-100 pt-4">
                <h4 className="text-sm font-semibold text-slate-800">Name cleanup</h4>
                <div className="grid gap-4 sm:grid-cols-2">
                  {(!!data.ignore_lists?.length || draft.ignore_list_id) && <label className={labelClass}><span>Words to ignore</span><select aria-label="Words to ignore" className={selectClass} value={draft.ignore_list_id ?? ""} onChange={event => setDraft({ ...draft, ignore_list_id: event.target.value || undefined })}><option value="">Keep all words</option>{draft.ignore_list_id && !data.ignore_lists?.some(list => list.id === draft.ignore_list_id) && <option value={draft.ignore_list_id} disabled>Unavailable list</option>}{data.ignore_lists?.map(list => <option key={list.id} value={list.id}>{list.name}</option>)}</select><span className="block text-xs font-normal text-slate-500">Removes words from a list; this is not a watchlist.</span></label>}
                  <label className={labelClass}><span>Minimum name length</span><Input className={inputClass} type="number" min={0} max={200} step={1} value={draft.minimum_name_length ?? 0} onChange={event => setDraft({ ...draft, minimum_name_length: Number(event.target.value) })} /></label>
                </div>
                <label className="flex items-center gap-2 text-sm text-slate-700"><input type="checkbox" className="size-4 accent-blue-700" checked={draft.remove_digits ?? false} onChange={event => setDraft({ ...draft, remove_digits: event.target.checked })} />Remove numbers from names</label>
                <p className="text-xs text-slate-500">Short or empty names are skipped. Set the minimum to 0 to turn off the length check.</p>
                {(data.name_recognition_supported || draft.use_ner) && <div className="space-y-3">
                  <label className="flex items-center gap-2 text-sm text-slate-700"><input type="checkbox" className="size-4 accent-blue-700" checked={draft.use_ner ?? false} onChange={event => setDraft({ ...draft, use_ner: event.target.checked, ner_ignore_classification: event.target.checked ? draft.ner_ignore_classification : false })} />Extract names from text</label>
                  {draft.use_ner && <><label className="flex items-center gap-2 text-sm text-slate-700"><input type="checkbox" className="size-4 accent-blue-700" checked={draft.ner_ignore_classification ?? false} onChange={event => setDraft({ ...draft, ner_ignore_classification: event.target.checked })} />Ignore detected entity type</label><p className="text-xs text-slate-500">Uses the configured name recognition service and may take longer.</p>{invalidRecognition && <p role="alert" className="text-sm text-amber-800">Name recognition is unavailable. Turn it off or restore the service before saving.</p>}</>}
                </div>}
                {invalidIgnoreList && <p role="alert" className="text-sm text-amber-800">The word list is unavailable. Choose another list or keep all words.</p>}
              </div>
            </OptionalSettings>
          </fieldset>
          <aside aria-label="Screening summary" className="sticky top-6 hidden space-y-4 xl:block">
            <Card className="rounded-2xl shadow-none"><CardContent className="p-5">
              <h2 className="mb-5 flex items-center gap-2 text-sm font-semibold text-slate-900"><ScanSearch aria-hidden="true" className="size-4 text-blue-700" />At a glance</h2>
              <dl className="space-y-4 text-sm"><div><dt className="text-xs text-slate-500">Screening</dt><dd className="mt-1 font-medium">{draft.entity_type === "Organization" ? "Company" : "Person"}</dd></div><div><dt className="text-xs text-slate-500">Name to check</dt><dd className="mt-1 break-all font-medium">{draft.name_field || "Not selected"}</dd></div><div><dt className="text-xs text-slate-500">Minimum similarity</dt><dd className="mt-1 font-medium">{draft.threshold !== undefined ? `${draft.threshold}%` : data.default_threshold !== undefined ? `${data.default_threshold}% (default)` : "Provider default"}</dd></div><div><dt className="text-xs text-slate-500">Timing</dt><dd className="mt-1 font-medium">{draft.forced_outcome ? "During decision" : "After decision"}</dd></div><div><dt className="text-xs text-slate-500">On a match</dt><dd className="mt-1 font-medium">{draft.forced_outcome ? `At least ${outcomeLabel(draft.forced_outcome)}` : "Send for review"}</dd></div><div className="border-t border-slate-100 pt-4"><dt className="flex items-center gap-2 text-xs text-slate-500"><InboxIcon aria-hidden="true" className="size-3.5" />Review inbox</dt><dd className="mt-1 break-words font-medium">{inboxes.data?.inboxes.find(inbox => inbox.id === draft.inbox_id)?.name ?? "Not selected"}</dd></div></dl>
            </CardContent></Card>
          </aside>
        </div>
        {inboxes.isLoading && <p role="status" className="text-sm text-slate-500">Loading inboxes…</p>}
        {inboxes.isError && <div role="alert" className="space-y-2 text-sm text-red-700"><p>Could not load review inboxes.</p><Button type="button" variant="outline" onClick={() => inboxes.refetch()}>Retry inboxes</Button></div>}
        <div className="flex flex-wrap items-center justify-end gap-2 rounded-2xl border border-slate-200 bg-white p-3 shadow-sm">
          <span className="mr-auto hidden pl-2 text-xs text-slate-500 sm:block">Changes apply to future decisions.</span>
          <Button type="button" variant="outline" className="rounded-xl" disabled={saving} onClick={() => { setDraft(null); setError(""); }}>Cancel</Button>
          <Button type="submit" className="rounded-xl" disabled={saveDisabled}><Save aria-hidden="true" className="size-4" />{saving ? "Saving…" : "Save screening"}</Button>
        </div>
      </form>}
    </>}
  </div>;
}
