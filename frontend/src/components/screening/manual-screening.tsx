"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { Search, ShieldCheck, LoaderCircle, Users } from "lucide-react";
import { Button } from "@/components/ui/button";
import { SearchHistory } from "./search-history";
import { OpenInvestigation } from "./open-investigation";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";

type Capabilities = { id: string; name: string; configured: boolean; health: string; fields: Record<string, string[]>; dataset_selection: boolean; limitations: string[]; entity_types: string[]; threshold_min: number; threshold_max: number; limit_max: number };

type SearchInput = { operation_id: string; name: string; entity_type: string; threshold: number; limit: number; birth_date?: string; nationality?: string; country?: string; registration_number?: string; address?: string; datasets?: string[] };
type Candidate = { id: string; entity_id: string; name: string; score: number; schema: string; datasets: string[] | null; properties: Record<string, string[]> };
type Result = { id: string; status: string; partial?: boolean; matches?: Candidate[]; query?: SearchInput; created_at?: string };
const pending = (status: string) => ["pending", "queued", "processing"].includes(status);
const labels: Record<string, string> = { alias: "Also known as", country: "Countries", nationality: "Nationality", birthDate: "Date of birth", address: "Address", topics: "Topics" };
const field = "mt-2 w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm focus:outline-2 focus:outline-blue-600";

async function request<T>(url: string, init: RequestInit): Promise<T> {
  const response = await fetch(url, { ...init, cache: "no-store" });
  const body = await response.json();
  if (!response.ok) throw new Error(body.error ?? "Search is unavailable. Please try again.");
  return body;
}

