"use client";

import Link from "next/link";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, CalendarRange, Info, Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  decisionEngineApi,
  type EvaluateDecisionRequest,
  type ScheduledExecution,
} from "@/lib/decision-engine-api";
import { cn } from "@/lib/utils";

type ExecutionMode = "ingested_candidates" | "explicit_items";

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

function deriveRequestItems(execution: ScheduledExecution) {
  const body = execution.request_body;

  if (Array.isArray(body)) {
    return body;
  }

  if (body && typeof body === "object" && "items" in body) {
    return Array.isArray(body.items) ? body.items : [];
  }

  return [];
}

function deriveCandidateLimit(execution: ScheduledExecution) {
  const body = execution.request_body;

  if (body && typeof body === "object" && "candidate_limit" in body) {
    const candidateLimit = body.candidate_limit;
    return typeof candidateLimit === "number" ? candidateLimit : null;
  }

  return null;
}

function deriveExecutionSource(execution: ScheduledExecution) {
  return deriveRequestItems(execution).length > 0 ? "Explicit items" : "Ingested candidates";
}

function parseExplicitItems(value: string) {
  let parsed: unknown;

  try {
    parsed = JSON.parse(value);
  } catch {
    throw new Error("Explicit items must be valid JSON.");
  }

  if (!Array.isArray(parsed)) {
    throw new Error("Explicit items must be a JSON array.");
  }

  return parsed.map((item, index) => {
    if (!item || typeof item !== "object") {
      throw new Error(`Item ${index + 1} must be an object.`);
    }

    const candidate = item as Record<string, unknown>;
    if (typeof candidate.object_id !== "string" || candidate.object_id.trim().length === 0) {
      throw new Error(`Item ${index + 1} is missing a valid object_id.`);
    }

    if (typeof candidate.object_type !== "string" || candidate.object_type.trim().length === 0) {
      throw new Error(`Item ${index + 1} is missing a valid object_type.`);
    }

    const fields =
      candidate.fields && typeof candidate.fields === "object" && !Array.isArray(candidate.fields)
        ? candidate.fields
        : {};

    return {
      object_id: candidate.object_id,
      object_type: candidate.object_type,
      fields,
    } satisfies EvaluateDecisionRequest;
  });
}

