"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type ReactNode, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  CheckCircle2,
  ChevronDown,
  CircleDot,
  Filter,
  Info,
  Lightbulb,
  MinusCircle,
  Pencil,
  Plus,
  Search,
  ShieldAlert,
  ShieldX,
  Trash2,
  Workflow,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  type Iteration,
  type Rule,
  decisionEngineApi,
} from "@/lib/decision-engine-api";
import { useToastStore } from "@/stores/toast-store";
import { cn } from "@/lib/utils";

type EditorTab = "Trigger" | "Rules" | "Decision";

const sampleTriggerConditions = [
  ["where", "payment_method", "=", '"CARD"'],
  ["and", "transactions_accounts.account...", "=", '"DE"'],
  ["and", "direction", "=", '"PAYOUT"'],
  ["and", "transactions_accounts.bala...", ">", "10,000"],
];

const triggerOperandOptions = [
  "payment_method",
  "direction",
  "transactions_accounts.country",
  "transactions_accounts.balance",
  "transactions.amount",
  "balanceAverage",
] as const;

const triggerOperatorOptions = [
  "=",
  "!=",
  ">",
  ">=",
  "<",
  "<=",
  "is in",
  "contains",
] as const;

const EMPTY_ITERATIONS: Iteration[] = [];

function scenarioStatusLabel(version?: number, live = false) {
  if (live && version) {
    return `V${version} Live`;
  }

  return "Draft";
}

function EditorTabButton({
  active,
  icon,
  children,
  onClick,
}: {
  active: boolean;
  icon: ReactNode;
  children: ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-2 rounded-xl px-3 py-2 text-[14px] transition",
        active ? "bg-[#1f4f96] font-medium text-white" : "text-[#1f4f96]"
      )}
    >
      {icon}
      {children}
    </button>
  );
}

