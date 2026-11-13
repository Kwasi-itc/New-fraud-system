import type { ReactNode } from "react";

type Evidence = { screening: { provider?: string; status?: string; partial?: boolean; created_at?: string; threshold?: number; queries?: Record<string, string>[] }; matches: { id?: string; entity_id?: string; name?: string; status?: string; score?: number; datasets: string[]; properties: Record<string, string[]> }[] };
const labels: Record<string, string> = { birthDate: "Birth date", nationality: "Nationality", registrationNumber: "Registration number", country: "Country", address: "Address", alias: "Also known as" };

export function StoredScreeningEvidence({ data, renderReview }: { data: unknown; renderReview?: (id: string) => ReactNode }) {
  const evidence = data as Evidence;
  const matches = [...evidence.matches].sort((a, b) => (b.score ?? -1) - (a.score ?? -1) || (a.entity_id ?? "").localeCompare(b.entity_id ?? ""));
  const pending = matches.filter(m => m.status === "pending").length;
  return <section className="space-y-4" aria-label="Stored screening evidence">
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div><h3 className="font-semibold">Stored screening evidence</h3><p className="mt-1 text-sm text-slate-600">{evidence.screening.provider} · {evidence.screening.status?.replaceAll("_", " ")}{evidence.screening.created_at ? ` · ${new Date(evidence.screening.created_at).toLocaleString()}` : ""}</p></div>
      <span className="rounded-full bg-slate-100 px-3 py-1 text-sm text-slate-700">{pending} / {matches.length} to review</span>
    </div>
    <p className="text-xs text-slate-500">Persisted search data with current review states. Opening this view does not rerun the provider. Similarity is not a fraud score or a confirmed identity.</p>
    {evidence.screening.partial && <p role="status" className="rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900">Incomplete provider results; additional matches may exist. Refine the search before reviewing matches or approving the decision.</p>}
    <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(220px,.5fr)]">
      <div className="min-w-0 space-y-3"><h4 className="text-sm font-semibold">Potential matches</h4>
        {!matches.length && <p className="rounded-lg border border-dashed p-4 text-sm text-slate-600">No stored candidates.</p>}
        {matches.map((match, i) => <details key={match.id ?? i} open={i === 0} className="rounded-xl border border-slate-200 bg-white p-4">
          <summary className="cursor-pointer text-sm"><span className="font-semibold">{match.name || match.entity_id || "Candidate"}</span><span className="ml-2 inline-block rounded-full bg-slate-100 px-2 py-1 text-xs">{typeof match.score === "number" ? `${Math.round(match.score * 100)}% similarity` : "Similarity unavailable"}</span><span className="ml-2 text-xs text-slate-500">{match.status?.replaceAll("_", " ")}</span></summary>
          <div className="mt-4 space-y-3">
            {!!match.datasets?.length && <p className="text-sm">Datasets: {match.datasets.join(", ")}</p>}
            <dl className="grid gap-3 sm:grid-cols-2">{Object.entries(match.properties).map(([key, values]) => <div key={key}><dt className="text-xs text-slate-500">{labels[key] ?? key}</dt><dd className="break-words text-sm">{values.join(" · ")}</dd></div>)}</dl>
            <p className="break-all text-xs text-slate-500">Provider identifier: {match.entity_id ?? "Unavailable"}</p>
            {match.id && renderReview?.(match.id)}
          </div>
        </details>)}
      </div>
      <aside className="space-y-3 rounded-xl border border-slate-200 bg-slate-50 p-4"><h4 className="text-sm font-semibold">Search input</h4>
        {evidence.screening.threshold !== undefined && <p className="text-sm">Matching threshold: {evidence.screening.threshold}%</p>}
        {evidence.screening.queries?.map((query, i) => <dl key={i} className="space-y-3">{Object.entries(query).map(([label, value]) => value && <div key={label}><dt className="text-xs capitalize text-slate-500">{labels[label] ?? label.replaceAll("_", " ")}</dt><dd className="break-words text-sm">{value}</dd></div>)}</dl>)}
      </aside>
    </div>
  </section>;
}
