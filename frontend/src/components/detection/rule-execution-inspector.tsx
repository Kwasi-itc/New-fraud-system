"use client";

import Link from "next/link";
import { useEffect, useRef } from "react";
import { ChevronDown, X } from "lucide-react";
import type { Rule, RuleEvaluationNode, RuleExecution } from "@/lib/decision-engine-api";
import { summarizeRuleFormula } from "@/lib/rule-builder";
import { cn } from "@/lib/utils";
import { aggregateFilters, aggregateNodes, displayValue, expression, humanField, nodeName, operatorLabel } from "./rule-execution-evidence";

const comparisonNames = new Set(["eq", "neq", "gt", "gte", "lt", "lte", "contains", "in", "startswith", "endswith"]);

function Result({ node }: { node: RuleEvaluationNode }) {
  if (node.skipped) return <span className="shrink-0 rounded-md border border-slate-200 bg-slate-50 px-2 py-1 text-xs font-medium text-slate-600">Not evaluated</span>;
  if (typeof node.return_value !== "boolean") return null;
  return <span className={cn("shrink-0 rounded-md border px-2 py-1 text-xs font-medium", node.return_value ? "border-emerald-200 bg-emerald-50 text-emerald-700" : "border-slate-200 bg-slate-50 text-slate-600")}>{node.return_value ? "True" : "False"}</span>;
}

function AggregateScope({ node }: { node: RuleEvaluationNode }) {
  const { windows, filters, unsupported } = aggregateFilters(node);
  if (!windows.length && !filters.length && !unsupported) return null;
  return <div className="space-y-3 border-t border-slate-100 px-4 py-4">
    <div className="text-xs font-semibold text-[#1f4f96]">{expression(node)} · filters{unsupported ? "" : " (all required)"}</div>
    {unsupported ? <p className="text-xs text-slate-600">This filter structure cannot be summarized. See the raw evaluation trace.</p> : null}
    {windows.map((window) => <div key={window.field} className="rounded-lg border border-blue-100 bg-slate-50 p-3">
      <div className="text-xs font-semibold text-[#1f4f96]">Time window · {humanField(window.field)}</div>
      <p className="mt-2 text-sm font-semibold text-slate-900">{window.summary}</p>
      <p className="mt-1 text-xs text-slate-500">{window.boundary}</p>
      <details className="mt-2 text-xs text-slate-600">
        <summary className="cursor-pointer font-semibold text-[#1f4f96]">View configured time filters</summary>
        <ul className="mt-2 list-disc space-y-1 pl-4">{window.filters.map((filter, index) => <li key={index} className="break-all">{humanField(filter.field)} {filter.operator} {expression(filter.value)} → {displayValue(filter.value.return_value)}</li>)}</ul>
      </details>
    </div>)}
    {filters.length ? <div className="flex flex-wrap gap-2">{filters.map((filter, index) => <span key={index} className="break-all rounded-md border border-blue-100 bg-blue-50/50 px-2 py-1.5 text-xs text-slate-700">{humanField(filter.field)} <strong>{filter.operator} {displayValue(filter.value.return_value)}</strong></span>)}</div> : null}
  </div>;
}

function EvidenceNode({ node }: { node: RuleEvaluationNode }) {
  const name = nodeName(node);
  if (!node.skipped && (name === "and" || name === "or") && node.children?.length) {
    return <div className="space-y-3 rounded-xl border border-blue-100 bg-blue-50/60 p-3">
      <div className="flex flex-wrap items-center gap-2"><span className="rounded-md bg-blue-100 px-2 py-1 text-xs font-bold text-[#1f4f96]">{name.toUpperCase()}</span><span className="text-xs text-slate-600">{name === "and" ? "Every condition must be true" : "At least one condition must be true"}</span><Result node={node} /></div>
      {node.children.map((child, index) => <EvidenceNode key={index} node={child} />)}
    </div>;
  }
  const children = node.children ?? [];
  const isComparison = comparisonNames.has(name) && children.length === 2;
  const observed = isComparison
    ? `${displayValue(children[0].return_value)} ${operatorLabel(name)} ${displayValue(children[1].return_value)}`
    : name === "not" && children[0] ? displayValue(children[0].return_value) : displayValue(node.return_value);
  const aggregates = node.skipped ? [] : aggregateNodes(node);
  return <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
    <div className="flex items-center justify-between gap-4 px-4 py-4">
      <div className="min-w-0">
        <div className="break-words text-sm font-semibold text-slate-900">{expression(node)}</div>
        {!node.skipped ? <div className="mt-1 text-sm text-slate-500">Observed: <strong className="font-semibold text-slate-900">{observed}</strong></div> : null}
      </div>
      <Result node={node} />
    </div>
    {aggregates.map((aggregate, index) => <AggregateScope key={index} node={aggregate} />)}
  </div>;
}

type Props = {
  execution: RuleExecution;
  executions: RuleExecution[];
  rule?: Rule;
  onSelect: (id: string) => void;
};

