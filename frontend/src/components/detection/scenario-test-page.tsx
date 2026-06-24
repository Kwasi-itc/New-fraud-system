"use client";

import Link from "next/link";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, GitCompareArrows, Play, Plus, XCircle } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  decisionEngineApi,
  type TestRun,
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
    <div className="rounded-xl border border-slate-200 bg-white p-4">
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

export function ScenarioTestPage({ scenarioId }: { scenarioId: string }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const queryClient = useQueryClient();
  const pushToast = useToastStore((state) => state.pushToast);
  const [selectedTestRunId, setSelectedTestRunId] = useState<string | null>(null);
  const [phantomIterationId, setPhantomIterationId] = useState("");
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

  const testRunsQuery = useQuery({
    queryKey: ["decision-engine", "test-runs", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.listTestRuns(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });

  const createTestRunMutation = useMutation({
    mutationFn: async () => {
      const scenario = scenarioQuery.data?.scenario;
      if (!scenario?.live_iteration_id) {
        throw new Error("Publish a live iteration before creating a comparison.");
      }
      if (!activePhantomIterationId) {
        throw new Error("Select a phantom iteration to compare against live.");
      }

      const expiresAt = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toISOString();
      return decisionEngineApi.createTestRun(tenantId, scenarioId, {
        phantom_iteration_id: activePhantomIterationId,
        expires_at: expiresAt,
      });
    },
    onSuccess: async ({ test_run }) => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "test-runs", tenantId, scenarioId],
      });
      setSelectedTestRunId(test_run.id);
      setComparisonResult(null);
      pushToast({
        title: "Comparison created",
        description: "A new test run is ready for evaluation.",
        variant: "success",
      });
    },
    onError: (error) => {
      pushToast({
        title: "Failed to create comparison",
        description: error instanceof Error ? error.message : "The comparison could not be created.",
        variant: "error",
      });
    },
  });

  const evaluateTestRunMutation = useMutation({
    mutationFn: async () => {
      if (!activeTestRunId) {
        throw new Error("Select or create a comparison before evaluating.");
      }

      const scenario = scenarioQuery.data?.scenario;
      if (!scenario) {
        throw new Error("Scenario is not loaded.");
      }

      return decisionEngineApi.evaluateTestRun(tenantId, activeTestRunId, {
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

  const scenario = scenarioQuery.data?.scenario;
  const iterations = iterationsQuery.data?.iterations ?? [];
  const liveIterationId = scenario?.live_iteration_id ?? null;
  const phantomIterations = iterations
    .filter((iteration) => iteration.id !== liveIterationId)
    .sort((left, right) => right.version - left.version);
  const testRuns = (testRunsQuery.data?.test_runs ?? []).sort(
    (left, right) =>
      new Date(right.created_at).getTime() - new Date(left.created_at).getTime()
  );
  const selectedTestRun =
    testRuns.find((testRun) => testRun.id === selectedTestRunId) ?? testRuns[0] ?? null;
  const iterationById = new Map(iterations.map((iteration) => [iteration.id, iteration]));
  const activePhantomIteration =
    phantomIterations.find((iteration) => iteration.id === phantomIterationId) ?? phantomIterations[0] ?? null;
  const activePhantomIterationId = phantomIterationId || activePhantomIteration?.id || "";
  const activeTestRunId = selectedTestRunId || selectedTestRun?.id || null;
  const selectedTestRunQuery = useQuery({
    queryKey: ["decision-engine", "test-run", tenantId, activeTestRunId],
    queryFn: () => decisionEngineApi.getTestRun(tenantId, activeTestRunId!),
    enabled: Boolean(tenantId && activeTestRunId),
  });
  const testRunDecisionSummariesQuery = useQuery({
    queryKey: ["decision-engine", "test-run-decision-summaries", tenantId, activeTestRunId],
    queryFn: () => decisionEngineApi.listTestRunDecisionSummaries(tenantId, activeTestRunId!),
    enabled: Boolean(tenantId && activeTestRunId),
  });
  const testRunRuleStatsQuery = useQuery({
    queryKey: ["decision-engine", "test-run-rule-stats", tenantId, activeTestRunId],
    queryFn: () => decisionEngineApi.listTestRunRuleStats(tenantId, activeTestRunId!),
    enabled: Boolean(tenantId && activeTestRunId),
  });
  const cancelTestRunMutation = useMutation({
    mutationFn: async () => {
      if (!activeTestRunId) {
        throw new Error("Select a comparison to cancel.");
      }

      return decisionEngineApi.cancelTestRun(tenantId, activeTestRunId);
    },
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

  if (!tenantId) {
    return (
      <Card className="rounded-2xl border border-amber-200 bg-amber-50 shadow-none">
        <CardContent className="p-5 text-sm text-amber-800">
          Set `NEXT_PUBLIC_DATA_MODEL_TENANT_ID` to load this scenario.
        </CardContent>
      </Card>
    );
  }

  if (scenarioQuery.isLoading || iterationsQuery.isLoading || testRunsQuery.isLoading) {
    return (
      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="p-5 text-sm text-slate-600">
          Loading test and compare...
        </CardContent>
      </Card>
    );
  }

  if (
    scenarioQuery.isError ||
    iterationsQuery.isError ||
    testRunsQuery.isError ||
    !scenarioQuery.data?.scenario
  ) {
    const error =
      scenarioQuery.error ?? iterationsQuery.error ?? testRunsQuery.error ?? new Error("Failed to load data.");

    return (
      <Card className="rounded-2xl border border-red-200 bg-red-50 shadow-none">
        <CardContent className="p-5 text-sm text-red-700">
          {error instanceof Error ? error.message : "Failed to load data."}
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="mx-auto w-full max-w-[1280px] space-y-6 px-4 sm:px-6 xl:px-8">
      <div className="flex flex-wrap items-center gap-3 border-b border-slate-200 pb-4">
        <Link
          href={`/detection/${scenarioId}`}
          className="inline-flex size-10 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"
        >
          <ArrowLeft className="size-4" />
        </Link>
        <div>
          <h1 className="text-[1.65rem] font-semibold tracking-tight text-slate-950">
            Test and compare
          </h1>
          <p className="text-[14px] text-slate-600">
            Compare the live version against another iteration before publishing.
          </p>
        </div>
      </div>

      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="space-y-4 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p className="text-[15px] font-semibold text-slate-950">Create comparison</p>
              <p className="text-[13px] text-slate-500">
                Live iteration:{" "}
                <span className="font-medium">
                  {scenarioStatusLabel(
                    iterationById.get(liveIterationId ?? "")?.version,
                    true
                  )}
                </span>
              </p>
            </div>
            {!liveIterationId ? (
              <span className="text-[13px] text-amber-700">
                Publish a live iteration before using test and compare.
              </span>
            ) : null}
          </div>

          <div className="grid gap-3 md:grid-cols-[1fr_auto]">
            <select
              value={activePhantomIterationId}
              onChange={(event) => setPhantomIterationId(event.target.value)}
              className="h-10 rounded-xl border border-slate-200 bg-white px-3 text-[14px] text-slate-950 shadow-none outline-none"
            >
              <option value="">Select iteration to compare</option>
              {phantomIterations.map((iteration) => (
                <option key={iteration.id} value={iteration.id}>
                  {scenarioStatusLabel(iteration.version)}
                </option>
              ))}
            </select>
            <Button
              onClick={() => createTestRunMutation.mutate()}
              disabled={!liveIterationId || phantomIterations.length === 0 || createTestRunMutation.isPending}
              className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
            >
              <Plus className="size-4" />
              {createTestRunMutation.isPending ? "Creating..." : "Create comparison"}
            </Button>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-4 xl:grid-cols-[0.9fr_1.1fr]">
        <Card className="rounded-2xl border border-slate-200 shadow-none">
          <CardContent className="space-y-4 p-4">
            <div className="flex items-center gap-2">
              <GitCompareArrows className="size-4 text-[#1f4f96]" />
              <h2 className="text-[15px] font-semibold text-slate-950">Comparison runs</h2>
            </div>
            {testRuns.length === 0 ? (
              <div className="rounded-xl border border-slate-200 px-4 py-8 text-center text-[14px] text-slate-500">
                No comparison runs yet.
              </div>
            ) : (
              <div className="space-y-2">
                {testRuns.map((testRun) => {
                  const phantomIteration = iterationById.get(testRun.phantom_iteration_id);
                  return (
                    <button
                      key={testRun.id}
                      type="button"
                      onClick={() => {
                        setSelectedTestRunId(testRun.id);
                        setComparisonResult(null);
                      }}
                      className={cn(
                        "w-full rounded-xl border px-3 py-3 text-left",
                        selectedTestRun?.id === testRun.id
                          ? "border-[#2d63b8] bg-[#edf4ff]"
                          : "border-slate-200 bg-white hover:border-slate-300"
                      )}
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div>
                          <p className="text-[14px] font-medium text-slate-950">
                            Live vs {scenarioStatusLabel(phantomIteration?.version)}
                          </p>
                          <p className="text-[12px] text-slate-500">
                            Created {formatDateTime(testRun.created_at)}
                          </p>
                        </div>
                        <Badge className="rounded-full border-slate-200 bg-white px-2 py-0.5 text-[12px] font-medium tracking-normal normal-case text-slate-700">
                          {testRun.status}
                        </Badge>
                      </div>
                    </button>
                  );
                })}
              </div>
            )}
          </CardContent>
        </Card>

        <div className="space-y-4">
          {activeTestRunId ? (
            <Card className="rounded-2xl border border-slate-200 shadow-none">
              <CardContent className="space-y-4 p-4">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <h2 className="text-[15px] font-semibold text-slate-950">Selected run</h2>
                    <p className="text-[13px] text-slate-500">
                      Inspect the active comparison session and manage its lifecycle.
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    onClick={() => cancelTestRunMutation.mutate()}
                    disabled={
                      cancelTestRunMutation.isPending ||
                      selectedTestRunQuery.data?.test_run.status.toLowerCase() !== "up"
                    }
                    className="h-9 rounded-xl border-red-200 px-3.5 text-[13px] text-red-700 shadow-none"
                  >
                    <XCircle className="size-4" />
                    {cancelTestRunMutation.isPending ? "Cancelling..." : "Cancel run"}
                  </Button>
                </div>
                {selectedTestRunQuery.isLoading ? (
                  <div className="rounded-xl border border-slate-200 px-4 py-6 text-[14px] text-slate-600">
                    Loading run details...
                  </div>
                ) : selectedTestRunQuery.data?.test_run ? (
                  <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                    <div className="rounded-xl border border-slate-200 px-3 py-3">
                      <p className="text-[12px] text-slate-500">Status</p>
                      <Badge
                        className={cn(
                          "mt-2 rounded-full border px-2 py-0.5 text-[12px] font-medium tracking-normal normal-case",
                          testRunStatusClasses(selectedTestRunQuery.data.test_run.status)
                        )}
                      >
                        {selectedTestRunQuery.data.test_run.status}
                      </Badge>
                    </div>
                    <div className="rounded-xl border border-slate-200 px-3 py-3">
                      <p className="text-[12px] text-slate-500">Live iteration</p>
                      <p className="mt-1 text-[14px] font-medium text-slate-950">
                        {scenarioStatusLabel(
                          iterationById.get(selectedTestRunQuery.data.test_run.live_iteration_id)?.version,
                          true
                        )}
                      </p>
                    </div>
                    <div className="rounded-xl border border-slate-200 px-3 py-3">
                      <p className="text-[12px] text-slate-500">Phantom iteration</p>
                      <p className="mt-1 text-[14px] font-medium text-slate-950">
                        {scenarioStatusLabel(
                          iterationById.get(selectedTestRunQuery.data.test_run.phantom_iteration_id)?.version
                        )}
                      </p>
                    </div>
                    <div className="rounded-xl border border-slate-200 px-3 py-3">
                      <p className="text-[12px] text-slate-500">Expires at</p>
                      <p className="mt-1 text-[14px] font-medium text-slate-950">
                        {formatDateTime(selectedTestRunQuery.data.test_run.expires_at)}
                      </p>
                    </div>
                  </div>
                ) : null}
              </CardContent>
            </Card>
          ) : null}

          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="space-y-4 p-4">
              <div>
                <h2 className="text-[15px] font-semibold text-slate-950">Evaluate record</h2>
                <p className="text-[13px] text-slate-500">
                  Send one payload through the selected test run and compare live vs phantom outcomes.
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
                  disabled={!activeTestRunId || evaluateTestRunMutation.isPending}
                  className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
                >
                  <Play className="size-4" />
                  {evaluateTestRunMutation.isPending ? "Evaluating..." : "Compare"}
                </Button>
              </div>
            </CardContent>
          </Card>

          {activeTestRunId ? (
            <div className="grid gap-4 xl:grid-cols-2">
              <Card className="rounded-2xl border border-slate-200 shadow-none">
                <CardContent className="space-y-3 p-4">
                  <h3 className="text-[15px] font-semibold text-slate-950">Decision summaries</h3>
                  {testRunDecisionSummariesQuery.isLoading ? (
                    <p className="text-[14px] text-slate-600">Loading summaries...</p>
                  ) : testRunDecisionSummariesQuery.data?.decisions?.length ? (
                    <div className="space-y-2">
                      {testRunDecisionSummariesQuery.data.decisions.map(
                        (summary: TestRunDecisionSummary, index: number) => (
                          <div
                            key={`${summary.outcome}-${summary.score}-${index}`}
                            className="flex items-center justify-between rounded-xl border border-slate-200 px-3 py-3"
                          >
                            <div>
                              <p className="text-[13px] font-medium text-slate-950">
                                {summary.outcome}
                              </p>
                              <p className="text-[12px] text-slate-500">Score {summary.score}</p>
                            </div>
                            <span className="text-[14px] font-semibold text-slate-950">
                              {summary.count}
                            </span>
                          </div>
                        )
                      )}
                    </div>
                  ) : (
                    <div className="rounded-xl border border-slate-200 px-3 py-3 text-[13px] text-slate-500">
                      No decision summaries yet for this run.
                    </div>
                  )}
                </CardContent>
              </Card>

              <Card className="rounded-2xl border border-slate-200 shadow-none">
                <CardContent className="space-y-3 p-4">
                  <h3 className="text-[15px] font-semibold text-slate-950">Rule stats</h3>
                  {testRunRuleStatsQuery.isLoading ? (
                    <p className="text-[14px] text-slate-600">Loading rule stats...</p>
                  ) : testRunRuleStatsQuery.data?.rules?.length ? (
                    <div className="space-y-2">
                      {testRunRuleStatsQuery.data.rules.map((rule: TestRunRuleStat) => (
                        <div
                          key={rule.rule_id}
                          className="rounded-xl border border-slate-200 px-3 py-3"
                        >
                          <p className="text-[13px] font-medium text-slate-950">
                            {rule.rule_name}
                          </p>
                          <div className="mt-2 grid grid-cols-4 gap-2 text-[12px] text-slate-600">
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
          ) : null}

          {comparisonResult ? (
            <div className="grid gap-4 xl:grid-cols-2">
              <ResultPanel title="Live result" result={comparisonResult.live} />
              <ResultPanel title="Phantom result" result={comparisonResult.phantom} />
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
