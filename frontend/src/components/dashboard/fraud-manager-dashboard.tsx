"use client";

import Link from "next/link";
import { useEffect, useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  Check,
  Circle,
  ClipboardCheck,
  Database,
  FileWarning,
  ListChecks,
  Radio,
  ShieldCheck,
  Workflow,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useAssembledDataModelQuery } from "@/lib/data-model-query";
import { decisionEngineApi, type Scenario } from "@/lib/decision-engine-api";
import {
  markFraudManagerOnboardingCompleted,
  useFraudManagerOnboardingCompleted,
} from "@/lib/fraud-manager-onboarding";
import { cn } from "@/lib/utils";

const scenarioQueryKey = (tenantId: string) =>
  ["decision-engine", "scenarios", tenantId] as const;

const outcomeCards = [
  {
    outcome: "approve",
    label: "Approved",
    description: "Transactions cleared by scenario evaluation",
    color: "bg-emerald-500",
    surface: "border-emerald-200 bg-emerald-50",
    text: "text-emerald-700",
  },
  {
    outcome: "review",
    label: "Review",
    description: "Decisions requiring a fraud manager's attention",
    color: "bg-amber-400",
    surface: "border-amber-200 bg-amber-50",
    text: "text-amber-700",
  },
  {
    outcome: "block_and_review",
    label: "Block and review",
    description: "Transactions held while an investigation is completed",
    color: "bg-orange-500",
    surface: "border-orange-200 bg-orange-50",
    text: "text-orange-700",
  },
  {
    outcome: "decline",
    label: "Declined",
    description: "Transactions rejected by your decision policy",
    color: "bg-rose-500",
    surface: "border-rose-200 bg-rose-50",
    text: "text-rose-700",
  },
] as const;

function formatCount(value: number) {
  return new Intl.NumberFormat("en-US").format(value);
}

function DashboardSkeleton() {
  return (
    <div className="space-y-5" aria-label="Loading dashboard">
      <div className="h-24 animate-pulse rounded-2xl bg-slate-100" />
      <div className="grid gap-4 md:grid-cols-3">
        {[0, 1, 2].map((item) => (
          <div key={item} className="h-36 animate-pulse rounded-2xl bg-slate-100" />
        ))}
      </div>
      <div className="h-72 animate-pulse rounded-2xl bg-slate-100" />
    </div>
  );
}

function DashboardError({ message }: { message: string }) {
  return (
    <Card className="rounded-2xl border border-rose-200 bg-rose-50 shadow-none">
      <CardContent className="flex items-start gap-3 p-5">
        <FileWarning className="mt-0.5 size-5 shrink-0 text-rose-600" />
        <div>
          <h2 className="font-semibold text-rose-900">Dashboard data is unavailable</h2>
          <p className="mt-1 text-sm leading-6 text-rose-700">{message}</p>
        </div>
      </CardContent>
    </Card>
  );
}

