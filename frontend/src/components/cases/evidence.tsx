"use client";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { caseKeys } from "@/lib/case-manager-query";
import { Problem, readable } from "./shared";

function EvidenceValue({ value, depth = 0 }: { value: unknown; depth?: number }) {
  if (value === null || value === undefined) return <span className="text-slate-400">Not recorded</span>;
  if (typeof value !== "object") return <span className="break-all">{String(value)}</span>;
  if (depth > 5) return <pre className="overflow-auto whitespace-pre-wrap text-xs">{JSON.stringify(value, null, 2)}</pre>;
  if (Array.isArray(value)) return value.length ? <ol className="space-y-3">{value.map((item, i) => <li className="rounded border p-3" key={i}><EvidenceValue value={item} depth={depth + 1} /></li>)}</ol> : <span className="text-slate-500">None recorded</span>;
  return <dl className="grid gap-2">{Object.entries(value).map(([key, item]) => <div key={key} className="grid gap-1 sm:grid-cols-[160px_1fr]"><dt className="font-medium text-slate-500">{readable(key)}</dt><dd className="min-w-0"><EvidenceValue value={item} depth={depth + 1} /></dd></div>)}</dl>;
}
export function Evidence({ tenant, session, caseId, kind, resource }: { tenant: string; session: string; caseId: string; kind: "decisions" | "screenings"; resource: string }) {
  const [open, setOpen] = useState(false);
  const query = useQuery({ queryKey: [...caseKeys.scope(tenant, session), caseId, "evidence", kind, resource], enabled: open, retry: false, staleTime: 0, gcTime: 0,
    queryFn: async () => {
      const response = await fetch(`/api/case-evidence/${tenant}/${caseId}/${kind}/${resource}`, { cache: "no-store" });
      const data = await response.json();
      if (!response.ok) throw new Error(typeof data.error === "string" ? data.error : data.error?.message ?? "Evidence is unavailable.");
      return data as unknown;
    } });
  return <div className="space-y-3"><Button size="sm" variant="outline" onClick={() => setOpen(!open)} aria-expanded={open}>{open ? "Hide evidence" : "Inspect evidence"}</Button>{open && <div className="rounded-lg bg-slate-50 p-4 text-sm"><Problem error={query.error} retry={() => void query.refetch()} />{query.isPending && <p role="status">Loading source evidence…</p>}{query.isSuccess && <EvidenceValue value={query.data} />}</div>}</div>;
}
