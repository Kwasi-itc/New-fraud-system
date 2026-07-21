"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronLeft, ChevronUp, Copy } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { summarizeRuleFormula } from "@/lib/rule-builder";
import { cn } from "@/lib/utils";
import { decisionEngineApi } from "@/lib/decision-engine-api";
import { formatExecutionRequestBody } from "@/components/detection/scheduled-execution-shared";

function formatDecisionField(value: unknown) {
  if (value === null || value === undefined) {
    return "-";
  }
  if (typeof value === "string") {
    return value;
  }
  return JSON.stringify(value);
}

function formatDecisionDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

function formatDecisionOutcome(value: string) {
  switch (value) {
    case "approve":
      return "Approve";
    case "block_and_review":
      return "Block and Review";
    case "decline":
      return "Decline";
    case "review":
      return "Review";
    default:
      return value;
  }
}

function scoreCardClass(value: string) {
  switch (value) {
    case "decline":
      return "bg-[linear-gradient(180deg,#f6d4cc_0%,#f2c9bf_100%)] text-[#dd3719]";
    case "block_and_review":
      return "bg-[linear-gradient(180deg,#ffedd5_0%,#fed7aa_100%)] text-[#ea580c]";
    case "review":
      return "bg-[linear-gradient(180deg,#fef3c7_0%,#fde68a_100%)] text-[#b45309]";
    default:
      return "bg-[linear-gradient(180deg,#2d63b8_0%,#1f4f96_100%)] text-white";
  }
}

