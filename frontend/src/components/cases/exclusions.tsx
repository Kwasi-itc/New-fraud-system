"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { caseKeys } from "@/lib/case-manager-query";
import { InvestigatorReview } from "./investigator-review";
import { Problem, timestamp } from "./shared";

type Scope = { tenant: string; session: string; caseId: string };
type ExclusionEvent = {
  operation_id: string; match_id: string; object_type: string; object_id: string;
  provider: string; entity_id: string; actor_id: string; findings: string;
  activated: boolean; active: boolean; created_at: string;
};
type Page = {
  events: ExclusionEvent[];
  applications: { screening_id: string; match_id: string; applied_at: string }[];
  next_cursor: string;
};

function useHistory(scope: Scope, operation: string, cursor: string, enabled: boolean) {
  return useQuery<Page>({
    queryKey: [...caseKeys.scope(scope.tenant, scope.session), scope.caseId, "exclusions", operation, cursor],
    enabled, retry: false, gcTime: 0, staleTime: 0,
    queryFn: async () => {
      const query = new URLSearchParams({ cursor, limit: "20" });
      if (operation) query.set("operation_id", operation);
      const response = await fetch(`/api/case-exclusions/${scope.tenant}/${scope.caseId}?${query}`, { cache: "no-store" });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? "Exclusion history is unavailable.");
      return data;
    },
  });
}
function Pages({ cursors, next, busy, change }: { cursors: string[]; next: string; busy: boolean; change: (value: string[]) => void }) {
  return <nav aria-label="Exclusion history pages" className="flex items-center gap-3">
    <Button variant="outline" size="sm" disabled={busy || cursors.length === 1} onClick={() => change(cursors.slice(0, -1))}>Previous</Button>
    <span className="text-sm">Page {cursors.length}</span>
    <Button variant="outline" size="sm" disabled={busy || !next} onClick={() => change([...cursors, next])}>Next</Button>
  </nav>;
}
function Applications({ scope, operation }: { scope: Scope; operation: string }) {
  const [cursors, setCursors] = useState([""]);
  const query = useHistory(scope, operation, cursors.at(-1)!, true);
  return <section aria-label="Suppression history" className="mt-3 space-y-3 rounded-lg bg-slate-50 p-3">
    <h4 className="font-medium">Suppression history linked to this case</h4>
    <p className="text-sm text-slate-600">Only applications of this exclusion action linked to this case are shown. An empty list does not mean it was never applied elsewhere.</p>
    <Problem error={query.error} retry={() => void query.refetch()} />
    {query.isPending && <p role="status">Loading suppression history…</p>}
    {query.isSuccess && !query.error && <>
      {!query.data.applications.length && <p>No applications linked to this case.</p>}
      <ul className="space-y-2">{query.data.applications.map(a => <li key={a.match_id} className="break-words text-sm">
        <p>Screening: {a.screening_id}</p><p>Candidate: {a.match_id}</p><p>Applied {timestamp(a.applied_at)}</p>
      </li>)}</ul>
      <Pages cursors={cursors} next={query.data.next_cursor} busy={query.isFetching} change={setCursors} />
    </>}
  </section>;
}
export function CaseExclusions(scope: Scope) {
  const [open, setOpen] = useState(false);
  const [cursors, setCursors] = useState([""]);
  const [selected, setSelected] = useState("");
  const query = useHistory(scope, "", cursors.at(-1)!, open);
  return <section aria-label="Exclusion management" className="rounded-xl border border-slate-200 bg-white p-4 space-y-4">
    <div className="flex flex-wrap justify-between gap-3">
      <div><h3 className="font-semibold">Monitored-object exclusions</h3><p className="text-sm text-slate-600">Actions recorded in this case, including archived screening sources.</p></div>
      <Button variant="outline" onClick={() => setOpen(!open)} aria-expanded={open}>{open ? "Hide exclusions" : "Manage exclusions"}</Button>
    </div>
    {open && <>
      <p className="text-sm text-slate-600">Current state applies only to the same object, provider entity and identifying query. Removal affects future screenings; it does not run a new search. Closed cases must be reopened before changing an exclusion.</p>
      <Problem error={query.error} retry={() => void query.refetch()} />
      {query.isPending && <p role="status">Loading exclusions…</p>}
      {query.isSuccess && !query.error && <>
        {!query.data.events.length && <p>No exclusion actions recorded in this case.</p>}
        <ul className="space-y-4">{query.data.events.map(event => <li key={event.operation_id} className="rounded-lg border border-slate-200 p-4 space-y-3">
          <div className="flex flex-wrap justify-between gap-2"><h4 className="font-medium">{event.activated ? "Exclusion activated" : "Exclusion removed"}</h4><span className="text-sm">{event.active ? "Currently excluded" : "Currently included"}</span></div>
          <dl className="grid gap-2 text-sm sm:grid-cols-2">
            <div className="break-words"><dt className="text-slate-500">Monitored object</dt><dd>{event.object_type}: {event.object_id}</dd></div>
            <div className="break-words"><dt className="text-slate-500">Provider entity</dt><dd>{event.provider}: {event.entity_id}</dd></div>
            <div><dt className="text-slate-500">Recorded</dt><dd>{timestamp(event.created_at)}</dd></div>
            <div className="break-words"><dt className="text-slate-500">Actor</dt><dd>{event.actor_id}</dd></div>
          </dl>
          <p className="whitespace-pre-wrap break-words text-sm">{event.findings}</p>
          {event.activated && <Button variant="outline" size="sm" aria-expanded={selected === event.operation_id} onClick={() => setSelected(selected === event.operation_id ? "" : event.operation_id)}>View suppression history</Button>}
          {selected === event.operation_id && <Applications key={event.operation_id} scope={scope} operation={event.operation_id} />}
          {event.active && <InvestigatorReview {...scope} kind="matches" resource={event.match_id} />}
        </li>)}</ul>
        <Pages cursors={cursors} next={query.data.next_cursor} busy={query.isFetching} change={value => { setSelected(""); setCursors(value); }} />
      </>}
    </>}
  </section>;
}
