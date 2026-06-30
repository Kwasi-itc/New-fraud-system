"use client";

import Link from "next/link";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Play, XCircle } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  decisionEngineApi,
  type TestRunDecisionSummary,
  type TestRunEvaluationResult,
  type TestRunRuleStat,
} from "@/lib/decision-engine-api";
import { useToastStore } from "@/stores/toast-store";
import { cn } from "@/lib/utils";

function formatDateTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

function scenarioStatusLabel(version?: number, live = false) {
  if (!version) {
    return live ? "Live" : "Iteration";
  }

  return live ? `V${version} Live` : `V${version}`;
}

function outcomeClasses(outcome?: string) {
  switch ((outcome ?? "").toLowerCase()) {
    case "approve":
      return "border-emerald-200 bg-emerald-50 text-emerald-700";
    case "review":
      return "border-amber-200 bg-amber-50 text-amber-700";
    case "block_and_review":
    case "block and review":
      return "border-orange-200 bg-orange-50 text-orange-700";
    case "decline":
      return "border-red-200 bg-red-50 text-red-700";
    default:
      return "border-slate-200 bg-slate-50 text-slate-700";
  }
}

function parseFields(value: string) {
  if (!value.trim()) {
    return {};
  }

  const parsed = JSON.parse(value);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("Fields must be a JSON object.");
  }
  return parsed;
}

function testRunStatusClasses(status: string) {
  switch (status.toLowerCase()) {
    case "up":
      return "border-emerald-200 bg-emerald-50 text-emerald-700";
    case "down":
    case "cancelled":
      return "border-red-200 bg-red-50 text-red-700";
    default:
      return "border-slate-200 bg-slate-50 text-slate-700";
  }
}

function ResultPanel({
  title,
  result,
}: {
  title: string;
  result: TestRunEvaluationResult["live"] | TestRunEvaluationResult["phantom"];
}) {
  const decision = result.decision;

  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-[15px] font-semibold text-slate-950">{title}</h3>
          <p className="text-[13px] text-slate-500">
            {result.triggered ? "Decision generated" : "No decision generated"}
          </p>
        </div>
        <Badge
          className={cn(
            "rounded-full border px-2 py-0.5 text-[12px] font-medium tracking-normal normal-case",
            outcomeClasses(decision?.outcome)
          )}
        >
          {decision?.outcome ?? "No outcome"}
        </Badge>
      </div>

      <div className="mt-4 grid gap-3 sm:grid-cols-2">
        <div className="rounded-xl border border-slate-200 px-3 py-3">
          <p className="text-[12px] text-slate-500">Score</p>
          <p className="mt-1 text-[16px] font-semibold text-slate-950">
            {decision?.score ?? "-"}
          </p>
        </div>
        <div className="rounded-xl border border-slate-200 px-3 py-3">
          <p className="text-[12px] text-slate-500">Object</p>
          <p className="mt-1 text-[14px] text-slate-950">
            {decision ? `${decision.object_type} / ${decision.object_id}` : "-"}
          </p>
        </div>
      </div>

      <div className="mt-4 space-y-2">
        <p className="text-[13px] font-medium text-slate-950">Rule executions</p>
        {result.rule_executions?.length ? (
          <div className="space-y-2">
            {result.rule_executions.map((ruleExecution) => (
              <div key={ruleExecution.id} className="rounded-xl border border-slate-200 px-3 py-3">
                <div className="flex items-center justify-between gap-3">
                  <p className="text-[13px] font-medium text-slate-950">
                    {ruleExecution.rule_name}
                  </p>
                  <span className="text-[13px] text-[#dd3719]">
                    {ruleExecution.score_modifier >= 0
                      ? `+${ruleExecution.score_modifier}`
                      : ruleExecution.score_modifier}
                  </span>
                </div>
                <p className="mt-1 text-[12px] text-slate-500">
                  Outcome: {ruleExecution.outcome}
                </p>
              </div>
            ))}
          </div>
        ) : (
          <div className="rounded-xl border border-slate-200 px-3 py-3 text-[13px] text-slate-500">
            No rule executions returned.
          </div>
        )}
      </div>
    </div>
  );
}