function formatExecutionLabel(value: string) {
  return value
    .split("_")
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function executionStatusClass(value: string) {
  switch (value.toLowerCase()) {
    case "completed":
    case "success":
      return "border-emerald-200 bg-emerald-50 text-emerald-700";
    case "failed":
    case "dispatch_failed":
      return "border-rose-200 bg-rose-50 text-rose-700";
    case "pending":
    case "queued":
      return "border-amber-200 bg-amber-50 text-amber-700";
    case "sent":
    case "processing":
      return "border-sky-200 bg-sky-50 text-sky-700";
    default:
      return "border-slate-200 bg-slate-50 text-slate-700";
  }
}

function formatRuleSummaryForDisplay(summary: string) {
  return summary
    .replace(/\)\s+and\s+\(/g, ")\nAND\n(")
    .replace(/\)\s+or\s+\(/g, ")\nOR\n(")
    .replace(/\s+and\s+/g, "\nAND\n")
    .replace(/\s+or\s+/g, "\nOR\n");
}

export function DecisionDetailPage({ decisionId }: { decisionId: string }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const [detailOpen, setDetailOpen] = useState(true);
  const [responseOpen, setResponseOpen] = useState(true);
  const [triggerObjectOpen, setTriggerObjectOpen] = useState(true);
  const [rulesOpen, setRulesOpen] = useState(true);
  const [workflowExecutionsOpen, setWorkflowExecutionsOpen] = useState(true);
  const [screeningExecutionsOpen, setScreeningExecutionsOpen] = useState(true);
  const [openRuleIds, setOpenRuleIds] = useState<Record<string, boolean>>({});

  const decisionQuery = useQuery({
    queryKey: ["decision-engine", "decision", tenantId, decisionId],
    queryFn: () => decisionEngineApi.getDecision(tenantId, decisionId),
    enabled: Boolean(tenantId && decisionId),
  });
  const workflowExecutionsQuery = useQuery({
    queryKey: ["decision-engine", "workflow-executions", tenantId, decisionId],
    queryFn: () => decisionEngineApi.listWorkflowExecutions(tenantId, decisionId),
    enabled: Boolean(tenantId && decisionId),
  });
  const screeningExecutionsQuery = useQuery({
    queryKey: ["decision-engine", "screening-executions", tenantId, decisionId],
    queryFn: () => decisionEngineApi.listScreeningExecutions(tenantId, decisionId),
    enabled: Boolean(tenantId && decisionId),
  });
  const scoringRequestsQuery = useQuery({
    queryKey: ["decision-engine", "scoring-requests", tenantId, decisionId],
    queryFn: () => decisionEngineApi.listScoringRequests(tenantId, decisionId),
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
    queryKey: [
      "decision-engine",
      "rules",
      tenantId,
      decision?.scenario_id,
      decision?.scenario_iteration_id,
    ],
    queryFn: () =>
      decisionEngineApi.listRules(
        tenantId,
        decision!.scenario_id,
        decision!.scenario_iteration_id
      ),
    enabled: Boolean(tenantId && decision?.scenario_id && decision?.scenario_iteration_id),
  });

  const isLoading =
    decisionQuery.isLoading ||
    workflowExecutionsQuery.isLoading ||
    screeningExecutionsQuery.isLoading ||
    scoringRequestsQuery.isLoading ||
    rulesQuery.isLoading;
  const error =
    decisionQuery.error ??
    workflowExecutionsQuery.error ??
    screeningExecutionsQuery.error ??
    scoringRequestsQuery.error ??
    rulesQuery.error;

  const scenario = scenarioQuery.data?.scenario;
  const iterationVersion = useMemo(() => {
    if (!decision) {
      return null;
    }

    const iteration = (iterationsQuery.data?.iterations ?? []).find(
      (item) => item.id === decision.scenario_iteration_id
    );
    return iteration?.version ?? null;
  }, [decision, iterationsQuery.data?.iterations]);

  const ruleExecutions = useMemo(
    () => decisionQuery.data?.rule_executions ?? [],
    [decisionQuery.data?.rule_executions]
  );
  const requestBodyEntries =
    decision?.request_body &&
    typeof decision.request_body === "object" &&
    !Array.isArray(decision.request_body)
      ? Object.entries(decision.request_body)
      : [];
  const detectionResponse = useMemo(
    () => ({
      decision: {
        id: decision.id,
        tenant_id: decision.tenant_id,
        scenario_id: decision.scenario_id,
        scenario_iteration_id: decision.scenario_iteration_id,
        object_id: decision.object_id,
        object_type: decision.object_type,
        outcome: decision.outcome,
        score: decision.score,
        triggered: decision.triggered,
        created_at: decision.created_at,
      },
      rule_executions: ruleExecutions,
    }),
    [decision, ruleExecutions]
  );
  const rulesById = useMemo(
    () =>
      new Map((rulesQuery.data?.rules ?? []).map((rule) => [rule.id, rule])),
    [rulesQuery.data?.rules]
  );
  const workflowExecutions = workflowExecutionsQuery.data?.workflow_executions ?? [];
  const screeningExecutions = screeningExecutionsQuery.data?.screening_executions ?? [];
  void scoringRequestsQuery.data?.scoring_requests;

  if (!tenantId) {
    return (
      <Card className="rounded-2xl border border-amber-200 bg-amber-50 shadow-none">
        <CardContent className="p-5 text-sm text-amber-800">
          Set `NEXT_PUBLIC_DATA_MODEL_TENANT_ID` to load this decision.
        </CardContent>
      </Card>
    );
  }

  if (isLoading) {
    return (
      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="p-5 text-sm text-slate-600">
          Loading decision details...
        </CardContent>
      </Card>
    );
  }

  if (error || !decision) {
    return (
      <Card className="rounded-2xl border border-red-200 bg-red-50 shadow-none">
        <CardContent className="p-5 text-sm text-red-700">
          {error instanceof Error ? error.message : "Failed to load decision details."}
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="mx-auto w-full max-w-[1380px] space-y-6 px-4 sm:px-6 xl:px-8">
      <div className="flex items-center justify-between gap-4 border-b border-slate-200 pb-4">
        <div className="flex flex-wrap items-center gap-3">
          <Link
            href="/detection"
            className="inline-flex size-10 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"
          >
            <ChevronLeft className="size-4" />
          </Link>
          <div className="flex flex-wrap items-center gap-3 text-[15px] text-slate-500">
            <span>Detection</span>
            <span>/</span>
            <span>Decisions</span>
            <span>/</span>
            <span className="font-semibold text-slate-950">Decision</span>
            <div className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2 text-[14px] text-slate-950">
              <span>ID {decision.id}</span>
              <Copy className="size-4 text-slate-500" />
            </div>
          </div>
        </div>
      </div>

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.1fr)_420px]">
        <div className="space-y-5">
          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="p-0">
              <div className="flex items-center justify-between border-b border-slate-200 px-6 py-5">
                <h2 className="text-[18px] font-semibold text-slate-950">Decision Detail</h2>
                <button
                  type="button"
                  onClick={() => setDetailOpen((current) => !current)}
                  className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200"
                >
                  {detailOpen ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
                </button>
              </div>
              {detailOpen ? (
                <div className="grid gap-4 px-6 py-5 md:grid-cols-[160px_1fr]">
                  {[
                    ["Date", formatDecisionDate(decision.created_at)],
                    ["Scenario", scenario?.name ?? decision.scenario_id],
                    ["Version", iterationVersion ? `V${iterationVersion}` : decision.scenario_iteration_id],
                    ["Object Type", decision.object_type],
                    ["Case", "-"],
                  ].map(([label, value]) => (
                    <div key={label} className="contents">
                      <div className="text-[14px] font-semibold text-slate-950">{label}</div>
                      <div
                        className={cn(
                          "text-[14px]",
                          label === "Scenario" ? "font-medium text-[#2d63b8]" : "text-slate-800"
                        )}
                      >
                        {value}
                      </div>
                    </div>
                  ))}
                </div>
              ) : null}
            </CardContent>
          </Card>

          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="p-0">
              <div className="flex items-center justify-between border-b border-slate-200 px-6 py-5">
                <h2 className="text-[18px] font-semibold text-slate-950">Rules</h2>
                <button
                  type="button"
                  onClick={() => setRulesOpen((current) => !current)}
                  className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200"
                >
                  {rulesOpen ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
                </button>
              </div>
              {rulesOpen ? (
                <div className="space-y-3 px-6 py-5">
                  {ruleExecutions.length === 0 ? (
                    <div className="rounded-xl border border-dashed border-slate-200 px-4 py-5 text-[13px] text-slate-500">
                      No rule executions were recorded for this decision.
                    </div>
                  ) : (
                    ruleExecutions.map((item) => {
                      const isOpen = openRuleIds[item.id] ?? false;
                      const ruleDefinition = rulesById.get(item.rule_id);
                      const ruleSummary = ruleDefinition
                        ? summarizeRuleFormula(ruleDefinition.formula)
                        : null;
                      const formattedRuleSummary = ruleSummary
                        ? formatRuleSummaryForDisplay(ruleSummary)
                        : null;
                      return (
                        <div key={item.id} className="rounded-xl border border-slate-200 bg-white">
                          <button
                            type="button"
                            onClick={() =>
                              setOpenRuleIds((current) => ({
                                ...current,
                                [item.id]: !isOpen,
                              }))
                            }
                            className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left"
                          >
                            <div className="flex items-center gap-3">
                              {isOpen ? (
                                <ChevronDown className="size-4 text-slate-500" />
                              ) : (
                                <ChevronLeft className="size-4 rotate-[-90deg] text-slate-500" />
                              )}
                              <span className="text-[15px] font-medium text-slate-950">
                                {item.rule_name}
                              </span>
                            </div>
                            <div className="flex items-center gap-2">
                              <span className="rounded-full border border-[#b7d1f3] bg-[#edf5ff] px-3 py-1 text-[13px] font-medium text-[#2d63b8]">
                                {item.score_modifier >= 0
                                  ? `+${item.score_modifier}`
                                  : item.score_modifier}
                              </span>
                              <span
                                className={cn(
                                  "rounded-full border px-3 py-1 text-[13px] font-medium",
                                  item.outcome.toLowerCase() === "hit"
                                    ? "border-rose-200 text-rose-600"
                                    : "border-emerald-200 text-emerald-600"
                                )}
                              >
                                {item.outcome}
                              </span>
                            </div>
                          </button>
                          {isOpen ? (
                            <div className="border-t border-slate-200 px-4 py-4">
                              <div className="space-y-4 rounded-xl border border-slate-200 bg-slate-50 px-4 py-4 text-[14px] text-slate-700">
                                <div className="flex flex-wrap gap-6">
                                  <div>
                                    <div className="text-[12px] font-medium text-slate-500">
                                      Rule ID
                                    </div>
                                    <div className="mt-1">{item.rule_id}</div>
                                  </div>
                                  <div>
                                    <div className="text-[12px] font-medium text-slate-500">
                                      Created
                                    </div>
                                    <div className="mt-1">{formatDecisionDate(item.created_at)}</div>
                                  </div>
                                  <div>
                                    <div className="text-[12px] font-medium text-slate-500">
                                      Score contribution
                                    </div>
                                    <div className="mt-1">
                                      {item.score_modifier >= 0
                                        ? `+${item.score_modifier}`
                                        : item.score_modifier}
                                    </div>
                                  </div>
                                </div>
                                {ruleDefinition ? (
                                  <div className="grid gap-4 md:grid-cols-2">
                                    <div>
                                      <div className="text-[12px] font-medium text-slate-500">
                                        Description
                                      </div>
                                      <div className="mt-1 text-[14px] text-slate-800">
                                        {ruleDefinition.description || "No description"}
                                      </div>
                                    </div>
                                    <div>
                                      <div className="text-[12px] font-medium text-slate-500">
                                        Rule group
                                      </div>
                                      <div className="mt-1 text-[14px] text-slate-800">
                                        {ruleDefinition.rule_group || "No rule group"}
                                      </div>
                                    </div>
                                  </div>
                                ) : null}
                                <div>
                                  <div className="text-[12px] font-medium text-slate-500">
                                    Rule logic
                                  </div>
                                  <div className="mt-2 overflow-hidden rounded-xl border border-[#d7e7fb] bg-[#f7fbff]">
                                    <div className="border-b border-[#d7e7fb] px-3 py-2 text-[12px] font-medium text-[#2d63b8]">
                                      Readable summary
                                    </div>
                                    <pre className="whitespace-pre-wrap break-words px-3 py-3 font-mono text-[13px] leading-6 text-slate-800">
                                      {formattedRuleSummary ??
                                        "Rule definition is not available for this iteration."}
                                    </pre>
                                  </div>
                                </div>
                              </div>
                            </div>
                          ) : null}
                        </div>
                      );
                    })
                  )}
                </div>
              ) : null}
            </CardContent>
          </Card>

          <div className="space-y-5">
            <Card className="rounded-2xl border border-slate-200 shadow-none">
              <CardContent className="p-0">
                <div className="flex items-center justify-between gap-3 border-b border-slate-200 px-5 py-5">
                  <div>
                    <div className="text-[15px] font-semibold text-slate-950">Workflow Executions</div>
                    <div className="text-[13px] text-slate-500">
                      Actions dispatched from this decision
                    </div>
                  </div>
                  <div className="flex items-center gap-3">
                    <Badge className="rounded-full border-slate-200 bg-white px-2.5 py-0.5 text-[12px] font-medium tracking-normal normal-case text-slate-700">
                      {workflowExecutions.length}
                    </Badge>
                    <button
                      type="button"
                      onClick={() => setWorkflowExecutionsOpen((current) => !current)}
                      className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200"
                    >
                      {workflowExecutionsOpen ? (
                        <ChevronUp className="size-4" />
                      ) : (
                        <ChevronDown className="size-4" />
                      )}
                    </button>
                  </div>
                </div>
                {workflowExecutionsOpen ? (
                  <div className="space-y-3 p-5">
                    {workflowExecutions.length === 0 ? (
                      <div className="rounded-xl border border-dashed border-slate-200 bg-slate-50 px-4 py-5 text-[13px] text-slate-500">
                        No workflow actions were dispatched for this decision.
                      </div>
                    ) : (
                      workflowExecutions.map((item) => (
                        <div
                          key={item.id}
                          className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-4"
                        >
                          <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                              <div className="text-[14px] font-semibold text-slate-950">
                                {formatExecutionLabel(item.action_type)}
                              </div>
                              <div className="mt-1 text-[12px] text-slate-500">
                                Workflow {item.workflow_id}
                              </div>
                            </div>
                            <Badge
                              className={cn(
                                "rounded-full border px-2.5 py-0.5 text-[12px] font-medium tracking-normal normal-case",
                                executionStatusClass(item.status)
                              )}
                            >
                              {formatExecutionLabel(item.status)}
                            </Badge>
                          </div>
                          <div className="mt-3 grid gap-2 text-[12px] text-slate-500">
                            <div>Created {formatDecisionDate(item.created_at)}</div>
                            <div className="truncate">Execution ID {item.id}</div>
                          </div>
                        </div>
                      ))
                    )}
                  </div>
                ) : null}
              </CardContent>
            </Card>

            <Card className="rounded-2xl border border-slate-200 shadow-none">
              <CardContent className="p-0">
                <div className="flex items-center justify-between gap-3 border-b border-slate-200 px-5 py-5">
                  <div>
                    <div className="text-[15px] font-semibold text-slate-950">Screening Executions</div>
                    <div className="text-[13px] text-slate-500">
                      External screening requests created from this decision
                    </div>
                  </div>
                  <div className="flex items-center gap-3">
                    <Badge className="rounded-full border-slate-200 bg-white px-2.5 py-0.5 text-[12px] font-medium tracking-normal normal-case text-slate-700">
                      {screeningExecutions.length}
                    </Badge>
                    <button
                      type="button"
                      onClick={() => setScreeningExecutionsOpen((current) => !current)}
                      className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200"
                    >
                      {screeningExecutionsOpen ? (
                        <ChevronUp className="size-4" />
                      ) : (
                        <ChevronDown className="size-4" />
                      )}
                    </button>
                  </div>
                </div>
                {screeningExecutionsOpen ? (
                  <div className="space-y-3 p-5">
                    {screeningExecutions.length === 0 ? (
                      <div className="rounded-xl border border-dashed border-slate-200 bg-slate-50 px-4 py-5 text-[13px] text-slate-500">
                        No screening requests were created for this decision.
                      </div>
                    ) : (
                      screeningExecutions.map((item) => (
                        <div
                          key={item.id}
                          className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-4"
                        >
                          <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                              <div className="text-[14px] font-semibold text-slate-950">
                                {item.provider_reference || item.config_id}
                              </div>
                              <div className="mt-1 text-[12px] text-slate-500">
                                Config {item.config_id}
                              </div>
                            </div>
                            <Badge
                              className={cn(
                                "rounded-full border px-2.5 py-0.5 text-[12px] font-medium tracking-normal normal-case",
                                executionStatusClass(item.status)
                              )}
                            >
                              {formatExecutionLabel(item.status)}
                            </Badge>
                          </div>
                          <div className="mt-3 grid gap-2 text-[12px] text-slate-500">
                            <div>Created {formatDecisionDate(item.created_at)}</div>
                            {item.failed_at && item.last_error ? (
                              <div className="line-clamp-2 text-rose-700">
                                Failed: {item.last_error}
                              </div>
                            ) : null}
                            <div className="truncate">Execution ID {item.id}</div>
                          </div>
                        </div>
                      ))
                    )}
                  </div>
                ) : null}
              </CardContent>
            </Card>

          </div>
        </div>

        <div className="space-y-5">
          <div className="grid gap-5 md:grid-cols-2">
            <div
              className={cn(
                "rounded-2xl px-6 py-4 text-center shadow-none",
                "bg-[linear-gradient(180deg,#2d63b8_0%,#1f4f96_100%)] text-white"
              )}
            >
              <div className="text-[16px] opacity-90">Score</div>
              <div className="mt-2 text-[44px] font-semibold leading-none">{decision.score}</div>
            </div>
            <div
              className={cn(
                "rounded-2xl px-6 py-4 text-center shadow-none",
                scoreCardClass(decision.outcome)
              )}
            >
              <div className="text-[16px] opacity-90">Outcome</div>
              <div className="mt-2 text-[28px] font-semibold">
                {formatDecisionOutcome(decision.outcome)}
              </div>
            </div>
          </div>

          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="p-0">
              <div className="flex items-center justify-between border-b border-slate-200 px-6 py-5">
                <h2 className="text-[18px] font-semibold text-slate-950">
                  Evaluated request body
                </h2>
                <button
                  type="button"
                  onClick={() => setTriggerObjectOpen((current) => !current)}
                  className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200"
                >
                  {triggerObjectOpen ? (
                    <ChevronUp className="size-4" />
                  ) : (
                    <ChevronDown className="size-4" />
                  )}
                </button>
              </div>
              {triggerObjectOpen ? (
                <div className="space-y-4 px-6 py-5">
                  {requestBodyEntries.length > 0 ? (
                    <div className="space-y-3">
                      <div className="grid gap-3 sm:grid-cols-2">
                        {requestBodyEntries.map(([key, value]) => (
                          <div
                            key={key}
                            className={cn(
                              "rounded-xl border border-slate-200 bg-slate-50/70 px-3 py-3",
                              key === "fields" ? "sm:col-span-2" : ""
                            )}
                          >
                            <div className="text-[11px] uppercase tracking-[0.08em] text-slate-500">
                              {key.replace(/_/g, " ")}
                            </div>
                            <div className="mt-1 whitespace-pre-wrap break-words text-[13px] text-slate-900">
                              {formatDecisionField(value)}
                            </div>
                          </div>
                        ))}
                      </div>
                      <details className="rounded-xl border border-slate-200 bg-slate-50">
                        <summary className="cursor-pointer px-3 py-2 text-[13px] font-medium text-slate-800">
                          View raw JSON
                        </summary>
                        <pre className="max-h-[320px] overflow-auto whitespace-pre-wrap break-words border-t border-slate-200 px-3 py-3 font-mono text-[12px] text-slate-800">
                          {formatExecutionRequestBody(decision.request_body ?? null)}
                        </pre>
                      </details>
                    </div>
                  ) : (
                    <pre className="max-h-[320px] overflow-auto whitespace-pre-wrap break-words rounded-xl bg-slate-50 px-3 py-3 font-mono text-[12px] text-slate-800">
                      {formatExecutionRequestBody(decision.request_body ?? null)}
                    </pre>
                  )}
                </div>
              ) : null}
            </CardContent>
          </Card>

          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="p-0">
              <div className="flex items-center justify-between border-b border-slate-200 px-6 py-5">
                <h2 className="text-[18px] font-semibold text-slate-950">
                  Detection response
                </h2>
                <button
                  type="button"
                  onClick={() => setResponseOpen((current) => !current)}
                  className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200"
                >
                  {responseOpen ? (
                    <ChevronUp className="size-4" />
                  ) : (
                    <ChevronDown className="size-4" />
                  )}
                </button>
              </div>
              {responseOpen ? (
                <div className="space-y-4 px-6 py-5">
                  <div className="grid gap-3 sm:grid-cols-3">
                    <div className="rounded-xl border border-slate-200 px-3 py-3">
                      <p className="text-[12px] text-slate-500">Outcome</p>
                      <p className="mt-1 text-[16px] font-semibold text-slate-950">
                        {formatDecisionOutcome(decision.outcome)}
                      </p>
                    </div>
                    <div className="rounded-xl border border-slate-200 px-3 py-3">
                      <p className="text-[12px] text-slate-500">Score</p>
                      <p className="mt-1 text-[16px] font-semibold text-slate-950">
                        {decision.score}
                      </p>
                    </div>
                    <div className="rounded-xl border border-slate-200 px-3 py-3">
                      <p className="text-[12px] text-slate-500">Triggered</p>
                      <p className="mt-1 text-[16px] font-semibold text-slate-950">
                        {decision.triggered ? "Yes" : "No"}
                      </p>
                    </div>
                  </div>
                  <details className="rounded-xl border border-slate-200 bg-slate-50" open>
                    <summary className="cursor-pointer px-3 py-2 text-[13px] font-medium text-slate-800">
                      View response JSON
                    </summary>
                    <pre className="max-h-[360px] overflow-auto whitespace-pre-wrap break-words border-t border-slate-200 px-3 py-3 font-mono text-[12px] text-slate-800">
                      {JSON.stringify(detectionResponse, null, 2)}
                    </pre>
                  </details>
                </div>
              ) : null}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
