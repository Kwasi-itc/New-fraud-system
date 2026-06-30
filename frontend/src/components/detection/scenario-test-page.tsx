"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, GitCompareArrows, Plus } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { decisionEngineApi } from "@/lib/decision-engine-api";
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

export function ScenarioTestPage({ scenarioId }: { scenarioId: string }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const router = useRouter();
  const queryClient = useQueryClient();
  const pushToast = useToastStore((state) => state.pushToast);
  const [phantomIterationId, setPhantomIterationId] = useState("");

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

  const scenario = scenarioQuery.data?.scenario;
  const iterations = iterationsQuery.data?.iterations ?? [];
  const liveIterationId = scenario?.live_iteration_id ?? null;
  const iterationById = new Map(iterations.map((iteration) => [iteration.id, iteration]));
  const phantomIterations = iterations
    .filter((iteration) => iteration.id !== liveIterationId)
    .sort((left, right) => right.version - left.version);
  const activePhantomIteration =
    phantomIterations.find((iteration) => iteration.id === phantomIterationId) ??
    phantomIterations[0] ??
    null;
  const activePhantomIterationId = phantomIterationId || activePhantomIteration?.id || "";
  const testRuns = (testRunsQuery.data?.test_runs ?? []).sort(
    (left, right) =>
      new Date(right.created_at).getTime() - new Date(left.created_at).getTime()
  );

  const createTestRunMutation = useMutation({
    mutationFn: async () => {
      if (!scenario?.live_iteration_id) {
        throw new Error("Publish a live iteration before creating a comparison.");
      }
      if (!activePhantomIterationId) {
        throw new Error("Create another iteration before creating a comparison.");
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
      pushToast({
        title: "Comparison created",
        description: "The test run is ready for inspection.",
        variant: "success",
      });
      router.push(`/detection/${scenarioId}/tests/${test_run.id}`);
    },
    onError: (error) => {
      pushToast({
        title: "Failed to create comparison",
        description: error instanceof Error ? error.message : "The comparison could not be created.",
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

  if (scenarioQuery.isError || iterationsQuery.isError || testRunsQuery.isError || !scenario) {
    const error =
      scenarioQuery.error ??
      iterationsQuery.error ??
      testRunsQuery.error ??
      new Error("Failed to load data.");

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
            Create comparison runs, then inspect each run on its own details page.
          </p>
        </div>
      </div>

      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="space-y-4 p-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p className="text-[16px] font-semibold text-slate-950">Create comparison</p>
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

          <div className="grid gap-3 rounded-2xl border border-slate-200 bg-slate-50/70 p-4 md:grid-cols-[1fr_auto] md:items-center">
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
              disabled={!liveIterationId || !activePhantomIterationId || createTestRunMutation.isPending}
              className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
            >
              <Plus className="size-4" />
              {createTestRunMutation.isPending ? "Creating..." : "Create comparison"}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="space-y-4 p-5">
          <div className="flex items-center gap-2">
            <GitCompareArrows className="size-4 text-[#1f4f96]" />
            <h2 className="text-[16px] font-semibold text-slate-950">Comparison runs</h2>
          </div>

          {testRuns.length === 0 ? (
            <div className="rounded-xl border border-slate-200 px-4 py-8 text-center text-[14px] text-slate-500">
              No comparison runs yet.
            </div>
          ) : (
            <div className="space-y-3">
              {testRuns.map((testRun) => {
                const phantomIteration = iterationById.get(testRun.phantom_iteration_id);
                return (
                  <Link
                    key={testRun.id}
                    href={`/detection/${scenarioId}/tests/${testRun.id}`}
                    className="block rounded-2xl border border-slate-200 bg-white p-4 transition hover:border-slate-300 hover:bg-slate-50"
                  >
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div>
                        <p className="text-[15px] font-semibold text-[#1f4f96]">
                          Live vs {scenarioStatusLabel(phantomIteration?.version)}
                        </p>
                        <p className="mt-1 text-[13px] text-slate-500">
                          Created {formatDateTime(testRun.created_at)}
                        </p>
                      </div>
                      <Badge
                        className={cn(
                          "rounded-full border px-2 py-0.5 text-[12px] font-medium tracking-normal normal-case",
                          testRunStatusClasses(testRun.status)
                        )}
                      >
                        {testRun.status}
                      </Badge>
                    </div>
                    <div className="mt-4 grid gap-3 sm:grid-cols-3">
                      <div className="rounded-xl border border-slate-200 bg-slate-50/70 px-3 py-3">
                        <div className="text-[12px] text-slate-500">Live iteration</div>
                        <div className="mt-1 text-[14px] font-medium text-slate-950">
                          {scenarioStatusLabel(
                            iterationById.get(testRun.live_iteration_id)?.version,
                            true
                          )}
                        </div>
                      </div>
                      <div className="rounded-xl border border-slate-200 bg-slate-50/70 px-3 py-3">
                        <div className="text-[12px] text-slate-500">Phantom iteration</div>
                        <div className="mt-1 text-[14px] font-medium text-slate-950">
                          {scenarioStatusLabel(phantomIteration?.version)}
                        </div>
                      </div>
                      <div className="rounded-xl border border-slate-200 bg-slate-50/70 px-3 py-3">
                        <div className="text-[12px] text-slate-500">Expires at</div>
                        <div className="mt-1 text-[14px] font-medium text-slate-950">
                          {formatDateTime(testRun.expires_at)}
                        </div>
                      </div>
                    </div>
                  </Link>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
