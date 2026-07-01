"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  CircleDot,
  Filter,
  GitCommitHorizontal,
  Info,
  Lightbulb,
  MinusCircle,
  Pencil,
  Plus,
  Search,
  ShieldAlert,
  ShieldX,
  Workflow,
} from "lucide-react";

import {
  type FunctionVariableTableFieldOption,
} from "@/components/detection/function-variable-modal";
import { RuleBuilderSimple } from "@/components/detection/rule-builder-simple";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useAssembledDataModelQuery } from "@/lib/data-model-query";
import {
  type Iteration,
  type Rule,
  decisionEngineApi,
} from "@/lib/decision-engine-api";
import {
  compileConditionGroupsToAst,
  createSimpleRuleGroup,
  getRuleOperatorOption,
  extractAccessorOptions,
  simpleRuleOperatorOptions,
  type SimpleRuleConditionGroup,
  summarizeRuleFormula,
  tryParseAstToConditionGroups,
} from "@/lib/rule-builder";
import { useToastStore } from "@/stores/toast-store";
import { cn } from "@/lib/utils";

type EditorTab = "Trigger" | "Rules" | "Decision";

const EMPTY_ITERATIONS: Iteration[] = [];

function scenarioStatusLabel(version?: number, live = false, status?: string) {
  if (!version) {
    return live ? "Live" : "Draft";
  }

  if (live) {
    return `v${version}. Live`;
  }

  if (status === "committed") {
    return `v${version}. Committed`;
  }

  return `v${version}. Draft`;
}

function recurringScheduleSummary(schedule: {
  enabled: boolean;
  frequency: string;
  time_of_day: string;
  minute_of_hour: number;
  day_of_week: string;
  day_of_month: number;
  timezone: string;
}) {
  if (!schedule.enabled) {
    return "Recurring schedule disabled.";
  }

  if (schedule.frequency === "hourly") {
    return `Recurring schedule saved: hourly at minute ${String(
      schedule.minute_of_hour ?? 0
    ).padStart(2, "0")} ${schedule.timezone}`;
  }

  if (schedule.frequency === "weekly") {
    const weekday = schedule.day_of_week
      ? `${schedule.day_of_week.slice(0, 1).toUpperCase()}${schedule.day_of_week.slice(1)}`
      : "Monday";
    return `Recurring schedule saved: weekly on ${weekday} at ${schedule.time_of_day} ${schedule.timezone}`;
  }

  if (schedule.frequency === "monthly") {
    return `Recurring schedule saved: monthly on day ${schedule.day_of_month} at ${schedule.time_of_day} ${schedule.timezone}`;
  }

  return `Recurring schedule saved: daily at ${schedule.time_of_day} ${schedule.timezone}`;
}

function formatNextRunLabel(value: string | null | undefined, timezone = "UTC") {
  if (!value) {
    return null;
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: timezone,
  }).format(date) + ` ${timezone}`;
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
  isSubmitting,
  setConfirmStop,
  setConfirmImmediate,
  onClose,
  onConfirm,
}: {
  isOpen: boolean;
  confirmStop: boolean;
  confirmImmediate: boolean;
  isSubmitting: boolean;
  setConfirmStop: (value: boolean) => void;
  setConfirmImmediate: (value: boolean) => void;
  onClose: () => void;
  onConfirm: () => void;
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
            disabled={isSubmitting}
            className="h-10 flex-1 rounded-xl border-slate-200 px-4 text-[14px] shadow-none"
          >
            Cancel
          </Button>
          <Button
            disabled={!confirmStop || !confirmImmediate || isSubmitting}
            onClick={onConfirm}
            className="h-10 flex-1 rounded-xl bg-[#dd3719] px-4 text-[14px] shadow-none hover:bg-[#c43014]"
          >
            <MinusCircle className="size-4" />
            {isSubmitting ? "Deactivating..." : "Deactivate"}
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}

