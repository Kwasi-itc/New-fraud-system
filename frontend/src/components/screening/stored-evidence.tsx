type Evidence = { screening: { provider?: string; status?: string; partial?: boolean; created_at?: string; threshold?: number; queries?: Record<string, string>[] }; matches: { id?: string; entity_id?: string; name?: string; status?: string; score?: number; datasets: string[]; properties: Record<string, string[]> }[] };

export function StoredScreeningEvidence({ data }: { data: unknown }) {
  const evidence = data as Evidence;
  return <section className="space-y-4" aria-label="Stored screening evidence">
    <h3 className="font-semibold">Stored screening evidence</h3>
    <p className="text-sm text-slate-600">{evidence.screening.provider} · {evidence.screening.status?.replaceAll("_", " ")}{evidence.screening.created_at ? ` · ${new Date(evidence.screening.created_at).toLocaleString()}` : ""}</p>
    <p className="text-xs text-slate-500">Persisted search data with current review states. Opening this view does not rerun the provider.</p>
    {evidence.screening.partial && <p role="status" className="rounded bg-amber-50 p-3 text-amber-900">Incomplete provider results; additional matches may exist.</p>}
    {evidence.screening.threshold !== undefined && <p>Matching threshold: {evidence.screening.threshold}%</p>}
    {evidence.screening.queries?.map((query, i) => <dl key={i} className="grid gap-2 sm:grid-cols-2">{Object.entries(query).map(([label, value]) => value && <div key={label}><dt className="text-xs capitalize text-slate-500">{label.replaceAll("_", " ")}</dt><dd className="break-words">{value}</dd></div>)}</dl>)}
    {evidence.matches.length === 0 && <p>No stored candidates.</p>}
    {evidence.matches.map((match, i) => <article key={match.id ?? i} className="space-y-2 rounded-lg border border-slate-200 p-4"><h4 className="font-semibold">{match.name || match.entity_id || "Candidate"}</h4><p>{match.status?.replaceAll("_", " ")}{typeof match.score === "number" ? ` · ${Math.round(match.score * 100)}% similarity` : ""}</p><p className="break-all text-xs text-slate-500">Provider identifier: {match.entity_id ?? "Unavailable"}</p>{!!match.datasets.length && <p>Datasets: {match.datasets.join(", ")}</p>}<dl className="space-y-2">{Object.entries(match.properties).map(([key, values]) => <div key={key}><dt className="text-xs text-slate-500">{key === "birthDate" ? "Birth date" : key}</dt><dd className="break-words">{values.join(" · ")}</dd></div>)}</dl></article>)}
  </section>;
}
