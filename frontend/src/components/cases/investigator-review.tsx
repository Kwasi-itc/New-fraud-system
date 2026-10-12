"use client";
import { useId, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { caseKeys } from "@/lib/case-manager-query";
import { fieldClass, Problem, readable } from "./shared";

type ReviewState = { status: string; version: string; eligible: boolean; machine_outcome?: string; actions: string[] };
export function InvestigatorReview({ tenant, session, caseId, kind, resource }: { tenant: string; session: string; caseId: string; kind: "decisions" | "matches"; resource: string }) {
  const label = useId(); const client = useQueryClient();
  const [open, setOpen] = useState(false), [action, setAction] = useState(""), [findings, setFindings] = useState("");
  const [confirm, setConfirm] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState<Error | null>(null), [saved, setSaved] = useState(false);
  const operation = useRef<{ signature: string; id: string } | null>(null);
  const path = `/api/case-review/${tenant}/${caseId}/${kind}`;
  const query = useQuery<ReviewState>({ queryKey: [...caseKeys.scope(tenant, session), caseId, "review", kind, resource], enabled: open, retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false, staleTime: 0, gcTime: 0,
    queryFn: async () => { const response = await fetch(`${path}?resource_id=${encodeURIComponent(resource)}`, { cache: "no-store" }); const data = await response.json(); if (!response.ok) throw new Error(data.error ?? "Review is unavailable."); return data; } });
  return <div className="space-y-3"><Button size="sm" variant="outline" onClick={() => setOpen(!open)} aria-expanded={open}>{kind === "decisions" ? "Review decision" : "Review match"}</Button>{open && <div className="rounded border p-3">
    <Problem error={query.error} retry={() => void query.refetch()} />{query.isPending && <p role="status">Loading review state…</p>}
    {saved && <p role="status">Review recorded with case history.</p>}
    {query.isSuccess && !query.error && <><p>Review status: {readable(query.data.status)}</p>{query.data.machine_outcome && <p className="text-sm">Machine outcome: {readable(query.data.machine_outcome)}. Human review is recorded separately.</p>}
      {!query.data.eligible ? <p className="text-sm">No first-review action is available. Reviews require an open case in an active inbox and an eligible pending resource.</p> : <form className="mt-3 space-y-3" onSubmit={async event => {
        event.preventDefault(); setError(null);
        if (!action || !findings.trim() || new TextEncoder().encode(findings.trim()).length > 10000) { setError(new Error("Choose an action and enter findings of 1–10000 UTF-8 bytes.")); return; }
        if (!confirm) { setConfirm(true); return; }
        const payload = { resource_id: resource, expected_version: query.data.version, action, findings: findings.trim() };
        const signature = JSON.stringify(payload); if (operation.current?.signature !== signature) operation.current = { signature, id: crypto.randomUUID() };
        setBusy(true);
        try { const response = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ...payload, operation_id: operation.current.id }) }); const data = await response.json(); if (!response.ok) throw new Error(data.error ?? "Review failed."); setSaved(true); setConfirm(false); await client.invalidateQueries({ queryKey: caseKeys.scope(tenant, session) }); }
        catch (error) { setError(error instanceof Error ? error : new Error("Review response unavailable. Retry the same operation.")); }
        finally { setBusy(false); }
      }}><fieldset disabled={busy} className="space-y-3">
        <label className="block" htmlFor={`${label}-action`}>Review action</label><select id={`${label}-action`} className={fieldClass} required value={action} onChange={event => { setAction(event.target.value); setConfirm(false); }}><option value="">Choose an action</option>{query.data.actions.map(value => <option key={value} value={value}>{readable(value)}</option>)}</select>
        <label className="block" htmlFor={`${label}-findings`}>Review findings</label><textarea id={`${label}-findings`} className={fieldClass} required value={findings} onChange={event => { setFindings(event.target.value); setConfirm(false); }} />
        <Problem error={error} />{error && <Button type="button" variant="outline" onClick={() => { setConfirm(false); setError(null); void query.refetch(); }}>Refresh review state</Button>}{confirm && <p role="status">Confirm {readable(action)}. This first review cannot be edited here.</p>}
        <Button type="submit">{busy ? "Recording…" : confirm ? "Confirm review" : "Check review"}</Button>
      </fieldset></form>}</>}
  </div>}</div>;
}