export function ManualScreening({ scope }: { scope: string }) {
  const catalog = useQuery({ queryKey: ["screening-capabilities", scope], queryFn: () => request<Capabilities>("/api/screening-search/capabilities", {}), retry: false, gcTime: 0 });

  const [name, setName] = useState("");
  const [entityType, setEntityType] = useState("Person");
  const [threshold, setThreshold] = useState(70);
  const [limit, setLimit] = useState(10);
  const [birthDate, setBirthDate] = useState(""); const [country, setCountry] = useState(""); const [nationality, setNationality] = useState("");
  const [registration, setRegistration] = useState(""); const [address, setAddress] = useState(""); const [datasets, setDatasets] = useState("");
  const [historical, setHistorical] = useState(false);
  const [refining, setRefining] = useState(false);
  const nameInput = useRef<HTMLInputElement>(null);
  const [submitted, setSubmitted] = useState<SearchInput>();
  const [result, setResult] = useState<Result>();
  const [error, setError] = useState("");
  const [sending, setSending] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const submission = useRef(false);
  const busy = sending || (!!result && pending(result.status) && !error);

  useEffect(() => {
    if (!result || !pending(result.status) || error) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    let attempts = 0;
    async function poll() {
      try {
        const next = await request<Result>(`/api/screening-search/${result!.id}`, { signal: controller.signal });
        if (controller.signal.aborted) return;
        if (next.query) setSubmitted(next.query);
        if (!pending(next.status)) { setResult(next); return; }
        if (++attempts >= 60) { setError("This search is taking longer than expected. Check its status again shortly."); return; }
        timer = setTimeout(poll, 2000);
      } catch (cause) { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "Unable to load results."); }
    }
    void poll();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [result, error, refresh]);

  async function submit(input: SearchInput) {
    if (submission.current) return;
    submission.current = true; setSending(true); setHistorical(false); setError(""); setResult(undefined); setSubmitted(input);
    try {
      const created = await request<Result>("/api/screening-search", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(input) });
      // Always fetch persisted details, including an idempotent terminal response.
      setResult({ id: created.id, status: "pending" });
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to start search."); }
    finally { submission.current = false; setSending(false); }
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!catalogReady || incompatibleDraft || !settingsSupported) return;
    if (!name.trim()) { setError("Enter a name to search."); return; }
    setRefining(false);
    void submit({ operation_id: crypto.randomUUID(), name: name.trim(), entity_type: entityType, threshold, limit,
      ...Object.fromEntries(Object.entries(identityValues).filter(([key, value]) => supports(key) && value.trim())),
      ...(datasetSelection && datasets.trim() ? { datasets: datasets.split(",").map(value => value.trim()).filter(Boolean) } : {}),
    });
  }

  function refine() {
    const query = result?.query;
    if (!query || busy) return;
    setName(query.name); setEntityType(query.entity_type);
    setThreshold(query.threshold); setLimit(query.limit);
    setBirthDate(query.birth_date ?? "");
    setCountry(query.country ?? ""); setNationality(query.nationality ?? "");
    setRegistration(query.registration_number ?? ""); setAddress(query.address ?? "");
    setDatasets(query.datasets?.join(", ") ?? "");
    setRefining(true);
    nameInput.current?.focus();
  }

  const cap = catalog.data;
  const catalogReady = !!cap?.configured && !catalog.isError &&
    Array.isArray(cap.entity_types) && !!cap.fields &&
    Number.isInteger(cap.threshold_min) && Number.isInteger(cap.threshold_max) &&
    cap.threshold_min >= 0 && cap.threshold_max <= 100 && cap.threshold_min <= cap.threshold_max &&
    Number.isInteger(cap.limit_max) && cap.limit_max >= 1 && cap.limit_max <= 50;
  const entityTypes = (cap?.entity_types ?? []).filter(value => ["Person", "Organization"].includes(value));
  const supports = (key: string) => cap?.fields?.[entityType]?.includes(key) === true;
  const datasetSelection = cap?.dataset_selection === true;
  const identityValues: Record<string, string> = entityType === "Person"
    ? { birth_date: birthDate, nationality, address }
    : { country, registration_number: registration, address };
  const advanced = Object.keys(identityValues).some(supports) || datasetSelection;
  const incompatibleDraft = Object.entries(identityValues).some(([key, value]) => !!value.trim() && !supports(key)) || (!!datasets.trim() && !datasetSelection);
  const settingsSupported = entityTypes.includes(entityType) && supports("name") && !!cap &&
    threshold >= cap.threshold_min && threshold <= cap.threshold_max && limit >= 1 && limit <= cap.limit_max;
  function clearUnsupported() {
    if (entityType === "Person") {
      if (!supports("birth_date")) setBirthDate("");
      if (!supports("nationality")) setNationality("");
    } else {
      if (!supports("country")) setCountry("");
      if (!supports("registration_number")) setRegistration("");
    }
    if (!supports("address")) setAddress("");
    if (!datasetSelection) setDatasets("");
  }
  const thresholdOptions = Array.from(new Set([0,40,50,60,70,80,90,cap?.threshold_min,cap?.threshold_max,threshold]))
    .filter((v): v is number => typeof v === "number" && Number.isInteger(v)).sort((a,b) => a-b);
  const limitOptions = Array.from(new Set([1,10,20,50,cap?.limit_max,limit]))
    .filter((v): v is number => typeof v === "number" && Number.isInteger(v)).sort((a,b) => a-b);
  const matches = result?.matches ?? [];
  const complete = result && !pending(result.status) && !["failed", "archived"].includes(result.status);
  return <div className="mx-auto w-full max-w-7xl p-6 lg:p-8">
    <header className="mb-8"><h1 className="text-2xl font-semibold text-slate-950">Screening</h1><p className="mt-2 text-sm text-slate-600">Search people and organizations against OpenSanctions data.</p></header>
    <div className="grid items-start gap-6 lg:grid-cols-[320px_minmax(0,1fr)]">
      <form onSubmit={onSubmit} className="rounded-2xl border border-slate-200 bg-white p-6">
        <h2 className="mb-5 font-semibold">New search</h2>
        {refining && <p role="status" className="mb-4 text-sm text-blue-800">Stored settings copied. Edit them and select Search to create a separate search. Original results and investigations stay unchanged.</p>}
        {incompatibleDraft && <div role="alert" className="mb-4 text-sm text-amber-900">This connection cannot use some entered fields or datasets. <button type="button" className="underline" onClick={clearUnsupported}>Clear unsupported settings</button> before searching.</div>}
        <div className="mb-5 space-y-2 text-xs text-slate-600" aria-live="polite">
          {catalog.isPending && <p>Loading provider capabilities…</p>}
          {catalog.isError && <div role="alert">Provider settings unavailable. <button type="button" className="underline" onClick={() => void catalog.refetch()}>Retry provider settings</button></div>}
          {catalog.data && <><p className="text-sm font-medium text-slate-900">{catalog.data.name}</p><p>{catalog.data.configured ? "Connection configured · availability not verified" : "Provider configuration required before searching"}</p>{catalog.data.limitations.map(text => <p key={text}>{text}</p>)}{!advanced && <p>Additional identity fields and dataset selection are unavailable for this connection. Existing search history remains accessible.</p>}</>}
        </div>
        {cap?.configured && !catalogReady && !catalog.isError && <p role="alert">Provider capabilities are incomplete or invalid. New searches are unavailable.</p>}
        <fieldset disabled={busy || !catalogReady} className="space-y-5 disabled:opacity-60">
          <label className="block text-sm font-medium">Entity type<select className={field} value={entityType} onChange={e => setEntityType(e.target.value)}>{!entityTypes.includes(entityType) && <option value={entityType} disabled>{entityType} (unsupported)</option>}{entityTypes.map(value => <option key={value} value={value}>{value}</option>)}</select></label>
          <label className="block text-sm font-medium">Name<input ref={nameInput} required maxLength={200} autoComplete="off" className={field} value={name} onChange={e => setName(e.target.value)} placeholder={entityType === "Person" ? "Full name" : "Organization name"} /></label>
          {advanced && <details><summary className="cursor-pointer text-sm font-medium text-blue-700">Additional search fields</summary><div className="mt-4 space-y-4">
            <p className="text-xs text-slate-500">These supported fields are sent to the provider. Identity fields affect matching; they are not guaranteed exact filters.</p>
            {entityType === "Person" && supports("birth_date") && <label className="block text-sm font-medium">Birth date<input className={field} value={birthDate} onChange={e => setBirthDate(e.target.value)} placeholder="YYYY or YYYY-MM-DD" maxLength={10} /></label>}
            {entityType === "Organization" && supports("registration_number") && <label className="block text-sm font-medium">Registration number<input className={field} value={registration} onChange={e => setRegistration(e.target.value)} maxLength={100} /></label>}
            {supports(entityType === "Person" ? "nationality" : "country") && <label className="block text-sm font-medium">{entityType === "Person" ? "Nationality" : "Country"}<input className={field} value={entityType === "Person" ? nationality : country} onChange={e => entityType === "Person" ? setNationality(e.target.value) : setCountry(e.target.value)} placeholder="Two-letter country code, e.g. GB" minLength={2} maxLength={2} pattern="[A-Za-z]{2}" /></label>}
            {supports("address") && <label className="block text-sm font-medium">Address<input className={field} value={address} onChange={e => setAddress(e.target.value)} maxLength={300} /></label>}
            {datasetSelection && <label className="block text-sm font-medium">Dataset IDs<input className={field} value={datasets} onChange={e => setDatasets(e.target.value)} placeholder="us_ofac_sdn, gb_hmt_sanctions" maxLength={810} /><span className="mt-2 block text-xs font-normal text-slate-500">Optional, comma-separated provider dataset IDs. Availability is checked by the provider.</span></label>}
          </div></details>}
          <label className="block text-sm font-medium">Matching threshold<select className={field} value={threshold} onChange={e => setThreshold(Number(e.target.value))}>{thresholdOptions.map(value => <option key={value} value={value} disabled={!cap || value < cap.threshold_min || value > cap.threshold_max}>{value}%</option>)}</select><span className="mt-2 block text-xs font-normal text-slate-500">Lower thresholds include less similar matches.</span></label>
          <label className="block text-sm font-medium">Result limit<select className={field} value={limit} onChange={e => setLimit(Number(e.target.value))}>{limitOptions.map(value => <option key={value} disabled={!cap || value > cap.limit_max}>{value}</option>)}</select></label>
          {catalogReady && !settingsSupported && <p role="alert" className="text-sm text-amber-900">Choose a supported entity type, matching threshold and result limit before searching.</p>}
          <Button type="submit" disabled={incompatibleDraft || !settingsSupported} className="w-full"><Search className="size-4" />{busy ? "Searching…" : "Search"}</Button>
        </fieldset>
      </form>
      <section aria-label="Search results" aria-busy={busy} className="min-w-0 space-y-4">
        {result?.query && !busy && <Button type="button" variant="outline" disabled={!catalogReady} onClick={refine}>Refine as new search</Button>}
        {historical && <p className="rounded-lg bg-blue-50 p-3 text-sm text-blue-900">Stored search results · no new provider search{result?.created_at ? ` · ${new Date(result.created_at).toLocaleString()}` : ""}</p>}
        {submitted && <div className="text-sm text-slate-600">Results for <strong className="text-slate-950">{submitted.name}</strong> · {submitted.entity_type} · threshold {submitted.threshold}%{[submitted.birth_date, submitted.nationality, submitted.country, submitted.registration_number, submitted.address, submitted.datasets?.join(", ")].filter(Boolean).map((value, i) => <span key={i} className="mt-1 block text-xs">{value}</span>)}</div>}
        {error && <div role="alert" className="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800"><p>{error}</p>{(submitted || result) && <Button type="button" variant="outline" className="mt-3" disabled={sending} onClick={() => result ? (setError(""), setRefresh(value => value + 1)) : void submit(submitted!)}>{result ? "Check status again" : "Retry same search"}</Button>}</div>}
        {busy && <div role="status" className="flex items-center gap-3 rounded-xl border border-slate-200 bg-white p-8 text-sm"><LoaderCircle className="size-5 animate-spin" />Searching provider data…</div>}
        {!submitted && !result && <div className="rounded-2xl border border-dashed border-slate-300 p-12 text-center text-slate-500"><Search className="mx-auto mb-4 size-8" /><h2 className="font-medium text-slate-800">Screen a person or organization</h2><p className="mt-2 text-sm">Enter a name and choose matching settings to begin.</p></div>}
        {result?.status === "failed" && <div role="alert" className="rounded-xl border border-red-200 bg-red-50 p-5 text-sm">The provider could not complete this search. You can submit a new search using the form.</div>}
        {result?.status === "archived" && <p role="status">This search has been archived.</p>}
        {result?.partial && <div role="status" className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">Incomplete results. More candidates may exist. Refine the name or increase the result limit and search again.</div>}
        {complete && <>
          <Link href={`/screening-search/${result.id}/report`} className="inline-block text-sm font-medium text-blue-700 underline">View printable report</Link>
          <h2 className="font-semibold">{matches.length} potential {matches.length === 1 ? "match" : "matches"}</h2>
          {matches.length === 0 && <div className="rounded-xl border border-slate-200 bg-white p-10 text-center"><ShieldCheck className="mx-auto mb-3 size-8 text-slate-500" /><p>{result.partial ? "No candidates returned in this incomplete response." : "No matches found for these search settings."}</p></div>}
          {matches.map(match => <article key={match.id} className="rounded-xl border border-slate-200 bg-white p-5">
            <OpenInvestigation tenant={scope.split(":")[0]} scope={scope} screening={result.id} candidate={match.id} />
            <div className="flex items-start justify-between gap-4"><div className="flex min-w-0 gap-3"><Users className="mt-1 size-5 shrink-0 text-slate-400" /><div><h3 className="break-words font-semibold text-slate-950">{match.name || match.entity_id}</h3><p className="text-xs text-slate-500">{match.schema || submitted?.entity_type}</p></div></div><span className="shrink-0 rounded-md bg-blue-50 px-2 py-1 text-sm text-blue-800">{Math.round(match.score * 100)}% similarity</span></div>
            {!!match.datasets?.length && <div className="mt-4 flex flex-wrap gap-2">{match.datasets.map(dataset => <span key={dataset} className="rounded bg-slate-100 px-2 py-1 text-xs text-slate-600">{dataset}</span>)}</div>}
            <details className="mt-4 text-sm"><summary className="cursor-pointer font-medium text-blue-700">Candidate details</summary><dl className="mt-3 space-y-3"><div><dt className="text-xs text-slate-500">Provider identifier</dt><dd className="break-words">{match.entity_id}</dd></div>{Object.entries(match.properties).map(([key, values]) => <div key={key}><dt className="text-xs text-slate-500">{labels[key] ?? key}</dt><dd className="break-words">{values.join(" · ")}</dd></div>)}</dl></details>
          </article>)}
          <p className="text-xs text-slate-500">Potential matches require investigation. Similarity scores are not confirmed identities.</p>
        </>}
      </section>
    </div>
    <SearchHistory scope={scope} disabled={sending} refreshKey={`${result?.id ?? ""}:${result?.status ?? ""}`} onOpen={id => { if (submission.current) return; setHistorical(true); setSubmitted(undefined); setError(""); setResult({ id, status: "pending" }); }} />
  </div>;
}
