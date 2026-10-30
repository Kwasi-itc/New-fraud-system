"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";

type Report = {
  id: string; provider?: string; status: string; partial?: boolean; created_at?: string;
  query?: { name: string; entity_type: string; threshold: number; limit: number; birth_date?: string; nationality?: string; country?: string; registration_number?: string; address?: string; datasets?: string[] };
  matches?: { id: string; entity_id: string; name: string; status?: string; score: number; schema: string; datasets?: string[]; properties: Record<string, string[]> }[];
};
const labels: Record<string, string> = { alias: "Also known as", birthDate: "Birth date", nationality: "Nationality", country: "Country", address: "Address", topics: "Topics" };

export function ScreeningReport({ id, scope }: { id: string; scope: string }) {
  const report = useQuery({
    queryKey: ["screening-report", scope, id], gcTime: 0, retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false,
    queryFn: async ({ signal }) => {
      const response = await fetch(`/api/screening-search/${id}`, { signal, cache: "no-store" });
      if (!response.ok) throw new Error(response.status === 403 || response.status === 404 ? "This search is unavailable or you do not have access." : "The report could not be loaded.");
      const data: Report = await response.json();
      if (data.id !== id) throw new Error("The report could not be loaded.");
      return { data, retrievedAt: new Date().toISOString() };
    },
  });
  const data = report.data?.data;
  const ready = data && ["awaiting_review", "confirmed_hit", "no_hit"].includes(data.status);
  const query = data?.query;
  return <main className="mx-auto max-w-4xl space-y-6 bg-white p-6 text-slate-950 sm:p-10 print:max-w-none print:p-0">
    <div className="flex flex-wrap items-center justify-between gap-4 print:hidden">
      <Link href="/screening-search" className="text-sm text-blue-700 underline">Back to screening</Link>
      <Button disabled={!ready || report.isFetching || report.isError} onClick={() => window.print()}>Print report</Button>
    </div>
    <header className="border-b border-slate-300 pb-4"><h1 className="text-2xl font-semibold">Screening report</h1><p className="mt-2 text-sm">Stored results and review states at retrieval. No new provider search.</p></header>
    {report.isPending && <p role="status">Loading stored screening report…</p>}
    {report.isError && <div role="alert"><p>{report.error.message}</p><Button className="mt-3 print:hidden" variant="outline" onClick={() => void report.refetch()}>Retry report</Button></div>}
    {data && !report.isError && <>
      <dl className="grid gap-3 text-sm sm:grid-cols-2 print:grid-cols-2">
        <div><dt className="font-medium">Search ID</dt><dd className="break-all">{data.id}</dd></div>
        <div><dt className="font-medium">Provider</dt><dd>{data.provider ?? "Unavailable"}</dd></div>
        <div><dt className="font-medium">Search created (UTC)</dt><dd>{data.created_at ?? "Unavailable"}</dd></div>
        <div><dt className="font-medium">Report retrieved (UTC)</dt><dd>{report.data?.retrievedAt}</dd></div>
        <div><dt className="font-medium">Screening status</dt><dd>{data.status.replaceAll("_", " ")}</dd></div>
      </dl>
      {!ready && <p role="status">This search is not ready for a results report. Return to screening to check its status.</p>}
      {data.partial && <p role="status" className="border-2 border-amber-700 p-3 font-medium">Incomplete results. Additional candidates may exist; this report does not establish a negative screening.</p>}
      {query && <section aria-label="Search settings" className="space-y-2">
        <h2 className="text-lg font-semibold">Search settings</h2>
        <p className="break-words">{query.name} · {query.entity_type}</p>
        <p>Matching threshold: {query.threshold}% · Result limit: {query.limit}</p>
        <dl className="space-y-2 text-sm">{[["Birth date", query.birth_date], ["Nationality", query.nationality], ["Country", query.country], ["Registration number", query.registration_number], ["Address", query.address], ["Requested datasets", query.datasets?.join(", ")]].map(([label, value]) => value && <div key={label}><dt className="font-medium">{label}</dt><dd className="break-words">{value}</dd></div>)}</dl>
      </section>}
      {ready && <section aria-label="Stored candidates" className="space-y-4">
        <h2 className="text-lg font-semibold">{data.matches?.length ?? 0} stored candidates</h2>
        {!data.matches?.length && <p>{data.partial ? "No candidates returned in this incomplete response." : "No matches returned for these search settings."}</p>}
        {data.matches?.map(match => <article key={match.id} className="space-y-2 border-t border-slate-300 pt-4 print:break-inside-avoid">
          <h3 className="break-words font-semibold">{match.name || match.entity_id}</h3>
          <p>{match.schema} · {Math.round(match.score * 100)}% similarity · Review: {match.status?.replaceAll("_", " ") || "Unavailable"}</p>
          <p className="break-all text-sm">Provider identifier: {match.entity_id}</p>
          {!!match.datasets?.length && <p className="break-words text-sm">Datasets: {match.datasets.join(", ")}</p>}
          <dl className="space-y-2 text-sm">{Object.entries(match.properties).map(([key, values]) => <div key={key}><dt className="font-medium">{labels[key] ?? key}</dt><dd className="break-words">{values.join(", ")}</dd></div>)}</dl>
        </article>)}
        <p className="border-t border-slate-300 pt-4 text-sm">Similarity scores do not confirm identity. Review states may change after this report is retrieved.</p>
      </section>}
    </>}
  </main>;
}
