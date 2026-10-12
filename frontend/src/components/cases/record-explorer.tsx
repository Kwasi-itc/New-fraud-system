"use client";
import { useState } from "react";
import Link from "next/link";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { requestCase, writeCase } from "@/lib/case-manager-api";
import { caseKeys } from "@/lib/case-manager-query";
import { Problem, readable } from "./shared";

type Hop = { connection: string; object_id: string; label: string };
type Connection = { id: string; label: string; target: string; reason: string };
type AssociatedDecision = { id: string; scenario_name: string; outcome: string; score: number; triggered: boolean; created_at: string; case?: { id: string; name: string; status: string }; can_attach: boolean };
type Result = { table: string; current: { object_id: string; data: Record<string, unknown> }; connections: Connection[]; records: { object_id: string }[]; next_after: string; decisions: AssociatedDecision[]; next_decision_after: string };
export function RecordExplorer({ tenant, session, caseId, decisionId }: { tenant: string; session: string; caseId: string; decisionId: string }) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false), [path, setPath] = useState<Hop[]>([]), [connection, setConnection] = useState(""), [after, setAfter] = useState("");
  const [saving, setSaving] = useState(false), [notice, setNotice] = useState(""), [saveError, setSaveError] = useState<Error | null>(null);
  const [decisionAfter, setDecisionAfter] = useState(""), [attaching, setAttaching] = useState("");
  const query = useQuery<Result>({ queryKey: [...caseKeys.scope(tenant, session), caseId, "explore", decisionId, path, connection, after, decisionAfter], enabled: open, retry: false, staleTime: 0, gcTime: 0,
    queryFn: ({ signal }) => requestCase(tenant, `cases/${caseId}/explore`, { method: "POST", signal, body: JSON.stringify({ decision_id: decisionId, path: path.map(({ connection, object_id }) => ({ connection, object_id })), connection, after, decision_after: decisionAfter }) }) });
  function navigate(next: Hop[]) { setPath(next); setConnection(""); setAfter(""); setDecisionAfter(""); setNotice(""); setSaveError(null); }
  const data = query.error ? undefined : query.data;
  const selected = data?.connections.find(e => e.id === connection);
  async function attachDecision(target: AssociatedDecision) {
    if (!window.confirm(`Attach decision ${target.id} to this case? This may start the investigation and assign you under the inbox rules. Decisions in another case require an explicit transfer.`)) return;
    setAttaching(target.id); setSaveError(null); setNotice("");
    try {
      await writeCase(tenant, `cases/${caseId}/explore/attach`, { decision_id: decisionId, path: path.map(({ connection, object_id }) => ({ connection, object_id })), target_decision_id: target.id });
      setNotice("Decision attached to this case.");
    } catch (e) { setSaveError(e instanceof Error ? e : new Error("Attachment could not be confirmed. Refresh before retrying.")); }
    finally { setAttaching(""); void queryClient.invalidateQueries({ queryKey: caseKeys.scope(tenant, session) }); }
  }
  async function saveTrail() {
    if (!data || !window.confirm("Add this investigation path to the case notes? This records the path, not a snapshot of the live records.")) return;
    setSaving(true); setSaveError(null);
    try { await writeCase(tenant, `cases/${caseId}/comments`, { comment: `Investigation connection trail\nDecision: ${decisionId}\n${path.map(h => `${h.label}: ${h.object_id} [${h.connection}]`).join("\n")}\nCurrent record: ${data.table} / ${data.current.object_id}\nObserved: ${new Date().toISOString()}\nLive record navigation; not an evidence snapshot.` }); setNotice("Connection trail added to case notes."); }
    catch (e) { setSaveError(e instanceof Error ? e : new Error("Could not save the connection trail.")); }
    finally { setSaving(false); void queryClient.invalidateQueries({ queryKey: caseKeys.scope(tenant, session) }); }
  }
  return <div className="space-y-3"><Button size="sm" variant="outline" aria-expanded={open} onClick={() => setOpen(v => !v)}>{open ? "Hide connections" : "Explore connections"}</Button>
    {open && <section aria-label="Record connections" className="space-y-4 rounded border p-4">
      <p className="text-sm text-slate-600">Explore current records through configured relationships. These values may differ from the evidence recorded when the decision ran.</p>
      <nav aria-label="Investigation trail" className="flex flex-wrap gap-2"><button className="underline" onClick={() => navigate([])}>Decision record</button>{path.map((hop, index) => <button key={index} className="underline" onClick={() => navigate(path.slice(0, index + 1))}>→ {hop.label}: {hop.object_id}</button>)}</nav>
      <Problem error={query.error} retry={() => void query.refetch()} />
      {query.isPending && <p role="status">Loading connected records…</p>}
      {data && <>
        <h3 className="font-semibold">{data.table}: {data.current.object_id}</h3>
        <dl className="space-y-2 text-sm">{Object.entries(data.current.data).map(([key, value]) => <div key={key} className="grid gap-2 sm:grid-cols-[160px_1fr]"><dt className="font-medium">{readable(key)}</dt><dd className="break-all whitespace-pre-wrap">{value == null ? "Not recorded" : typeof value === "object" ? JSON.stringify(value, null, 2) : String(value)}</dd></div>)}</dl>
        <label className="block text-sm font-medium">Connection<select className="ml-2 max-w-full rounded border p-2" value={connection} onChange={e => { setConnection(e.target.value); setAfter(""); }}><option value="">Choose a relationship</option>{data.connections.map(e => <option key={e.id} value={e.id}>{e.label} → {e.target}</option>)}</select></label>
        {!data.connections.length && <p>No active configured links, pivots or navigation options for this record.</p>}
        {selected && <><p className="text-sm">Connected because: {selected.reason}</p><ul className="space-y-2">{data.records.map(record => <li key={record.object_id}><Button variant="outline" size="sm" disabled={path.length >= 6} onClick={() => navigate([...path, { connection, object_id: record.object_id, label: selected.target }])}>Open {selected.target}: {record.object_id}</Button></li>)}</ul>{!data.records.length && <p>No matching records on this page. Empty relationship values do not match missing values.</p>}<div className="flex gap-3">{after && <Button variant="outline" size="sm" onClick={() => setAfter("")}>First records</Button>}{data.next_after && <Button variant="outline" size="sm" onClick={() => setAfter(data.next_after)}>More connected records</Button>}</div></>}
        {path.length >= 6 && <p>Six-step exploration limit reached. Use the trail to return to an earlier record.</p>}
        <section aria-label="Associated decisions" className="space-y-3 border-t pt-4"><h4 className="font-semibold">Decisions for this record</h4><p className="text-sm text-slate-600">Newest first. Decisions belonging to cases outside your access are omitted. Discovery does not attach them automatically.</p>
          {!data.decisions?.length && <p>No accessible decisions on this page.</p>}
          <ul className="space-y-3">{data.decisions?.map(d => <li key={d.id} className="space-y-2 rounded border p-3 text-sm"><p className="font-medium">{d.scenario_name} — {readable(d.outcome)}</p><p className="break-all">Decision {d.id}</p><p>Score: {d.score} · Triggered: {d.triggered ? "Yes" : "No"} · {new Date(d.created_at).toLocaleString()}</p>
            {d.case ? <p>{d.case.id === caseId ? "Already in this case: " : "Case: "}<Link className="underline" href={`/cases/${d.case.id}?${new URLSearchParams({ tenant })}`}>{d.case.name}</Link> · {readable(d.case.status)} <Link className="ml-2 underline" href={`/cases/decisions/${d.id}?${new URLSearchParams({ tenant })}`}>Open decision investigation</Link></p> : <p>Not attached to a case.</p>}
            {d.can_attach && <Button size="sm" variant="outline" disabled={!!attaching} onClick={() => void attachDecision(d)}>{attaching === d.id ? "Attaching…" : "Attach decision to this case"}</Button>}
          </li>)}</ul>
          <div className="flex gap-3">{decisionAfter && <Button size="sm" variant="outline" onClick={() => setDecisionAfter("")}>Newest decisions</Button>}{data.next_decision_after && <Button size="sm" variant="outline" onClick={() => setDecisionAfter(data.next_decision_after)}>Older decisions</Button>}</div>
        </section>
        <Button variant="outline" size="sm" disabled={saving} onClick={() => void saveTrail()}>Save connection trail to case notes</Button>
      </>}
      <Problem error={saveError} />{notice && <p role="status">{notice}</p>}
    </section>}
  </div>;
}
