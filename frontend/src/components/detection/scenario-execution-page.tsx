"use client";

import Link from "next/link";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, CalendarRange, Info, Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { decisionEngineApi, type ScheduledExecution } from "@/lib/decision-engine-api";

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

function deriveRequestCount(execution: ScheduledExecution) {
  const body = execution.request_body;

  if (Array.isArray(body)) {
    return body.length;
  }

  if (body && typeof body === "object" && "items" in body) {
    const items = body.items;
    return Array.isArray(items) ? items.length : null;
  }

  return null;
}

export function ScenarioExecutionPage({ scenarioId }: { scenarioId: string }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const queryClient = useQueryClient();
  const [scheduledFor, setScheduledFor] = useState("");
  const [itemCount, setItemCount] = useState("10");

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
      const scenario = scenarioQuery.data?.scenario;
      if (!scenario?.live_iteration_id) {
        throw new Error("Activate a live scenario version before scheduling batch executions.");
      }

      if (!scheduledFor) {
        throw new Error("Choose when the execution should run.");
      }

      const count = Math.max(1, Number(itemCount) || 1);

      return decisionEngineApi.createScheduledExecution(tenantId, scenarioId, {
        scenario_iteration_id: scenario.live_iteration_id,
        scheduled_for: new Date(scheduledFor).toISOString(),
        request_body: {
          items: Array.from({ length: count }, (_, index) => ({
            object_id: `scheduled-item-${index + 1}`,
          })),
        },
      });
    },
    onSuccess: async () => {
      setScheduledFor("");
      setItemCount("10");
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
          Schedule executions ({executions.length})
        </h1>
        <p className="flex items-center gap-2 text-[14px] text-slate-600">
          <Info className="size-4 text-slate-400" />
          Scheduled batch runs for the live version of this scenario.
        </p>
      </div>

      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="space-y-4 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p className="text-[15px] font-semibold text-slate-950">Create scheduled execution</p>
              <p className="text-[13px] text-slate-500">
                Queue a batch run for the live scenario iteration.
              </p>
            </div>
            {!canSchedule ? (
              <span className="text-[13px] text-amber-700">
                This scenario needs a live version before it can be scheduled.
              </span>
            ) : null}
          </div>

          <div className="grid gap-3 md:grid-cols-[1.4fr_0.8fr_auto]">
            <Input
              type="datetime-local"
              value={scheduledFor}
              onChange={(event) => setScheduledFor(event.target.value)}
              className="h-10 rounded-xl border-slate-200 text-[14px] shadow-none"
            />
            <Input
              type="number"
              min="1"
              value={itemCount}
              onChange={(event) => setItemCount(event.target.value)}
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
                  <th className="px-4 py-3.5">Decisions created</th>
                  <th className="px-4 py-3.5">Decisions evaluated</th>
                  <th className="px-4 py-3.5">Decisions to do</th>
                  <th className="px-4 py-3.5">Status</th>
                  <th className="px-4 py-3.5">Created at</th>
                </tr>
              </thead>
              <tbody>
                {executions.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="px-4 py-10 text-center text-[14px] text-slate-500">
                      <div className="flex flex-col items-center gap-3">
                        <CalendarRange className="size-9 text-slate-300" />
                        <span>No scheduled executions yet.</span>
                      </div>
                    </td>
                  </tr>
                ) : (
                  executions.map((execution) => {
                    const requestCount = deriveRequestCount(execution);
                    return (
                      <tr
                        key={execution.id}
                        className="border-b border-slate-100 text-[14px] text-slate-900 last:border-b-0"
                      >
                        <td className="px-4 py-3.5">{requestCount ?? "—"}</td>
                        <td className="px-4 py-3.5">—</td>
                        <td className="px-4 py-3.5">—</td>
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
