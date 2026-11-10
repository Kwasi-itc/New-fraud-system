"use client";

import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import {
  ChevronDown,
  ChevronUp,
  Layers,
  ShieldAlert,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { StoredScreeningEvidence } from "@/components/screening/stored-evidence";
import { caseKeys } from "@/lib/case-manager-query";
import {
  type Rule,
  type RuleExecution,
  type RuleEvaluationNode,
} from "@/lib/decision-engine-api";
import { summarizeRuleFormula, summarizeRuleAstNode } from "@/lib/rule-builder";
import { cn } from "@/lib/utils";
import { Problem, timestamp } from "./shared";
import { CopyButton } from "./structured-data-viewer";

type EvaluationEvidenceRow = {
  key: string;
  expression: string;
  result: boolean;
};

const comparisonOperatorLabels: Record<string, string> = {
  eq: "=",
  neq: "!=",
  gt: ">",
  gte: ">=",
  lt: "<",
  lte: "<=",
  contains: "contains",
  in: "in",
  starts_with: "starts with",
  ends_with: "ends with",
};

function formatNodeValue(value: unknown): string {
  if (value === null || value === undefined) return "null";
  if (typeof value === "string") return `"${value}"`;
  if (typeof value === "object") {
    try {
      return JSON.stringify(value);
    } catch {
      return String(value);
    }
  }
  return String(value);
}

function formatEvaluationExpression(node: RuleEvaluationNode): string | null {
  const fn = node.function?.trim().toLowerCase() ?? "";
  if (fn in comparisonOperatorLabels && node.children && node.children.length === 2) {
    const [left, right] = node.children;
    const lSummary = summarizeRuleAstNode(left as unknown);
    const lVal = formatNodeValue(left.return_value ?? left.constant);
    const rVal = formatNodeValue(right.return_value ?? right.constant);
    const leftLabel = lSummary
      ? lSummary.length > 40
        ? `Result [${lVal}]`
        : `${lSummary} [${lVal}]`
      : lVal;
    return `${leftLabel} ${comparisonOperatorLabels[fn]} ${rVal}`;
  }
  if (node.function && typeof node.return_value !== "undefined") {
    return `${node.function}(): ${formatNodeValue(node.return_value)}`;
  }
  return null;
}

function collectEvaluationEvidence(
  node: RuleEvaluationNode | null | undefined,
  path: string[] = []
): EvaluationEvidenceRow[] {
  if (!node || node.skipped) return [];

  const rows: EvaluationEvidenceRow[] = [];
  const expression = formatEvaluationExpression(node);
  if (expression && typeof node.return_value === "boolean") {
    rows.push({
      key: [...path, node.function?.toLowerCase() || "node"].join("."),
      expression,
      result: node.return_value,
    });
  }

  node.children?.forEach((child, index) => {
    rows.push(...collectEvaluationEvidence(child, [...path, `c${index}`]));
  });

  Object.entries(node.named_children ?? {}).forEach(([name, child]) => {
    rows.push(...collectEvaluationEvidence(child, [...path, name]));
  });

  return rows;
}

function formatRuleSummaryForDisplay(summary: string) {
  return summary
    .replace(/\)\s+and\s+\(/g, ")\nAND\n(")
    .replace(/\)\s+or\s+\(/g, ")\nOR\n(")
    .replace(/\s+where\s+/g, "\n  where ")
    .replace(/\s+and\s+/g, "\n  and ")
    .replace(/\s+or\s+/g, "\n  or ");
}

function ruleOutcomeBadgeClass(outcome: string) {
  switch (outcome?.trim().toLowerCase()) {
    case "hit":
      return "border-rose-200 bg-rose-50 text-rose-700 font-bold";
    case "snoozed":
      return "border-amber-200 bg-amber-50 text-amber-700 font-semibold";
    case "error":
      return "border-red-200 bg-red-50 text-red-700 font-semibold";
    case "no_hit":
    case "pass":
      return "border-emerald-200 bg-emerald-50 text-emerald-700 font-medium";
    default:
      return "border-slate-200 bg-slate-50 text-slate-700 font-medium";
  }
}

function RuleExecutionItem({
  item,
  ruleDefinition,
}: {
  item: RuleExecution;
  ruleDefinition?: Rule;
}) {
  const normOutcome = item.outcome?.toLowerCase() ?? "";
  const isHit = normOutcome === "hit";
  const scoreContribution = isHit ? item.score_modifier : 0;
  const ruleFormula = ruleDefinition?.formula;
  const ruleSummary = ruleFormula
    ? summarizeRuleFormula(ruleFormula)
    : item.evaluation
    ? summarizeRuleFormula(item.evaluation)
    : null;
  const formattedRuleSummary = ruleSummary ? formatRuleSummaryForDisplay(ruleSummary) : null;
  const evidenceRows = collectEvaluationEvidence(item.evaluation);
  const description = ruleDefinition?.description;

  return (
    <div
      className={cn(
        "rounded-xl border transition-all p-4 space-y-3.5",
        isHit
          ? "border-rose-200/90 bg-rose-50/20"
          : "border-slate-200 bg-white"
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 max-w-xl">
          <span className="text-[15px] font-bold text-slate-950 block">
            {item.rule_name || `Rule ${item.rule_id.slice(0, 8)}…`}
          </span>
          {description && (
            <p className="mt-1 text-xs text-slate-600 font-normal leading-relaxed">
              {description}
            </p>
          )}
        </div>

        <div className="flex items-center gap-2 shrink-0">
          <span
            className={cn(
              "rounded-full border px-3 py-1 text-xs font-semibold",
              ruleOutcomeBadgeClass(item.outcome)
            )}
          >
            {item.outcome.toUpperCase()}
          </span>

          <span
            className={cn(
              "rounded-full border px-3 py-1 text-xs font-semibold",
              scoreContribution > 0
                ? "border-blue-200 bg-blue-50 text-blue-700"
                : scoreContribution < 0
                ? "border-emerald-200 bg-emerald-50 text-emerald-700"
                : "border-slate-200 bg-slate-50 text-slate-600"
            )}
          >
            {scoreContribution >= 0 ? `+${scoreContribution}` : scoreContribution} score
          </span>
        </div>
      </div>

      {formattedRuleSummary && (
        <div className="overflow-hidden rounded-xl border border-slate-200 bg-slate-50/60">
          <div className="border-b border-slate-200/80 px-3 py-1.5 text-xs font-semibold text-slate-700">
            Rule
          </div>
          <pre className="whitespace-pre-wrap break-words px-3.5 py-2.5 font-mono text-xs leading-relaxed text-slate-800">
            {formattedRuleSummary}
          </pre>
        </div>
      )}

      {evidenceRows.length > 0 && (
        <div className="space-y-1.5">
          <div className="text-xs font-semibold text-slate-600">
            Evaluation evidence
          </div>
          <div className="space-y-1.5">
            {evidenceRows.map((row) => (
              <div
                key={row.key}
                className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs"
              >
                <div className="font-mono text-slate-800">
                  {row.expression}
                </div>
                <span
                  className={cn(
                    "rounded-full border px-2.5 py-0.5 text-[11px] font-semibold",
                    row.result
                      ? "border-emerald-200 bg-emerald-50 text-emerald-700"
                      : "border-rose-200 bg-rose-50 text-rose-700"
                  )}
                >
                  {String(row.result).toUpperCase()}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="flex flex-wrap items-center justify-between gap-2 pt-1 border-t border-slate-100 text-[11px] text-slate-400">
        <div className="flex items-center gap-1.5 font-mono">
          <span>Rule ID: {item.rule_id}</span>
          <CopyButton text={item.rule_id} />
        </div>
        {item.created_at && <span>Evaluated {timestamp(item.created_at)}</span>}
      </div>
    </div>
  );
}

function DecisionRuleExecutions({ data }: { data: unknown }) {
  const ruleExecutions: RuleExecution[] = Array.isArray(data)
    ? (data as RuleExecution[])
    : Array.isArray((data as { rule_executions?: RuleExecution[] })?.rule_executions)
    ? (data as { rule_executions: RuleExecution[] }).rule_executions
    : [];

  const rules = (data as { rules?: Rule[] })?.rules;

  const rulesById = useMemo(() => {
    const map = new Map<string, Rule>();
    for (const rule of rules ?? []) {
      map.set(rule.id, rule);
    }
    return map;
  }, [rules]);

  if (ruleExecutions.length === 0) {
    return (
      <div className="rounded-xl border border-dashed border-slate-200 p-6 text-center text-sm text-slate-500 italic">
        No rule executions recorded for this decision.
      </div>
    );
  }

  const hits = ruleExecutions.filter((r) => r.outcome?.toLowerCase() === "hit").length;

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2 border-b border-slate-100 pb-2">
        <div className="flex items-center gap-2">
          <Layers className="size-4 text-blue-600" />
          <span className="text-xs font-bold uppercase tracking-wider text-slate-700">
            Rule Executions ({ruleExecutions.length})
          </span>
        </div>
        <div className="flex items-center gap-2 text-xs">
          {hits > 0 && (
            <span className="font-semibold text-rose-600 bg-rose-50 border border-rose-200 rounded-full px-2 py-0.5">
              {hits} rule {hits === 1 ? "hit" : "hits"}
            </span>
          )}
        </div>
      </div>

      <div className="space-y-2.5">
        {ruleExecutions.map((item) => (
          <RuleExecutionItem
            key={item.id || item.rule_id}
            item={item}
            ruleDefinition={rulesById.get(item.rule_id)}
          />
        ))}
      </div>
    </div>
  );
}

export function Evidence({
  tenant,
  session,
  caseId,
  kind,
  resource,
}: {
  tenant: string;
  session: string;
  caseId: string;
  kind: "decisions" | "screenings";
  resource: string;
}) {
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: [...caseKeys.scope(tenant, session), caseId, "evidence", kind, resource],
    enabled: open,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    queryFn: async () => {
      const response = await fetch(`/api/case-evidence/${tenant}/${caseId}/${kind}/${resource}${kind === "decisions" ? "?rules=1" : ""}`, {
        cache: "no-store",
      });
      const data = await response.json();
      if (!response.ok)
        throw new Error(
          typeof data.error === "string" ? data.error : data.error?.message ?? "Evidence is unavailable."
        );
      return data as unknown;
    },
  });

  const buttonText =
    kind === "decisions"
      ? open
        ? "Hide rule executions"
        : "Inspect rule executions"
      : open
      ? "Hide evidence"
      : "Inspect evidence";

  return (
    <div className="space-y-3">
      <Button
        size="sm"
        variant="outline"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        className="gap-1.5 font-medium border-slate-200 hover:bg-slate-50 transition-colors"
      >
        <ShieldAlert className="size-3.5 text-blue-600" />
        {buttonText}
        {open ? <ChevronUp className="size-3.5 text-slate-400" /> : <ChevronDown className="size-3.5 text-slate-400" />}
      </Button>

      {open && (
        <div className="rounded-xl border border-slate-200 bg-white p-4 text-sm shadow-xs transition-all">
          <Problem error={query.error} retry={() => void query.refetch()} />
          {query.isPending && (
            <div className="flex items-center gap-2 py-4 text-slate-500">
              <span className="inline-block size-3 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
              <p role="status">
                {kind === "decisions" ? "Loading rule executions…" : "Loading source evidence…"}
              </p>
            </div>
          )}
          {query.isSuccess && (
            kind === "decisions" ? (
              <DecisionRuleExecutions data={query.data} />
            ) : (
              <StoredScreeningEvidence data={query.data} />
            )
          )}
        </div>
      )}
    </div>
  );
}