function InspectorContent({ execution, executions, rule, onSelect }: Props) {
  const contribution = execution.outcome.toLowerCase() === "hit" ? execution.score_modifier : 0;
  const summary = rule ? summarizeRuleFormula(rule.formula) : null;
  return <div className="space-y-5">
    <label className="grid gap-1 text-xs font-medium text-slate-600">Rule execution
      <span className="relative block w-full max-w-md">
        <select value={execution.id} onChange={(event) => onSelect(event.target.value)} className="min-h-10 w-full cursor-pointer appearance-none rounded-lg border border-slate-300 bg-white py-2 pl-3 pr-10 text-sm text-slate-900 [-webkit-appearance:none]">
          {executions.map((item) => <option key={item.id} value={item.id}>{item.rule_name} · {item.outcome.replace(/_/g, " ")}</option>)}
        </select>
        <ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-slate-500" />
      </span>
    </label>
    <section className="rounded-xl border border-slate-200 bg-white p-5">
      <div className="text-xs text-slate-500">Execution result</div>
      <h2 className="mt-1 text-lg font-semibold text-slate-950">{execution.rule_name}</h2>
      <div className="mt-2 flex flex-wrap gap-4 text-sm"><span className="font-medium">{execution.outcome.replace(/_/g, " ")}</span><span>{contribution > 0 ? "+" : ""}{contribution} points</span></div>
      {execution.skip_reason ? <p className="mt-3 text-sm text-amber-800">{execution.skip_reason}</p> : null}
    </section>
    <section className="overflow-hidden rounded-xl border border-slate-200 bg-white">
      <div className="border-b border-slate-200 px-5 py-4"><h3 className="text-base font-semibold">How the rule was evaluated</h3></div>
      <div className="space-y-3 p-5">
        {execution.evaluation ? <EvidenceNode node={execution.evaluation} /> : <p className="text-sm text-slate-600">No evaluation trace was recorded for this execution.</p>}
        {summary ? <details className="rounded-lg border border-slate-200"><summary className="cursor-pointer px-3 py-2 text-sm font-medium text-[#1f4f96]">View configured rule</summary><pre className="whitespace-pre-wrap break-words border-t border-slate-200 p-3 text-xs">{summary}</pre></details> : null}
        <details className="rounded-lg border border-slate-200"><summary className="cursor-pointer px-3 py-2 text-sm font-medium text-[#1f4f96]">Execution information</summary><dl className="grid gap-3 border-t border-slate-200 p-3 text-xs">
          <div><dt className="text-slate-500">Rule ID</dt><dd className="break-all">{execution.rule_id}</dd></div>
          <div><dt className="text-slate-500">Execution ID</dt><dd className="break-all">{execution.id}</dd></div>
          <div><dt className="text-slate-500">Created</dt><dd>{execution.created_at}</dd></div>
          {rule?.description ? <div><dt className="text-slate-500">Description</dt><dd>{rule.description}</dd></div> : null}
          {rule?.rule_group ? <div><dt className="text-slate-500">Rule group</dt><dd>{rule.rule_group}</dd></div> : null}
        </dl></details>
        {execution.evaluation ? <details className="rounded-lg border border-slate-200"><summary className="cursor-pointer px-3 py-2 text-sm font-medium text-[#1f4f96]">View raw evaluation trace</summary><pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words border-t border-slate-200 p-3 text-xs">{JSON.stringify(execution.evaluation, null, 2)}</pre></details> : null}
      </div>
    </section>
  </div>;
}

export function RuleExecutionInspector(props: Props & { onClose: () => void; fullPageHref: string }) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    dialog.showModal();
    return () => dialog.close();
  }, []);
  return <dialog ref={ref} aria-labelledby="rule-inspector-title" onCancel={(event) => { event.preventDefault(); props.onClose(); }} className="fixed inset-y-0 right-0 left-auto m-0 h-full max-h-none w-full max-w-[780px] border-0 border-l border-slate-200 bg-[#f7f9fc] p-0 text-slate-900 shadow-2xl backdrop:bg-slate-950/40">
    <header className="sticky top-0 z-10 flex items-center justify-between gap-3 border-b border-slate-200 bg-white px-5 py-3"><h2 id="rule-inspector-title" className="font-semibold">Rule execution</h2><div className="flex items-center gap-2"><Link href={props.fullPageHref} onClick={props.onClose} className="rounded-lg border border-slate-200 px-3 py-2 text-xs font-semibold">Open full page</Link><button type="button" aria-label="Close rule execution" onClick={props.onClose} className="rounded-lg border border-slate-200 p-2"><X className="size-4" /></button></div></header>
    <div className="p-5"><InspectorContent {...props} /></div>
  </dialog>;
}

export function RuleExecutionFullPage(props: Props & { backHref: string }) {
  return <div className="mx-auto max-w-5xl space-y-5 px-4 sm:px-6"><Link href={props.backHref} className="text-sm font-semibold text-[#1f4f96]">← Back to decision</Link><h1 className="text-2xl font-semibold">Rule execution</h1><InspectorContent {...props} /></div>;
}
