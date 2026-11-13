"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { DecisionEngineApiError, type ScreeningExecution } from "@/lib/decision-engine-api";

type ScreeningSummary = Pick<ScreeningExecution, "id" | "config_id" | "status" | "request_json" | "response_json"> & { name?: string; provider_label?: string; mode?: string; forced_outcome?: string; source_id?: string; review_case_id?: string };

function screeningState(item: ScreeningSummary) {
  const source = item.response_json && typeof item.response_json === "object" && !Array.isArray(item.response_json)
    ? item.response_json : {};
  const count = typeof source.match_count === "number" && Number.isSafeInteger(source.match_count) && source.match_count >= 0
    ? source.match_count : null;
  if (item.status === "failed") return { label: "Screening failed", text: "Screening could not finish. No clear result has been established.", tone: "border-rose-200 bg-rose-50 text-rose-800" };
  if (item.status === "pending" || item.status === "queued") return { label: "Queued", text: "Waiting for screening to start.", tone: "border-slate-200 bg-slate-50 text-slate-700" };
  if (item.status === "sent") return { label: "Processing", text: "Screening is in progress. Results will update automatically.", tone: "border-blue-200 bg-blue-50 text-blue-800" };
  if (item.status !== "completed") return { label: "Status unavailable", text: "Refresh to check the latest screening state.", tone: "border-slate-200 bg-slate-50 text-slate-700" };
  if (source.status === "confirmed_hit") return { label: "Confirmed match", text: `An analyst confirmed a match during review.${source.partial === true ? " Provider results are still incomplete." : ""}`, tone: "border-rose-200 bg-rose-50 text-rose-800" };
  if (source.partial === true) return { label: "Incomplete results", text: `${count === null ? "Candidate count unavailable." : `${count} recorded ${count === 1 ? "candidate" : "candidates"}.`} The provider returned incomplete results; this is not a clear result.`, tone: "border-amber-200 bg-amber-50 text-amber-900" };
  if (source.status === "awaiting_review") return { label: "Needs review", text: `${count === null ? "Potential matches require review." : `${count} recorded ${count === 1 ? "candidate" : "candidates"}. Review is still pending.`} Potential matches are not confirmed hits.`, tone: "border-amber-200 bg-amber-50 text-amber-900" };
  if (source.status === "no_hit") return { label: "No outstanding matches", text: "No matches remain to review. This can reflect no provider matches or completed dismissal.", tone: "border-emerald-200 bg-emerald-50 text-emerald-800" };
  if (source.status === "archived") return { label: "Archived", text: "This screening has been archived.", tone: "border-slate-200 bg-slate-50 text-slate-700" };
  return { label: "Execution completed", text: "The execution finished, but its screening outcome is unavailable.", tone: "border-slate-200 bg-slate-50 text-slate-700" };
}

export function DecisionScreening({ tenantId, decisionId }: { tenantId: string; decisionId: string }) {
  const query = useQuery({
    queryKey: ["authorized-decision-screening", tenantId, decisionId],
    queryFn: async (): Promise<{ screening_executions: ScreeningSummary[] }> => {
      const response = await fetch(`/api/decision-screening/${encodeURIComponent(decisionId)}`, { cache: "no-store" });
      if (!response.ok) throw new DecisionEngineApiError(response.status, null);
      return response.json();
    },
    enabled: Boolean(tenantId && decisionId),
    retry: false,
    refetchInterval: (q) => !q.state.error && q.state.data?.screening_executions.some(item => ["pending", "queued", "sent"].includes(item.status) || (item.source_id && !item.review_case_id && item.response_json && typeof item.response_json === "object" && !Array.isArray(item.response_json) && item.response_json.status === "awaiting_review")) ? 3000 : false,
  });
  const denied = query.error instanceof DecisionEngineApiError && [401, 403].includes(query.error.status);
  return <Card className="rounded-2xl border border-slate-200 shadow-none" role="region" aria-label="Decision screening">
    <CardContent className="space-y-4 p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-[15px] font-semibold">Screening results</h2>
          <p className="mt-1 text-sm text-slate-500">Screening during evaluation can raise the decision outcome while leaving its numeric score unchanged. Potential matches require investigation.</p>
        </div>
        <Button variant="outline" size="sm" disabled={query.isFetching} onClick={() => void query.refetch()} aria-label="Refresh screening results">
          {query.isFetching ? "Refreshing…" : "Refresh"}
        </Button>
      </div>
      {query.isLoading ? <p role="status" className="text-sm text-slate-500">Loading screening results…</p>
        : query.error ? <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800">
          {denied ? "You do not have access to these screening results." : "Screening results are unavailable. Refresh to try again. The original decision remains visible."}
        </p>
          : !query.data?.screening_executions.length ? <p className="rounded-xl border border-dashed border-slate-200 p-4 text-sm text-slate-500">No screening requests were created for this decision.</p>
            : query.data.screening_executions.map(item => {
              const state = screeningState(item);
              const request = item.request_json && typeof item.request_json === "object" && !Array.isArray(item.request_json) ? item.request_json : {};
              return <article key={item.id} aria-label={`Screening ${item.id}`} className="space-y-3 rounded-xl border border-slate-200 bg-slate-50 p-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h3 className="text-sm font-semibold">{item.name ?? "Screening rule"}</h3>
                  <Badge className={`border ${state.tone}`}>{state.label}</Badge>
                </div>
                <p className="text-sm text-slate-700">{state.text}</p>
                <p className="text-xs text-slate-500">{item.provider_label ?? (request.provider === "opensanctions" ? "Configured screening provider" : "Screening provider")} · {item.mode === "during" ? `During evaluation${item.forced_outcome ? `; match outcome: ${item.forced_outcome.replaceAll("_", " ")}` : ""}` : "After the decision"}</p>
                {item.review_case_id && item.source_id ? <Button asChild size="sm"><Link href={`/cases/${item.review_case_id}?tab=screenings#screening-${item.source_id}`}>Start reviewing</Link></Button> : state.label === "Needs review" || state.label === "Incomplete results" ? <p className="text-xs text-slate-500">Waiting for the authorized case link.</p> : null}
                <details className="text-xs text-slate-500"><summary className="cursor-pointer">Execution details</summary><dl className="mt-2 space-y-1 break-all">
                  <div><dt className="inline">Configuration: </dt><dd className="inline">{item.config_id}</dd></div>
                  <div><dt className="inline">Execution: </dt><dd className="inline">{item.id}</dd></div>
                </dl></details>
              </article>;
            })}
    </CardContent>
  </Card>;
}