export function ScenarioExecutionPage({ scenarioId }: { scenarioId: string }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const queryClient = useQueryClient();
  const [scheduledFor, setScheduledFor] = useState("");
  const [mode, setMode] = useState<ExecutionMode>("ingested_candidates");
  const [candidateLimit, setCandidateLimit] = useState("100");
  const [explicitItemsDraft, setExplicitItemsDraft] = useState(
    JSON.stringify(
      [
        {
          object_id: "txn_001",
          object_type: "transaction",
          fields: {},
        },
      ],
      null,
      2
    )
  );

  const scenarioQuery = useQuery({
    queryKey: ["decision-engine", "scenario", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.getScenario(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });

  const executionsQuery = useQuery({
    queryKey: ["decision-engine", "scheduled-executions", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.listScheduledExecutions(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });

  const createExecutionMutation = useMutation({
    mutationFn: async () => {
      if (!scenarioQuery.data?.scenario?.live_iteration_id) {
        throw new Error("Publish a live scenario version before scheduling executions.");
      }

      if (!scheduledFor) {
        throw new Error("Choose when the execution should run.");
      }

      const payload =
        mode === "explicit_items"
          ? {
              scheduled_for: new Date(scheduledFor).toISOString(),
              items: parseExplicitItems(explicitItemsDraft),
              candidate_limit: 0,
            }
          : {
              scheduled_for: new Date(scheduledFor).toISOString(),
              items: [],
              candidate_limit: Math.max(1, Number(candidateLimit) || 1),
            };

      return decisionEngineApi.createScheduledExecution(tenantId, scenarioId, payload);
    },
    onSuccess: async () => {
      setScheduledFor("");
      setCandidateLimit("100");
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "scheduled-executions", tenantId, scenarioId],
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

  if (scenarioQuery.isLoading || executionsQuery.isLoading) {
    return (
      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="p-5 text-sm text-slate-600">
          Loading scheduled executions...
        </CardContent>
      </Card>
    );
  }

  if (scenarioQuery.isError || !scenarioQuery.data?.scenario) {
    return (
      <Card className="rounded-2xl border border-red-200 bg-red-50 shadow-none">
        <CardContent className="p-5 text-sm text-red-700">
          {scenarioQuery.error instanceof Error
            ? scenarioQuery.error.message
            : "Failed to load scenario."}
        </CardContent>
      </Card>
    );
  }

  if (executionsQuery.isError) {
    return (
      <Card className="rounded-2xl border border-red-200 bg-red-50 shadow-none">
        <CardContent className="p-5 text-sm text-red-700">
          {executionsQuery.error instanceof Error
            ? executionsQuery.error.message
            : "Failed to load scheduled executions."}
        </CardContent>
      </Card>
    );
  }

  const scenario = scenarioQuery.data.scenario;
  const executions = executionsQuery.data?.scheduled_executions ?? [];
  const canSchedule = Boolean(scenario.live_iteration_id);

  return (
    <div className="mx-auto w-full max-w-[1200px] space-y-5 px-4 sm:px-6 xl:px-8">
      <div className="flex flex-wrap items-center gap-3 border-b border-slate-200 pb-4">
        <Link
          href={`/detection/${scenarioId}`}
          className="inline-flex size-10 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"
        >
          <ArrowLeft className="size-4" />
        </Link>
        <div className="flex flex-wrap items-center gap-2 text-[15px] text-slate-500">
          <Link href="/detection" className="font-medium text-[#1f4f96]">
            Detection
          </Link>
          <span>/</span>
          <span className="font-medium text-slate-700">{scenario.name}</span>
          <span>/</span>
          <span className="font-semibold text-slate-950">Execution</span>
        </div>
      </div>

      <div className="space-y-1.5">
        <h1 className="text-[1.65rem] font-semibold tracking-tight text-slate-950">
          Scheduled executions ({executions.length})
        </h1>
        <p className="flex items-center gap-2 text-[14px] text-slate-600">
          <Info className="size-4 text-slate-400" />
          Run this live scenario automatically against ingested candidates or manually against explicit items.
        </p>
      </div>

      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="space-y-4 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p className="text-[15px] font-semibold text-slate-950">Create scheduled execution</p>
              <p className="text-[13px] text-slate-500">
                Queue a one-off future run for the live iteration.
              </p>
            </div>
            {!canSchedule ? (
              <span className="text-[13px] text-amber-700">
                This scenario needs a live version before it can be scheduled.
              </span>
            ) : null}
          </div>

          <div className="grid gap-3 md:grid-cols-[1.4fr_auto]">
            <Input
              type="datetime-local"
              value={scheduledFor}
              onChange={(event) => setScheduledFor(event.target.value)}
              className="h-10 rounded-xl border-slate-200 text-[14px] shadow-none"
            />
            <Button
              onClick={() => createExecutionMutation.mutate()}
              disabled={createExecutionMutation.isPending || !canSchedule}
              className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
            >
              <Plus className="size-4" />
              {createExecutionMutation.isPending ? "Scheduling..." : "Schedule"}
            </Button>
          </div>

          <div className="grid gap-3 lg:grid-cols-2">
            <button
              type="button"
              onClick={() => setMode("ingested_candidates")}
              className={cn(
                "rounded-2xl border px-4 py-4 text-left transition",
                mode === "ingested_candidates"
                  ? "border-[#2d63b8] bg-[#edf4ff]"
                  : "border-slate-200 bg-white hover:border-slate-300"
              )}
            >
              <p className="text-[14px] font-semibold text-slate-950">Use ingested candidates</p>
              <p className="mt-1 text-[13px] text-slate-600">
                Schedule a future run that scans ingested records using the live scenario setup.
              </p>
            </button>
            <button
              type="button"
              onClick={() => setMode("explicit_items")}
              className={cn(
                "rounded-2xl border px-4 py-4 text-left transition",
                mode === "explicit_items"
                  ? "border-[#2d63b8] bg-[#edf4ff]"
                  : "border-slate-200 bg-white hover:border-slate-300"
              )}
            >
              <p className="text-[14px] font-semibold text-slate-950">Provide explicit items</p>
              <p className="mt-1 text-[13px] text-slate-600">
                Schedule a future run with an explicit JSON list of objects to evaluate.
              </p>
            </button>
          </div>

          {mode === "ingested_candidates" ? (
            <div className="space-y-2 rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
              <label className="block text-[13px] font-medium text-slate-700">Candidate limit</label>
              <Input
                type="number"
                min="1"
                value={candidateLimit}
                onChange={(event) => setCandidateLimit(event.target.value)}
                className="h-10 rounded-xl border-slate-200 bg-white text-[14px] shadow-none"
              />
              <p className="text-[12px] text-slate-500">
                The worker will pull up to this many ingested candidates when the scheduled time arrives.
              </p>
            </div>
          ) : (
            <div className="space-y-2 rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
              <label className="block text-[13px] font-medium text-slate-700">
                Explicit items JSON
              </label>
              <textarea
                value={explicitItemsDraft}
                onChange={(event) => setExplicitItemsDraft(event.target.value)}
                className="min-h-48 w-full rounded-xl border border-slate-200 bg-white px-3 py-2 font-mono text-[13px] text-slate-900 shadow-none outline-none focus:border-[#2d63b8]"
              />
              <p className="text-[12px] text-slate-500">
                Each item must include `object_id`, `object_type`, and optional `fields`.
              </p>
            </div>
          )}

          {createExecutionMutation.error instanceof Error ? (
            <div className="rounded-xl border border-red-200 bg-red-50 px-3.5 py-3 text-[13px] text-red-700">
              {createExecutionMutation.error.message}
            </div>
          ) : null}
        </CardContent>
      </Card>

      <Card className="overflow-hidden rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="p-0">
          <div className="overflow-x-auto">
            <table className="min-w-full text-left">
              <thead>
                <tr className="border-b border-slate-200 bg-white text-[13px] font-semibold text-slate-950">
                  <th className="px-4 py-3.5">Scheduled for</th>
                  <th className="px-4 py-3.5">Source</th>
                  <th className="px-4 py-3.5">Candidate limit</th>
                  <th className="px-4 py-3.5">Explicit items</th>
                  <th className="px-4 py-3.5">Status</th>
                  <th className="px-4 py-3.5">Created at</th>
                </tr>
              </thead>
              <tbody>
                {executions.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="px-4 py-10 text-center text-[14px] text-slate-500">
                      <div className="flex flex-col items-center gap-3">
                        <CalendarRange className="size-9 text-slate-300" />
                        <span>No scheduled executions yet.</span>
                      </div>
                    </td>
                  </tr>
                ) : (
                  executions.map((execution) => {
                    const items = deriveRequestItems(execution);
                    const executionCandidateLimit = deriveCandidateLimit(execution);

                    return (
                      <tr
                        key={execution.id}
                        className="border-b border-slate-100 text-[14px] text-slate-900 last:border-b-0"
                      >
                        <td className="px-4 py-3.5">{formatDateTime(execution.scheduled_for)}</td>
                        <td className="px-4 py-3.5">{deriveExecutionSource(execution)}</td>
                        <td className="px-4 py-3.5">{executionCandidateLimit ?? "-"}</td>
                        <td className="px-4 py-3.5">{items.length > 0 ? items.length : "-"}</td>
                        <td className="px-4 py-3.5 capitalize">{execution.status}</td>
                        <td className="px-4 py-3.5">{formatDateTime(execution.created_at)}</td>
                      </tr>
                    );
                  })
                )}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
