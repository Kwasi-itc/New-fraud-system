"use client";

import { WorkflowConditionEditor, conditionCount, validCondition, type WorkflowCondition } from "./workflow-condition-editor";
import Link from "next/link";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { requestCase, type Inbox } from "@/lib/case-manager-api";

type Rule = { use_ner?: boolean; ner_ignore_classification?: boolean; ignore_list_id?: string; minimum_name_length?: number; remove_digits?: boolean; condition?: WorkflowCondition; threshold?: number; result_limit?: number; datasets?: string[]; id: string; name: string; enabled: boolean; entity_type: string; name_field: string; address_field?: string; birth_date_field?: string; nationality_field?: string; country_field?: string; registration_number_field?: string; outcomes: string[]; inbox_id: string; updated_at: string | null };
type ConfigurationPage = { name_recognition_supported?: boolean; ignore_lists?: { id: string; name: string }[]; matching_settings_supported: boolean; tenant_id: string; can_edit: boolean; scenario_name: string; fields: string[]; address_supported: boolean; birth_date_supported: boolean; nationality_supported: boolean; country_supported: boolean; registration_number_supported: boolean; rules: Rule[]; other_rules: number };
const identityMappings = [{ key: "nationality_field", capability: "nationality_supported", entity: "Person", label: "Nationality field (optional)" }, { key: "country_field", capability: "country_supported", entity: "Organization", label: "Country field (optional)" }, { key: "registration_number_field", capability: "registration_number_supported", entity: "Organization", label: "Registration-number field (optional)" }] as const;
const outcomes = [["approve", "Approve"], ["review", "Review"], ["block_and_review", "Block and review"], ["decline", "Decline"]];
async function request(scenario: string, rule?: Rule): Promise<ConfigurationPage | Rule> {
  const response = await fetch(`/api/scenario-screening/scenarios/${encodeURIComponent(scenario)}${rule ? `/configs/${rule.id}` : ""}`, {
    method: rule ? "PUT" : "GET", headers: { "Content-Type": "application/json" }, body: rule ? JSON.stringify(rule) : undefined, cache: "no-store",
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error ?? "Screening configuration unavailable.");
  return data;
}

export function ScenarioScreeningPage({ scenarioId }: { scenarioId: string }) {
  const [draft, setDraft] = useState<Rule | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const page = useQuery({ queryKey: ["scenario-screening", scenarioId], queryFn: async () => await request(scenarioId) as ConfigurationPage });
  const tenant = page.data?.tenant_id ?? "";
  const inboxes = useQuery({ queryKey: ["cases", tenant, "inboxes"], queryFn: () => requestCase<{ inboxes: Inbox[] }>(tenant, "inboxes"), enabled: !!tenant });
  const edit = (rule?: Rule) => {
    setError(""); setNotice("");
    setDraft(rule ? { ...rule, outcomes: [...rule.outcomes] } : { id: crypto.randomUUID(), name: "", enabled: false, entity_type: "Person", name_field: "", address_field: "", birth_date_field: "", nationality_field: "", country_field: "", registration_number_field: "", outcomes: ["review"], inbox_id: "", updated_at: null });
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
  const selectClass = "h-10 w-full rounded-xl border border-slate-200 bg-white px-3";
  return <div className="mx-auto max-w-5xl space-y-6 px-4 sm:px-6">
    <Link href={`/detection/${scenarioId}`} className="text-sm text-blue-800">← Back to scenario</Link>
    <header className="space-y-2"><h1 className="text-2xl font-semibold">Scenario screening</h1>
      <p className="text-slate-600">{data?.scenario_name}</p>
      <p className="max-w-3xl text-sm text-slate-600">Screen an object with OpenSanctions after a decision has one of the selected outcomes. Screening runs asynchronously and does not recalculate the original decision score. Potential matches are routed to the selected review inbox.</p>
    </header>
    {page.isLoading && <p role="status">Loading screening configurations…</p>}
    {page.isError && <div role="alert" className="space-y-2 text-red-700"><p>{page.error.message}</p><Button variant="outline" onClick={() => page.refetch()}>Retry loading</Button></div>}
    {notice && <p role="status" className="text-green-800">{notice}</p>}
    {error && <div className="space-y-2"><p role="alert" className="text-red-700">{error} Reload the saved configuration to resolve a stale edit or uncertain response.</p><Button variant="outline" disabled={saving} onClick={() => { setDraft(null); setError(""); void page.refetch(); }}>Discard draft and reload</Button></div>}
    {data && <>
      {!data.can_edit && <p className="text-sm text-slate-600">Read-only. A platform administrator with case access must save live configuration changes.</p>}
      {data.other_rules > 0 && <p className="text-sm text-amber-800">{data.other_rules} existing configuration(s) use settings this editor does not support. They remain unchanged.</p>}
      <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-lg font-semibold">Configurations</h2>
        <Button disabled={!data.can_edit || saving || !!draft} onClick={() => edit()}>Create screening configuration</Button>
      </div>
      {!data.rules.length && <p className="rounded-xl border border-dashed p-6 text-slate-500">No editable screening configurations yet.</p>}
      <div className="space-y-3">{data.rules.map(rule => <Card key={rule.id} className="shadow-none"><CardContent className="flex flex-wrap items-center justify-between gap-3 p-4">
        <div><h3 className="font-semibold">{rule.name}</h3><p className="text-sm text-slate-600">{rule.entity_type} · {rule.name_field} · {rule.enabled ? "Enabled" : "Disabled"}</p><p className="text-xs text-slate-500">After: {rule.outcomes.map(v => outcomes.find(o => o[0] === v)?.[1] ?? v).join(", ")}</p></div>
        <Button variant="outline" disabled={saving || !!draft} onClick={() => edit(rule)}>{data.can_edit ? "Edit" : "View"}</Button>
      </CardContent></Card>)}</div>
      {draft && <Card className="shadow-none"><CardContent className="p-5">
        <form aria-label="Screening configuration" className="space-y-4" onSubmit={e => { e.preventDefault(); void save(); }}>
          <h2 className="text-lg font-semibold">{draft.updated_at ? "Edit configuration" : "New configuration"}</h2>
          <fieldset disabled={saving || !data.can_edit} className="space-y-4">
            <legend className="sr-only">Screening settings</legend>
            <label className="block space-y-1"><span>Name</span><Input required maxLength={200} value={draft.name} onChange={e => setDraft({ ...draft, name: e.target.value })} /></label>
            <div className="grid gap-4 sm:grid-cols-2">
              <label className="block space-y-1"><span>Entity type</span><select aria-label="Entity type" className={selectClass} value={draft.entity_type} onChange={e => setDraft({ ...draft, entity_type: e.target.value })}><option>Person</option><option>Organization</option></select></label>
              <label className="block space-y-1"><span>Name field</span><select aria-label="Name field" required className={selectClass} value={draft.name_field} onChange={e => setDraft({ ...draft, name_field: e.target.value })}><option value="">Select a text field</option>{draft.name_field && !data.fields.includes(draft.name_field) && <option value={draft.name_field} disabled>Unavailable field ({draft.name_field})</option>}{data.fields.map(field => <option key={field}>{field}</option>)}</select></label>
            </div>
            {(data.address_supported || draft.address_field) && <label className="block space-y-1"><span>Address field (optional)</span><select aria-label="Address field (optional)" className={selectClass} value={draft.address_field ?? ""} onChange={e => setDraft({ ...draft, address_field: e.target.value })}><option value="">Do not send an address</option>{draft.address_field && (!data.address_supported || !data.fields.includes(draft.address_field)) && <option value={draft.address_field} disabled>Unavailable field ({draft.address_field})</option>}{data.address_supported && data.fields.map(field => <option key={field}>{field}</option>)}</select><p className="text-xs text-slate-500">A nonblank mapped address is sent to OpenSanctions for matching. Missing or blank values are omitted; this is not an exact-address filter.</p></label>}
            {((draft.entity_type === "Person" && data.birth_date_supported) || draft.birth_date_field) && <label className="block space-y-1"><span>Birth-date field (optional)</span><select aria-label="Birth-date field (optional)" className={selectClass} value={draft.birth_date_field ?? ""} onChange={e => setDraft({ ...draft, birth_date_field: e.target.value })}><option value="">Do not send a birth date</option>{draft.birth_date_field && (draft.entity_type !== "Person" || !data.birth_date_supported || !data.fields.includes(draft.birth_date_field)) && <option value={draft.birth_date_field} disabled>Unavailable field ({draft.birth_date_field})</option>}{draft.entity_type === "Person" && data.birth_date_supported && data.fields.map(field => <option key={field}>{field}</option>)}</select><p className="text-xs text-slate-500">Map a text field containing YYYY or YYYY-MM-DD. Missing or blank values are omitted; invalid or future dates stop decision execution before persistence.</p></label>}
            {draft.entity_type !== "Person" && draft.birth_date_field && <p role="alert" className="text-amber-800">Birth date is supported only for Person. Clear its mapping before saving as Organization.</p>}
            {draft.entity_type === "Person" && !data.birth_date_supported && <p className="text-xs text-slate-500">Birth-date mapping is unavailable for the current provider capability response.</p>}
            {identityMappings.map(mapping => {
              const value = draft[mapping.key] ?? "";
              const supported = draft.entity_type === mapping.entity && data[mapping.capability];
              if (!supported && !value) return draft.entity_type === mapping.entity ? <p key={mapping.key} className="text-xs text-slate-500">{mapping.label} is unavailable for the current provider capability response.</p> : null;
              return <div key={mapping.key} className="space-y-1"><label className="block space-y-1"><span>{mapping.label}</span><select aria-label={mapping.label} className={selectClass} value={value} onChange={e => setDraft({ ...draft, [mapping.key]: e.target.value })}><option value="">Do not send this property</option>{value && (!supported || !data.fields.includes(value)) && <option value={value} disabled>Unavailable field ({value})</option>}{supported && data.fields.map(field => <option key={field}>{field}</option>)}</select></label><p className="text-xs text-slate-500">{mapping.key === "registration_number_field" ? "Map a text identifier of at most 100 characters. Surrounding spaces are trimmed; leading zeros and letter case are preserved. Missing or blank values are omitted." : "Map a text field containing a two-letter country code, such as GB or US. Missing or blank values are omitted. This is identity matching input, not a strict geographic filter."}</p>{value && draft.entity_type !== mapping.entity && <p role="alert" className="text-amber-800">This mapping is supported only for {mapping.entity}. Clear it before saving as {draft.entity_type}.</p>}</div>;
            })}
            {!data.address_supported && <p className="text-xs text-slate-500">Optional address mapping requires an available capability response from a configured direct OpenSanctions adapter. Name-only configuration remains available.</p>}
            {(data.matching_settings_supported || draft.threshold !== undefined || draft.result_limit !== undefined || !!draft.datasets?.length) && <fieldset className="space-y-3 rounded-xl border p-4"><legend>Matching settings</legend>
              <p className="text-xs text-slate-500">Leave blank to use provider defaults. Threshold selects potential matches; it does not confirm a match or change the decision score. Dataset IDs restrict the provider request; availability depends on your provider subscription.</p>
              <div className="grid gap-4 sm:grid-cols-2"><label className="block space-y-1"><span>Match threshold (0-100)</span><Input type="number" min={0} max={100} step={1} value={draft.threshold ?? ""} onChange={e => setDraft({ ...draft, threshold: e.target.value === "" ? undefined : Number(e.target.value) })} /></label>
              <label className="block space-y-1"><span>Result limit (1-50)</span><Input type="number" min={1} max={50} step={1} value={draft.result_limit ?? ""} onChange={e => setDraft({ ...draft, result_limit: e.target.value === "" ? undefined : Number(e.target.value) })} /></label></div>
              <label className="block space-y-1"><span>Dataset IDs (comma separated)</span><Input value={draft.datasets?.join(",") ?? ""} onChange={e => setDraft({ ...draft, datasets: e.target.value === "" ? undefined : e.target.value.split(",") })} onBlur={() => setDraft({ ...draft, datasets: draft.datasets?.map(v => v.trim()) })} /></label>
              {!data.matching_settings_supported && <p role="alert">Matching settings are unavailable. Clear all three settings or restore provider capability before saving.</p>}
            </fieldset>}
            {!data.fields.length && <p role="alert">The scenario object has no supported text fields. Configure its data model first.</p>}
            <fieldset className="flex flex-wrap gap-4"><legend className="mb-2">Screen after these decision outcomes</legend>{outcomes.map(([value, label]) => <label key={value} className="flex items-center gap-2"><input type="checkbox" checked={draft.outcomes.includes(value)} onChange={e => setDraft({ ...draft, outcomes: e.target.checked ? [...draft.outcomes, value] : draft.outcomes.filter(v => v !== value) })} />{label}</label>)}</fieldset>
            <fieldset className="space-y-3 rounded-xl border p-4"><legend>Name preprocessing</legend>{(data.name_recognition_supported || draft.use_ner) && <div className="space-y-2"><label className="flex items-center gap-2"><input type="checkbox" checked={draft.use_ner ?? false} onChange={e => setDraft({ ...draft, use_ner: e.target.checked, ner_ignore_classification: e.target.checked ? draft.ner_ignore_classification : false })} />Recognize names in the input</label>{draft.use_ner && <><label className="flex items-center gap-2"><input type="checkbox" checked={draft.ner_ignore_classification ?? false} onChange={e => setDraft({ ...draft, ner_ignore_classification: e.target.checked })} />Ignore recognized entity classification</label><p className="text-xs text-slate-500">The configured recognition service receives the name. Distinct recognized names supplement the original query. Recognition failure prevents screening submission; availability is not guaranteed by configuration.</p>{(!data.name_recognition_supported || !data.matching_settings_supported) && <p role="alert">Name recognition requires its configured service and the direct matching adapter. Disable it or restore configuration before saving.</p>}</>}</div>}<label className="block space-y-1"><span>Ignore list (optional)</span><select aria-label="Ignore list (optional)" className={selectClass} value={draft.ignore_list_id ?? ""} onChange={e => setDraft({ ...draft, ignore_list_id: e.target.value || undefined })}><option value="">Do not remove list words</option>{draft.ignore_list_id && !data.ignore_lists?.some(l => l.id === draft.ignore_list_id) && <option value={draft.ignore_list_id} disabled>Unavailable list</option>}{data.ignore_lists?.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}</select><p className="text-xs text-slate-500">Remove whole words case-insensitively using the current tenant list, after digit removal. List updates affect future decisions only. At most 1000 entries are supported; unavailable or oversized lists fail execution.</p></label><label className="block space-y-1"><span>Minimum name length</span><Input type="number" min={0} max={200} step={1} value={draft.minimum_name_length ?? 0} onChange={e => setDraft({ ...draft, minimum_name_length: Number(e.target.value) })} /></label><label className="flex items-center gap-2"><input type="checkbox" checked={draft.remove_digits ?? false} onChange={e => setDraft({ ...draft, remove_digits: e.target.checked })} />Remove digits from the name</label><p className="text-xs text-slate-500">Zero disables the length check. Length counts Unicode characters before and after digit removal. A name emptied or shortened below the minimum creates no screening request; this is not a provider no-hit result. Other identity fields are unchanged.</p></fieldset>
            <fieldset className="space-y-3"><legend>Additional screening condition</legend><label className="flex items-center gap-2"><input type="checkbox" checked={!!draft.condition} onChange={e => setDraft({ ...draft, condition: e.target.checked ? { kind: "all", children: [{ kind: "always" }] } : undefined })} />Require an additional condition</label><p className="text-xs text-slate-500">The selected outcome and this condition must both match. Conditions read fields on the evaluated object only. Missing fields do not match comparisons; use an existence condition when needed. A false condition creates no screening request.</p>{draft.condition && <WorkflowConditionEditor value={draft.condition} total={conditionCount(draft.condition)} onChange={condition => setDraft({ ...draft, condition })} />}</fieldset>
            <label className="block space-y-1"><span>Review inbox</span><select aria-label="Review inbox" required className={selectClass} value={draft.inbox_id} onChange={e => setDraft({ ...draft, inbox_id: e.target.value })}><option value="">Select an inbox</option>{draft.inbox_id && !inboxes.data?.inboxes.some(i => i.id === draft.inbox_id) && <option value={draft.inbox_id} disabled>Unavailable inbox</option>}{inboxes.data?.inboxes.filter(i => i.status === "active" || i.id === draft.inbox_id).map(i => <option key={i.id} value={i.id}>{i.name}{i.status !== "active" ? " (inactive)" : ""}</option>)}</select></label>
            <label className="flex items-center gap-2"><input type="checkbox" checked={draft.enabled} onChange={e => setDraft({ ...draft, enabled: e.target.checked })} />Enable for future decisions</label>
            <p className="text-xs text-slate-500">Provider: OpenSanctions. Saving configuration is not proof of provider or worker availability. Optional address mapping is available only when supported by the configured adapter.</p>
          </fieldset>
          {inboxes.isLoading && <p role="status">Loading authorized inboxes…</p>}
          {inboxes.isError && <div role="alert"><p>Could not load review inboxes.</p><Button type="button" variant="outline" onClick={() => inboxes.refetch()}>Retry inboxes</Button></div>}
          <div className="flex gap-2"><Button type="submit" disabled={(!!draft.use_ner && (!data.name_recognition_supported || !data.matching_settings_supported)) || (!!draft.ignore_list_id && !data.ignore_lists?.some(l => l.id === draft.ignore_list_id)) || (!!draft.condition && !validCondition(draft.condition)) || (!data.matching_settings_supported && (draft.threshold !== undefined || draft.result_limit !== undefined || !!draft.datasets?.length)) || identityMappings.some(m => !!draft[m.key] && (draft.entity_type !== m.entity || !data[m.capability] || !data.fields.includes(draft[m.key]!))) || (!!draft.birth_date_field && (draft.entity_type !== "Person" || !data.birth_date_supported || !data.fields.includes(draft.birth_date_field))) || saving || !data.can_edit || inboxes.isError || inboxes.isLoading || !draft.outcomes.length || !data.fields.includes(draft.name_field) || (!!draft.address_field && (!data.address_supported || !data.fields.includes(draft.address_field)))}>{saving ? "Saving…" : "Save configuration"}</Button><Button type="button" variant="outline" disabled={saving} onClick={() => { setDraft(null); setError(""); }}>Cancel</Button></div>
        </form>
      </CardContent></Card>}
    </>}
  </div>;
}