function DeactivateModal({
  isOpen,
  confirmStop,
  confirmImmediate,
  setConfirmStop,
  setConfirmImmediate,
  onClose,
}: {
  isOpen: boolean;
  confirmStop: boolean;
  confirmImmediate: boolean;
  setConfirmStop: (value: boolean) => void;
  setConfirmImmediate: (value: boolean) => void;
  onClose: () => void;
}) {
  if (!isOpen) {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-500/30 p-6 backdrop-blur-sm">
      <div className="w-full max-w-[640px] overflow-hidden rounded-2xl bg-white shadow-[0_18px_50px_rgba(15,23,42,0.18)]">
        <div className="border-b border-slate-200 px-5 py-5 text-center">
          <h2 className="text-[17px] font-semibold text-slate-950">
            Deactivate version
          </h2>
        </div>
        <div className="space-y-4 px-5 py-5">
          <p className="text-[14px] font-semibold text-slate-950">
            By deactivating, I understand :
          </p>
          <label className="flex items-start gap-3 text-[14px] leading-7 text-slate-950">
            <input
              type="checkbox"
              checked={confirmStop}
              onChange={(event) => setConfirmStop(event.target.checked)}
              className="mt-1 size-6 rounded-md border border-[#2d63b8]"
            />
            <span>The scenario will stop operating and no longer make any decision.</span>
          </label>
          <label className="flex items-start gap-3 text-[14px] leading-7 text-slate-950">
            <input
              type="checkbox"
              checked={confirmImmediate}
              onChange={(event) => setConfirmImmediate(event.target.checked)}
              className="mt-1 size-6 rounded-md border border-[#2d63b8]"
            />
            <span>This action is immediate.</span>
          </label>
          <p className="text-[13px] font-medium text-slate-300">
            You can roll back to a previous version directly from the version page
          </p>
        </div>
        <div className="flex gap-3 border-t border-slate-200 px-5 py-4">
          <Button
            variant="outline"
            onClick={onClose}
            className="h-10 flex-1 rounded-xl border-slate-200 px-4 text-[14px] shadow-none"
          >
            Cancel
          </Button>
          <Button
            disabled={!confirmStop || !confirmImmediate}
            className="h-10 flex-1 rounded-xl bg-[#dd3719] px-4 text-[14px] shadow-none hover:bg-[#c43014]"
          >
            <MinusCircle className="size-4" />
            Deactivate
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}

function DecisionThresholdRow({
  label,
  icon,
  colorClassName,
  text,
  inputValue,
}: {
  label: string;
  icon: ReactNode;
  colorClassName: string;
  text: string;
  inputValue?: string;
}) {
  return (
    <div className="flex flex-wrap items-center gap-3 text-[14px] text-slate-950">
      <div className={cn("inline-flex min-w-[176px] items-center gap-2 rounded-lg px-4 py-3", colorClassName)}>
        {icon}
        <span>{label}</span>
      </div>
      <span>{text}</span>
      {inputValue ? (
        <div className="inline-flex min-w-[120px] rounded-lg border border-slate-200 bg-white px-4 py-3">
          {inputValue}
        </div>
      ) : null}
    </div>
  );
}

export function ScenarioEditPage({ scenarioId }: { scenarioId: string }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const router = useRouter();
  const queryClient = useQueryClient();
  const pushToast = useToastStore((state) => state.pushToast);
  const [activeTab, setActiveTab] = useState<EditorTab>("Trigger");
  const [deactivateOpen, setDeactivateOpen] = useState(false);
  const [confirmStop, setConfirmStop] = useState(false);
  const [confirmImmediate, setConfirmImmediate] = useState(false);
  const [scheduleEnabledDraft, setScheduleEnabledDraft] = useState<boolean | null>(null);
  const [scheduleTimeDraft, setScheduleTimeDraft] = useState<string | null>(null);
  const [scheduleMessage, setScheduleMessage] = useState<string | null>(null);
  const [triggerBuilderOpen, setTriggerBuilderOpen] = useState(false);
  const [ruleFiltersOpen, setRuleFiltersOpen] = useState(false);
  const [ruleGroupFilterOpen, setRuleGroupFilterOpen] = useState(false);
  const [iterationMenuOpen, setIterationMenuOpen] = useState(false);
  const [selectedIterationId, setSelectedIterationId] = useState<string | null>(null);
  const [ruleGroupFilter, setRuleGroupFilter] = useState("");
  const [ruleSearch, setRuleSearch] = useState("");
  const [triggerRows, setTriggerRows] = useState([
    { id: "row-1", prefix: "where", left: "", operator: "", right: "" },
    { id: "row-2", prefix: "and", left: "", operator: "", right: "" },
    { id: "row-3", prefix: "and", left: "", operator: "", right: "" },
  ]);

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

  const recurringScheduleQuery = useQuery({
    queryKey: ["decision-engine", "recurring-schedule", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.getRecurringSchedule(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId && scenarioQuery.data?.scenario?.live_iteration_id),
  });

  const updateRecurringScheduleMutation = useMutation({
    mutationFn: (payload: {
      enabled: boolean;
      frequency: string;
      time_of_day: string;
      timezone: string;
      candidate_limit: number;
    }) => decisionEngineApi.updateRecurringSchedule(tenantId, scenarioId, payload),
    onSuccess: ({ recurring_schedule }) => {
      void queryClient.invalidateQueries({
        queryKey: ["decision-engine", "recurring-schedule", tenantId, scenarioId],
      });
      setScheduleEnabledDraft(null);
      setScheduleTimeDraft(null);
      setScheduleMessage(
        recurring_schedule.enabled
          ? `Recurring schedule saved: daily at ${recurring_schedule.time_of_day} UTC`
          : "Recurring schedule disabled."
      );
    },
    onError: (error) => {
      setScheduleMessage(error instanceof Error ? error.message : "Failed to save schedule.");
    },
  });

  const scenario = scenarioQuery.data?.scenario;
  const hasLiveIteration = Boolean(scenario?.live_iteration_id);
  const iterations = iterationsQuery.data?.iterations ?? EMPTY_ITERATIONS;
  const sortedIterations = [...iterations].sort((a, b) => b.version - a.version);
  const draftIteration =
    [...sortedIterations]
      .filter((iteration) => iteration.status === "draft")
      [0] ?? null;
  const liveIteration = scenario?.live_iteration_id
    ? iterations.find((iteration) => iteration.id === scenario.live_iteration_id) ?? null
    : null;
  const preferredIteration =
    draftIteration ??
    liveIteration ??
    sortedIterations[0] ??
    null;
  const currentIteration =
    iterations.find((iteration) => iteration.id === selectedIterationId) ??
    preferredIteration;
  const rulesQuery = useQuery({
    queryKey: ["decision-engine", "rules", tenantId, scenarioId, currentIteration?.id],
    queryFn: () =>
      decisionEngineApi.listRules(tenantId, scenarioId, currentIteration!.id),
    enabled: Boolean(tenantId && scenarioId && currentIteration?.id),
  });
  const validationQuery = useQuery({
    queryKey: ["decision-engine", "validation", tenantId, scenarioId, currentIteration?.id],
    queryFn: () =>
      decisionEngineApi.validateIteration(tenantId, scenarioId, currentIteration!.id),
    enabled: Boolean(activeTab === "Rules" && tenantId && scenarioId && currentIteration?.id),
  });
  const createDraftMutation = useMutation({
    mutationFn: async () => {
      if (currentIteration) {
        const response = await decisionEngineApi.createDraftIteration(
          tenantId,
          scenarioId,
          currentIteration.id
        );
        return response.iteration;
      }

      const response = await decisionEngineApi.createIteration(tenantId, scenarioId);
      return response.iteration;
    },
    onSuccess: async (iteration) => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "iterations", tenantId, scenarioId],
      });
      setSelectedIterationId(iteration.id);
      setIterationMenuOpen(false);
      pushToast({
        title: "Draft created",
        description: currentIteration
          ? `Draft v${iteration.version} was created from ${scenarioStatusLabel(
              currentIteration.version,
              currentIteration.id === scenario?.live_iteration_id
            )}.`
          : `Draft v${iteration.version} is ready for editing.`,
        variant: "success",
      });
    },
    onError: (error) => {
      pushToast({
        title: "Failed to create draft",
        description:
          error instanceof Error ? error.message : "The draft iteration could not be created.",
        variant: "error",
      });
    },
  });
  const createRuleMutation = useMutation({
    mutationFn: async () => {
      if (!draftIteration) {
        throw new Error("Create a draft iteration before adding rules.");
      }

      const nextDisplayOrder =
        rulesQuery.data?.rules?.reduce(
          (maxDisplayOrder, rule) => Math.max(maxDisplayOrder, rule.display_order),
          0
        ) ?? 0;

      return decisionEngineApi.createRule(tenantId, scenarioId, draftIteration.id, {
        display_order: nextDisplayOrder + 1,
        name: "New rule",
        description: "",
        formula: { constant: true },
        score_modifier: 0,
        rule_group: "",
        stable_rule_id: `new-rule-${nextDisplayOrder + 1}`,
      });
    },
    onSuccess: async ({ rule }) => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "rules", tenantId, scenarioId, draftIteration?.id],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "validation", tenantId, scenarioId, draftIteration?.id],
      });
      router.push(`/detection/${scenarioId}/edit/rules/${rule.id}`);
    },
    onError: (error) => {
      pushToast({
        title: "Failed to create rule",
        description: error instanceof Error ? error.message : "The rule could not be created.",
        variant: "error",
      });
    },
  });
  const deleteRuleMutation = useMutation({
    mutationFn: (rule: Rule) =>
      decisionEngineApi.deleteRule(tenantId, scenarioId, currentIteration!.id, rule.id),
    onSuccess: async (_, rule) => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "rules", tenantId, scenarioId, currentIteration?.id],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "validation", tenantId, scenarioId, currentIteration?.id],
      });
      pushToast({
        title: "Rule deleted",
        description: `${rule.name} was removed.`,
        variant: "success",
      });
    },
    onError: (error) => {
      pushToast({
        title: "Failed to delete rule",
        description: error instanceof Error ? error.message : "The rule could not be deleted.",
        variant: "error",
      });
    },
  });

  useEffect(() => {
    if (!rulesQuery.isError) {
      return;
    }

    pushToast({
      title: "Failed to load rules",
      description:
        rulesQuery.error instanceof Error
          ? rulesQuery.error.message
          : "The decision engine rules could not be loaded.",
      variant: "error",
    });
  }, [pushToast, rulesQuery.error, rulesQuery.isError]);

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

  const description = scenario.description || "No description provided";
  const statusLabel = scenarioStatusLabel(
    currentIteration?.version,
    currentIteration?.id === scenario.live_iteration_id
  );
  const rules = rulesQuery.data?.rules ?? [];
  const isDraftIteration = currentIteration?.status === "draft";
  const distinctRuleGroups = Array.from(
    new Set(rules.map((rule) => rule.rule_group).filter(Boolean))
  );
  const filteredRules = rules.filter((rule) => {
    const matchesSearch =
      !ruleSearch ||
      rule.name.toLowerCase().includes(ruleSearch.toLowerCase()) ||
      rule.description.toLowerCase().includes(ruleSearch.toLowerCase());
    const matchesGroup = !ruleGroupFilter || rule.rule_group === ruleGroupFilter;
    return matchesSearch && matchesGroup;
  });

  const recurringSchedule = recurringScheduleQuery.data?.recurring_schedule;
  const effectiveScheduleEnabled = scheduleEnabledDraft ?? recurringSchedule?.enabled ?? false;
  const effectiveScheduleTime = scheduleTimeDraft ?? recurringSchedule?.time_of_day ?? "00:00";
  const validation = validationQuery.data?.validation;
  const validationTriggerErrors = validation?.trigger_errors ?? [];
  const validationErrors = validation?.errors ?? [];
  const ruleErrorsById = new Map(
    (validation?.rule_results ?? [])
      .filter((result) => !result.valid)
      .map((result) => [result.rule_id, result.errors] as const)
  );

  function handleSaveRecurringSchedule() {
    setScheduleMessage(null);
    void updateRecurringScheduleMutation.mutate({
      enabled: effectiveScheduleEnabled,
      frequency: "daily",
      time_of_day: effectiveScheduleTime,
      timezone: "UTC",
      candidate_limit: recurringSchedule?.candidate_limit ?? 100,
    });
  }

  return (
    <>
      <div className="mx-auto w-full max-w-[1280px] space-y-4 px-4 sm:px-6 xl:px-8">
        <div className="border-b border-slate-200 pb-3">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
            <div className="flex flex-wrap items-center gap-2.5">
              <Link
                href={`/detection/${scenarioId}`}
                className="inline-flex size-9 items-center justify-center rounded-xl border border-slate-200 bg-white"
              >
                <ArrowLeft className="size-4" />
              </Link>
              <div className="flex flex-wrap items-center gap-2.5">
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
                <div className="relative">
                  <button
                    type="button"
                    onClick={() => setIterationMenuOpen((current) => !current)}
                    disabled={sortedIterations.length === 0}
                    className="inline-flex items-center gap-2 rounded-full border border-slate-400 px-3.5 py-1 text-[14px] text-[#1f4f96] disabled:cursor-not-allowed disabled:opacity-60"
                  >
                    {statusLabel}
                    <ChevronDown className="size-4" />
                  </button>
                  {iterationMenuOpen && sortedIterations.length > 0 ? (
                    <div className="absolute left-0 top-full z-20 mt-2 min-w-[220px] overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_18px_50px_rgba(15,23,42,0.12)]">
                      <div className="border-b border-slate-100 px-3 py-2 text-[12px] font-medium uppercase tracking-[0.08em] text-slate-500">
                        Iterations
                      </div>
                      <div className="p-1.5">
                        {sortedIterations.map((iteration) => {
                          const isSelected = iteration.id === currentIteration?.id;
                          const isLive = iteration.id === scenario.live_iteration_id;
                          const iterationLabel = scenarioStatusLabel(iteration.version, isLive);

                          return (
                            <button
                              key={iteration.id}
                              type="button"
                              onClick={() => {
                                setSelectedIterationId(iteration.id);
                                setIterationMenuOpen(false);
                              }}
                              className={cn(
                                "flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-left text-[14px]",
                                isSelected
                                  ? "bg-[#edf4ff] text-[#1f4f96]"
                                  : "text-slate-950 hover:bg-slate-50"
                              )}
                            >
                              <div className="flex flex-col">
                                <span className="font-medium">{iterationLabel}</span>
                                <span className="text-[12px] text-slate-500">
                                  Version {iteration.version}
                                </span>
                              </div>
                              {isSelected ? <CheckCircle2 className="size-4" /> : null}
                            </button>
                          );
                        })}
                      </div>
                    </div>
                  ) : null}
                </div>
              </div>
            </div>

            <div className="flex flex-wrap gap-3">
              {hasLiveIteration ? (
                <>
                  <Button
                    onClick={() => createDraftMutation.mutate()}
                    disabled={createDraftMutation.isPending}
                    className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
                  >
                    <Plus className="size-4" />
                    {createDraftMutation.isPending ? "Creating..." : "New draft"}
                  </Button>
                  <Button
                    onClick={() => setDeactivateOpen(true)}
                    className="h-10 rounded-xl bg-[#dd3719] px-4 text-[14px] shadow-none hover:bg-[#c43014]"
                  >
                    <MinusCircle className="size-4" />
                    Deactivate
                  </Button>
                </>
              ) : (
                <Button
                  onClick={() => createDraftMutation.mutate()}
                  disabled={createDraftMutation.isPending}
                  className="h-10 rounded-xl bg-[#1f4f96] px-5 text-[14px] shadow-none hover:bg-[#163f79]"
                >
                  <CircleDot className="size-4" />
                  {createDraftMutation.isPending ? "Creating..." : "New draft"}
                </Button>
              )}
            </div>
          </div>
        </div>

        <div className="rounded-xl border border-slate-200 bg-white px-4 py-3 text-[13px] leading-6 text-slate-700">
          {description}
        </div>

        <div className="flex flex-wrap gap-1.5">
          <EditorTabButton
            active={activeTab === "Trigger"}
            icon={<CircleDot className="size-4" />}
            onClick={() => setActiveTab("Trigger")}
          >
            Trigger
          </EditorTabButton>
          <EditorTabButton
            active={activeTab === "Rules"}
            icon={<Workflow className="size-4" />}
            onClick={() => setActiveTab("Rules")}
          >
            Rules
          </EditorTabButton>
          <EditorTabButton
            active={activeTab === "Decision"}
            icon={<CircleDot className="size-4" />}
            onClick={() => setActiveTab("Decision")}
          >
            Decision
          </EditorTabButton>
        </div>

        {activeTab === "Trigger" ? (
          <>
            <Card className="rounded-xl border border-slate-200 shadow-none">
              <CardContent className="p-0">
                <div className="flex items-center justify-between border-b border-slate-200 px-5 py-3.5">
                  <h2 className="text-[16px] font-semibold text-slate-950">
                    How to run this scenario ?
                  </h2>
                  <button
                    type="button"
                    className="inline-flex size-7 items-center justify-center rounded-lg border border-slate-200"
                  >
                    <ChevronDown className="size-4" />
                  </button>
                </div>
                <div className="space-y-3 px-5 py-4 text-[14px] leading-6 text-slate-950">
                  <p>
                    There are two ways to execute a scenario{" "}
                    <span className="font-medium text-[#1f4f96]">(learn more)</span>
                  </p>
                  <div>
                    <p>
                      1. <span className="font-semibold">API Execution</span>: Send the
                      trigger object via API (see our docs{" "}
                      <span className="font-semibold text-[#1f4f96]">here</span>)
                    </p>
                    <p className="pl-6">
                      You&apos;ll need the scenario_id:{" "}
                      <span className="rounded bg-white px-1 font-mono text-[13px]">
                        {scenario.id}
                      </span>
                    </p>
                  </div>
                  <div>
                    <p>
                      2. <span className="font-semibold">Batch Execution</span>: Run
                      automatically or manually on ingested data.
                    </p>
                    {!hasLiveIteration ? (
                      <p className="pl-6">
                        Publish a live iteration before you can enable a recurring schedule.
                      </p>
                    ) : (
                      <div className="mt-2 space-y-3 pl-6">
                        <label className="flex items-center gap-3">
                          <input
                            type="checkbox"
                            checked={effectiveScheduleEnabled}
                            onChange={(event) => setScheduleEnabledDraft(event.target.checked)}
                            className="size-5 rounded-md border border-[#2d63b8]"
                          />
                          <span>Run this scenario on a schedule</span>
                        </label>
                        {effectiveScheduleEnabled ? (
                          <div className="flex flex-wrap items-center gap-3">
                            <span>Run</span>
                            <div className="inline-flex min-w-[104px] items-center justify-between rounded-lg border border-slate-200 bg-white px-3 py-2">
                              daily
                            </div>
                            <span>at</span>
                            <Input
                              type="time"
                              value={effectiveScheduleTime}
                              onChange={(event) => setScheduleTimeDraft(event.target.value)}
                              className="h-10 w-[120px] rounded-lg border-slate-200 px-3 text-[14px] shadow-none"
                            />
                          </div>
                        ) : null}
                        <div className="flex items-center gap-3">
                          <Button
                            type="button"
                            onClick={handleSaveRecurringSchedule}
                            disabled={updateRecurringScheduleMutation.isPending || recurringScheduleQuery.isLoading}
                            className="h-8 rounded-xl bg-[#1f4f96] px-3.5 text-[13px] shadow-none hover:bg-[#163f79]"
                          >
                            {updateRecurringScheduleMutation.isPending ? "Saving..." : "Save schedule"}
                          </Button>
                          {scheduleMessage ? (
                            <p className="text-[13px] text-slate-600">{scheduleMessage}</p>
                          ) : null}
                        </div>
                      </div>
                    )}
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card className="rounded-xl border border-slate-200 shadow-none">
              <CardContent className="p-0">
                <div className="flex items-center justify-between border-b border-slate-200 px-5 py-3.5">
                  <h2 className="text-[16px] font-semibold text-slate-950">
                    Trigger conditions
                  </h2>
                  <button
                    type="button"
                    className="inline-flex size-7 items-center justify-center rounded-lg border border-slate-200"
                  >
                    <ChevronDown className="size-4" />
                  </button>
                </div>
                <div className="space-y-4 px-5 py-4">
                  <div className="rounded-lg border border-slate-200 bg-white px-3.5 py-3.5">
                    <div className="flex items-center gap-3 text-[13px] leading-6 text-slate-950">
                      <Lightbulb className="size-4" />
                      <p>
                        Determines whether the scenario is relevant for each trigger
                        object{" "}
                        <span className="font-medium text-[#1f4f96]">(learn more)</span>
                      </p>
                    </div>
                  </div>

                  {hasLiveIteration ? (
                    <div className="space-y-4 rounded-xl border border-slate-200 bg-white px-3.5 py-3.5">
                      <div className="inline-flex rounded-md bg-slate-50 px-3 py-1.5 text-[14px] font-medium text-[#1f4f96]">
                        {scenario.trigger_object_type}
                      </div>
                      <div className="space-y-2.5">
                        {sampleTriggerConditions.map(([prefix, field, operator, value]) => (
                          <div key={`${prefix}-${field}`} className="flex flex-wrap items-center gap-2.5 text-[14px] text-slate-950">
                            <span className="rounded-md bg-slate-50 px-3 py-2.5 text-slate-600">
                              {prefix}
                            </span>
                            <span className="rounded-md bg-slate-50 px-2.5 py-2.5 text-slate-600">
                              {field.includes("bala") ? "#" : "Tt"}
                            </span>
                            <span className="rounded-md bg-slate-50 px-3 py-2.5">
                              {field}
                            </span>
                            <span className="rounded-md bg-slate-50 px-3 py-2.5">
                              {operator}
                            </span>
                            <span className="rounded-md bg-slate-50 px-3 py-2.5">
                              {value}
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  ) : (
                    <>
                      {!triggerBuilderOpen ? (
                        <>
                          <div className="rounded-lg border border-[#3b82f6] bg-blue-50 px-3.5 py-2.5 text-[14px] text-[#2563eb]">
                            All{" "}
                            <span className="font-semibold">
                              {scenario.trigger_object_type}
                            </span>{" "}
                            will be checked
                          </div>
                          <div className="flex justify-end gap-3">
                            <Button
                              variant="outline"
                              onClick={() => setTriggerBuilderOpen(true)}
                              className="h-8 rounded-full border-slate-200 px-3.5 text-[13px] shadow-none"
                            >
                              Add trigger condition
                            </Button>
                            <Button className="h-8 rounded-xl bg-[#1f4f96] px-3.5 text-[13px] shadow-none hover:bg-[#163f79]">
                              Save
                            </Button>
                          </div>
                        </>
                      ) : (
                        <div className="space-y-4 rounded-xl border border-slate-200 bg-white px-3.5 py-3.5">
                          <div className="inline-flex rounded-md bg-slate-50 px-3 py-1.5 text-[14px] font-medium text-[#1f4f96]">
                            {scenario.trigger_object_type}
                          </div>
                          <div className="space-y-3">
                            {triggerRows.map((row) => (
                              <div key={row.id} className="space-y-2">
                                <div className="flex flex-wrap items-center gap-2.5 text-[14px]">
                                  <span className="rounded-md bg-slate-50 px-3 py-2.5 text-slate-600">
                                    {row.prefix}
                                  </span>
                                  <select
                                    value={row.left}
                                    onChange={(event) =>
                                      setTriggerRows((current) =>
                                        current.map((item) =>
                                          item.id === row.id ? { ...item, left: event.target.value } : item
                                        )
                                      )
                                    }
                                    className="rounded-md border border-[#ff6b57] px-3 py-2.5 text-slate-700 outline-none"
                                  >
                                    <option value="">Select an operand...</option>
                                    {triggerOperandOptions.map((option) => (
                                      <option key={option} value={option}>
                                        {option}
                                      </option>
                                    ))}
                                  </select>
                                  <select
                                    value={row.operator}
                                    onChange={(event) =>
                                      setTriggerRows((current) =>
                                        current.map((item) =>
                                          item.id === row.id ? { ...item, operator: event.target.value } : item
                                        )
                                      )
                                    }
                                    className="rounded-md border border-[#ff6b57] px-3 py-2.5 text-slate-700 outline-none"
                                  >
                                    <option value="">...</option>
                                    {triggerOperatorOptions.map((option) => (
                                      <option key={option} value={option}>
                                        {option}
                                      </option>
                                    ))}
                                  </select>
                                  <select
                                    value={row.right}
                                    onChange={(event) =>
                                      setTriggerRows((current) =>
                                        current.map((item) =>
                                          item.id === row.id ? { ...item, right: event.target.value } : item
                                        )
                                      )
                                    }
                                    className="rounded-md border border-[#ff6b57] px-3 py-2.5 text-slate-700 outline-none"
                                  >
                                    <option value="">Select an operand...</option>
                                    {triggerOperandOptions.map((option) => (
                                      <option key={option} value={option}>
                                        {option}
                                      </option>
                                    ))}
                                  </select>
                                  <button
                                    type="button"
                                    onClick={() =>
                                      setTriggerRows((current) => current.filter((item) => item.id !== row.id))
                                    }
                                    className="inline-flex size-8 items-center justify-center rounded-md border border-slate-200 text-slate-400"
                                  >
                                    <Trash2 className="size-3.5" />
                                  </button>
                                </div>
                                <div className="inline-flex rounded-md bg-[#ffd9d2] px-3 py-1 text-[13px] text-[#dd3719]">
                                  {[row.left, row.operator, row.right].filter(Boolean).length} / 3 filled
                                </div>
                              </div>
                            ))}
                          </div>
                          <div className="flex flex-wrap items-center justify-between gap-3">
                            <div className="space-y-3">
                              <Button
                                variant="outline"
                                onClick={() =>
                                  setTriggerRows((current) => [
                                    ...current,
                                    {
                                      id: `row-${current.length + 1}`,
                                      prefix: "and",
                                      left: "",
                                      operator: "",
                                      right: "",
                                    },
                                  ])
                                }
                                className="h-8 rounded-xl border-[#2d63b8] px-3.5 text-[13px] text-[#1f4f96] shadow-none"
                              >
                                <Plus className="size-3.5" />
                                Condition
                              </Button>
                              <div className="inline-flex rounded-md bg-[#ffd9d2] px-3 py-2 text-[13px] text-[#dd3719]">
                                The formula must return a boolean
                              </div>
                            </div>
                            <div className="flex gap-3">
                              <Button
                                variant="outline"
                                className="h-8 rounded-xl border-slate-200 px-3.5 text-[13px] shadow-none"
                              >
                                Delete trigger condition
                              </Button>
                              <Button className="h-8 rounded-xl bg-[#1f4f96] px-3.5 text-[13px] shadow-none hover:bg-[#163f79]">
                                Save
                              </Button>
                            </div>
                          </div>
                        </div>
                      )}
                    </>
                  )}
                </div>
              </CardContent>
            </Card>
          </>
        ) : null}

        {activeTab === "Rules" ? (
          <div className="space-y-4">
            <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
              <div className="relative w-full max-w-[720px]">
                <Search className="pointer-events-none absolute left-4 top-1/2 size-5 -translate-y-1/2 text-slate-500" />
                <Input
                  value={ruleSearch}
                  onChange={(event) => setRuleSearch(event.target.value)}
                  placeholder="Search"
                  className="h-10 rounded-xl border-slate-200 pl-11 text-[14px] shadow-none"
                />
              </div>
              <div className="relative flex gap-3">
                <Button
                  variant="outline"
                  onClick={() => {
                    setRuleFiltersOpen((current) => !current);
                    setRuleGroupFilterOpen(false);
                  }}
                  className="h-9 rounded-xl border-slate-200 bg-white px-3.5 text-[13px] shadow-none"
                >
                  <Filter className="size-4" />
                  Filters
                </Button>
                {ruleGroupFilter ? (
                  <div className="absolute right-[126px] top-11 rounded-xl border border-slate-200 bg-white px-3 py-2 text-[13px] text-[#1f4f96] shadow-[0_18px_30px_rgba(15,23,42,0.08)]">
                    Rule group: {ruleGroupFilter}
                  </div>
                ) : null}
                <div className="relative">
                  <Button
                    disabled={!draftIteration || createRuleMutation.isPending}
                    onClick={() => createRuleMutation.mutate()}
                    className="h-9 rounded-xl bg-[#1f4f96] px-3.5 text-[13px] shadow-none hover:bg-[#163f79]"
                  >
                    <Plus className="size-4" />
                    {createRuleMutation.isPending ? "Creating..." : "Add"}
                  </Button>
                </div>
                {!draftIteration ? (
                  <div className="absolute right-0 top-11 w-[280px] rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-[12px] text-amber-800 shadow-[0_18px_30px_rgba(15,23,42,0.08)]">
                    Create a draft iteration first. Use the <span className="font-medium">New draft</span> button above.
                  </div>
                ) : null}
                {ruleFiltersOpen ? (
                  <div className="absolute right-[126px] top-11 z-10 w-[250px] rounded-xl border border-slate-200 bg-white p-2 shadow-[0_18px_30px_rgba(15,23,42,0.08)]">
                    {ruleGroupFilterOpen ? (
                      <div className="space-y-2 p-1">
                        <div className="h-12 rounded-lg border border-slate-200" />
                        {distinctRuleGroups.length === 0 ? (
                          <p className="px-2 py-2 text-[14px] text-slate-500">
                            No rule group. Create a new one to group your rules
                          </p>
                        ) : (
                          distinctRuleGroups.map((group) => (
                            <button
                              key={group}
                              type="button"
                              onClick={() => {
                                setRuleGroupFilter(group);
                                setRuleFiltersOpen(false);
                                setRuleGroupFilterOpen(false);
                              }}
                              className="flex w-full items-center rounded-lg px-3 py-2.5 text-left text-[14px] text-slate-950 hover:bg-slate-50"
                            >
                              {group}
                            </button>
                          ))
                        )}
                      </div>
                    ) : (
                      <button
                        type="button"
                        onClick={() => setRuleGroupFilterOpen(true)}
                        className="flex w-full items-center rounded-lg px-3 py-2.5 text-left text-[14px] text-slate-950 hover:bg-slate-50"
                      >
                        Rule group
                      </button>
                    )}
                  </div>
                ) : null}
              </div>
            </div>

            <Card className="overflow-hidden rounded-xl border border-slate-200 shadow-none">
              <CardContent className="p-0">
                {validation ? (
                  <div
                    className={cn(
                      "border-b px-4 py-3 text-[13px]",
                      validation.valid
                        ? "border-emerald-200 bg-emerald-50 text-emerald-800"
                        : "border-amber-200 bg-amber-50 text-amber-800"
                    )}
                  >
                    {validation.valid
                      ? "This iteration is valid against the current data model."
                      : "This iteration has validation issues. Review trigger and rule errors below."}
                    {validationTriggerErrors.length > 0 ? (
                      <div className="mt-2 space-y-1">
                        {validationTriggerErrors.map((error, index) => (
                          <p key={`${error}-${index}`}>Trigger: {error}</p>
                        ))}
                      </div>
                    ) : null}
                    {validationErrors.length > 0 ? (
                      <div className="mt-2 space-y-1">
                        {validationErrors.map((error, index) => (
                          <p key={`${error}-${index}`}>{error}</p>
                        ))}
                      </div>
                    ) : null}
                  </div>
                ) : null}
                <div className="overflow-x-auto">
                  {rulesQuery.isLoading ? (
                    <div className="flex min-h-[140px] items-center justify-center text-[16px] text-slate-600">
                      Loading rules...
                    </div>
                  ) : filteredRules.length > 0 ? (
                    <table className="min-w-full text-left">
                      <thead>
                        <tr
                          className="border-b border-slate-200 bg-white text-[13px] font-semibold text-slate-950"
                        >
                          <th className="px-4 py-3">Name</th>
                          <th className="px-4 py-3">Description</th>
                          <th className="px-4 py-3">Rule group</th>
                          <th className="px-4 py-3 text-center">Score Modifier</th>
                          <th className="px-4 py-3">Outcome</th>
                          <th className="px-4 py-3 text-right">Actions</th>
                        </tr>
                      </thead>
                      <tbody>
                        {filteredRules.map((rule) => (
                          <tr
                            key={rule.id}
                            className="border-b border-slate-100 text-[14px] text-slate-950 last:border-b-0"
                          >
                            <td className="px-4 py-3 text-[14px] font-medium">
                              {isDraftIteration ? (
                                <Link
                                  href={`/detection/${scenarioId}/edit/rules/${rule.id}`}
                                  className="text-left hover:text-[#1f4f96]"
                                >
                                  {rule.name}
                                </Link>
                              ) : (
                                <span>{rule.name}</span>
                              )}
                              {ruleErrorsById.has(rule.id) ? (
                                <div className="mt-1 text-[12px] text-amber-700">
                                  {ruleErrorsById.get(rule.id)?.[0]}
                                </div>
                              ) : null}
                            </td>
                            <td className="max-w-[440px] px-4 py-3 text-[13px]">{rule.description}</td>
                            <td className="px-4 py-3">
                              {rule.rule_group ? (
                                <Badge className="rounded-full border-[#2d63b8] bg-white px-2 py-0.5 text-[12px] font-medium tracking-normal normal-case text-[#2d63b8]">
                                  {rule.rule_group}
                                </Badge>
                              ) : null}
                            </td>
                            <td className="px-4 py-3 text-center text-[14px] text-[#dd3719]">
                              {rule.score_modifier >= 0
                                ? `+${rule.score_modifier}`
                                : `${rule.score_modifier}`}
                            </td>
                            <td className="px-4 py-3 text-[13px]">Score based</td>
                            <td className="px-4 py-3 text-right">
                              <div className="flex justify-end gap-2">
                                <Button
                                  variant="outline"
                                  asChild
                                  disabled={!isDraftIteration}
                                  className="h-8 rounded-lg border-slate-200 px-3 text-[12px] shadow-none"
                                >
                                  <Link href={`/detection/${scenarioId}/edit/rules/${rule.id}`}>
                                    Edit
                                  </Link>
                                </Button>
                                <Button
                                  variant="outline"
                                  disabled={!isDraftIteration || deleteRuleMutation.isPending}
                                  onClick={() => {
                                    if (
                                      typeof window !== "undefined" &&
                                      window.confirm(`Delete rule "${rule.name}"?`)
                                    ) {
                                      deleteRuleMutation.mutate(rule);
                                    }
                                  }}
                                  className="h-8 rounded-lg border-red-200 px-3 text-[12px] text-red-700 shadow-none"
                                >
                                  Delete
                                </Button>
                              </div>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  ) : (
                    <div className="flex min-h-[140px] items-center justify-center text-[16px] text-slate-600">
                      No rules match the current filters.
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>
          </div>
        ) : null}

        {activeTab === "Decision" ? (
          <Card className="rounded-xl border border-slate-200 shadow-none">
            <CardContent className="p-0">
              <div className="flex items-center justify-between border-b border-slate-200 px-5 py-4">
                <h2 className="text-[16px] font-semibold text-slate-950">
                  Score based decision
                </h2>
                <button
                  type="button"
                  className="inline-flex size-7 items-center justify-center rounded-lg border border-slate-200"
                >
                  <ChevronDown className="size-4" />
                </button>
              </div>
              <div className="space-y-4 px-5 py-4">
                <div className="rounded-lg border border-slate-200 bg-white px-3.5 py-3.5">
                  <div className="flex items-center gap-3 text-[13px] leading-7 text-slate-950">
                    <Lightbulb className="size-4" />
                    <p>
                      The decision is made by comparing the total score computed by the
                      rules to the thresholds defined below.{" "}
                      <span className="font-medium text-[#1f4f96]">(learn more)</span>
                    </p>
                  </div>
                </div>
                <div className="space-y-3">
                  <DecisionThresholdRow
                    label="Approve"
                    icon={<CheckCircle2 className="size-4 text-[#16a34a]" />}
                    colorClassName="bg-[#e1f3ea] text-[#16a34a]"
                    text="When score <"
                    inputValue="1"
                  />
                  <DecisionThresholdRow
                    label="Review"
                    icon={<CircleDot className="size-4 text-[#f59e0b]" />}
                    colorClassName="bg-[#fef0c7] text-[#f59e0b]"
                    text="When 1 ≤ score <"
                    inputValue="10"
                  />
                  <DecisionThresholdRow
                    label="Block and Review"
                    icon={<ShieldAlert className="size-4 text-[#f97316]" />}
                    colorClassName="bg-[#ffedd5] text-[#f97316]"
                    text="When 10 ≤ score <"
                    inputValue="20"
                  />
                  <DecisionThresholdRow
                    label="Decline"
                    icon={<ShieldX className="size-4 text-[#dd3719]" />}
                    colorClassName="bg-[#f9d8d2] text-[#dd3719]"
                    text="When score ≥ 20"
                  />
                </div>
                <div className="flex justify-end">
                  <Button className="h-8 rounded-xl bg-[#1f4f96] px-3.5 text-[13px] shadow-none hover:bg-[#163f79]">
                    Save
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>
        ) : null}
      </div>

      <DeactivateModal
        isOpen={deactivateOpen}
        confirmStop={confirmStop}
        confirmImmediate={confirmImmediate}
        setConfirmStop={setConfirmStop}
        setConfirmImmediate={setConfirmImmediate}
        onClose={() => {
          setDeactivateOpen(false);
          setConfirmStop(false);
          setConfirmImmediate(false);
        }}
      />
    </>
  );
}
