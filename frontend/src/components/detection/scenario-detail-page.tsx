"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  ChevronRight,
  Eye,
  Info,
  Pencil,
  Plus,
  SquarePen,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { decisionEngineApi } from "@/lib/decision-engine-api";

function ResourceCard({
  title,
  accent,
}: {
  title: string;
  accent: string;
}) {
  return (
    <Card className="overflow-hidden rounded-xl border border-slate-200 shadow-none">
      <div className={`h-32 border-b border-slate-200 ${accent}`}>
        <div className="flex h-full items-center justify-center">
          <div className="grid w-[180px] gap-2.5 rounded-xl bg-white/80 p-4 shadow-[0_10px_24px_rgba(37,99,235,0.08)] backdrop-blur">
            <div className="h-2.5 w-16 rounded-full bg-slate-200" />
            <div className="space-y-1.5">
              <div className="h-2 rounded-full bg-slate-100" />
              <div className="h-2 w-5/6 rounded-full bg-slate-100" />
              <div className="h-2 w-2/3 rounded-full bg-slate-100" />
            </div>
          </div>
        </div>
      </div>
      <CardContent className="flex items-center justify-between px-3.5 py-2.5">
        <span className="text-[14px] font-medium text-slate-950">{title}</span>
        <ChevronRight className="size-4 text-slate-600" />
      </CardContent>
    </Card>
  );
}

export function ScenarioDetailPage({ scenarioId }: { scenarioId: string }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";

  const scenarioQuery = useQuery({
    queryKey: ["decision-engine", "scenario", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.getScenario(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
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

  if (scenarioQuery.isLoading) {
    return (
      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="p-5 text-sm text-slate-600">
          Loading scenario...
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

  const scenario = scenarioQuery.data.scenario;
  const description = scenario.description || "No description provided";
  const hasLiveIteration = Boolean(scenario.live_iteration_id);
  return (
    <div className="mx-auto w-full max-w-[1280px] space-y-6 px-4 sm:px-6 xl:px-8">
      <div className="border-b border-slate-200 pb-3">
        <div className="flex flex-wrap items-center gap-2.5">
          <Link
            href="/detection"
            className="inline-flex size-9 items-center justify-center rounded-xl border border-slate-200 bg-white"
          >
            <ArrowLeft className="size-4" />
          </Link>
          <h1 className="text-[1.5rem] font-semibold tracking-tight text-slate-950">
            {scenario.name}
          </h1>
          <button
            type="button"
            className="inline-flex size-7 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-700"
          >
            <Pencil className="size-3.5" />
          </button>
          <Badge className="rounded-full border-slate-300 bg-white px-2.5 py-0.5 text-[12px] font-medium tracking-normal normal-case text-slate-500">
            {scenario.trigger_object_type}
            <Info className="ml-1 inline size-3.5" />
          </Badge>
        </div>
      </div>

      <div className="grid gap-3 xl:grid-cols-[1fr_auto] xl:items-start">
        <div className="flex items-start justify-between rounded-xl border border-slate-200 bg-white px-4 py-4">
          <p className="text-[14px] text-slate-700">{description}</p>
          <button
            type="button"
            className="ml-4 inline-flex size-8 shrink-0 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-700"
          >
            <Pencil className="size-4" />
          </button>
        </div>
        <div className="flex flex-wrap gap-3">
          {hasLiveIteration ? (
            <Link href={`/detection/${scenarioId}/live`}>
              <Button
                variant="outline"
                className="h-10 rounded-xl border-[#2d63b8] bg-white px-4 text-[14px] text-[#1f4f96] shadow-none"
              >
                <Eye className="size-4" />
                See live version
              </Button>
            </Link>
          ) : null}
          <Link href={`/detection/${scenarioId}/edit`}>
            <Button className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]">
              <Pencil className="size-4" />
              Edit Scenario
            </Button>
          </Link>
        </div>
      </div>

      <section className="space-y-3">
        <h2 className="text-[1rem] font-semibold text-slate-950">Execution</h2>
        <div className="grid gap-3 xl:grid-cols-2">
          <Card className="rounded-xl border border-slate-200 shadow-none">
            <CardContent className="space-y-3 p-4">
              <div className="-mt-7 w-fit rounded-t-xl border border-b-0 border-slate-200 bg-white px-3 py-1.5 text-[14px] text-slate-700">
                Version comparison
              </div>
              <div className="space-y-4">
                <p className="text-[14px] text-slate-950">
                  Test and compare a scenario version with a live one
                </p>
                {hasLiveIteration ? (
                  <Link href={`/detection/${scenarioId}/tests`}>
                    <Button
                      variant="outline"
                      className="h-8 rounded-full border-[#2d63b8] px-3.5 text-[13px] text-[#1f4f96] shadow-none"
                    >
                      <SquarePen className="size-3.5" />
                      Manage tests
                    </Button>
                  </Link>
                ) : (
                  <Button
                    disabled
                    variant="outline"
                    className="h-8 rounded-full border-slate-200 px-3.5 text-[13px] shadow-none"
                  >
                    <Plus className="size-3.5" />
                    New Test
                  </Button>
                )}
              </div>
            </CardContent>
          </Card>

          <Card className="rounded-xl border border-slate-200 shadow-none">
            <CardContent className="space-y-3 p-4">
              <div className="-mt-7 w-fit rounded-t-xl border border-b-0 border-slate-200 bg-white px-3 py-1.5 text-[14px] text-slate-700">
                Workflow
              </div>
              <div className="space-y-4">
                <p className="text-[14px] leading-6 text-slate-950">
                  Workflows are a series of actions that are automatically performed based on a trigger. They help you automate your work and save time.
                </p>
                <Link href={`/detection/${scenarioId}/workflow`}>
                  <Button
                    variant="outline"
                    className="h-8 rounded-full border-[#2d63b8] px-3.5 text-[13px] text-[#1f4f96] shadow-none"
                  >
                    {hasLiveIteration ? (
                      <>
                        <SquarePen className="size-3.5" />
                        Edit workflow
                      </>
                    ) : (
                      <>
                        <Plus className="size-3.5" />
                        New workflow
                      </>
                    )}
                  </Button>
                </Link>
              </div>
            </CardContent>
          </Card>
        </div>
      </section>

      <section className="space-y-3">
        <h2 className="text-[1rem] font-semibold text-slate-950">Resources</h2>
        <div className="grid gap-3 xl:grid-cols-3">
          <ResourceCard
            title="Scenario guide"
            accent="bg-[linear-gradient(180deg,#eef2ff_0%,#e0e7ff_100%)]"
          />
          <ResourceCard
            title="API"
            accent="bg-[linear-gradient(180deg,#eef4ff_0%,#dbeafe_100%)]"
          />
          <ResourceCard
            title="Workflow"
            accent="bg-[linear-gradient(180deg,#f3f0ff_0%,#ede9fe_100%)]"
          />
        </div>
      </section>
    </div>
  );
}
