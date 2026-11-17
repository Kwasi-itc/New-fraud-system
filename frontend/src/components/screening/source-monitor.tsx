"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { OpenInvestigation } from "./open-investigation";

type Monitor = {
  enrolled: boolean; enabled: boolean; current?: boolean; pending?: number; failed?: number;
  latest_result?: { id: string; status: string; partial?: boolean; matches: { id: string; name: string; score: number }[] };
  history?: { id: string; status: string; attempts: number; created_at: string }[];
  history_cursor?: string;
};

export function SourceMonitor({ searchId, scope }: { searchId: string; scope: string }) {
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [cursors, setCursors] = useState<string[]>([]);
  const url = `/api/screening-search/${searchId}/monitor`;
  const state = useQuery({ queryKey: ["source-monitor", scope, searchId, cursors.at(-1) ?? ""], gcTime: 0, retry: false, refetchInterval: 5000,
    queryFn: async () => { const response = await fetch(`${url}${cursors.length ? `?after=${encodeURIComponent(cursors.at(-1)!)}` : ""}`, { cache: "no-store" }); const body = await response.json(); if (!response.ok) throw new Error(body.error ?? "Monitoring unavailable."); return body as Monitor; },
  });
  async function update(body: {enabled: boolean} | {retry_target: string}) {
    setSaving(true); setError("");
    try { const response = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); const result = await response.json(); if (!response.ok) throw new Error(result.error ?? "Unable to update monitoring."); await state.refetch(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to update monitoring."); }
    finally { setSaving(false); }
  }
  const data = state.data;
  return <section aria-label="Dataset monitoring" className="rounded-xl border border-slate-200 bg-white p-4 text-sm">
    <div className="flex flex-wrap items-center justify-between gap-3"><div><h3 className="font-semibold text-slate-900">Dataset updates</h3><p className="mt-1 text-slate-600">Monitor this saved query when its selected datasets change.</p></div>
      <Button variant="outline" disabled={saving || state.isPending || state.isError} onClick={() => void update({enabled:!data?.enabled})}>{saving ? "Saving..." : data?.enabled ? "Pause monitoring" : "Monitor updates"}</Button>
    </div>
    {(error || state.error) && <p role="alert" className="mt-3 text-red-700">{error || state.error?.message}</p>}
    {data?.enrolled && <div className="mt-3 space-y-3">
      <p role="status" className={data.current ? "text-emerald-800" : "rounded-lg bg-amber-50 p-3 text-amber-900"}>{data.current ? "Latest screening uses the current dataset versions." : "The saved screening is out of date. A successful rescreen is required before treating it as current."}</p>
      <p className="text-slate-600">{data.enabled ? "Monitoring active" : "Monitoring paused"} · {data.pending ?? 0} pending · {data.failed ?? 0} failed</p>
      {data.latest_result && data.latest_result.id !== searchId && <div className="rounded-lg border border-slate-200 p-3"><p className="font-medium">Latest successful screening · {data.latest_result.status.replaceAll("_", " ")}</p>{data.latest_result.matches.length ? <ul className="mt-2 space-y-2">{data.latest_result.matches.map(match => <li key={match.id} className="flex flex-wrap items-center justify-between gap-2"><span>{match.name} · {Math.round(match.score * 100)}% similarity</span><OpenInvestigation tenant={scope.split(":")[0]} scope={scope} screening={data.latest_result!.id} candidate={match.id} /></li>)}</ul> : <p className="mt-2">No candidates returned{data.current ? " on the current versions" : " on those historical versions"}.</p>}</div>}
      {!!data.history?.length && <details><summary className="cursor-pointer font-medium">Rescreen history</summary><ul className="mt-2 space-y-2 text-slate-600">{data.history.map(run => <li key={run.id}>{new Date(run.created_at).toLocaleString()} / {run.status} / {run.attempts} attempts {data.enabled && ["failed","cancelled"].includes(run.status) && <Button variant="outline" disabled={saving} onClick={() => void update({retry_target:run.id})}>Retry</Button>}</li>)}</ul><div className="mt-3 flex gap-2"><Button variant="outline" disabled={!cursors.length || state.isFetching} onClick={() => setCursors(value => value.slice(0,-1))}>Newer rescreens</Button><Button variant="outline" disabled={!data.history_cursor || state.isFetching} onClick={() => setCursors(value => [...value,data.history_cursor!])}>Older rescreens</Button></div></details>}
    </div>}
  </section>;
}