export function ScenarioTestRunDetailPage({
  scenarioId,
  testRunId,
}: {
  scenarioId: string;
  testRunId: string;
}) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const queryClient = useQueryClient();
  const pushToast = useToastStore((state) => state.pushToast);
  const [objectId, setObjectId] = useState("test_object_001");
  const [objectType, setObjectType] = useState("");
  const [fieldsDraft, setFieldsDraft] = useState("{\n  \n}");
  const [comparisonResult, setComparisonResult] = useState<TestRunEvaluationResult | null>(null);

  const scenarioQuery = useQuery({
    queryKey: ["decision-engine", "scenario", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.getScenario(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });
  const iterationsQuery = useQuery({
    queryKey: ["decision-engine", "iterations", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.listIterations(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });
  const testRunQuery = useQuery({
    queryKey: ["decision-engine", "test-run", tenantId, testRunId],
    queryFn: () => decisionEngineApi.getTestRun(tenantId, testRunId),
    enabled: Boolean(tenantId && testRunId),
  });
  const testRunDecisionSummariesQuery = useQuery({
    queryKey: ["decision-engine", "test-run-decision-summaries", tenantId, testRunId],
    queryFn: () => decisionEngineApi.listTestRunDecisionSummaries(tenantId, testRunId),
    enabled: Boolean(tenantId && testRunId),
  });
  const testRunRuleStatsQuery = useQuery({
    queryKey: ["decision-engine", "test-run-rule-stats", tenantId, testRunId],
    queryFn: () => decisionEngineApi.listTestRunRuleStats(tenantId, testRunId),
    enabled: Boolean(tenantId && testRunId),
  });

  const scenario = scenarioQuery.data?.scenario;
  const iterations = iterationsQuery.data?.iterations ?? [];
  const testRun = testRunQuery.data?.test_run;
  const iterationById = new Map(iterations.map((iteration) => [iteration.id, iteration]));
  const testRunDecisionSummaries = testRunDecisionSummariesQuery.data?.decisions ?? [];
  const testRunRuleStats = testRunRuleStatsQuery.data?.rules ?? [];

  const cancelTestRunMutation = useMutation({
    mutationFn: () => decisionEngineApi.cancelTestRun(tenantId, testRunId),
    onSuccess: async ({ test_run }) => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "test-runs", tenantId, scenarioId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "test-run", tenantId, test_run.id],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "test-run-decision-summaries", tenantId, test_run.id],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "test-run-rule-stats", tenantId, test_run.id],
      });
      setComparisonResult(null);
      pushToast({
        title: "Comparison cancelled",
        description: "The selected test run is no longer active.",
        variant: "success",
      });
    },
    onError: (error) => {
      pushToast({
        title: "Failed to cancel comparison",
        description: error instanceof Error ? error.message : "The comparison could not be cancelled.",
        variant: "error",
      });
    },
  });

  const evaluateTestRunMutation = useMutation({
    mutationFn: async () => {
      if (!scenario) {
        throw new Error("Scenario is not loaded.");
      }

      return decisionEngineApi.evaluateTestRun(tenantId, testRunId, {
        object_id: objectId,
        object_type: objectType.trim() || scenario.trigger_object_type,
        fields: parseFields(fieldsDraft),
      });
    },
    onSuccess: ({ result }) => {
      setComparisonResult(result);
      pushToast({
        title: "Comparison evaluated",
        description: "Live and phantom results are ready.",
        variant: "success",
      });
    },
    onError: (error) => {
      pushToast({
        title: "Failed to evaluate comparison",
        description: error instanceof Error ? error.message : "The comparison could not be evaluated.",
        variant: "error",
      });
    },
  });

  const isLoading =
    scenarioQuery.isLoading ||
    iterationsQuery.isLoading ||
    testRunQuery.isLoading ||
    testRunDecisionSummariesQuery.isLoading ||
    testRunRuleStatsQuery.isLoading;
  const error =
    scenarioQuery.error ??
    iterationsQuery.error ??
    testRunQuery.error ??
    testRunDecisionSummariesQuery.error ??
    testRunRuleStatsQuery.error;

  if (!tenantId) {
    return (
      <Card className="rounded-2xl border border-amber-200 bg-amber-50 shadow-none">
        <CardContent className="p-5 text-sm text-amber-800">
          Set `NEXT_PUBLIC_DATA_MODEL_TENANT_ID` to load this test run.
        </CardContent>
      </Card>
    );
  }

  if (isLoading) {
    return (
      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="p-5 text-sm text-slate-600">
          Loading comparison run...
        </CardContent>
      </Card>
    );
  }

  if (error || !scenario || !testRun) {
    return (
      <Card className="rounded-2xl border border-red-200 bg-red-50 shadow-none">
        <CardContent className="p-5 text-sm text-red-700">
          {error instanceof Error ? error.message : "Failed to load test run details."}
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="mx-auto w-full max-w-[1280px] space-y-6 px-4 sm:px-6 xl:px-8">
      <div className="flex flex-wrap items-center gap-3 border-b border-slate-200 pb-4">
        <Link
          href={`/detection/${scenarioId}/tests`}
          className="inline-flex size-10 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"
        >
          <ArrowLeft className="size-4" />
        </Link>
        <div>
          <h1 className="text-[1.65rem] font-semibold tracking-tight text-slate-950">
            Comparison run details
          </h1>
          <p className="text-[14px] text-slate-600">
            Inspect one live-vs-phantom run, compare metrics, and evaluate a sample record.
          </p>
        </div>
      </div>

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.1fr)_380px]">
        <div className="space-y-5">
          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="space-y-5 p-5">
              <div className="flex flex-wrap items-start justify-between gap-4">
                <div>
                  <h2 className="text-[18px] font-semibold text-slate-950">Selected run</h2>
                  <p className="mt-1 text-[13px] text-slate-500">
                    {scenario.name} comparing live against the selected phantom iteration.
                  </p>
                </div>
                <Button
                  variant="outline"
                  onClick={() => cancelTestRunMutation.mutate()}
                  disabled={
                    cancelTestRunMutation.isPending || testRun.status.toLowerCase() !== "up"
                  }
                  className="h-9 rounded-xl border-red-200 px-3.5 text-[13px] text-red-700 shadow-none"
                >
                  <XCircle className="size-4" />
                  {cancelTestRunMutation.isPending ? "Cancelling..." : "Cancel run"}
                </Button>
              </div>

              <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                <div className="rounded-xl border border-slate-200 px-3 py-3">
                  <p className="text-[12px] text-slate-500">Status</p>
                  <Badge
                    className={cn(
                      "mt-2 rounded-full border px-2 py-0.5 text-[12px] font-medium tracking-normal normal-case",
                      testRunStatusClasses(testRun.status)
                    )}
                  >
                    {testRun.status}
                  </Badge>
                </div>
                <div className="rounded-xl border border-slate-200 px-3 py-3">
                  <p className="text-[12px] text-slate-500">Live iteration</p>
                  <p className="mt-1 text-[14px] font-medium text-slate-950">
                    {scenarioStatusLabel(
                      iterationById.get(testRun.live_iteration_id)?.version,
                      true
                    )}
                  </p>
                </div>
                <div className="rounded-xl border border-slate-200 px-3 py-3">
                  <p className="text-[12px] text-slate-500">Phantom iteration</p>
                  <p className="mt-1 text-[14px] font-medium text-slate-950">
                    {scenarioStatusLabel(
                      iterationById.get(testRun.phantom_iteration_id)?.version
                    )}
                  </p>
                </div>
                <div className="rounded-xl border border-slate-200 px-3 py-3">
                  <p className="text-[12px] text-slate-500">Expires at</p>
                  <p className="mt-1 text-[14px] font-medium text-slate-950">
                    {formatDateTime(testRun.expires_at)}
                  </p>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="space-y-4 p-5">
              <div>
                <h2 className="text-[16px] font-semibold text-slate-950">Evaluate record</h2>
                <p className="text-[13px] text-slate-500">
                  Send one payload through this test run and compare live vs phantom outcomes.
                </p>
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <Input
                  value={objectId}
                  onChange={(event) => setObjectId(event.target.value)}
                  placeholder="Object ID"
                  className="h-10 rounded-xl border-slate-200 text-[14px] shadow-none"
                />
                <Input
                  value={objectType}
                  onChange={(event) => setObjectType(event.target.value)}
                  placeholder={scenario.trigger_object_type}
                  className="h-10 rounded-xl border-slate-200 text-[14px] shadow-none"
                />
              </div>
              <textarea
                value={fieldsDraft}
                onChange={(event) => setFieldsDraft(event.target.value)}
                className="min-h-56 w-full rounded-xl border border-slate-200 px-3 py-2 font-mono text-[13px] text-slate-900 outline-none focus:border-[#2d63b8]"
              />
              <div className="flex justify-end">
                <Button
                  onClick={() => evaluateTestRunMutation.mutate()}
                  disabled={evaluateTestRunMutation.isPending}
                  className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
                >
                  <Play className="size-4" />
                  {evaluateTestRunMutation.isPending ? "Evaluating..." : "Compare"}
                </Button>
              </div>
            </CardContent>
          </Card>

          {comparisonResult ? (
            <div className="grid gap-4 xl:grid-cols-2">
              <ResultPanel title="Live result" result={comparisonResult.live} />
              <ResultPanel title="Phantom result" result={comparisonResult.phantom} />
            </div>
          ) : null}
        </div>

        <div className="space-y-5">
          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="space-y-3 p-5">
              <h3 className="text-[16px] font-semibold text-slate-950">Decision summaries</h3>
              {testRunDecisionSummaries.length ? (
                <div className="space-y-2">
                  {testRunDecisionSummaries.map((summary: TestRunDecisionSummary, index: number) => (
                    <div
                      key={`${summary.outcome}-${summary.score}-${index}`}
                      className="flex items-center justify-between rounded-xl border border-slate-200 px-3 py-3"
                    >
                      <div>
                        <p className="text-[13px] font-medium text-slate-950">{summary.outcome}</p>
                        <p className="text-[12px] text-slate-500">Score {summary.score}</p>
                      </div>
                      <span className="text-[14px] font-semibold text-slate-950">
                        {summary.count}
                      </span>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="rounded-xl border border-slate-200 px-3 py-3 text-[13px] text-slate-500">
                  No decision summaries yet for this run.
                </div>
              )}
            </CardContent>
          </Card>

          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="space-y-3 p-5">
              <h3 className="text-[16px] font-semibold text-slate-950">Rule stats</h3>
              {testRunRuleStats.length ? (
                <div className="space-y-2">
                  {testRunRuleStats.map((rule: TestRunRuleStat) => (
                    <div key={rule.rule_id} className="rounded-xl border border-slate-200 px-3 py-3">
                      <p className="text-[13px] font-medium text-slate-950">{rule.rule_name}</p>
                      <div className="mt-2 grid grid-cols-2 gap-2 text-[12px] text-slate-600">
                        <span>Hit: {rule.hit_count}</span>
                        <span>No hit: {rule.no_hit_count}</span>
                        <span>Snoozed: {rule.snoozed_count}</span>
                        <span>Total: {rule.total_count}</span>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="rounded-xl border border-slate-200 px-3 py-3 text-[13px] text-slate-500">
                  No rule stats yet for this run.
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