function GettingStartedChecklist({
  hasDataModel,
  scenarios,
  hasRule,
  rulesAreLoading,
  scenarioWithRuleId,
}: {
  hasDataModel: boolean;
  scenarios: Scenario[];
  hasRule: boolean;
  rulesAreLoading: boolean;
  scenarioWithRuleId?: string;
}) {
  const scenario =
    scenarios.find((item) => item.id === scenarioWithRuleId) ?? scenarios[0];
  const completedCount = [hasDataModel, scenarios.length > 0, hasRule, false].filter(Boolean)
    .length;
  const progress = Math.round((completedCount / 4) * 100);
  const ruleHref = scenario ? `/detection/${scenario.id}/edit?tab=Rules` : "/detection";

  const steps = [
    {
      title: "Create your data model",
      description: "Define the events and entities your fraud rules can evaluate.",
      complete: hasDataModel,
      href: "/your-data",
      action: hasDataModel ? "View data model" : "Create data model",
      icon: Database,
    },
    {
      title: "Create a scenario",
      description: "Choose the event that starts evaluation for a specific fraud risk.",
      complete: scenarios.length > 0,
      href: "/detection?action=new-scenario",
      action: scenarios.length > 0 ? "View scenarios" : "Create scenario",
      icon: Workflow,
    },
    {
      title: "Add your first rule",
      description: "Describe the behavior to detect and the score it contributes.",
      complete: hasRule,
      pending: rulesAreLoading,
      href: ruleHref,
      action: hasRule ? "Review rules" : "Add a rule",
      icon: ShieldCheck,
    },
    {
      title: "Publish your protection",
      description: "Publish a scenario version to make all of its rules live.",
      complete: false,
      href: scenario ? `/detection/${scenario.id}/edit` : "/detection",
      action: "Review and publish",
      icon: Radio,
    },
  ];

  return (
    <div className="space-y-6">
      <section className="overflow-hidden rounded-3xl bg-[#102f5c] text-white shadow-[0_24px_60px_rgba(15,47,92,0.18)]">
        <div className="grid gap-8 px-6 py-8 lg:grid-cols-[1fr_340px] lg:px-10 lg:py-10">
          <div>
            <p className="text-sm font-semibold uppercase tracking-[0.16em] text-blue-200">
              Fraud manager setup
            </p>
            <h1 className="mt-3 max-w-2xl text-3xl font-semibold tracking-tight sm:text-4xl">
              Build and publish your first fraud protection
            </h1>
            <p className="mt-4 max-w-2xl text-sm leading-7 text-blue-100 sm:text-base">
              Complete the configuration steps below. No transactions or decision requests are
              required to finish setup.
            </p>
          </div>
          <div className="self-end rounded-2xl border border-white/15 bg-white/10 p-5 backdrop-blur">
            <div className="flex items-end justify-between gap-4">
              <div>
                <p className="text-sm text-blue-100">Setup progress</p>
                <p className="mt-1 text-2xl font-semibold">{completedCount} of 4 complete</p>
              </div>
              <span className="text-2xl font-semibold text-blue-100">{progress}%</span>
            </div>
            <div
              className="mt-4 h-2 overflow-hidden rounded-full bg-white/15"
              role="progressbar"
              aria-label="Fraud manager setup progress"
              aria-valuemin={0}
              aria-valuemax={4}
              aria-valuenow={completedCount}
            >
              <div
                className="h-full rounded-full bg-[#63a4ff] transition-[width] duration-500"
                style={{ width: `${progress}%` }}
              />
            </div>
          </div>
        </div>
      </section>

      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="p-0">
          <div className="border-b border-slate-200 px-5 py-4 sm:px-6">
            <h2 className="text-lg font-semibold text-slate-950">Getting started</h2>
            <p className="mt-1 text-sm text-slate-600">
              Your progress is based on saved workspace configuration.
            </p>
          </div>
          <div className="divide-y divide-slate-100">
            {steps.map((step, index) => {
              const Icon = step.icon;
              const isAvailable = index === 0 || steps.slice(0, index).every((item) => item.complete);

              return (
                <div
                  key={step.title}
                  className={cn(
                    "flex flex-col gap-4 px-5 py-5 sm:flex-row sm:items-center sm:px-6",
                    !isAvailable && "bg-slate-50/70"
                  )}
                >
                  <div
                    className={cn(
                      "flex size-11 shrink-0 items-center justify-center rounded-2xl border",
                      step.complete
                        ? "border-emerald-200 bg-emerald-50 text-emerald-600"
                        : isAvailable
                          ? "border-blue-200 bg-blue-50 text-[#1f4f96]"
                          : "border-slate-200 bg-white text-slate-400"
                    )}
                  >
                    {step.complete ? <Check className="size-5" /> : <Icon className="size-5" />}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="font-semibold text-slate-950">{step.title}</h3>
                      <span
                        className={cn(
                          "rounded-full px-2.5 py-1 text-[11px] font-semibold uppercase tracking-wide",
                          step.complete
                            ? "bg-emerald-100 text-emerald-700"
                            : step.pending
                              ? "bg-slate-100 text-slate-500"
                            : isAvailable
                              ? "bg-blue-50 text-[#1f4f96]"
                              : "bg-slate-100 text-slate-500"
                        )}
                      >
                        {step.complete
                          ? "Complete"
                          : step.pending
                            ? "Checking"
                            : isAvailable
                              ? "Next step"
                              : "Locked"}
                      </span>
                    </div>
                    <p className="mt-1 text-sm leading-6 text-slate-600">{step.description}</p>
                  </div>
                  {isAvailable && !step.pending ? (
                    <Button
                      asChild
                      variant={step.complete ? "outline" : "accent"}
                      size="sm"
                      className="self-start rounded-xl shadow-none sm:self-auto"
                    >
                      <Link href={step.href}>
                        {step.action}
                        <ArrowRight className="size-4" />
                      </Link>
                    </Button>
                  ) : (
                    <Button
                      variant={step.complete ? "outline" : "accent"}
                      size="sm"
                      className="self-start rounded-xl shadow-none sm:self-auto"
                      disabled
                    >
                      {step.action}
                      <ArrowRight className="size-4" />
                    </Button>
                  )}
                </div>
              );
            })}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function OperationsDashboard({ scenarios }: { scenarios: Scenario[] }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const decisionCountQueries = useQueries({
    queries: [undefined, ...outcomeCards.map((item) => item.outcome)].map((outcome) => ({
      queryKey: ["decision-engine", "dashboard-decision-count", tenantId, outcome ?? "all"],
      queryFn: () =>
        decisionEngineApi.countDecisions(tenantId, outcome ? { outcome } : undefined),
      enabled: Boolean(tenantId),
    })),
  });
  const totalDecisions = decisionCountQueries[0]?.data?.count ?? 0;
  const decisionCounts = outcomeCards.map(
    (item, index) => decisionCountQueries[index + 1]?.data?.count ?? 0
  );
  const countsAreLoading = decisionCountQueries.some((query) => query.isLoading);
  const countError = decisionCountQueries.find((query) => query.isError)?.error;
  const liveScenarios = scenarios.filter((scenario) => Boolean(scenario.live_iteration_id)).length;

  return (
    <div className="space-y-6">
      <section className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p className="text-sm font-semibold uppercase tracking-[0.14em] text-[#1f4f96]">
            Fraud operations
          </p>
          <h1 className="mt-2 text-3xl font-semibold tracking-tight text-slate-950">
            Dashboard
          </h1>
          <p className="mt-2 text-sm leading-6 text-slate-600">
            Monitor your live protection and the outcomes produced by your scenarios.
          </p>
        </div>
        <Button asChild variant="accent" className="self-start rounded-xl shadow-none sm:self-auto">
          <Link href="/detection?action=new-scenario">
            Create scenario
            <ArrowRight className="size-4" />
          </Link>
        </Button>
      </section>

      {countError ? (
        <DashboardError
          message={countError instanceof Error ? countError.message : "Decision totals could not be loaded."}
        />
      ) : null}

      <section className="grid gap-4 md:grid-cols-3">
        {[
          {
            label: "Total scenarios",
            value: scenarios.length,
            description: "All draft and published scenarios",
            icon: Workflow,
            href: "/detection",
          },
          {
            label: "Live scenarios",
            value: liveScenarios,
            description: "Scenario versions currently protecting events",
            icon: Radio,
            href: "/detection",
          },
          {
            label: "Total decisions",
            value: totalDecisions,
            description: "All outcomes recorded by the decision engine",
            icon: ClipboardCheck,
            href: "/detection?tab=Decisions",
          },
        ].map((item) => {
          const Icon = item.icon;
          return (
            <Link key={item.label} href={item.href} className="group block">
              <Card className="h-full rounded-2xl border border-slate-200 shadow-none transition group-hover:border-blue-200 group-hover:bg-blue-50/30">
                <CardContent className="p-5">
                  <div className="flex items-start justify-between gap-4">
                    <div>
                      <p className="text-sm font-medium text-slate-600">{item.label}</p>
                      <p className="mt-3 text-3xl font-semibold tracking-tight text-slate-950">
                        {countsAreLoading && item.label === "Total decisions"
                          ? "—"
                          : formatCount(item.value)}
                      </p>
                    </div>
                    <span className="flex size-10 items-center justify-center rounded-2xl bg-blue-50 text-[#1f4f96]">
                      <Icon className="size-5" />
                    </span>
                  </div>
                  <p className="mt-4 text-sm leading-6 text-slate-500">{item.description}</p>
                </CardContent>
              </Card>
            </Link>
          );
        })}
      </section>

      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="p-0">
          <div className="flex flex-col gap-2 border-b border-slate-200 px-5 py-5 sm:px-6">
            <h2 className="text-lg font-semibold text-slate-950">Decision outcomes</h2>
            <p className="text-sm text-slate-600">
              All-time distribution across recorded decision outcomes.
            </p>
          </div>
          <div className="grid gap-4 p-5 sm:grid-cols-2 sm:p-6 xl:grid-cols-4">
            {outcomeCards.map((item, index) => {
              const count = decisionCounts[index];
              const percentage = totalDecisions > 0 ? Math.round((count / totalDecisions) * 100) : 0;

              return (
                <Link
                  key={item.outcome}
                  href={`/detection?tab=Decisions&outcome=${item.outcome}`}
                  className={cn(
                    "rounded-2xl border p-4 transition hover:-translate-y-0.5",
                    item.surface
                  )}
                >
                  <div className="flex items-center justify-between gap-3">
                    <span className={cn("text-sm font-semibold", item.text)}>{item.label}</span>
                    <span className={cn("text-sm font-semibold", item.text)}>{percentage}%</span>
                  </div>
                  <p className="mt-3 text-2xl font-semibold text-slate-950">
                    {countsAreLoading ? "—" : formatCount(count)}
                  </p>
                  <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-white/80">
                    <div
                      className={cn("h-full rounded-full transition-[width] duration-500", item.color)}
                      style={{ width: `${percentage}%` }}
                    />
                  </div>
                  <p className="mt-3 text-xs leading-5 text-slate-600">{item.description}</p>
                </Link>
              );
            })}
          </div>
          {!countsAreLoading && totalDecisions === 0 ? (
            <div className="mx-5 mb-5 flex items-start gap-3 rounded-2xl border border-dashed border-slate-300 bg-slate-50 p-4 sm:mx-6 sm:mb-6">
              <Circle className="mt-0.5 size-4 shrink-0 text-slate-400" />
              <p className="text-sm leading-6 text-slate-600">
                No decisions have been recorded yet. This dashboard is ready and will update when
                your integrations begin requesting decisions.
              </p>
            </div>
          ) : null}
        </CardContent>
      </Card>

      <section className="grid gap-4 md:grid-cols-3">
        {[
          {
            title: "Manage scenarios",
            description: "Create drafts, edit rules, and publish new versions.",
            href: "/detection",
            icon: ShieldCheck,
          },
          {
            title: "Manage lists",
            description: "Maintain reusable watchlists, allowlists, and IP lists.",
            href: "/detection?tab=Lists",
            icon: ListChecks,
          },
          {
            title: "Review decisions",
            description: "Inspect outcomes, scores, and the rules that were triggered.",
            href: "/detection?tab=Decisions",
            icon: ClipboardCheck,
          },
        ].map((item) => {
          const Icon = item.icon;
          return (
            <Link
              key={item.title}
              href={item.href}
              className="flex items-center gap-4 rounded-2xl border border-slate-200 bg-white p-4 transition hover:border-blue-200 hover:bg-blue-50/40"
            >
              <span className="flex size-10 shrink-0 items-center justify-center rounded-2xl bg-slate-100 text-slate-700">
                <Icon className="size-5" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block font-semibold text-slate-950">{item.title}</span>
                <span className="mt-1 block text-xs leading-5 text-slate-500">{item.description}</span>
              </span>
              <ArrowRight className="size-4 shrink-0 text-slate-400" />
            </Link>
          );
        })}
      </section>
    </div>
  );
}

export function FraudManagerDashboard() {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const persistedCompletion = useFraudManagerOnboardingCompleted(tenantId);
  const assembledModelQuery = useAssembledDataModelQuery(tenantId);
  const scenariosQuery = useQuery({
    queryKey: scenarioQueryKey(tenantId),
    queryFn: () => decisionEngineApi.listScenarios(tenantId),
    enabled: Boolean(tenantId),
  });
  const scenarios = useMemo(
    () => scenariosQuery.data?.scenarios ?? [],
    [scenariosQuery.data?.scenarios]
  );
  const hasLiveScenario = scenarios.some((scenario) => Boolean(scenario.live_iteration_id));

  const publicationQueries = useQueries({
    queries: scenarios.map((scenario) => ({
      queryKey: ["decision-engine", "publications", tenantId, scenario.id],
      queryFn: () => decisionEngineApi.listPublications(tenantId, scenario.id),
      enabled: Boolean(tenantId && !persistedCompletion && !hasLiveScenario),
    })),
  });
  const hasPublishedHistory = publicationQueries.some((query) =>
    query.data?.publications.some((publication) => publication.action === "publish")
  );
  const onboardingComplete = persistedCompletion || hasLiveScenario || hasPublishedHistory;

  useEffect(() => {
    if (tenantId && (hasLiveScenario || hasPublishedHistory)) {
      markFraudManagerOnboardingCompleted(tenantId);
    }
  }, [hasLiveScenario, hasPublishedHistory, tenantId]);

  const iterationQueries = useQueries({
    queries: scenarios.map((scenario) => ({
      queryKey: ["decision-engine", "iterations", tenantId, scenario.id],
      queryFn: () => decisionEngineApi.listIterations(tenantId, scenario.id),
      enabled: Boolean(tenantId && !onboardingComplete),
    })),
  });
  const iterationReferences = useMemo(
    () =>
      scenarios.flatMap((scenario, scenarioIndex) =>
        (iterationQueries[scenarioIndex]?.data?.iterations ?? []).map((iteration) => ({
          scenarioId: scenario.id,
          iterationId: iteration.id,
        }))
      ),
    [iterationQueries, scenarios]
  );
  const ruleQueries = useQueries({
    queries: iterationReferences.map((reference) => ({
      queryKey: [
        "decision-engine",
        "rules",
        tenantId,
        reference.scenarioId,
        reference.iterationId,
      ],
      queryFn: () =>
        decisionEngineApi.listRules(
          tenantId,
          reference.scenarioId,
          reference.iterationId
        ),
      enabled: Boolean(tenantId && !onboardingComplete),
    })),
  });
  const firstRuleQueryIndex = ruleQueries.findIndex(
    (query) => (query.data?.rules.length ?? 0) > 0
  );
  const hasRule = firstRuleQueryIndex >= 0;
  const scenarioWithRuleId =
    firstRuleQueryIndex >= 0
      ? iterationReferences[firstRuleQueryIndex]?.scenarioId
      : undefined;
  const rulesAreLoading =
    !onboardingComplete &&
    (iterationQueries.some((query) => query.isLoading) || ruleQueries.some((query) => query.isLoading));

  const modelTables = Object.values(assembledModelQuery.data?.data_model.tables ?? {});
  const hasDataModel = modelTables.some(
    (table) => !table.archived && Object.values(table.fields).some((field) => !field.archived)
  );
  const publicationHistoryIsLoading =
    !persistedCompletion &&
    !hasLiveScenario &&
    publicationQueries.some((query) => query.isLoading);
  const initialLoading =
    scenariosQuery.isLoading ||
    (!onboardingComplete &&
      (assembledModelQuery.isLoading || publicationHistoryIsLoading));
  const initialError =
    scenariosQuery.error ?? (!onboardingComplete ? assembledModelQuery.error : null);

  if (!tenantId) {
    return (
      <DashboardError message="Set NEXT_PUBLIC_DATA_MODEL_TENANT_ID to connect this dashboard to a tenant." />
    );
  }

  if (initialLoading) {
    return <DashboardSkeleton />;
  }

  if (initialError) {
    return (
      <DashboardError
        message={initialError instanceof Error ? initialError.message : "Workspace metadata could not be loaded."}
      />
    );
  }

  if (!onboardingComplete) {
    return (
      <GettingStartedChecklist
        hasDataModel={hasDataModel}
        scenarios={scenarios}
        hasRule={hasRule}
        rulesAreLoading={rulesAreLoading}
        scenarioWithRuleId={scenarioWithRuleId}
      />
    );
  }

  return <OperationsDashboard scenarios={scenarios} />;
}
