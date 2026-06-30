"use client";

import Link from "next/link";
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, Copy } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";
import { decisionEngineApi } from "@/lib/decision-engine-api";
import {
  deriveScheduledExecutionCandidateLimit,
  deriveScheduledExecutionItems,
  deriveScheduledExecutionSource,
  formatExecutionDateTime,
  formatExecutionRequestBody,
} from "@/components/detection/scheduled-execution-shared";
import { cn } from "@/lib/utils";

function formatRequestField(value: unknown) {
  if (value === null || value === undefined) {
    return "-";
  }
  if (typeof value === "string") {
    return value;
  }
  return JSON.stringify(value);
}

export function ScheduledExecutionDetailPage({
  scenarioId,
  executionId,
}: {
  scenarioId: string;
  executionId: string;
}) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";

  const scenarioQuery = useQuery({
    queryKey: ["decision-engine", "scenario", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.getScenario(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });
  const executionQuery = useQuery({
    queryKey: ["decision-engine", "scheduled-execution", tenantId, scenarioId, executionId],
    queryFn: () => decisionEngineApi.getScheduledExecution(tenantId, scenarioId, executionId),
    enabled: Boolean(tenantId && scenarioId && executionId),
  });
  const scenarioDecisionsQuery = useQuery({
    queryKey: ["decision-engine", "scenario-decisions", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.listScenarioDecisions(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });

  const scenario = scenarioQuery.data?.scenario;
  const execution = executionQuery.data?.scheduled_execution ?? null;
  const items = execution ? deriveScheduledExecutionItems(execution) : [];
  const candidateLimit = execution ? deriveScheduledExecutionCandidateLimit(execution) : null;
  const requestBody = execution?.request_body;
  const scenarioDecisions = scenarioDecisionsQuery.data?.decisions ?? [];
  const requestEntries =
    requestBody && typeof requestBody === "object" && !Array.isArray(requestBody)
      ? Object.entries(requestBody)
      : [];
  const executionIterationId = execution?.scenario_iteration_id ?? null;
  const executionErrorMessage =
    scenarioQuery.error instanceof Error
      ? scenarioQuery.error.message
      : executionQuery.error instanceof Error
        ? executionQuery.error.message
        : scenarioDecisionsQuery.error instanceof Error
          ? scenarioDecisionsQuery.error.message
          : "Failed to load scheduled execution details.";

  const explicitItemObjectIds = useMemo(
    () =>
      new Set(
        items
          .map((item) => {
            const candidate = item as Record<string, unknown>;
            return typeof candidate.object_id === "string" ? candidate.object_id : null;
          })
          .filter((value): value is string => Boolean(value))
      ),
    [items]
  );
  const relatedDecisions = useMemo(() => {
    if (explicitItemObjectIds.size === 0 || !executionIterationId) {
      return [];
    }

    return scenarioDecisions
      .filter(
        (decision) =>
          decision.scenario_iteration_id === executionIterationId &&
          explicitItemObjectIds.has(decision.object_id)
      )
      .sort(
        (left, right) =>
          new Date(right.created_at).getTime() - new Date(left.created_at).getTime()
      );
  }, [executionIterationId, explicitItemObjectIds, scenarioDecisions]);

  if (!tenantId) {
    return (
      <Card className="rounded-2xl border border-amber-200 bg-amber-50 shadow-none">
        <CardContent className="p-5 text-sm text-amber-800">
          Set `NEXT_PUBLIC_DATA_MODEL_TENANT_ID` to load this execution.
        </CardContent>
      </Card>
    );
  }

  if (scenarioQuery.isLoading || executionQuery.isLoading || scenarioDecisionsQuery.isLoading) {
    return (
      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="p-5 text-sm text-slate-600">
          Loading execution details...
        </CardContent>
      </Card>
    );
  }

  if (
    scenarioQuery.isError ||
    executionQuery.isError ||
    scenarioDecisionsQuery.isError ||
    !execution
  ) {
    return (
      <Card className="rounded-2xl border border-red-200 bg-red-50 shadow-none">
        <CardContent className="p-5 text-sm text-red-700">{executionErrorMessage}</CardContent>
      </Card>
    );
  }

  return (
    <div className="mx-auto w-full max-w-[1380px] space-y-6 px-4 sm:px-6 xl:px-8">
      <div className="flex items-center justify-between gap-4 border-b border-slate-200 pb-4">
        <div className="flex flex-wrap items-center gap-3">
          <Link
            href={`/detection/${scenarioId}/execution`}
            className="inline-flex size-10 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"
          >
            <ChevronLeft className="size-4" />
          </Link>
          <div className="flex flex-wrap items-center gap-3 text-[15px] text-slate-500">
            <Link href="/detection" className="font-medium text-[#1f4f96]">
              Detection
            </Link>
            <span>/</span>
            <span>{scenario?.name ?? "Scenario"}</span>
            <span>/</span>
            <Link href={`/detection/${scenarioId}/execution`} className="font-medium text-[#1f4f96]">
              Execution
            </Link>
            <span>/</span>
            <span className="font-semibold text-slate-950">Scheduled execution</span>
            <div className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2 text-[14px] text-slate-950">
              <span>ID {execution.id}</span>
              <Copy className="size-4 text-slate-500" />
            </div>
          </div>
        </div>
      </div>

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.1fr)_420px]">
        <div className="space-y-5">
          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="space-y-5 p-6">
              <div>
                <h2 className="text-[18px] font-semibold text-slate-950">Scheduled execution details</h2>
                <p className="mt-1 text-[14px] text-slate-500">
                  Inspect the exact payload and metadata for this scheduled run.
                </p>
              </div>

              <div className="grid gap-4 md:grid-cols-2">
                <div className="rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
                  <div className="text-[12px] text-slate-500">Source</div>
                  <div className="mt-1 text-[15px] font-semibold text-slate-950">
                    {deriveScheduledExecutionSource(execution)}
                  </div>
                </div>
                <div className="rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
                  <div className="text-[12px] text-slate-500">Status</div>
                  <div className="mt-1 text-[15px] font-semibold capitalize text-slate-950">
                    {execution.status}
                  </div>
                </div>
                <div className="rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
                  <div className="text-[12px] text-slate-500">Scheduled for</div>
                  <div className="mt-1 text-[15px] font-semibold text-slate-950">
                    {formatExecutionDateTime(execution.scheduled_for)}
                  </div>
                </div>
                <div className="rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
                  <div className="text-[12px] text-slate-500">Created at</div>
                  <div className="mt-1 text-[15px] font-semibold text-slate-950">
                    {formatExecutionDateTime(execution.created_at)}
                  </div>
                </div>
              </div>

              <div className="rounded-2xl border border-slate-200 bg-white p-4">
                <div className="mb-3 text-[13px] font-medium text-slate-900">Request body</div>
                {requestEntries.length > 0 ? (
                  <div className="space-y-3">
                    <div className="grid gap-3 md:grid-cols-2">
                      {requestEntries.map(([key, value]) => (
                        <div
                          key={key}
                          className={cn(
                            "rounded-xl border border-slate-200 bg-slate-50/70 px-3 py-3",
                            key === "items" ? "md:col-span-2" : ""
                          )}
                        >
                          <div className="text-[11px] uppercase tracking-[0.08em] text-slate-500">
                            {key.replace(/_/g, " ")}
                          </div>
                          <div className="mt-1 whitespace-pre-wrap break-words text-[13px] text-slate-900">
                            {key === "items"
                              ? `${items.length} item${items.length === 1 ? "" : "s"}`
                              : formatRequestField(value)}
                          </div>
                        </div>
                      ))}
                    </div>
                    <details className="rounded-xl border border-slate-200 bg-slate-50">
                      <summary className="cursor-pointer px-3 py-2 text-[13px] font-medium text-slate-800">
                        View raw JSON
                      </summary>
                      <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-words border-t border-slate-200 px-3 py-3 font-mono text-[12px] text-slate-800">
                        {formatExecutionRequestBody(execution.request_body)}
                      </pre>
                    </details>
                  </div>
                ) : (
                  <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-words rounded-xl bg-slate-50 px-3 py-3 font-mono text-[12px] text-slate-800">
                    {formatExecutionRequestBody(execution.request_body)}
                  </pre>
                )}
              </div>

              {items.length > 0 ? (
                <div className="rounded-2xl border border-slate-200 bg-white p-4">
                  <div className="mb-2 text-[13px] font-medium text-slate-900">Explicit items</div>
                  <div className="space-y-3">
                    {items.map((item, index) => {
                      const candidate = item as Record<string, unknown>;
                      return (
                        <div
                          key={`${candidate.object_id ?? "item"}-${index}`}
                          className="rounded-xl border border-slate-200 bg-slate-50/70 p-3"
                        >
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <div className="text-[13px] font-medium text-slate-950">
                              {String(candidate.object_id ?? `Item ${index + 1}`)}
                            </div>
                            <div className="text-[12px] text-slate-500">
                              {String(candidate.object_type ?? "-")}
                            </div>
                          </div>
                          <pre className="mt-3 max-h-[220px] overflow-auto whitespace-pre-wrap break-words rounded-lg bg-white px-3 py-3 font-mono text-[12px] text-slate-800">
                            {formatExecutionRequestBody((candidate.fields ?? {}) as never)}
                          </pre>
                        </div>
                      );
                    })}
                  </div>
                </div>
              ) : null}
            </CardContent>
          </Card>
        </div>

        <div className="space-y-5">
          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="space-y-4 p-5">
              <div className="text-[16px] font-semibold text-slate-950">Execution metadata</div>
              <div className="space-y-3 text-[14px] text-slate-700">
                <div>
                  <div className="text-[12px] text-slate-500">Scenario</div>
                  <div className="mt-1 font-medium text-slate-950">{scenario?.name ?? "-"}</div>
                </div>
                <div>
                  <div className="text-[12px] text-slate-500">Scenario iteration</div>
                  <div className="mt-1 break-all font-medium text-slate-950">
                    {execution.scenario_iteration_id}
                  </div>
                </div>
                <div>
                  <div className="text-[12px] text-slate-500">Candidate limit</div>
                  <div className="mt-1 font-medium text-slate-950">{candidateLimit ?? "-"}</div>
                </div>
                <div>
                  <div className="text-[12px] text-slate-500">Explicit item count</div>
                  <div className="mt-1 font-medium text-slate-950">{items.length}</div>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card className="rounded-2xl border border-slate-200 shadow-none">
            <CardContent className="space-y-3 p-5">
              <div className="text-[16px] font-semibold text-slate-950">Decisions</div>
              {items.length === 0 ? (
                <div className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-[13px] text-slate-700">
                  This execution ran against ingested candidates, and the backend does not yet link scheduled executions to created decisions directly.
                </div>
              ) : relatedDecisions.length === 0 ? (
                <div className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-[13px] text-slate-700">
                  No matching decisions were found yet for the explicit item ids in this iteration.
                </div>
              ) : (
                <div className="space-y-3">
                  <div className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-[13px] text-slate-700">
                    Showing decisions matched by explicit item `object_id` within this scenario iteration.
                  </div>
                  {relatedDecisions.map((decision) => (
                    <Link
                      key={decision.id}
                      href={`/detection/decisions/${decision.id}`}
                      className="block rounded-xl border border-slate-200 bg-white px-4 py-4 transition hover:border-slate-300 hover:bg-slate-50"
                    >
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        <div>
                          <div className="text-[14px] font-semibold text-[#1f4f96]">
                            {decision.object_id}
                          </div>
                          <div className="mt-1 text-[12px] text-slate-500">
                            {decision.object_type} · {formatExecutionDateTime(decision.created_at)}
                          </div>
                        </div>
                        <div className="text-right">
                          <div className="text-[12px] uppercase tracking-[0.08em] text-slate-500">
                            Outcome
                          </div>
                          <div className="mt-1 text-[14px] font-semibold capitalize text-slate-950">
                            {decision.outcome.replace(/_/g, " ")}
                          </div>
                        </div>
                      </div>
                      <div className="mt-3 text-[13px] text-slate-700">
                        Score {decision.score} · Triggered {decision.triggered ? "yes" : "no"}
                      </div>
                    </Link>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
