"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitBranch, Scale, Sigma } from "lucide-react";
import { DecisionCaseLink } from "@/components/cases/decision-case-link";
import { aggregateNodes, nodeName } from "@/components/detection/rule-execution-evidence";
import { RuleExecutionFullPage, RuleExecutionInspector } from "@/components/detection/rule-execution-inspector";
import { formatExecutionRequestBody } from "@/components/detection/scheduled-execution-shared";
import { Card, CardContent } from "@/components/ui/card";
import { decisionEngineApi, type Iteration } from "@/lib/decision-engine-api";
import { cn } from "@/lib/utils";

function formatValue(value: unknown) {
  if (value === null || value === undefined) return "-";
  return typeof value === "string" ? value : JSON.stringify(value);
}

function formatDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat("en-US", { dateStyle: "medium", timeStyle: "short" }).format(date);
}

function formatLabel(value: string) {
  return value.split("_").filter(Boolean).map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(" ");
}

function matchedClass(outcome: string) {
  switch (outcome.toLowerCase()) {
    case "decline": return "bg-[#fff7f6] text-[#b42318] shadow-[inset_3px_0_#d96a60]";
    case "block_and_review": return "bg-[#fff8f1] text-[#9a480b] shadow-[inset_3px_0_#d98542]";
    case "review": return "bg-[#fffbf3] text-[#92520b] shadow-[inset_3px_0_#d59a3d]";
    case "approve": return "bg-[#f3fbf6] text-[#167050] shadow-[inset_3px_0_#65ad85]";
    default: return "bg-slate-50 text-slate-700 shadow-[inset_3px_0_#94a3b8]";
  }
}

function outcomeClass(value: string) {
  switch (value.toLowerCase()) {
    case "approve": return "border-[#cbe9d8] bg-[#effaf4] text-[#167050]";
    case "review": return "border-[#f1dbb9] bg-[#fff8eb] text-[#92520b]";
    case "block_and_review": return "border-[#f2d3b6] bg-[#fff2e5] text-[#9a480b]";
    case "decline": return "border-[#f2c5c1] bg-[#fff1f0] text-[#b42318]";
    default: return "border-slate-200 bg-slate-50 text-slate-700";
  }
}

function outcomeTitle(value: string) {
  switch (value.toLowerCase()) {
    case "approve": return "Approved";
    case "review": return "Review required";
    case "block_and_review": return "Blocked for review";
    case "decline": return "Declined";
    default: return formatLabel(value);
  }
}

function ScoreVisualization({ score, iteration, loading }: { score: number; iteration?: Iteration; loading: boolean }) {
  const review = iteration?.score_review_threshold;
  const block = iteration?.score_block_and_review_threshold;
  const decline = iteration?.score_decline_threshold;
  if (review == null || block == null || decline == null || review > block || block > decline) return <p className="mt-3 text-xs text-slate-500">{loading ? "Loading score thresholds…" : "Score thresholds are unavailable for this iteration."}</p>;
  const min = Math.min(0, score, review);
  const maximum = Math.max(0, score, decline);
  const max = maximum + Math.max(1, (maximum - min) * 0.1);
  const percent = (value: number) => ((value - min) / (max - min)) * 100;
  const segments = [percent(review), percent(block) - percent(review), percent(decline) - percent(block), 100 - percent(decline)];
  return <div className="mt-[22px]">
    <div role="img" className="relative flex h-2 gap-[3px]" aria-label={`Score ${score}. Review threshold ${review}, block and review ${block}, decline ${decline}.`}>
      {segments.map((width, index) => <span key={index} style={{ width: `${width}%` }} className={cn("rounded-sm", ["bg-[#b9dfcb]", "bg-[#f0d39d]", "bg-orange-300", "bg-[#eeb6ad]"][index])} />)}
      <span className="absolute -top-[5px] h-[18px] w-[3px] rounded-sm bg-slate-900" style={{ left: `${percent(score)}%` }} />
    </div>
    <div className="mt-2 flex justify-between text-[11px] text-slate-500"><span>{min}</span><span>{Number(max.toFixed(2))}</span></div>
    <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-slate-500"><span>Review ≥ {review}</span><span>Block and review ≥ {block}</span><span>Decline ≥ {decline}</span></div>
    <p className="mt-4 text-[11px] text-slate-500">Points are not a fraud probability.</p>
  </div>;
}

