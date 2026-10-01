"use client";
import { useState } from "react";
import type { CaseRecord, Inbox } from "@/lib/case-manager-api";
import { useCaseWrite } from "@/lib/case-manager-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Panel, Problem, fieldClass, labelClass } from "./shared";

type Result = { case_id: string; success: boolean; replayed: boolean; error?: string };
export function BulkActions({ tenant, session, cases, inboxes }: { tenant: string; session: string; cases: CaseRecord[]; inboxes: Inbox[] }) {
  const write = useCaseWrite(tenant, session);
  const [action, setAction] = useState("assign");
  const [pending, setPending] = useState<Record<string, unknown>>();
  const [results, setResults] = useState<Result[]>();
  async function run(payload: Record<string, unknown>) {
    setPending(payload); setResults(undefined);
    try { const response = await write.mutateAsync({ path: "case-bulk", payload }) as { results: Result[] }; setResults(response.results); }
    catch { /* Keep the operation ID for safe retry; errors appear below. */ }
  }
  return <Panel title="Bulk actions"><p className="text-sm text-slate-600">Select up to 100 loaded cases. Each case is checked and saved separately; failures do not undo successful cases.</p>
    <form className="grid gap-3 md:grid-cols-2" onSubmit={event => {
      event.preventDefault(); const data = new FormData(event.currentTarget);
      void run({ operation_id: crypto.randomUUID(), case_ids: data.getAll("cases"), action, assignee: data.get("assignee") || null, inbox_id: data.get("inbox") || null, outcome: data.get("outcome") || "unset", comment: data.get("comment") || "" });
    }}>
      <label className={labelClass}>Selected cases<select name="cases" multiple required size={5} className={fieldClass} disabled={write.isPending}>{cases.slice(0, 100).map(item => <option key={item.id} value={item.id}>{item.name} ({item.status})</option>)}</select></label>
      <div className="space-y-3"><label className={labelClass}>Bulk action<select value={action} disabled={write.isPending} onChange={event => setAction(event.target.value)} className={fieldClass}><option value="assign">Assign / unassign</option><option value="move">Move</option><option value="close">Close</option><option value="reopen">Reopen</option></select></label>
      {action === "assign" && <label className={labelClass}>Assignee subject (blank unassigns)<Input name="assignee" /></label>}
      {action === "move" && <label className={labelClass}>Destination inbox<select name="inbox" required className={fieldClass}><option value="">Select destination</option>{inboxes.filter(i => i.status === "active").map(i => <option value={i.id} key={i.id}>{i.name}</option>)}</select></label>}
      {action === "close" && <><label className={labelClass}>Bulk closing outcome<select name="outcome" className={fieldClass}><option value="false_positive">False positive</option><option value="valuable_alert">Valuable alert</option><option value="confirmed_risk">Confirmed risk</option></select></label><label className={labelClass}>Bulk closing findings<textarea name="comment" required maxLength={10000} className={fieldClass} /></label></>}
      <Button type="submit" disabled={write.isPending || !cases.length}>Apply bulk action</Button></div>
    </form>
    <Problem error={write.error} />
    {pending && <Button variant="outline" disabled={write.isPending} onClick={() => void run(pending)}>Retry same operation</Button>}
    {results && <ul aria-live="polite" className="space-y-2 text-sm">{results.map(result => <li key={result.case_id} className={result.success ? "text-slate-700" : "text-red-700"}>{cases.find(c => c.id === result.case_id)?.name ?? result.case_id}: {result.success ? result.replayed ? "Already completed" : "Completed" : result.error}</li>)}</ul>}
  </Panel>;
}
