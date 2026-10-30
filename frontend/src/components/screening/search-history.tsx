"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";

type Entry = { id: string; name: string; entity_type: string; status: string; partial: boolean; created_at: string };
type Page = { items: Entry[]; next_cursor: string };
const statuses: Record<string, string> = { pending: "Queued", queued: "Queued", processing: "Searching", awaiting_review: "Potential matches", no_hit: "No matches", confirmed_hit: "Confirmed hit", failed: "Failed", archived: "Archived" };

export function SearchHistory({ onOpen, refreshKey, scope, disabled }: { onOpen: (id: string) => void; refreshKey: string; scope: string; disabled: boolean }) {
  const [full, setFull] = useState(false);
  const [status, setStatus] = useState(""); const [type, setType] = useState("");
  const [from, setFrom] = useState(""); const [to, setTo] = useState("");
  const [cursors, setCursors] = useState([""]);
  const query = new URLSearchParams({ limit: full ? "20" : "5" });
  if (full && status) query.set("status", status); if (full && type) query.set("entity_type", type);
  if (full && from) query.set("from", from); if (full && to) query.set("to", to);
  if (cursors.at(-1)) query.set("cursor", cursors.at(-1)!);
  const history = useQuery<Page>({ queryKey: ["manual-search-history", scope, query.toString(), refreshKey], staleTime: 0, gcTime: 0, retry: false,
    queryFn: async ({ signal }) => { const response = await fetch(`/api/screening-search?${query}`, { signal, cache: "no-store" }); const body = await response.json(); if (!response.ok) throw new Error(body.error ?? "History could not be loaded."); return body; },
  });
  const control = "mt-1 block w-full rounded-lg border border-slate-300 bg-white px-2 py-2 text-sm";
  return <section aria-label="Search history" className="mt-8 rounded-2xl border border-slate-200 bg-white p-6">
    <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold">{full ? "Search history" : "Recent searches"}</h2><p className="mt-1 text-xs text-slate-500">Your searches only. Opening a result does not rerun the provider.</p></div><div className="flex gap-2"><Button variant="outline" onClick={() => { setCursors([""]); void history.refetch(); }}>Refresh history</Button><Button variant="outline" onClick={() => { setFull(!full); setCursors([""]); }}>{full ? "Show recent" : "View all history"}</Button></div></div>
    {full && <div className="my-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <label className="text-xs font-medium">History status<select className={control} value={status} onChange={e => { setStatus(e.target.value); setCursors([""]); }}><option value="">All statuses</option>{Object.entries(statuses).filter(([key]) => key !== "queued").map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></label>
      <label className="text-xs font-medium">History entity type<select className={control} value={type} onChange={e => { setType(e.target.value); setCursors([""]); }}><option value="">All types</option><option>Person</option><option>Organization</option></select></label>
      <label className="text-xs font-medium">From (UTC)<input type="date" className={control} value={from} onChange={e => { setFrom(e.target.value); setCursors([""]); }} /></label>
      <label className="text-xs font-medium">To (UTC)<input type="date" min={from || undefined} className={control} value={to} onChange={e => { setTo(e.target.value); setCursors([""]); }} /></label>
    </div>}
    {history.isPending && <p role="status" className="py-6 text-sm">Loading search history…</p>}
    {history.isError && <div role="alert" className="my-4 text-sm text-red-700"><p>{history.error.message}</p><Button variant="outline" className="mt-2" onClick={() => void history.refetch()}>Retry history</Button></div>}
    {history.data && <><ul className="mt-4 divide-y divide-slate-100">{history.data.items.map(item => <li key={item.id}><button type="button" disabled={disabled} onClick={() => onOpen(item.id)} className="flex w-full flex-wrap items-center justify-between gap-3 rounded-lg py-4 text-left hover:bg-slate-50 focus-visible:outline-blue-600"><div className="min-w-0"><span className="block break-words text-sm font-medium text-slate-950">{item.name || "Unnamed search"}</span><span className="text-xs text-slate-500">{item.entity_type} · {new Date(item.created_at).toLocaleString()}</span></div><span className={`rounded-full px-3 py-1 text-xs ${item.partial ? "bg-amber-50 text-amber-800" : item.status === "failed" ? "bg-red-50 text-red-700" : "bg-slate-100 text-slate-700"}`}>{item.partial ? "Incomplete results" : statuses[item.status] ?? item.status}</span></button></li>)}</ul>
      {!history.data.items.length && <p className="py-6 text-sm text-slate-500">No searches match these filters.</p>}
      {full && <div className="mt-4 flex items-center justify-between"><Button variant="outline" disabled={cursors.length === 1 || history.isFetching} onClick={() => setCursors(values => values.slice(0, -1))}>Previous searches</Button><span className="text-xs text-slate-500">Page {cursors.length}</span><Button variant="outline" disabled={!history.data.next_cursor || history.isFetching} onClick={() => setCursors(values => [...values, history.data.next_cursor])}>Next searches</Button></div>}
    </>}
  </section>;
}
