"use client";

import { useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { caseKeys } from "@/lib/case-manager-query";

type Context = { version: string; can_refine: boolean; reason?: string; queries: { name: string; type?: string }[] };
type Capabilities = { configured: boolean; entity_types: string[]; fields: Record<string, string[]>; threshold_min: number; threshold_max: number; limit_max: number };
type Result = { id: string; status: string; partial?: boolean; matches?: { id: string; name: string; score: number }[] };
async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, { ...init, cache: "no-store" }); const body = await r.json();
  if (!r.ok) throw new Error(body.error ?? "Refinement is unavailable."); return body;
}

export function RefineScreening({ tenant, session, caseId, screeningId }: { tenant: string; session: string; caseId: string; screeningId: string }) {
  const client = useQueryClient();
  const [open, setOpen] = useState(false), [name, setName] = useState(""), [selectedType, setType] = useState("");
  const [threshold, setThreshold] = useState(80), [limit, setLimit] = useState(30);
  const [identity, setIdentity] = useState<Record<string, string>>({}), [findings, setFindings] = useState("");
  const [resultId, setResultId] = useState(""), [busy, setBusy] = useState(false), [error, setError] = useState(""), [saved, setSaved] = useState(false);
  const operation = useRef<{ signature: string; id: string } | null>(null);
  const previewOperation = useRef<{ signature: string; id: string } | null>(null);
  const pollStarted = useRef(0);
  const path = `/api/case-refinement/${tenant}/${caseId}/${screeningId}`;
  const context = useQuery({ queryKey: [...caseKeys.scope(tenant, session), caseId, "refinement", screeningId], enabled: open, retry: false,
    queryFn: () => request<Context>(path) });
  const cap = useQuery({ queryKey: ["refinement-capabilities", tenant, session], enabled: open, retry: false,
    queryFn: () => request<Capabilities>("/api/screening-search/capabilities") });
  const result = useQuery({ queryKey: ["refinement-preview", tenant, session, resultId], enabled: !!resultId, retry: false,
    queryFn: () => request<Result>(`/api/screening-search/${resultId}`),
    refetchInterval: q => Date.now() - pollStarted.current < 120000 && q.state.data && ["pending", "queued", "processing"].includes(q.state.data.status) ? 2000 : false });
  const originalType = context.data?.queries[0]?.type;
  const type = selectedType || (originalType && cap.data?.entity_types.includes(originalType) ? originalType : cap.data?.entity_types[0] ?? "Person");
  const labels: Record<string, string> = { birth_date: "Birth date", nationality: "Nationality", country: "Country", registration_number: "Registration number", address: "Address" };
  const preview = result.data;
  const ready = preview && ["no_hit", "awaiting_review"].includes(preview.status) && !preview.partial;
  return <div className="space-y-3">
    <Button type="button" variant="outline" size="sm" onClick={() => setOpen(v => !v)} aria-expanded={open}>Refine search</Button>
    {open && <section aria-label="Refine screening search" className="space-y-4 rounded-xl border border-blue-200 bg-white p-4">
      <h3 className="font-semibold">Refine search</h3>
      <p className="text-sm text-slate-600">Preview a new search, then apply its complete results to this case. The original search, candidates and decision outcome remain in history.</p>
      {(context.isPending || cap.isPending) && <p role="status">Loading refinement settings…</p>}
      {(context.error || cap.error || result.error || error) && <p role="alert" className="text-sm text-rose-800">{error || context.error?.message || cap.error?.message || result.error?.message}</p>}
      {context.data && !context.data.can_refine && <p role="status">{context.data.reason}</p>}
      {context.data?.can_refine && cap.data?.configured && !saved && <>
        <p className="text-sm text-slate-600">Original search: {context.data.queries.map(q => q.name).join("; ")}</p>
        <form className="space-y-3" onSubmit={async e => {
          e.preventDefault(); setBusy(true); setError(""); setResultId(""); operation.current = null;
          try {
            const payload = { name: name.trim() || context.data!.queries[0]?.name, entity_type: type, threshold, limit, ...Object.fromEntries(Object.entries(identity).filter(([key, value]) => cap.data!.fields[type]?.includes(key) && value.trim())) };
            const signature = JSON.stringify(payload);
            if (previewOperation.current?.signature !== signature) previewOperation.current = { signature, id: crypto.randomUUID() };
            const created = await request<Result>("/api/screening-search", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ...payload, operation_id: previewOperation.current.id }) }); pollStarted.current = Date.now(); setResultId(created.id);
          }
          catch (e) { setError(e instanceof Error ? e.message : "Search failed."); } finally { setBusy(false); }
        }}>
          <fieldset disabled={busy} className="space-y-3">
            <label className="block text-sm">Name<Input aria-label="Refined name" maxLength={200} placeholder={context.data.queries[0]?.name} value={name} onChange={e => setName(e.target.value)} /></label>
            <label className="block text-sm">Entity type<select aria-label="Refined entity type" className="mt-1 h-10 w-full rounded-lg border px-3" value={type} onChange={e => { setType(e.target.value); setIdentity({}); }}>{cap.data.entity_types.map(t => <option key={t}>{t}</option>)}</select></label>
            {Object.entries(labels).filter(([key]) => cap.data!.fields[type]?.includes(key)).map(([key, label]) => <label key={key} className="block text-sm">{label}<Input aria-label={`Refined ${label.toLowerCase()}`} value={identity[key] ?? ""} onChange={e => setIdentity(v => ({ ...v, [key]: e.target.value }))} /></label>)}
            <div className="grid gap-3 sm:grid-cols-2"><label className="text-sm">Similarity threshold (%)<Input aria-label="Refined similarity threshold" type="number" min={cap.data.threshold_min} max={cap.data.threshold_max} required value={threshold} onChange={e => setThreshold(Number(e.target.value))} /></label><label className="text-sm">Result limit<Input aria-label="Refined result limit" type="number" min={1} max={cap.data.limit_max} required value={limit} onChange={e => setLimit(Number(e.target.value))} /></label></div>
            <Button type="submit" variant="outline">{busy ? "Searching…" : "Preview results"}</Button>
          </fieldset>
        </form>
        {resultId && !ready && !result.isError && <p role="status">{preview?.partial ? "Results are incomplete. Narrow the search and preview again before applying." : preview?.status === "failed" ? "The search failed. Refine the inputs and try again." : "Waiting for persisted search results…"}</p>}
        {ready && <div className="space-y-3 rounded-lg border bg-slate-50 p-4">
          <h4 className="font-semibold">{preview.matches?.length ? `${preview.matches.length} potential matches` : "No possible matches found"}</h4>
          {preview.matches?.map(m => <p key={m.id} className="text-sm">{m.name} · {Math.round(m.score * 100)}% similarity</p>)}
          <label className="block text-sm">Refinement findings<textarea aria-label="Refinement findings" required maxLength={2000} className="mt-1 w-full rounded-lg border p-2" value={findings} onChange={e => setFindings(e.target.value)} /></label>
          <p className="text-xs text-slate-600">Applying changes replaces the active screening evidence for review. It does not approve this decision.</p>
          <Button type="button" disabled={busy || !findings.trim()} onClick={async () => {
            if (new TextEncoder().encode(findings.trim()).length > 2000) { setError("Enter refinement findings of 1-2000 UTF-8 bytes."); return; }
            const payload = { replacement_id: resultId, expected_version: context.data!.version, findings: findings.trim() }, signature = JSON.stringify(payload);
            if (operation.current?.signature !== signature) operation.current = { signature, id: crypto.randomUUID() };
            setBusy(true); setError("");
            try { await request(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ...payload, operation_id: operation.current.id }) }); setSaved(true); await client.invalidateQueries({ queryKey: caseKeys.scope(tenant, session) }); }
            catch (e) { setError(e instanceof Error ? e.message : "Apply failed. Retry the same operation."); } finally { setBusy(false); }
          }}>Apply changes</Button>
        </div>}
      </>}
      {saved && <p role="status" className="text-sm text-green-800">Refinement applied. Original evidence retained; review the replacement results in this case.</p>}
    </section>}
  </div>;
}