export function DecisionDetailPage({ decisionId, tenantId, session }: { decisionId: string; tenantId: string; session: string }) {
  const searchParams = useSearchParams();
  const pathname = usePathname();
  const router = useRouter();
  const [selectedRuleId, setSelectedRuleId] = useState<string | null>(null);
  const [ruleFilter, setRuleFilter] = useState<"all" | "matched" | "not-matched">("all");

  const decisionQuery = useQuery({
    queryKey: ["decision-engine", "decision", tenantId, decisionId],
    queryFn: () => decisionEngineApi.getDecision(tenantId, decisionId),
    enabled: Boolean(tenantId && decisionId),
  });
  const decision = decisionQuery.data?.decision;
  const scenarioQuery = useQuery({
    queryKey: ["decision-engine", "scenario", tenantId, decision?.scenario_id],
    queryFn: () => decisionEngineApi.getScenario(tenantId, decision!.scenario_id),
    enabled: Boolean(tenantId && decision?.scenario_id),
  });
  const iterationsQuery = useQuery({
    queryKey: ["decision-engine", "iterations", tenantId, decision?.scenario_id],
    queryFn: () => decisionEngineApi.listIterations(tenantId, decision!.scenario_id),
    enabled: Boolean(tenantId && decision?.scenario_id),
  });
  const rulesQuery = useQuery({
    queryKey: ["decision-engine", "rules", tenantId, decision?.scenario_id, decision?.scenario_iteration_id],
    queryFn: () => decisionEngineApi.listRules(tenantId, decision!.scenario_id, decision!.scenario_iteration_id),
    enabled: Boolean(tenantId && decision?.scenario_id && decision?.scenario_iteration_id),
  });
  const iteration = useMemo(() => (iterationsQuery.data?.iterations ?? []).find((item) => item.id === decision?.scenario_iteration_id), [decision?.scenario_iteration_id, iterationsQuery.data?.iterations]);
  const rulesById = useMemo(() => new Map((rulesQuery.data?.rules ?? []).map((rule) => [rule.id, rule])), [rulesQuery.data?.rules]);
  const executions = decisionQuery.data?.rule_executions ?? [];
  const selectedExecution = executions.find((item) => item.id === selectedRuleId);
  const fullPageId = searchParams.get("view") === "rule" ? searchParams.get("ruleExecution") : null;
  const fullPageExecution = executions.find((item) => item.id === fullPageId);
  const isLoading = decisionQuery.isLoading || rulesQuery.isLoading;
  const error = decisionQuery.error ?? rulesQuery.error;
  const fromTab = searchParams.get("fromTab");
  const backHref = fromTab === "Decisions" || fromTab === "Lists" || fromTab === "Scenarios" ? `/detection?tab=${encodeURIComponent(fromTab)}` : "/detection";
  function ruleViewHref(id?: string) {
    const params = new URLSearchParams(searchParams.toString());
    if (id) {
      params.set("view", "rule");
      params.set("ruleExecution", id);
    } else {
      params.delete("view");
      params.delete("ruleExecution");
    }
    return `${pathname}?${params.toString()}`;
  }

  if (!tenantId) return <Card className="rounded-2xl border border-amber-200 bg-amber-50 shadow-none"><CardContent className="p-5 text-sm text-amber-800">Your workspace is unavailable. Sign in again to continue.</CardContent></Card>;
  if (isLoading) return <Card className="rounded-2xl border border-slate-200 shadow-none"><CardContent className="p-5 text-sm text-slate-600">Loading decision details...</CardContent></Card>;
  if (error || !decision) return <Card className="rounded-2xl border border-red-200 bg-red-50 shadow-none"><CardContent className="p-5 text-sm text-red-700">{error instanceof Error ? error.message : "Failed to load decision details."}</CardContent></Card>;
  if (searchParams.get("view") === "rule") {
    if (!fullPageExecution) return <div className="space-y-4 p-5"><Link href={ruleViewHref()} className="text-sm font-semibold text-blue-700">← Back to decision</Link><p role="alert" className="text-sm text-red-700">Rule execution not found for this decision.</p></div>;
    return <RuleExecutionFullPage execution={fullPageExecution} executions={executions} rule={rulesById.get(fullPageExecution.rule_id)} onSelect={(id) => router.replace(ruleViewHref(id))} backHref={ruleViewHref()} />;
  }

  const request = decision.request_body && typeof decision.request_body === "object" && !Array.isArray(decision.request_body) ? Object.entries(decision.request_body) : [];
  const fields = decision.request_body && typeof decision.request_body === "object" && !Array.isArray(decision.request_body) && decision.request_body.fields && typeof decision.request_body.fields === "object" && !Array.isArray(decision.request_body.fields) ? Object.entries(decision.request_body.fields).sort(([a], [b]) => a.localeCompare(b)) : [];
  const metadata = request.filter(([key]) => key !== "fields" && key !== "object_id" && key !== "object_type");
  const matched = executions.filter((item) => item.outcome.toLowerCase() === "hit").length;
  const visible = executions.filter((item) => ruleFilter === "all" || (ruleFilter === "matched" ? item.outcome.toLowerCase() === "hit" : item.outcome.toLowerCase() !== "hit"));

  return <div className="mx-auto w-full max-w-[1280px] space-y-[22px]">
    <nav aria-label="Breadcrumb" className="text-[13px] text-slate-500"><Link href={backHref} className="text-[#1f4f96] hover:underline">Detection</Link><span className="px-2 text-slate-400">/</span><Link href="/detection?tab=Decisions" className="text-[#1f4f96] hover:underline">Decisions</Link><span className="px-2 text-slate-400">/</span><strong className="font-semibold text-slate-700">Decision details</strong></nav>
    <header className="flex flex-col justify-between gap-2 sm:flex-row sm:items-end sm:gap-5">
      <div><h1 className="text-[30px] font-semibold leading-tight tracking-[-0.045em] text-slate-900">Decision details</h1><p className="mt-1 text-[13px] text-slate-500">{scenarioQuery.data?.scenario.name ?? decision.scenario_id} · {iteration?.version != null ? `Scenario version ${iteration.version}` : decision.scenario_iteration_id} · {formatDate(decision.created_at)}</p></div>
      <span className="break-all font-mono text-xs text-slate-500">{decision.id}</span>
    </header>
    <Card className="overflow-hidden rounded-[14px] border border-slate-200 shadow-none"><CardContent className="grid p-0 lg:grid-cols-[minmax(0,1fr)_350px]">
      <div className="p-[25px]"><div className="text-[11px] font-bold uppercase tracking-[0.09em] text-slate-500">Decision outcome</div>
        <div className="mt-2 flex flex-wrap items-center gap-3"><h2 className="text-2xl font-semibold tracking-[-0.035em] text-slate-900">{outcomeTitle(decision.outcome)}</h2><span className={cn("inline-flex items-center gap-1.5 rounded-md border px-2 py-[3px] text-[11px] font-semibold", outcomeClass(decision.outcome))}><span className="size-1.5 rounded-full bg-current" />{formatLabel(decision.outcome)}</span></div>
        <div className="mt-[27px] grid grid-cols-2 gap-4 xl:grid-cols-3">
          <div><div className="mb-1 text-xs text-slate-500">Evaluated object</div><div className="break-all text-[13px] font-semibold">{decision.object_type} · {decision.object_id}</div></div>
          <div><div className="mb-1 text-xs text-slate-500">Triggered</div><div className="text-[13px] font-semibold">{decision.triggered ? "Yes" : "No"}</div></div>
          <div><div className="mb-1 text-xs text-slate-500">Case</div><DecisionCaseLink tenant={tenantId} session={session} decisionId={decision.id} /></div>
        </div>
      </div>
      <div className="border-t border-slate-200 bg-[#fafcff] p-[25px] lg:border-l lg:border-t-0"><div className="text-xs text-slate-500">Decision score</div><div className="mt-1 text-[38px] font-semibold leading-none tracking-[-0.04em]">{decision.score} <span className="text-[13px] font-normal tracking-normal text-slate-500">points</span></div><ScoreVisualization score={decision.score} iteration={iteration} loading={iterationsQuery.isLoading} /></div>
    </CardContent></Card>
    {scenarioQuery.isError || iterationsQuery.isError ? <p role="alert" className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">Some scenario details could not be loaded. <button type="button" className="font-semibold underline" onClick={() => { if (scenarioQuery.isError) void scenarioQuery.refetch(); if (iterationsQuery.isError) void iterationsQuery.refetch(); }}>Retry</button></p> : null}
    <section className="overflow-hidden rounded-[14px] border border-slate-200 bg-white" aria-labelledby="rule-breakdown-heading">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 px-[22px] py-[19px]"><div><h2 id="rule-breakdown-heading" className="text-[17px] font-semibold tracking-tight">Rule execution breakdown</h2><p className="mt-1 text-xs text-slate-500">Rule contributions and the evidence behind them</p></div><span className="text-[13px] font-semibold">{decision.score} points total</span></div>
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 bg-[#fcfdff] px-[22px] py-3.5"><div className="text-xs text-slate-500"><strong className="text-slate-900">{matched} matched</strong> · {executions.length - matched} other</div>
        <div className="flex gap-1 rounded-[9px] border border-slate-200 bg-slate-50 p-[3px]" role="group" aria-label="Filter rule executions">{(["all", "matched", "not-matched"] as const).map((filter) => <button key={filter} type="button" aria-pressed={ruleFilter === filter} onClick={() => setRuleFilter(filter)} className={cn("rounded-md border border-transparent px-2 py-1 text-xs", ruleFilter === filter ? "border-slate-200 bg-white font-semibold text-slate-900" : "text-slate-500")}>{filter === "all" ? "All" : filter === "matched" ? "Matched" : "Other"}</button>)}</div>
      </div>
      {visible.length ? visible.map((item) => {
        const hit = item.outcome.toLowerCase() === "hit";
        const contribution = hit ? item.score_modifier : 0;
        const aggregate = item.evaluation && aggregateNodes(item.evaluation).length > 0;
        const combined = ["and", "or"].includes(nodeName(item.evaluation));
        const Icon = aggregate ? Sigma : combined ? GitBranch : Scale;
        return <button key={item.id} type="button" onClick={() => setSelectedRuleId(item.id)} className={cn("grid w-full grid-cols-[32px_minmax(0,1fr)_auto] items-center gap-3 border-b border-slate-100 px-[22px] py-[18px] text-left last:border-b-0 hover:brightness-[0.98] max-sm:grid-cols-[28px_minmax(0,1fr)_auto] max-sm:gap-2 max-sm:px-[17px]", hit && matchedClass(decision.outcome))}>
          <span className="grid size-8 place-items-center rounded-[9px] border max-sm:size-7" style={{ borderColor: "currentColor" }}><Icon className="size-[18px]" /></span>
          <span className="flex min-w-0 flex-wrap items-center gap-2"><strong className="break-words text-base">{item.rule_name}</strong><span style={hit ? { borderColor: "currentColor" } : undefined} className={cn("rounded-md border px-2 py-[3px] text-[11px] font-semibold", !hit && "bg-slate-50 text-slate-600")}>{hit ? "Matched" : item.outcome.toLowerCase() === "no_hit" ? "Not matched" : formatLabel(item.outcome)}</span>{aggregate ? <span className="rounded-md border border-slate-200 bg-slate-50 px-2 py-[3px] text-[11px] font-semibold text-slate-600">Aggregate</span> : combined ? <span className="rounded-md border border-slate-200 bg-slate-50 px-2 py-[3px] text-[11px] font-semibold text-slate-600">Combined logic</span> : null}</span>
          <span className="shrink-0 text-right"><strong className="block text-[15px]">{contribution > 0 ? "+" : ""}{contribution}</strong><span className="text-[11px]">View execution →</span></span>
        </button>;
      }) : <p className="px-6 py-5 text-sm text-slate-500">{executions.length ? "No rule executions match this filter." : "No rule executions were recorded for this decision."}</p>}
    </section>
    <Card className="overflow-hidden rounded-[14px] border border-slate-200 shadow-none"><CardContent className="p-0">
      <div className="flex items-center justify-between gap-3 border-b border-slate-200 px-[22px] py-[19px]"><div><h2 className="text-[17px] font-semibold tracking-tight">Evaluated request</h2><p className="mt-1 text-xs text-slate-500">Values captured at decision time</p></div><span className="rounded-md border border-slate-200 bg-slate-50 px-2 py-[3px] text-[11px] font-semibold text-slate-600">Snapshot</span></div>
      {fields.length || metadata.length ? <dl className="grid gap-3.5 px-[22px] py-5">{[...fields, ...metadata].map(([name, value], index) => <div key={`${name}-${index}`} className="flex justify-between gap-4 text-xs"><dt className="text-slate-500">{name.replace(/_/g, " ")}</dt><dd className="whitespace-pre-wrap break-all text-right font-semibold">{formatValue(value)}</dd></div>)}</dl> : <p className="px-[22px] py-5 text-sm text-slate-500">No request fields were recorded.</p>}
      <details className="border-t border-slate-200"><summary className="cursor-pointer px-[22px] py-3.5 text-xs font-semibold text-[#1f4f96]">View raw request JSON</summary><pre className="mx-[22px] mb-5 max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs">{formatExecutionRequestBody(decision.request_body ?? null)}</pre></details>
    </CardContent></Card>
    {selectedExecution ? <RuleExecutionInspector execution={selectedExecution} executions={executions} rule={rulesById.get(selectedExecution.rule_id)} onSelect={setSelectedRuleId} onClose={() => setSelectedRuleId(null)} fullPageHref={ruleViewHref(selectedExecution.id)} /> : null}
  </div>;
}