function CommitModal({
  isOpen,
  iterationLabel,
  isSubmitting,
  onClose,
  onConfirm,
}: {
  isOpen: boolean;
  iterationLabel: string;
  isSubmitting: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  if (!isOpen) {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-500/30 p-6 backdrop-blur-sm">
      <div className="w-full max-w-[640px] overflow-hidden rounded-2xl bg-white shadow-[0_18px_50px_rgba(15,23,42,0.18)]">
        <div className="border-b border-slate-200 px-5 py-5 text-center">
          <h2 className="text-[17px] font-semibold text-slate-950">Commit version</h2>
        </div>
        <div className="space-y-4 px-5 py-5">
          <p className="text-[14px] text-slate-950">
            Commit <span className="font-semibold">{iterationLabel}</span> so it is ready for
            publication.
          </p>
          <div className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-[14px] text-slate-700">
            Committing freezes this draft version for publication workflows. Continue only if
            your trigger logic, rules, and thresholds are ready.
          </div>
        </div>
        <div className="flex gap-3 border-t border-slate-200 px-5 py-4">
          <Button
            variant="outline"
            onClick={onClose}
            disabled={isSubmitting}
            className="h-10 flex-1 rounded-xl border-slate-200 px-4 text-[14px] shadow-none"
          >
            Cancel
          </Button>
          <Button
            onClick={onConfirm}
            disabled={isSubmitting}
            className="h-10 flex-1 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
          >
            <GitCommitHorizontal className="size-4" />
            {isSubmitting ? "Committing..." : "Commit"}
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}

function PublishModal({
  isOpen,
  iterationLabel,
  isSubmitting,
  onClose,
  onConfirm,
}: {
  isOpen: boolean;
  iterationLabel: string;
  isSubmitting: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  if (!isOpen) {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-500/30 p-6 backdrop-blur-sm">
      <div className="w-full max-w-[640px] overflow-hidden rounded-2xl bg-white shadow-[0_18px_50px_rgba(15,23,42,0.18)]">
        <div className="border-b border-slate-200 px-5 py-5 text-center">
          <h2 className="text-[17px] font-semibold text-slate-950">Publish version</h2>
        </div>
        <div className="space-y-4 px-5 py-5">
          <p className="text-[14px] text-slate-950">
            Publish <span className="font-semibold">{iterationLabel}</span> as the live
            scenario iteration.
          </p>
          <div className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-[14px] text-slate-700">
            Publishing makes this version active immediately for scenario execution until
            another version is published or the scenario is deactivated.
          </div>
        </div>
        <div className="flex gap-3 border-t border-slate-200 px-5 py-4">
          <Button
            variant="outline"
            onClick={onClose}
            disabled={isSubmitting}
            className="h-10 flex-1 rounded-xl border-slate-200 px-4 text-[14px] shadow-none"
          >
            Cancel
          </Button>
          <Button
            onClick={onConfirm}
            disabled={isSubmitting}
            className="h-10 flex-1 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
          >
            <CheckCircle2 className="size-4" />
            {isSubmitting ? "Publishing..." : "Publish"}
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
  onInputChange,
  disabled = false,
}: {
  label: string;
  icon: ReactNode;
  colorClassName: string;
  text: string;
  inputValue?: string;
  onInputChange?: (value: string) => void;
  disabled?: boolean;
}) {
  return (
    <div className="flex flex-wrap items-center gap-3 text-[14px] text-slate-950">
      <div className={cn("inline-flex min-w-[176px] items-center gap-2 rounded-lg px-4 py-3", colorClassName)}>
        {icon}
        <span>{label}</span>
      </div>
      <span>{text}</span>
      {inputValue !== undefined ? (
        onInputChange ? (
          <Input
            value={inputValue}
            disabled={disabled}
            onChange={(event) => onInputChange(event.target.value)}
            inputMode="numeric"
            className="h-[50px] w-[120px] rounded-lg border-slate-200 bg-white px-4 py-3 text-[14px] shadow-none"
          />
        ) : (
          <div className="inline-flex min-w-[120px] rounded-lg border border-slate-200 bg-white px-4 py-3">
            {inputValue}
          </div>
        )
      ) : null}
    </div>
  );
}

function formatDecisionTimestamp(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

function decisionOutcomeClasses(outcome: string) {
  switch (outcome.toLowerCase()) {
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

export function ScenarioEditPage({
  scenarioId,
  initialIterationId = null,
  preferLiveIteration = false,
}: {
  scenarioId: string;
  initialIterationId?: string | null;
  preferLiveIteration?: boolean;
}) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const router = useRouter();
  const queryClient = useQueryClient();
  const pushToast = useToastStore((state) => state.pushToast);
  const [activeTab, setActiveTab] = useState<EditorTab>("Trigger");
  const [deactivateOpen, setDeactivateOpen] = useState(false);
  const [commitOpen, setCommitOpen] = useState(false);
  const [publishOpen, setPublishOpen] = useState(false);
  const [confirmStop, setConfirmStop] = useState(false);
  const [confirmImmediate, setConfirmImmediate] = useState(false);
  const [scheduleEnabledDraft, setScheduleEnabledDraft] = useState<boolean | null>(null);
  const [scheduleFrequencyDraft, setScheduleFrequencyDraft] = useState<string | null>(null);
  const [scheduleTimeDraft, setScheduleTimeDraft] = useState<string | null>(null);
  const [scheduleMinuteDraft, setScheduleMinuteDraft] = useState<string | null>(null);
  const [scheduleDayOfWeekDraft, setScheduleDayOfWeekDraft] = useState<string | null>(null);
  const [scheduleDayOfMonthDraft, setScheduleDayOfMonthDraft] = useState<string | null>(null);
  const [scheduleTimezoneDraft, setScheduleTimezoneDraft] = useState<string | null>(null);
  const [scheduleMessage, setScheduleMessage] = useState<string | null>(null);
  const [reviewThresholdDraft, setReviewThresholdDraft] = useState<string>("");
  const [blockAndReviewThresholdDraft, setBlockAndReviewThresholdDraft] = useState<string>("");
  const [declineThresholdDraft, setDeclineThresholdDraft] = useState<string>("");
  const [decisionMessage, setDecisionMessage] = useState<string | null>(null);
  const [decisionSearch, setDecisionSearch] = useState("");
  const [selectedDecisionId, setSelectedDecisionId] = useState<string | null>(null);
  const [decisionOpen, setDecisionOpen] = useState(true);
  const [triggerScheduleOpen, setTriggerScheduleOpen] = useState(true);
  const [triggerConditionsOpen, setTriggerConditionsOpen] = useState(true);
  const [triggerMessage, setTriggerMessage] = useState<string | null>(null);
  const [ruleFiltersOpen, setRuleFiltersOpen] = useState(false);
  const [ruleGroupFilterOpen, setRuleGroupFilterOpen] = useState(false);
  const [iterationMenuOpen, setIterationMenuOpen] = useState(false);
  const [selectedIterationId, setSelectedIterationId] = useState<string | null>(null);
  const [ruleGroupFilter, setRuleGroupFilter] = useState("");
  const [ruleSearch, setRuleSearch] = useState("");
  const [triggerConditionGroups, setTriggerConditionGroups] = useState<
    SimpleRuleConditionGroup[]
  >([createSimpleRuleGroup()]);

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
      minute_of_hour: number;
      day_of_week: string;
      day_of_month: number;
      timezone: string;
      candidate_limit: number;
    }) => decisionEngineApi.updateRecurringSchedule(tenantId, scenarioId, payload),
    onSuccess: ({ recurring_schedule }) => {
      void queryClient.invalidateQueries({
        queryKey: ["decision-engine", "recurring-schedule", tenantId, scenarioId],
      });
      setScheduleEnabledDraft(null);
      setScheduleFrequencyDraft(null);
      setScheduleTimeDraft(null);
      setScheduleMinuteDraft(null);
      setScheduleDayOfWeekDraft(null);
      setScheduleDayOfMonthDraft(null);
      setScheduleTimezoneDraft(null);
      setScheduleMessage(recurringScheduleSummary(recurring_schedule));
    },
    onError: (error) => {
      setScheduleMessage(error instanceof Error ? error.message : "Failed to save schedule.");
    },
  });

  const updateDecisionThresholdsMutation = useMutation({
    mutationFn: async (payload: {
      score_review_threshold: number | null;
      score_block_and_review_threshold: number | null;
      score_decline_threshold: number | null;
    }) => {
      if (!currentIteration) {
        throw new Error("No iteration selected.");
      }

      return decisionEngineApi.updateIteration(tenantId, scenarioId, currentIteration.id, {
        trigger_formula: currentIteration.trigger_formula ?? { constant: true },
        score_review_threshold: payload.score_review_threshold,
        score_block_and_review_threshold: payload.score_block_and_review_threshold,
        score_decline_threshold: payload.score_decline_threshold,
        schedule: currentIteration.schedule,
      });
    },
    onSuccess: async ({ iteration }) => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "iterations", tenantId, scenarioId],
      });
      setDecisionMessage(`Decision thresholds saved for V${iteration.version}.`);
      pushToast({
        title: "Decision thresholds saved",
        description: `Version ${iteration.version} now uses the updated score bands.`,
        variant: "success",
      });
    },
    onError: (error) => {
      const message =
        error instanceof Error ? error.message : "Failed to save decision thresholds.";
      setDecisionMessage(message);
      pushToast({
        title: "Failed to save decision thresholds",
        description: message,
        variant: "error",
      });
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
  const routeIteration =
    (initialIterationId
      ? iterations.find((iteration) => iteration.id === initialIterationId) ?? null
      : null) ??
    (preferLiveIteration ? liveIteration : null);
  const currentIteration =
    iterations.find((iteration) => iteration.id === selectedIterationId) ??
    routeIteration ??
    preferredIteration;
  const rulesQuery = useQuery({
    queryKey: ["decision-engine", "rules", tenantId, scenarioId, currentIteration?.id],
    queryFn: () =>
      decisionEngineApi.listRules(tenantId, scenarioId, currentIteration!.id),
    enabled: Boolean(tenantId && scenarioId && currentIteration?.id),
  });
  const ruleFunctionsQuery = useQuery({
    queryKey: ["decision-engine", "rule-functions"],
    queryFn: () => decisionEngineApi.listRuleFunctions(),
    enabled: Boolean(tenantId),
  });
  const assembledModelQuery = useAssembledDataModelQuery(tenantId);
  const editorIdentifiersQuery = useQuery({
    queryKey: ["decision-engine", "editor-identifiers", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.listEditorIdentifiers(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });
  const customListsQuery = useQuery({
    queryKey: ["decision-engine", "custom-lists", tenantId],
    queryFn: () => decisionEngineApi.listCustomLists(tenantId),
    enabled: Boolean(tenantId),
  });
  const validationQuery = useQuery({
    queryKey: ["decision-engine", "validation", tenantId, scenarioId, currentIteration?.id],
    queryFn: () =>
      decisionEngineApi.validateIteration(tenantId, scenarioId, currentIteration!.id),
    enabled: Boolean(
      (activeTab === "Trigger" || activeTab === "Rules") &&
        tenantId &&
        scenarioId &&
        currentIteration?.id
    ),
  });
  const scenarioDecisionsQuery = useQuery({
    queryKey: ["decision-engine", "scenario-decisions", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.listScenarioDecisions(tenantId, scenarioId),
    enabled: Boolean(activeTab === "Decision" && tenantId && scenarioId),
  });
  const decisionDetailQuery = useQuery({
    queryKey: ["decision-engine", "decision", tenantId, selectedDecisionId],
    queryFn: () => decisionEngineApi.getDecision(tenantId, selectedDecisionId!),
    enabled: Boolean(activeTab === "Decision" && tenantId && selectedDecisionId),
  });
  const createDraftMutation = useMutation({
    mutationFn: async () => {
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
        description: `Draft v${iteration.version} is ready for editing.`,
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
      router.push(
        `/detection/${scenarioId}/edit/rules/${rule.id}?iterationId=${rule.iteration_id}`
      );
    },
    onError: (error) => {
      pushToast({
        title: "Failed to create rule",
        description: error instanceof Error ? error.message : "The rule could not be created.",
        variant: "error",
      });
    },
  });

  const updateTriggerConditionsMutation = useMutation({
    mutationFn: async () => {
      if (!currentIteration) {
        throw new Error("No iteration selected.");
      }

      return decisionEngineApi.updateIteration(tenantId, scenarioId, currentIteration.id, {
        trigger_formula: compileConditionGroupsToAst(
          triggerConditionGroups,
          triggerAccessorOptions
        ),
        score_review_threshold: currentIteration.score_review_threshold ?? null,
        score_block_and_review_threshold:
          currentIteration.score_block_and_review_threshold ?? null,
        score_decline_threshold: currentIteration.score_decline_threshold ?? null,
        schedule: currentIteration.schedule,
      });
    },
    onSuccess: async ({ iteration }) => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "iterations", tenantId, scenarioId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "validation", tenantId, scenarioId, currentIteration?.id],
      });
      setTriggerMessage(`Trigger conditions saved for v${iteration.version}.`);
      pushToast({
        title: "Trigger conditions saved",
        description: `Version ${iteration.version} now uses the updated trigger formula.`,
        variant: "success",
      });
    },
    onError: (error) => {
      const message =
        error instanceof Error ? error.message : "Failed to save trigger conditions.";
      setTriggerMessage(message);
      pushToast({
        title: "Failed to save trigger conditions",
        description: message,
        variant: "error",
      });
    },
  });
  const commitIterationMutation = useMutation({
    mutationFn: async () => {
      if (!currentIteration) {
        throw new Error("No iteration selected.");
      }

      return decisionEngineApi.commitIteration(tenantId, scenarioId, currentIteration.id);
    },
    onSuccess: async ({ iteration }) => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "iterations", tenantId, scenarioId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "scenario", tenantId, scenarioId],
      });
      setCommitOpen(false);
      setSelectedIterationId(iteration.id);
      pushToast({
        title: "Iteration committed",
        description: `Version ${iteration.version} is ready for publication.`,
        variant: "success",
      });
    },
    onError: (error) => {
      pushToast({
        title: "Failed to commit iteration",
        description:
          error instanceof Error ? error.message : "The iteration could not be committed.",
        variant: "error",
      });
    },
  });
  const publishIterationMutation = useMutation({
    mutationFn: async () => {
      if (!currentIteration) {
        throw new Error("No iteration selected.");
      }

      return decisionEngineApi.publishIteration(tenantId, scenarioId, {
        action: "publish",
        iteration_id: currentIteration.id,
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "iterations", tenantId, scenarioId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "scenario", tenantId, scenarioId],
      });
      setPublishOpen(false);
      pushToast({
        title: "Iteration published",
        description: `${statusLabel} is now live.`,
        variant: "success",
      });
    },
    onError: (error) => {
      pushToast({
        title: "Failed to publish iteration",
        description:
          error instanceof Error ? error.message : "The iteration could not be published.",
        variant: "error",
      });
    },
  });
  const deactivateIterationMutation = useMutation({
    mutationFn: async () => {
      if (!currentIteration) {
        throw new Error("No iteration selected.");
      }

      return decisionEngineApi.deactivateIteration(tenantId, scenarioId, currentIteration.id);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "iterations", tenantId, scenarioId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "scenario", tenantId, scenarioId],
      });
      setDeactivateOpen(false);
      setConfirmStop(false);
      setConfirmImmediate(false);
      pushToast({
        title: "Iteration deactivated",
        description: `${statusLabel} is no longer live.`,
        variant: "success",
      });
    },
    onError: (error) => {
      pushToast({
        title: "Failed to deactivate iteration",
        description:
          error instanceof Error ? error.message : "The iteration could not be deactivated.",
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

  useEffect(() => {
    setReviewThresholdDraft(
      currentIteration?.score_review_threshold != null
        ? String(currentIteration.score_review_threshold)
        : "1"
    );
    setBlockAndReviewThresholdDraft(
      currentIteration?.score_block_and_review_threshold != null
        ? String(currentIteration.score_block_and_review_threshold)
        : "10"
    );
    setDeclineThresholdDraft(
      currentIteration?.score_decline_threshold != null
        ? String(currentIteration.score_decline_threshold)
        : "20"
    );
    setDecisionMessage(null);
  }, [
    currentIteration?.id,
    currentIteration?.score_block_and_review_threshold,
    currentIteration?.score_decline_threshold,
    currentIteration?.score_review_threshold,
  ]);

  const description = scenario?.description || "No description provided";
  const triggerObjectType = scenario?.trigger_object_type ?? "trigger";
  const isLiveSelectedIteration = currentIteration?.id === scenario?.live_iteration_id;
  const statusLabel = scenarioStatusLabel(
    currentIteration?.version,
    isLiveSelectedIteration,
    currentIteration?.status
  );
  const rules = rulesQuery.data?.rules ?? [];
  const isDraftIteration = currentIteration?.status === "draft";
  const isCommittedIteration = currentIteration?.status === "committed";
  const scenarioDecisions = scenarioDecisionsQuery.data?.decisions ?? [];
  const filteredScenarioDecisions = useMemo(
    () =>
      [...scenarioDecisions]
        .filter((decision) =>
          currentIteration ? decision.scenario_iteration_id === currentIteration.id : true
        )
        .filter((decision) => {
          const normalizedSearch = decisionSearch.trim().toLowerCase();
          if (!normalizedSearch) {
            return true;
          }

          return [
            decision.object_id,
            decision.object_type,
            decision.outcome,
            String(decision.score),
          ].some((value) => value.toLowerCase().includes(normalizedSearch));
        })
        .sort((left, right) => {
          const leftTime = new Date(left.created_at).getTime();
          const rightTime = new Date(right.created_at).getTime();
          return rightTime - leftTime;
        }),
    [currentIteration, decisionSearch, scenarioDecisions]
  );
  const selectedDecision =
    filteredScenarioDecisions.find((decision) => decision.id === selectedDecisionId) ??
    filteredScenarioDecisions[0] ??
    null;
  useEffect(() => {
    if (!filteredScenarioDecisions.length) {
      if (selectedDecisionId !== null) {
        setSelectedDecisionId(null);
      }
      return;
    }

    if (!selectedDecisionId) {
      setSelectedDecisionId(filteredScenarioDecisions[0].id);
      return;
    }

    if (!filteredScenarioDecisions.some((decision) => decision.id === selectedDecisionId)) {
      setSelectedDecisionId(filteredScenarioDecisions[0].id);
    }
  }, [filteredScenarioDecisions, selectedDecisionId]);
  const triggerAccessorOptions = useMemo(
    () =>
      extractAccessorOptions(
        editorIdentifiersQuery.data?.payload_accessors ?? [],
        editorIdentifiersQuery.data?.database_accessors ?? []
      ),
    [
      editorIdentifiersQuery.data?.database_accessors,
      editorIdentifiersQuery.data?.payload_accessors,
    ]
  );
  const triggerTableFieldOptions = useMemo<FunctionVariableTableFieldOption[]>(
    () =>
      Object.values(assembledModelQuery.data?.data_model.tables ?? {})
        .flatMap((table) =>
          Object.values(table.fields ?? {}).map((field) => ({
            tableName: table.name,
            fieldName: field.name,
            label: field.name,
          }))
        )
        .sort((left, right) =>
          left.tableName === right.tableName
            ? left.fieldName.localeCompare(right.fieldName)
            : left.tableName.localeCompare(right.tableName)
        ),
    [assembledModelQuery.data?.data_model.tables]
  );
  const triggerAccessorLabelLookup = useMemo(
    () => new Map(triggerAccessorOptions.map((option) => [option.id, option.label])),
    [triggerAccessorOptions]
  );
  const parsedTriggerConditionGroups = (
    tryParseAstToConditionGroups(currentIteration?.trigger_formula, triggerAccessorOptions) ?? []
  ).filter((group) =>
    group.conditions.some(
      (condition) =>
        Boolean(condition.operator) &&
        Boolean(
          (condition.leftMode === "function" && condition.leftFunction) || condition.left.trim()
        )
    )
  );
  useEffect(() => {
    const parsed =
      tryParseAstToConditionGroups(currentIteration?.trigger_formula, triggerAccessorOptions) ?? [];
    const nextGroups =
      parsed.length > 0 ? parsed : [createSimpleRuleGroup()];

    setTriggerConditionGroups(nextGroups);
    setTriggerMessage(null);
  }, [currentIteration?.id, currentIteration?.trigger_formula, triggerAccessorOptions]);
  const triggerRuleOperatorOptions = useMemo(() => {
    const availableFunctions = new Set(
      (ruleFunctionsQuery.data?.rule_functions ?? []).map((ruleFunction) => ruleFunction.name)
    );

    return availableFunctions.size === 0
      ? simpleRuleOperatorOptions
      : simpleRuleOperatorOptions.filter((option) => availableFunctions.has(option.value));
  }, [ruleFunctionsQuery.data?.rule_functions]);
  const triggerCustomLists = useMemo(
    () =>
      (customListsQuery.data?.custom_lists ?? []).map((item) => ({
        id: item.id,
        name: item.name,
      })),
    [customListsQuery.data?.custom_lists]
  );
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

  if (scenarioQuery.isError || !scenario) {
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

  const recurringSchedule = recurringScheduleQuery.data?.recurring_schedule;
  const effectiveScheduleEnabled = scheduleEnabledDraft ?? recurringSchedule?.enabled ?? false;
  const effectiveScheduleFrequency =
    scheduleFrequencyDraft ?? recurringSchedule?.frequency ?? "daily";
  const effectiveScheduleTime = scheduleTimeDraft ?? recurringSchedule?.time_of_day ?? "00:00";
  const effectiveScheduleMinute = scheduleMinuteDraft ?? String(recurringSchedule?.minute_of_hour ?? 0);
  const effectiveScheduleDayOfWeek =
    scheduleDayOfWeekDraft ?? recurringSchedule?.day_of_week ?? "monday";
  const effectiveScheduleDayOfMonth =
    scheduleDayOfMonthDraft ?? String(recurringSchedule?.day_of_month ?? 1);
  const effectiveScheduleTimezone =
    scheduleTimezoneDraft ?? recurringSchedule?.timezone ?? "UTC";
  const nextRunLabel = formatNextRunLabel(
    recurringSchedule?.next_run,
    recurringSchedule?.timezone ?? effectiveScheduleTimezone
  );
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
    const parsedMinute = Number(effectiveScheduleMinute);
    const parsedDayOfMonth = Number(effectiveScheduleDayOfMonth);
    void updateRecurringScheduleMutation.mutate({
      enabled: effectiveScheduleEnabled,
      frequency: effectiveScheduleFrequency,
      time_of_day: effectiveScheduleFrequency === "hourly" ? "" : effectiveScheduleTime,
      minute_of_hour:
        effectiveScheduleFrequency === "hourly" && Number.isFinite(parsedMinute)
          ? Math.max(0, Math.min(59, parsedMinute))
          : 0,
      day_of_week: effectiveScheduleFrequency === "weekly" ? effectiveScheduleDayOfWeek : "",
      day_of_month:
        effectiveScheduleFrequency === "monthly" && Number.isFinite(parsedDayOfMonth)
          ? Math.max(1, Math.min(31, parsedDayOfMonth))
          : 0,
      timezone: effectiveScheduleTimezone,
      candidate_limit: recurringSchedule?.candidate_limit ?? 100,
    });
  }

  function parseThreshold(value: string) {
    const trimmed = value.trim();
    if (!trimmed) {
      return null;
    }

    const parsed = Number(trimmed);
    return Number.isFinite(parsed) ? parsed : null;
  }

  function handleSaveDecisionThresholds() {
    setDecisionMessage(null);
    void updateDecisionThresholdsMutation.mutate({
      score_review_threshold: parseThreshold(reviewThresholdDraft),
      score_block_and_review_threshold: parseThreshold(blockAndReviewThresholdDraft),
      score_decline_threshold: parseThreshold(declineThresholdDraft),
    });
  }

  function getTriggerOperandPrefix(params: {
    mode?: string;
    valueType?: "string" | "number" | "boolean";
    hasFunction?: boolean;
  }) {
    if (params.hasFunction || params.mode === "function") {
      return "fx";
    }

    if (params.mode === "custom_list") {
      return "[]";
    }

    if (params.valueType === "number") {
      return "#";
    }

    if (params.valueType === "boolean") {
      return "?";
    }

    return "Tt";
  }

  function getTriggerOperandLabel(params: {
    value: string;
    mode?: string;
    valueType?: "string" | "number" | "boolean";
    functionLabel?: string | null;
  }) {
    if (params.mode === "function" && params.functionLabel) {
      return params.functionLabel;
    }

    if (params.mode === "custom_list") {
      return params.value;
    }

    if (params.mode === "constant") {
      return params.valueType === "string" ? `"${params.value}"` : params.value;
    }

    return triggerAccessorLabelLookup.get(params.value) ?? params.value;
  }

  function handleClearTriggerConditions() {
    setTriggerConditionGroups([createSimpleRuleGroup()]);
    setTriggerMessage("Trigger conditions cleared. Save to apply the default trigger.");
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
                          const iterationLabel = scenarioStatusLabel(
                            iteration.version,
                            isLive,
                            iteration.status
                          );

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
                              <span className="font-medium">{iterationLabel}</span>
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
                  {isLiveSelectedIteration ? (
                    <Button
                      onClick={() => setDeactivateOpen(true)}
                      className="h-10 rounded-xl bg-[#dd3719] px-4 text-[14px] shadow-none hover:bg-[#c43014]"
                    >
                      <MinusCircle className="size-4" />
                      Deactivate
                    </Button>
                  ) : null}
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

        <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
          <div className="min-w-0 flex-1 rounded-xl border border-slate-200 bg-white px-4 py-3 text-[13px] leading-6 text-slate-700">
            {description}
          </div>
          {!isLiveSelectedIteration ? (
            isDraftIteration ? (
              <Button
                disabled={commitIterationMutation.isPending}
                onClick={() => setCommitOpen(true)}
                className="h-12 rounded-xl bg-[#1f4f96] px-5 text-[14px] shadow-none hover:bg-[#163f79]"
              >
                <GitCommitHorizontal className="size-4" />
                Commit
              </Button>
            ) : isCommittedIteration ? (
              <Button
                disabled={publishIterationMutation.isPending}
                onClick={() => setPublishOpen(true)}
                className="h-12 rounded-xl bg-[#1f4f96] px-5 text-[14px] shadow-none hover:bg-[#163f79]"
              >
                <CheckCircle2 className="size-4" />
                Publish
              </Button>
            ) : null
          ) : null}
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
                    onClick={() => setTriggerScheduleOpen((current) => !current)}
                    className="inline-flex size-7 items-center justify-center rounded-lg border border-slate-200"
                  >
                    {triggerScheduleOpen ? (
                      <ChevronUp className="size-4" />
                    ) : (
                      <ChevronDown className="size-4" />
                    )}
                  </button>
                </div>
                {triggerScheduleOpen ? (
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
                            <select
                              value={effectiveScheduleFrequency}
                              onChange={(event) => setScheduleFrequencyDraft(event.target.value)}
                              className="h-10 min-w-[124px] rounded-lg border border-slate-200 bg-white px-3 text-[14px] text-slate-950 outline-none"
                            >
                              <option value="daily">daily</option>
                              <option value="hourly">hourly</option>
                              <option value="weekly">weekly</option>
                              <option value="monthly">monthly</option>
                            </select>
                            <span>timezone</span>
                            <select
                              value={effectiveScheduleTimezone}
                              onChange={(event) => setScheduleTimezoneDraft(event.target.value)}
                              className="h-10 min-w-[190px] rounded-lg border border-slate-200 bg-white px-3 text-[14px] text-slate-950 outline-none"
                            >
                              <option value="UTC">UTC</option>
                              <option value="Africa/Accra">Africa/Accra</option>
                              <option value="Europe/London">Europe/London</option>
                              <option value="America/New_York">America/New_York</option>
                              <option value="America/Chicago">America/Chicago</option>
                              <option value="America/Los_Angeles">America/Los_Angeles</option>
                              <option value="Asia/Dubai">Asia/Dubai</option>
                              <option value="Asia/Singapore">Asia/Singapore</option>
                              <option value="Asia/Tokyo">Asia/Tokyo</option>
                              <option value="Australia/Sydney">Australia/Sydney</option>
                            </select>
                            {effectiveScheduleFrequency === "hourly" ? (
                              <>
                                <span>at minute</span>
                                <Input
                                  type="number"
                                  min="0"
                                  max="59"
                                  value={effectiveScheduleMinute}
                                  onChange={(event) => setScheduleMinuteDraft(event.target.value)}
                                  className="h-10 w-[96px] rounded-lg border-slate-200 px-3 text-[14px] shadow-none"
                                />
                              </>
                            ) : effectiveScheduleFrequency === "monthly" ? (
                              <>
                                <span>on day</span>
                                <Input
                                  type="number"
                                  min="1"
                                  max="31"
                                  value={effectiveScheduleDayOfMonth}
                                  onChange={(event) => setScheduleDayOfMonthDraft(event.target.value)}
                                  className="h-10 w-[96px] rounded-lg border-slate-200 px-3 text-[14px] shadow-none"
                                />
                                <span>at</span>
                                <Input
                                  type="time"
                                  value={effectiveScheduleTime}
                                  onChange={(event) => setScheduleTimeDraft(event.target.value)}
                                  className="h-10 w-[120px] rounded-lg border-slate-200 px-3 text-[14px] shadow-none"
                                />
                              </>
                            ) : effectiveScheduleFrequency === "weekly" ? (
                              <>
                                <span>on</span>
                                <select
                                  value={effectiveScheduleDayOfWeek}
                                  onChange={(event) => setScheduleDayOfWeekDraft(event.target.value)}
                                  className="h-10 min-w-[132px] rounded-lg border border-slate-200 bg-white px-3 text-[14px] text-slate-950 outline-none"
                                >
                                  <option value="monday">Monday</option>
                                  <option value="tuesday">Tuesday</option>
                                  <option value="wednesday">Wednesday</option>
                                  <option value="thursday">Thursday</option>
                                  <option value="friday">Friday</option>
                                  <option value="saturday">Saturday</option>
                                  <option value="sunday">Sunday</option>
                                </select>
                                <span>at</span>
                                <Input
                                  type="time"
                                  value={effectiveScheduleTime}
                                  onChange={(event) => setScheduleTimeDraft(event.target.value)}
                                  className="h-10 w-[120px] rounded-lg border-slate-200 px-3 text-[14px] shadow-none"
                                />
                              </>
                            ) : (
                              <>
                                <span>at</span>
                                <Input
                                  type="time"
                                  value={effectiveScheduleTime}
                                  onChange={(event) => setScheduleTimeDraft(event.target.value)}
                                  className="h-10 w-[120px] rounded-lg border-slate-200 px-3 text-[14px] shadow-none"
                                />
                              </>
                            )}
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
                        {nextRunLabel ? (
                          <p className="text-[13px] text-slate-600">
                            Next run: <span className="font-medium text-slate-900">{nextRunLabel}</span>
                          </p>
                        ) : null}
                      </div>
                    )}
                  </div>
                </div>
                ) : null}
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
                    onClick={() => setTriggerConditionsOpen((current) => !current)}
                    className="inline-flex size-7 items-center justify-center rounded-lg border border-slate-200"
                  >
                    {triggerConditionsOpen ? (
                      <ChevronUp className="size-4" />
                    ) : (
                      <ChevronDown className="size-4" />
                    )}
                  </button>
                </div>
                {triggerConditionsOpen ? (
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

                  {!isDraftIteration ? (
                    <div className="space-y-4 rounded-xl border border-slate-200 bg-white px-3.5 py-3.5">
                      <div className="inline-flex rounded-md bg-slate-50 px-3 py-1.5 text-[14px] font-medium text-[#1f4f96]">
                        {scenario.trigger_object_type}
                      </div>
                      {parsedTriggerConditionGroups.length > 0 ? (
                        <div className="space-y-3">
                          {parsedTriggerConditionGroups.map((group, groupIndex) => (
                            <div key={group.id} className="space-y-2.5">
                              {group.conditions.map((condition, conditionIndex) => {
                                const operator = getRuleOperatorOption(condition.operator);
                                const leftLabel = getTriggerOperandLabel({
                                  value: condition.left,
                                  mode: condition.leftMode,
                                  valueType: condition.valueType,
                                  functionLabel: condition.leftFunction?.label ?? null,
                                });
                                const rightLabel = getTriggerOperandLabel({
                                  value: condition.right,
                                  mode: condition.rightMode,
                                  valueType: condition.valueType,
                                  functionLabel: condition.rightFunction?.label ?? null,
                                });

                                return (
                                  <div
                                    key={condition.id}
                                    className="flex flex-wrap items-center gap-2.5 text-[14px] text-slate-950"
                                  >
                                    <span className="rounded-md bg-slate-50 px-3 py-2.5 text-slate-600">
                                      {groupIndex === 0 && conditionIndex === 0
                                        ? "where"
                                        : conditionIndex === 0
                                          ? "or"
                                          : "and"}
                                    </span>
                                    <span className="rounded-md bg-slate-50 px-2.5 py-2.5 text-slate-600">
                                      {getTriggerOperandPrefix({
                                        mode: condition.leftMode,
                                        valueType: condition.valueType,
                                        hasFunction: Boolean(condition.leftFunction),
                                      })}
                                    </span>
                                    <span className="rounded-md bg-slate-50 px-3 py-2.5">
                                      {leftLabel}
                                    </span>
                                    {operator ? (
                                      <span className="rounded-md bg-slate-50 px-3 py-2.5">
                                        {operator.label}
                                      </span>
                                    ) : null}
                                    {!operator?.unary ? (
                                      <>
                                        <span className="rounded-md bg-slate-50 px-2.5 py-2.5 text-slate-600">
                                          {getTriggerOperandPrefix({
                                            mode: condition.rightMode,
                                            valueType: condition.valueType,
                                            hasFunction: Boolean(condition.rightFunction),
                                          })}
                                        </span>
                                        <span className="rounded-md bg-slate-50 px-3 py-2.5">
                                          {rightLabel}
                                        </span>
                                      </>
                                    ) : null}
                                  </div>
                                );
                              })}
                            </div>
                          ))}
                        </div>
                      ) : (
                        <div className="rounded-lg border border-slate-200 bg-slate-50 px-4 py-3 text-[14px] text-slate-700">
                          {summarizeRuleFormula(currentIteration?.trigger_formula) ??
                            "No trigger conditions are configured for this iteration."}
                        </div>
                      )}
                    </div>
                  ) : (
                    <div className="space-y-4 rounded-xl border border-slate-200 bg-white px-3.5 py-3.5">
                      <div className="inline-flex rounded-md bg-slate-50 px-3 py-1.5 text-[14px] font-medium text-[#1f4f96]">
                        {scenario.trigger_object_type}
                      </div>
                      {!parsedTriggerConditionGroups.length ? (
                        <div className="rounded-lg border border-[#3b82f6] bg-blue-50 px-3.5 py-2.5 text-[14px] text-[#2563eb]">
                          All <span className="font-semibold">{scenario.trigger_object_type}</span>{" "}
                          will be checked until you add trigger conditions.
                        </div>
                      ) : null}
                      {validationTriggerErrors.length > 0 ? (
                        <div className="rounded-lg border border-red-200 bg-red-50 px-3.5 py-3 text-[13px] text-red-700">
                          {validationTriggerErrors.map((error, index) => (
                            <p key={`${error}-${index}`}>{error}</p>
                          ))}
                        </div>
                      ) : null}
                      <RuleBuilderSimple
                        groups={triggerConditionGroups}
                        onChange={setTriggerConditionGroups}
                        accessorOptions={triggerAccessorOptions}
                        operatorOptions={triggerRuleOperatorOptions}
                        customListOptions={triggerCustomLists}
                        triggerObjectType={scenario.trigger_object_type}
                        tableFieldOptions={triggerTableFieldOptions}
                        disabled={
                          !isDraftIteration || updateTriggerConditionsMutation.isPending
                        }
                      />
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        <div className="flex items-center gap-3">
                          <Button
                            variant="outline"
                            disabled={
                              !isDraftIteration || updateTriggerConditionsMutation.isPending
                            }
                            onClick={handleClearTriggerConditions}
                            className="h-8 rounded-xl border-slate-200 px-3.5 text-[13px] shadow-none"
                          >
                            Clear trigger conditions
                          </Button>
                        </div>
                        <div className="flex items-center gap-3">
                          {triggerMessage ? (
                            <p className="text-[13px] text-slate-600">{triggerMessage}</p>
                          ) : null}
                          <Button
                            disabled={
                              !isDraftIteration || updateTriggerConditionsMutation.isPending
                            }
                            onClick={() => updateTriggerConditionsMutation.mutate()}
                            className="h-8 rounded-xl bg-[#1f4f96] px-3.5 text-[13px] shadow-none hover:bg-[#163f79]"
                          >
                            {updateTriggerConditionsMutation.isPending ? "Saving..." : "Save"}
                          </Button>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
                ) : null}
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
                    disabled={!isDraftIteration || createRuleMutation.isPending}
                    onClick={() => createRuleMutation.mutate()}
                    className="h-9 rounded-xl bg-[#1f4f96] px-3.5 text-[13px] shadow-none hover:bg-[#163f79]"
                  >
                    <Plus className="size-4" />
                    {createRuleMutation.isPending ? "Creating..." : "Add"}
                  </Button>
                </div>
                {!isDraftIteration ? (
                  <div className="absolute right-0 top-11 w-[280px] rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-[12px] text-amber-800 shadow-[0_18px_30px_rgba(15,23,42,0.08)]">
                    Select or create a draft iteration before adding rules.
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
                        {filteredRules.map((rule) => {
                          const ruleSummary = summarizeRuleFormula(rule.formula);

                          return (
                          <tr
                            key={rule.id}
                            className="border-b border-slate-100 text-[14px] text-slate-950 last:border-b-0"
                          >
                            <td className="px-4 py-3 text-[14px] font-medium">
                              {isDraftIteration ? (
                                <Link
                                  href={`/detection/${scenarioId}/edit/rules/${rule.id}?iterationId=${rule.iteration_id}`}
                                  className="text-left hover:text-[#1f4f96]"
                                >
                                  {rule.name}
                                </Link>
                              ) : (
                                <Link
                                  href={`/detection/${scenarioId}/edit/rules/${rule.id}?iterationId=${rule.iteration_id}`}
                                  className="text-left hover:text-[#1f4f96]"
                                >
                                  {rule.name}
                                </Link>
                              )}
                              {ruleErrorsById.has(rule.id) ? (
                                <div className="mt-1 text-[12px] text-amber-700">
                                  {ruleErrorsById.get(rule.id)?.[0]}
                                </div>
                              ) : null}
                            </td>
                            <td className="max-w-[440px] px-4 py-3 text-[13px]">
                              {rule.description ? (
                                <span>{rule.description}</span>
                              ) : ruleSummary ? (
                                <span className="text-slate-700">{ruleSummary}</span>
                              ) : (
                                <span className="text-slate-400">No description</span>
                              )}
                            </td>
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
                                {isDraftIteration ? (
                                  <Button
                                    variant="outline"
                                    asChild
                                    className="h-8 rounded-lg border-slate-200 px-3 text-[12px] shadow-none"
                                  >
                                    <Link href={`/detection/${scenarioId}/edit/rules/${rule.id}?iterationId=${rule.iteration_id}`}>
                                      Edit
                                    </Link>
                                  </Button>
                                ) : (
                                  <Button
                                    variant="outline"
                                    disabled
                                    className="h-8 rounded-lg border-slate-200 px-3 text-[12px] shadow-none"
                                  >
                                    Edit
                                  </Button>
                                )}
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
                        )})}
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
                  onClick={() => setDecisionOpen((current) => !current)}
                  className="inline-flex size-7 items-center justify-center rounded-lg border border-slate-200"
                >
                  {decisionOpen ? (
                    <ChevronUp className="size-4" />
                  ) : (
                    <ChevronDown className="size-4" />
                  )}
                </button>
              </div>
              {decisionOpen ? (
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
                <div className="hidden space-y-3">
                  <DecisionThresholdRow
                    label="Approve"
                    icon={<CheckCircle2 className="size-4 text-[#16a34a]" />}
                    colorClassName="bg-[#e1f3ea] text-[#16a34a]"
                    text="When score <"
                    inputValue={reviewThresholdDraft}
                    onInputChange={setReviewThresholdDraft}
                    disabled={!isDraftIteration || updateDecisionThresholdsMutation.isPending}
                  />
                  <DecisionThresholdRow
                    label="Review"
                    icon={<CircleDot className="size-4 text-[#f59e0b]" />}
                    colorClassName="bg-[#fef0c7] text-[#f59e0b]"
                    text="When 1 ≤ score <"
                    inputValue={blockAndReviewThresholdDraft}
                    onInputChange={setBlockAndReviewThresholdDraft}
                    disabled={!isDraftIteration || updateDecisionThresholdsMutation.isPending}
                  />
                  <DecisionThresholdRow
                    label="Block and Review"
                    icon={<ShieldAlert className="size-4 text-[#f97316]" />}
                    colorClassName="bg-[#ffedd5] text-[#f97316]"
                    text="When 10 ≤ score <"
                    inputValue={declineThresholdDraft}
                    onInputChange={setDeclineThresholdDraft}
                    disabled={!isDraftIteration || updateDecisionThresholdsMutation.isPending}
                  />
                  <DecisionThresholdRow
                    label="Decline"
                    icon={<ShieldX className="size-4 text-[#dd3719]" />}
                    colorClassName="bg-[#f9d8d2] text-[#dd3719]"
                    text="When score ≥ 20"
                  />
                </div>
                <div className="hidden justify-end">
                  <Button className="h-8 rounded-xl bg-[#1f4f96] px-3.5 text-[13px] shadow-none hover:bg-[#163f79]">
                    Save
                  </Button>
                </div>
                <div className="space-y-3">
                  <DecisionThresholdRow
                    label="Approve"
                    icon={<CheckCircle2 className="size-4 text-[#16a34a]" />}
                    colorClassName="bg-[#e1f3ea] text-[#16a34a]"
                    text="When score <"
                    inputValue={reviewThresholdDraft}
                    onInputChange={setReviewThresholdDraft}
                    disabled={!isDraftIteration || updateDecisionThresholdsMutation.isPending}
                  />
                  <DecisionThresholdRow
                    label="Review"
                    icon={<CircleDot className="size-4 text-[#f59e0b]" />}
                    colorClassName="bg-[#fef0c7] text-[#f59e0b]"
                    text={`When ${reviewThresholdDraft || "0"} <= score <`}
                    inputValue={blockAndReviewThresholdDraft}
                    onInputChange={setBlockAndReviewThresholdDraft}
                    disabled={!isDraftIteration || updateDecisionThresholdsMutation.isPending}
                  />
                  <DecisionThresholdRow
                    label="Block and Review"
                    icon={<ShieldAlert className="size-4 text-[#f97316]" />}
                    colorClassName="bg-[#ffedd5] text-[#f97316]"
                    text={`When ${blockAndReviewThresholdDraft || "0"} <= score <`}
                    inputValue={declineThresholdDraft}
                    onInputChange={setDeclineThresholdDraft}
                    disabled={!isDraftIteration || updateDecisionThresholdsMutation.isPending}
                  />
                  <DecisionThresholdRow
                    label="Decline"
                    icon={<ShieldX className="size-4 text-[#dd3719]" />}
                    colorClassName="bg-[#f9d8d2] text-[#dd3719]"
                    text={`When score >= ${declineThresholdDraft || "0"}`}
                  />
                </div>
                <div className="flex items-center justify-end gap-3">
                  {decisionMessage ? (
                    <p className="text-[13px] text-slate-600">{decisionMessage}</p>
                  ) : null}
                  <Button
                    disabled={!isDraftIteration || updateDecisionThresholdsMutation.isPending}
                    onClick={handleSaveDecisionThresholds}
                    className="h-8 rounded-xl bg-[#1f4f96] px-3.5 text-[13px] shadow-none hover:bg-[#163f79]"
                  >
                    {updateDecisionThresholdsMutation.isPending ? "Saving..." : "Save"}
                  </Button>
                </div>
                <div className="space-y-4 border-t border-slate-200 pt-4">
                  <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
                    <div>
                      <h3 className="text-[15px] font-semibold text-slate-950">Decisions</h3>
                      <p className="text-[13px] text-slate-500">
                        Review decisions created for {statusLabel}.
                      </p>
                    </div>
                    <div className="relative w-full max-w-[360px]">
                      <Search className="pointer-events-none absolute left-4 top-1/2 size-4 -translate-y-1/2 text-slate-500" />
                      <Input
                        value={decisionSearch}
                        onChange={(event) => setDecisionSearch(event.target.value)}
                        placeholder="Search by object, type, outcome, or score"
                        className="h-10 rounded-xl border-slate-200 pl-10 text-[14px] shadow-none"
                      />
                    </div>
                  </div>
                  {scenarioDecisionsQuery.isLoading ? (
                    <div className="rounded-xl border border-slate-200 px-4 py-10 text-center text-[14px] text-slate-600">
                      Loading decisions...
                    </div>
                  ) : scenarioDecisionsQuery.isError ? (
                    <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-4 text-[14px] text-red-700">
                      {scenarioDecisionsQuery.error instanceof Error
                        ? scenarioDecisionsQuery.error.message
                        : "Failed to load decisions."}
                    </div>
                  ) : filteredScenarioDecisions.length === 0 ? (
                    <div className="rounded-xl border border-slate-200 px-4 py-10 text-center text-[14px] text-slate-500">
                      No decisions were found for this iteration.
                    </div>
                  ) : (
                    <div className="grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(320px,0.8fr)]">
                      <div className="overflow-hidden rounded-xl border border-slate-200">
                        <div className="overflow-x-auto">
                          <table className="min-w-full text-left">
                            <thead>
                              <tr className="border-b border-slate-200 bg-white text-[13px] font-semibold text-slate-950">
                                <th className="px-4 py-3">Object</th>
                                <th className="px-4 py-3">Outcome</th>
                                <th className="px-4 py-3 text-center">Score</th>
                                <th className="px-4 py-3">Created at</th>
                              </tr>
                            </thead>
                            <tbody>
                              {filteredScenarioDecisions.map((decision) => (
                                <tr
                                  key={decision.id}
                                  onClick={() => setSelectedDecisionId(decision.id)}
                                  className={cn(
                                    "cursor-pointer border-b border-slate-100 text-[14px] text-slate-950 last:border-b-0 hover:bg-slate-50",
                                    selectedDecision?.id === decision.id ? "bg-slate-50" : "bg-white"
                                  )}
                                >
                                  <td className="px-4 py-3">
                                    <div className="font-medium">{decision.object_id}</div>
                                    <div className="text-[12px] text-slate-500">
                                      {decision.object_type}
                                    </div>
                                  </td>
                                  <td className="px-4 py-3">
                                    <Badge
                                      className={cn(
                                        "rounded-full border px-2 py-0.5 text-[12px] font-medium tracking-normal normal-case",
                                        decisionOutcomeClasses(decision.outcome)
                                      )}
                                    >
                                      {decision.outcome}
                                    </Badge>
                                  </td>
                                  <td className="px-4 py-3 text-center">{decision.score}</td>
                                  <td className="px-4 py-3 text-[13px] text-slate-600">
                                    {formatDecisionTimestamp(decision.created_at)}
                                  </td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        </div>
                      </div>
                      <div className="rounded-xl border border-slate-200 bg-white p-4">
                        {selectedDecision ? (
                          <div className="space-y-4">
                            <div className="flex items-start justify-between gap-3">
                              <div>
                                <p className="text-[15px] font-semibold text-slate-950">
                                  {selectedDecision.object_id}
                                </p>
                                <p className="text-[13px] text-slate-500">
                                  {selectedDecision.object_type}
                                </p>
                              </div>
                              <Badge
                                className={cn(
                                  "rounded-full border px-2 py-0.5 text-[12px] font-medium tracking-normal normal-case",
                                  decisionOutcomeClasses(selectedDecision.outcome)
                                )}
                              >
                                {selectedDecision.outcome}
                              </Badge>
                            </div>
                            <div className="grid gap-3 sm:grid-cols-2">
                              <div className="rounded-xl border border-slate-200 px-3 py-3">
                                <p className="text-[12px] text-slate-500">Score</p>
                                <p className="mt-1 text-[16px] font-semibold text-slate-950">
                                  {selectedDecision.score}
                                </p>
                              </div>
                              <div className="rounded-xl border border-slate-200 px-3 py-3">
                                <p className="text-[12px] text-slate-500">Triggered</p>
                                <p className="mt-1 text-[16px] font-semibold text-slate-950">
                                  {selectedDecision.triggered ? "Yes" : "No"}
                                </p>
                              </div>
                            </div>
                            <div className="rounded-xl border border-slate-200 px-3 py-3">
                              <p className="text-[12px] text-slate-500">Created at</p>
                              <p className="mt-1 text-[14px] text-slate-950">
                                {formatDecisionTimestamp(selectedDecision.created_at)}
                              </p>
                            </div>
                            <div className="space-y-2">
                              <div className="flex items-center justify-between">
                                <p className="text-[13px] font-medium text-slate-950">
                                  Rule executions
                                </p>
                                {decisionDetailQuery.isLoading &&
                                selectedDecisionId === selectedDecision.id ? (
                                  <span className="text-[12px] text-slate-500">Loading...</span>
                                ) : null}
                              </div>
                              {decisionDetailQuery.isError &&
                              selectedDecisionId === selectedDecision.id ? (
                                <div className="rounded-xl border border-red-200 bg-red-50 px-3 py-3 text-[13px] text-red-700">
                                  {decisionDetailQuery.error instanceof Error
                                    ? decisionDetailQuery.error.message
                                    : "Failed to load decision details."}
                                </div>
                              ) : null}
                              {selectedDecisionId === selectedDecision.id &&
                              decisionDetailQuery.data?.rule_executions?.length ? (
                                <div className="space-y-2">
                                  {decisionDetailQuery.data.rule_executions.map((ruleExecution) => (
                                    <div
                                      key={ruleExecution.id}
                                      className="rounded-xl border border-slate-200 px-3 py-3"
                                    >
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
                              ) : selectedDecisionId === selectedDecision.id &&
                                !decisionDetailQuery.isLoading &&
                                !decisionDetailQuery.isError ? (
                                <div className="rounded-xl border border-slate-200 px-3 py-3 text-[13px] text-slate-500">
                                  No rule executions were returned for this decision.
                                </div>
                              ) : null}
                            </div>
                          </div>
                        ) : null}
                      </div>
                    </div>
                  )}
                </div>
              </div>
              ) : null}
            </CardContent>
          </Card>
        ) : null}
      </div>

      <CommitModal
        isOpen={commitOpen}
        iterationLabel={statusLabel}
        isSubmitting={commitIterationMutation.isPending}
        onClose={() => setCommitOpen(false)}
        onConfirm={() => commitIterationMutation.mutate()}
      />

      <PublishModal
        isOpen={publishOpen}
        iterationLabel={statusLabel}
        isSubmitting={publishIterationMutation.isPending}
        onClose={() => setPublishOpen(false)}
        onConfirm={() => publishIterationMutation.mutate()}
      />

      <DeactivateModal
        isOpen={deactivateOpen}
        confirmStop={confirmStop}
        confirmImmediate={confirmImmediate}
        isSubmitting={deactivateIterationMutation.isPending}
        setConfirmStop={setConfirmStop}
        setConfirmImmediate={setConfirmImmediate}
        onConfirm={() => deactivateIterationMutation.mutate()}
        onClose={() => {
          if (deactivateIterationMutation.isPending) {
            return;
          }
          setDeactivateOpen(false);
          setConfirmStop(false);
          setConfirmImmediate(false);
        }}
      />
    </>
  );
}
