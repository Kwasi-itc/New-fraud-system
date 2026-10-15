"use client";

import { useDecisionDisplay, displayValue } from "@/components/detection/decision-display";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, createContext, useContext, type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ArrowRight,
  ClipboardCheck,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Copy,
  Eye,
  Filter,
  Info,
  Lightbulb,
  ListChecks,
  Loader2,
  Pencil,
  Plus,
  Search,
  Sliders,
  SquarePen,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  type CustomList,
  type Decision,
  type Scenario as DecisionEngineScenario,
  decisionEngineApi,
} from "@/lib/decision-engine-api";
import { useAssembledDataModelQuery } from "@/lib/data-model-query";
import { markFraudManagerNavigationTourCompleted } from "@/lib/fraud-manager-onboarding";
import { useToastStore } from "@/stores/toast-store";
import { cn } from "@/lib/utils";
import { CreateDecisionCaseModal, SelectPageCheckbox, useDecisionSelection } from "@/components/cases/decision-selection";

const DetectionIdentity = createContext({ tenant: "", session: "" });

const tabs = [
  "Scenarios",
  "Lists",
  // "Analytics",
  "Decisions",
] as const;

type DetectionTab = (typeof tabs)[number];

function parseDetectionTab(value: string | null): DetectionTab {
  switch (value) {
    case "Lists":
      return "Lists";
    case "Decisions":
      return "Decisions";
    default:
      return "Scenarios";
  }
}

type DetectionScenario = {
  id: string;
  status: string;
  name: string;
  description: string;
  trigger: string;
  created: string;
  liveIterationId?: string | null;
};

type DetectionList = {
  id: string;
  name: string;
  description: string;
  type: string;
  count: string;
};

const scenarioQueryKey = (tenantId: string) =>
  ["decision-engine", "scenarios", tenantId] as const;

function formatScenarioDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "short",
    timeStyle: "short",
  }).format(date);
}

function adaptScenario(item: DecisionEngineScenario): DetectionScenario {
  return {
    id: item.id,
    status: item.live_iteration_id ? "Live" : "Draft",
    name: item.name,
    description: item.description || "No description provided",
    trigger: item.trigger_object_type,
    created: formatScenarioDate(item.created_at),
    liveIterationId: item.live_iteration_id ?? null,
  };
}

function formatListCount(count: number) {
  return `${count} value${count === 1 ? "" : "s"}`;
}

function formatListKind(kind: string) {
  switch (kind) {
    case "ip_subnet":
      return "IP addresses and subnets";
    case "uuid":
      return "UUID values";
    default:
      return "Generic text";
  }
}

function adaptCustomLists(items: CustomList[], entryCounts: Map<string, number>): DetectionList[] {
  return items.map((item) => ({
    id: item.id,
    name: item.name,
    description: item.description || "No description provided",
    type: formatListKind(item.kind),
    count: formatListCount(entryCounts.get(item.id) ?? 0),
  }));
}

const decisionOutcomes = [
  { label: "Approve", color: "bg-emerald-300" },
  { label: "Review", color: "bg-amber-300" },
  { label: "Block and Review", color: "bg-orange-300" },
  { label: "Decline", color: "bg-rose-300" },
];

const DEFAULT_DECISIONS_PAGE_SIZE = 25;
const DECISIONS_PAGE_SIZE_OPTIONS = [10, 25, 50, 100] as const;

type DecisionPaginationToken =
  | { type: "page"; page: number }
  | { type: "ellipsis"; key: string };

function buildDecisionPaginationTokens(
  currentPage: number,
  totalPages: number
): DecisionPaginationToken[] {
  const pages = new Set<number>([1, currentPage - 1, currentPage, currentPage + 1, totalPages]);
  const visiblePages = Array.from(pages)
    .filter((page) => page >= 1 && page <= totalPages)
    .sort((left, right) => left - right);

  const tokens: DecisionPaginationToken[] = [];
  for (let index = 0; index < visiblePages.length; index += 1) {
    const page = visiblePages[index];
    const previousPage = visiblePages[index - 1];
    if (previousPage != null && page-previousPage > 1) {
      tokens.push({ type: "ellipsis", key: `gap-${previousPage}-${page}` });
    }
    tokens.push({ type: "page", page });
  }
  return tokens;
}

function formatDecisionDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

function formatDecisionOutcome(value: string) {
  switch (value) {
    case "approve":
      return "Approve";
    case "block_and_review":
      return "Block and Review";
    case "decline":
      return "Decline";
    case "review":
      return "Review";
    default:
      return value;
  }
}

function outcomeBadgeClass(value: string) {
  switch (value) {
    case "approve":
      return "bg-emerald-100 text-emerald-700";
    case "block_and_review":
      return "bg-orange-100 text-orange-700";
    case "decline":
      return "bg-rose-100 text-rose-700";
    default:
      return "bg-amber-100 text-amber-700";
  }
}

function outcomeFilterToApiValue(value: string) {
  switch (value) {
    case "Approve":
      return "approve";
    case "Block and Review":
      return "block_and_review";
    case "Decline":
      return "decline";
    case "Review":
      return "review";
    default:
      return value.toLowerCase().replace(/\s+/g, "_");
  }
}

function TabButton({
  tab,
  activeTab,
  onClick,
}: {
  tab: DetectionTab;
  activeTab: DetectionTab;
  onClick: (tab: DetectionTab) => void;
}) {
  return (
    <button
      type="button"
      onClick={() => onClick(tab)}
      className={cn(
        "rounded-xl px-3.5 py-2 text-[15px] font-medium transition",
        activeTab === tab
          ? "bg-[#1f4f96] text-white"
          : "text-[#1f4f96] hover:bg-blue-50"
      )}
    >
      {tab}
    </button>
  );
}

function TablePill({ children }: { children: ReactNode }) {
  return (
    <Badge className="rounded-full border-[#2d63b8] bg-white px-2.5 py-0.5 text-[13px] font-medium tracking-normal normal-case text-[#2d63b8]">
      {children}
    </Badge>
  );
}

function RowIconButton({
  children,
  label,
  disabled = false,
  onClick,
}: {
  children: ReactNode;
  label: string;
  disabled?: boolean;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "inline-flex size-8 items-center justify-center rounded-xl border bg-white transition",
        disabled
          ? "cursor-not-allowed border-slate-200 text-slate-300"
          : "border-slate-200 text-slate-800 hover:bg-slate-50"
      )}
    >
      {children}
    </button>
  );
}

function PageHeader({
  activeTab,
  onTabChange,
  action,
}: {
  activeTab: DetectionTab;
  onTabChange: (tab: DetectionTab) => void;
  action: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
      <div className="space-y-4">
        <h1 className="text-[1.9rem] font-semibold tracking-tight text-slate-950">
          Detection
        </h1>
        <div className="flex flex-wrap gap-2">
          {tabs.map((tab) => (
            <TabButton
              key={tab}
              tab={tab}
              activeTab={activeTab}
              onClick={onTabChange}
            />
          ))}
        </div>
      </div>
      {action}
    </div>
  );
}

function InfoBanner() {
  return (
    <Card className="rounded-xl border border-slate-200 shadow-none">
      <CardContent className="p-0">
        <div className="flex items-center gap-2.5 px-4 py-2.5 text-[14px] leading-6 text-slate-900">
          <Lightbulb className="size-4 shrink-0 text-slate-700" />
          <p>
            A scenario identifies a certain risk type, based on specific business rules, for a specific trigger event.
          </p>
        </div>
      </CardContent>
    </Card>
  );
}

function NavigationGuideBanner({
  step,
  onContinue,
  onSkip,
}: {
  step: "lists" | "decisions";
  onContinue: () => void;
  onSkip: () => void;
}) {
  const isListsStep = step === "lists";
  const Icon = isListsStep ? ListChecks : ClipboardCheck;

  return (
    <Card className="rounded-2xl border border-blue-200 bg-blue-50/70 shadow-none">
      <CardContent className="flex flex-col gap-4 p-5 lg:flex-row lg:items-center">
        <span className="flex size-11 shrink-0 items-center justify-center rounded-2xl bg-[#1f4f96] text-white">
          <Icon className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-xs font-semibold uppercase tracking-[0.13em] text-[#1f4f96]">
            Workspace tour · {isListsStep ? "1 of 2" : "2 of 2"}
          </p>
          <h2 className="mt-1 text-lg font-semibold text-slate-950">
            {isListsStep ? "Keep reusable values in Lists" : "Review evaluated activity in Decisions"}
          </h2>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-slate-600">
            {isListsStep
              ? "Create watchlists, blocklists, allowlists, or IP subnet lists once, then reference them from multiple rules. Use New List above whenever you need one."
              : "This is where fraud managers inspect outcomes, scores, trigger objects, and the individual rules that hit. It is ready even when no decisions have arrived yet."}
          </p>
        </div>
        <div className="flex flex-wrap gap-2 lg:justify-end">
          <Button
            variant="outline"
            size="sm"
            onClick={onSkip}
            className="rounded-xl bg-white shadow-none"
          >
            {isListsStep ? "Skip tour" : "Back to dashboard"}
          </Button>
          <Button
            variant="accent"
            size="sm"
            onClick={onContinue}
            className="rounded-xl shadow-none"
          >
            {isListsStep ? "Show me Decisions" : "Finish tour"}
            <ArrowRight className="size-4" />
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function ScenariosTable({
  scenarios,
  onEdit,
  onDuplicate,
  onArchive,
}: {
  scenarios: DetectionScenario[];
  onEdit: (scenario: DetectionScenario) => void;
  onDuplicate: (scenario: DetectionScenario) => void;
  onArchive: (scenario: DetectionScenario) => void;
}) {
  if (scenarios.length === 0) {
    return (
      <Card className="rounded-xl border border-slate-200 shadow-none">
        <CardContent className="p-6 text-sm text-slate-600">
          No scenarios created yet. Create a scenario before decisions can be generated.
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="overflow-hidden rounded-xl border border-slate-200 shadow-none">
      <CardContent className="p-0">
        <div className="overflow-x-auto">
          <table className="min-w-full text-left">
            <thead>
              <tr className="border-b border-slate-200 bg-white text-[13px] font-semibold text-slate-950">
                <th className="w-[120px] px-4 py-3">Status</th>
                <th className="w-[320px] px-4 py-3">Name of the scenario</th>
                <th className="w-[300px] px-4 py-3">Description</th>
                <th className="w-[160px] px-4 py-3">Trigger</th>
                <th className="w-[200px] px-4 py-3">Created</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {scenarios.map((scenario) => (
                <tr
                  key={`${scenario.name}-${scenario.created}`}
                  className="border-b border-slate-100 text-[14px] text-slate-950 last:border-b-0"
                >
                  <td className="px-4 py-3">
                    <TablePill>{scenario.status}</TablePill>
                  </td>
                  <td className="px-4 py-3 text-[15px] font-medium">
                    <Link
                      href={`/detection/${scenario.id}`}
                      className="hover:text-[#1f4f96]"
                    >
                      {scenario.name}
                    </Link>
                  </td>
                  <td className="max-w-[300px] px-4 py-3 text-[14px] text-slate-900">
                    <div className="truncate">{scenario.description}</div>
                  </td>
                  <td className="px-4 py-3">
                    <TablePill>{scenario.trigger}</TablePill>
                  </td>
                  <td className="px-4 py-3 text-[14px] text-slate-900">
                    {scenario.created}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-1.5">
                      <RowIconButton
                        label={`Edit ${scenario.name}`}
                        onClick={() => onEdit(scenario)}
                      >
                        <Pencil className="size-4" />
                      </RowIconButton>
                      <RowIconButton
                        label={`Duplicate ${scenario.name}`}
                        onClick={() => onDuplicate(scenario)}
                      >
                        <Copy className="size-4" />
                      </RowIconButton>
                      <RowIconButton
                        label={`Archive ${scenario.name}`}
                        onClick={() => onArchive(scenario)}
                      >
                        <Archive className="size-4" />
                      </RowIconButton>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  );
}

function ListsTable({ lists }: { lists: DetectionList[] }) {
  if (lists.length === 0) {
    return (
      <Card className="rounded-xl border border-slate-200 shadow-none">
        <CardContent className="p-6 text-sm text-slate-600">
          No list items yet. Create a list to get started.
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="overflow-hidden rounded-xl border border-slate-200 shadow-none">
      <CardContent className="p-0">
        <div className="overflow-x-auto">
          <table className="min-w-full text-left">
            <thead>
              <tr className="border-b border-slate-200 bg-white text-[13px] font-semibold text-slate-950">
                <th className="px-4 py-3">Name</th>
                <th className="px-4 py-3">Description</th>
                <th className="px-4 py-3">Type of list</th>
                <th className="px-4 py-3 text-center">Values count</th>
              </tr>
            </thead>
            <tbody>
              {lists.map((item) => (
                <tr
                  key={item.name}
                  className="border-b border-slate-100 text-[14px] text-slate-950 last:border-b-0"
                >
                  <td className="px-4 py-3 text-[15px] font-medium">
                    <Link href={`/detection/lists/${item.id}`} className="hover:text-[#1f4f96]">
                      {item.name}
                    </Link>
                  </td>
                  <td className="px-4 py-3 text-[14px]">{item.description}</td>
                  <td className="px-4 py-3">
                    <TablePill>{item.type}</TablePill>
                  </td>
                  <td className="px-4 py-3 text-center text-[14px]">
                    {item.count}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  );
}

function AnalyticsView() {
  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
        <div className="flex flex-col gap-2.5">
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              className="inline-flex h-9 items-center gap-2.5 rounded-xl bg-[#1f4f96] px-3.5 text-[14px] font-medium text-white"
            >
              German - Validate card payouts
              <ChevronDown className="size-4" />
            </button>
            <button
              type="button"
              className="inline-flex h-9 items-center rounded-xl bg-white px-3.5 text-[14px] text-[#1f4f96]"
            >
              Last 30 days
            </button>
            <button
              type="button"
              className="inline-flex h-9 items-center gap-2.5 rounded-xl bg-white px-3.5 text-[14px] text-[#1f4f96]"
            >
              Add period to compare
              <span className="text-lg leading-none text-[#1f4f96]">×</span>
            </button>
          </div>
          <Button className="h-9 w-fit rounded-xl bg-[#6b7280] px-6 text-[14px] shadow-none hover:bg-[#5b6473]">
            Apply
          </Button>
        </div>

        <button
          type="button"
          className="inline-flex items-center gap-2 self-start text-[14px] text-slate-700 hover:text-slate-950"
        >
          <SquarePen className="size-4" />
          View investigations analytics
        </button>
      </div>

      <div className="grid gap-3 xl:grid-cols-[1.65fr_0.85fr]">
        <Card className="rounded-xl border border-slate-200 shadow-none">
          <CardContent className="space-y-3 p-3.5">
            <div className="flex items-center justify-between">
              <p className="text-[16px] font-semibold text-slate-950">Decisions</p>
              <Button variant="outline" size="sm" className="h-8 rounded-xl px-3 text-[13px] shadow-none">
                Export
              </Button>
            </div>
            <div className="rounded-xl border border-slate-200 p-3">
              <div className="flex flex-wrap items-center justify-between gap-3 text-[14px]">
                <div className="flex items-center gap-2">
                  <span>Count:</span>
                  <button
                    type="button"
                    className="inline-flex size-6 items-center justify-center rounded-lg border border-slate-200 text-[13px] text-slate-400"
                  >
                    %
                  </button>
                  <button
                    type="button"
                    className="inline-flex size-6 items-center justify-center rounded-lg border border-[#2d63b8] text-[13px] text-[#2d63b8]"
                  >
                    #
                  </button>
                </div>
                <div className="flex items-center gap-2">
                  <span>Scale:</span>
                  <TablePill>Linear</TablePill>
                  <button
                    type="button"
                    className="rounded-lg border border-slate-200 px-2.5 py-1 text-[13px]"
                  >
                    Log
                  </button>
                </div>
              </div>

              <div className="mt-3 rounded-xl border border-transparent px-2 pb-2 pt-1">
                <div className="relative h-[330px]">
                  <div className="absolute inset-0">
                    {Array.from({ length: 10 }).map((_, index) => (
                      <div
                        key={index}
                        className="absolute left-0 right-0 border-t border-dashed border-slate-200"
                        style={{ top: `${index * 11}%` }}
                      />
                    ))}
                  </div>
                  <div className="absolute bottom-8 left-0 right-0 flex justify-between px-8 text-[13px] text-slate-500">
                    <span>May 11</span>
                    <span>Jun 10</span>
                  </div>
                  <div className="absolute bottom-0 left-0 right-0 flex items-center justify-center gap-6 text-[13px] text-slate-700">
                    {decisionOutcomes.map((item) => (
                      <div key={item.label} className="flex items-center gap-2">
                        <span className={cn("size-3.5 rounded-sm", item.color)} />
                        <span>{item.label}</span>
                        <Eye className="size-4 text-[#2d63b8]" />
                      </div>
                    ))}
                  </div>
                  <div className="absolute bottom-8 right-0 flex gap-1.5 text-[13px]">
                    <button
                      type="button"
                      className="rounded-lg border border-[#9ab5e0] px-2.5 py-1 text-[#2d63b8]"
                    >
                      Day
                    </button>
                    <button
                      type="button"
                      className="rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1 text-slate-400"
                    >
                      Week
                    </button>
                    <button
                      type="button"
                      className="rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1 text-slate-400"
                    >
                      Month
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card className="rounded-xl border border-slate-200 shadow-none">
          <CardContent className="space-y-3 p-3.5">
            <div className="flex items-center justify-between">
              <p className="text-[16px] font-semibold text-slate-950">
                Decisions score distribution
              </p>
              <Button variant="outline" size="sm" className="h-8 rounded-xl px-3 text-[13px] shadow-none">
                Export
              </Button>
            </div>
            <div className="rounded-xl border border-slate-200 p-3">
              <div className="mb-3 flex items-center justify-between">
                <p className="text-[14px] text-slate-950">Count in %</p>
                <button
                  type="button"
                  className="rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1 text-[13px] text-slate-300"
                >
                  Zoom out
                </button>
              </div>
              <div className="surface-grid h-[300px] rounded-xl border border-dashed border-slate-200" />
            </div>
          </CardContent>
        </Card>
      </div>

      {[
        {
          title: "Decision rules",
          columns: ["Rule", "# hits", "% hits", "% false pos. among hits", "# distinct accounts", "Repeat alerts rate"],
        },
        {
          title: "Decision outcome by rule with hit",
          columns: [],
          legend: true,
        },
        {
          title: "Screening hits",
          columns: ["Rule", "# executions", "# hits", "% hits", "Avg hits per screening"],
        },
      ].map((section) => (
        <Card
          key={section.title}
          className="rounded-xl border border-slate-200 shadow-none"
        >
          <CardContent className="space-y-3 p-3.5">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <p className="text-[16px] font-semibold text-slate-950">
                  {section.title}
                </p>
                {section.title === "Decision outcome by rule with hit" ? (
                  <Info className="size-4 text-[#2d63b8]" />
                ) : null}
              </div>
              {section.legend ? (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 rounded-xl px-3 text-[13px] text-slate-300 shadow-none"
                >
                  Export
                </Button>
              ) : null}
            </div>
            <div className="overflow-hidden rounded-xl border border-slate-200">
              {section.columns.length > 0 ? (
                <div className="grid min-h-[42px] grid-cols-1 gap-0 border-b border-slate-200 bg-white text-[13px] font-semibold text-slate-950 xl:grid-cols-[1.3fr_repeat(5,1fr)]">
                  {section.columns.map((column) => (
                    <div
                      key={column}
                      className="border-r border-slate-200 px-3 py-2.5 last:border-r-0"
                    >
                      {column}
                    </div>
                  ))}
                </div>
              ) : null}
              <div className="flex min-h-[130px] items-center justify-center bg-white text-[18px] text-slate-300">
                No data available
              </div>
            </div>
            {section.legend ? (
              <div className="flex items-center justify-center gap-6 text-[13px] text-slate-500">
                {decisionOutcomes.map((item) => (
                  <div key={item.label} className="flex items-center gap-2">
                    <span className={cn("size-3.5 rounded-sm", item.color)} />
                    <span>{item.label}</span>
                  </div>
                ))}
              </div>
            ) : null}
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

function formatFieldLabel(name: string, alias?: string): string {
  if (alias && alias.trim()) return alias.trim();
  return name
    .replace(/^col_/, "")
    .replace(/_/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

function LiveDecisionsView({
  tenantId,
  scenarios,
  initialOutcome,
}: {
  tenantId: string;
  scenarios: DetectionScenario[];
  initialOutcome?: string;
}) {
  const queryClient = useQueryClient();
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [columnPickerOpen, setColumnPickerOpen] = useState(false);
  const [activeFilterMenu, setActiveFilterMenu] = useState<string | null>(null);
  const [filterSearchQuery, setFilterSearchQuery] = useState("");
  const [columnSearchQuery, setColumnSearchQuery] = useState("");

  const filtersContainerRef = useRef<HTMLDivElement>(null);
  const columnPickerContainerRef = useRef<HTMLDivElement>(null);
  const activeFilterContainerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClickOutside(event: MouseEvent | TouchEvent) {
      const target = event.target as Node;
      if (filtersOpen && filtersContainerRef.current && !filtersContainerRef.current.contains(target)) {
        setFiltersOpen(false);
        setFilterSearchQuery("");
      }
      if (columnPickerOpen && columnPickerContainerRef.current && !columnPickerContainerRef.current.contains(target)) {
        setColumnPickerOpen(false);
        setColumnSearchQuery("");
      }
      if (activeFilterMenu && activeFilterContainerRef.current && !activeFilterContainerRef.current.contains(target)) {
        setActiveFilterMenu(null);
      }
    }

    document.addEventListener("mousedown", handleClickOutside);
    document.addEventListener("touchstart", handleClickOutside);
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
      document.removeEventListener("touchstart", handleClickOutside);
    };
  }, [filtersOpen, columnPickerOpen, activeFilterMenu]);

  const [searchTerm, setSearchTerm] = useState("");
  const [debouncedSearchTerm, setDebouncedSearchTerm] = useState("");
  const [pageOffset, setPageOffset] = useState(0);
  const [pageSize, setPageSize] = useState(DEFAULT_DECISIONS_PAGE_SIZE);
  const [selectedFilters, setSelectedFilters] = useState<Array<{ type: string; value: string }>>(
    () =>
      initialOutcome
        ? [{ type: "Outcome", value: formatDecisionOutcome(initialOutcome) }]
        : []
  );
  const selectedScenarioFilter = selectedFilters.find((item) => item.type === "Scenario")?.value;
  const selectedObjectTypeFilter = selectedFilters.find(
    (item) => item.type === "Trigger object"
  )?.value;
  const selectedObjectIDFilter = selectedFilters.find((item) => item.type === "Object ID")?.value;
  const selectedOutcomeFilter = selectedFilters.find((item) => item.type === "Outcome")?.value;
  const selectedScoreMinFilter = selectedFilters.find((item) => item.type === "Score (min)")?.value;
  const selectedScoreMaxFilter = selectedFilters.find((item) => item.type === "Score (max)")?.value;

  const assembledModelQuery = useAssembledDataModelQuery(tenantId);
  const allModelTables = useMemo(() => {
    const tables = assembledModelQuery.data?.data_model?.tables;
    if (!tables) return [];
    return Object.values(tables)
      .filter((t) => !t.archived)
      .map((t) => t.name)
      .sort();
  }, [assembledModelQuery.data?.data_model?.tables]);

  // Dynamically resolve default trigger table from tenant scenarios or data model (no hardcoded table names)
  const defaultModelTable = useMemo(() => {
    if (selectedScenarioFilter) {
      const matched = scenarios.find((s) => s.name === selectedScenarioFilter);
      if (matched?.trigger && allModelTables.includes(matched.trigger)) {
        return matched.trigger;
      }
    }
    const scenarioWithTrigger = scenarios.find((s) => s.trigger && allModelTables.includes(s.trigger));
    if (scenarioWithTrigger?.trigger) {
      return scenarioWithTrigger.trigger;
    }
    return allModelTables[0] || "";
  }, [allModelTables, scenarios, selectedScenarioFilter]);

  const scenarioNameById = useMemo(
    () => new Map(scenarios.map((scenario) => [scenario.id, scenario.name])),
    [scenarios]
  );
  const scenarioIdByName = useMemo(
    () => new Map(scenarios.map((scenario) => [scenario.name, scenario.id])),
    [scenarios]
  );

  useEffect(() => {
    const timeoutId = window.setTimeout(() => {
      setDebouncedSearchTerm(searchTerm.trim());
    }, 300);
    return () => window.clearTimeout(timeoutId);
  }, [searchTerm]);

  const decisionListFilters = useMemo(
    () => ({
      scenario_id: scenarioIdByName.get(selectedScenarioFilter ?? "") ?? undefined,
      object_type: selectedObjectTypeFilter || undefined,
      object_id: selectedObjectIDFilter || undefined,
      outcome: selectedOutcomeFilter
        ? outcomeFilterToApiValue(selectedOutcomeFilter)
        : undefined,
      search: debouncedSearchTerm || undefined,
      min_score: selectedScoreMinFilter && !isNaN(Number(selectedScoreMinFilter)) ? Number(selectedScoreMinFilter) : undefined,
      max_score: selectedScoreMaxFilter && !isNaN(Number(selectedScoreMaxFilter)) ? Number(selectedScoreMaxFilter) : undefined,
    }),
    [
      debouncedSearchTerm,
      scenarioIdByName,
      selectedObjectIDFilter,
      selectedObjectTypeFilter,
      selectedOutcomeFilter,
      selectedScenarioFilter,
      selectedScoreMinFilter,
      selectedScoreMaxFilter,
    ]
  );
  const decisionsQueryKey = useMemo(
    () => [
      "decision-engine",
      "decisions",
      tenantId,
      pageSize,
      pageOffset,
      decisionListFilters.scenario_id ?? "",
      decisionListFilters.object_type ?? "",
      decisionListFilters.object_id ?? "",
      decisionListFilters.outcome ?? "",
      decisionListFilters.search ?? "",
      decisionListFilters.min_score ?? "",
      decisionListFilters.max_score ?? "",
    ],
    [pageOffset, pageSize, tenantId, decisionListFilters]
  );
  const decisionsQuery = useQuery({
    queryKey: decisionsQueryKey,
    queryFn: () =>
      decisionEngineApi.listDecisions(tenantId, {
        ...decisionListFilters,
        limit: pageSize,
        offset: pageOffset,
      }),
    enabled: Boolean(tenantId),
    placeholderData: (previous) => previous,
  });
  const decisionsCountQuery = useQuery({
    queryKey: [
      "decision-engine",
      "decisions-count",
      tenantId,
      decisionListFilters.scenario_id ?? "",
      decisionListFilters.object_type ?? "",
      decisionListFilters.object_id ?? "",
      decisionListFilters.outcome ?? "",
      decisionListFilters.search ?? "",
      decisionListFilters.min_score ?? "",
      decisionListFilters.max_score ?? "",
    ],
    queryFn: () => decisionEngineApi.countDecisions(tenantId, decisionListFilters),
    enabled: Boolean(tenantId),
  });
  const iterationQueries = useQueries({
    queries: scenarios
      .filter((scenario) => scenario.liveIterationId)
      .map((scenario) => ({
        queryKey: ["decision-engine", "iterations", tenantId, scenario.id],
        queryFn: () => decisionEngineApi.listIterations(tenantId, scenario.id),
        enabled: Boolean(tenantId),
      })),
  });
  const liveVersionByScenarioId = useMemo(() => {
    const entries = scenarios
      .filter((scenario) => scenario.liveIterationId)
      .map((scenario, index) => {
        const iterations = iterationQueries[index]?.data?.iterations ?? [];
        const liveIteration = iterations.find(
          (iteration) => iteration.id === scenario.liveIterationId
        );
        return [scenario.id, liveIteration ? `V${liveIteration.version}` : "Live"] as const;
      });

    return new Map(entries);
  }, [iterationQueries, scenarios]);
  const scenarioFilterOptions = useMemo(
    () => scenarios.map((scenario) => scenario.name).sort((a, b) => a.localeCompare(b)),
    [scenarios]
  );
  const decisions = decisionsQuery.data?.decisions ?? [];
  const { session } = useContext(DetectionIdentity);

  // Available trigger object tables in the loaded decisions and data model
  const availableTables = useMemo(() => {
    const set = new Set<string>();
    for (const d of decisions) {
      if (d.object_type) set.add(d.object_type);
    }
    for (const t of allModelTables) {
      set.add(t);
    }
    return Array.from(set).sort();
  }, [decisions, allModelTables]);

  const decisionsFirstObjectType = useMemo(() => {
    return decisions.find((d) => Boolean(d.object_type))?.object_type;
  }, [decisions]);

  const effectiveDisplayTable =
    selectedObjectTypeFilter || decisionsFirstObjectType || defaultModelTable;

  const tableDef = useMemo(() => {
    if (!effectiveDisplayTable) return undefined;
    const tables = assembledModelQuery.data?.data_model?.tables;
    return (
      tables?.[effectiveDisplayTable] ??
      Object.values(tables ?? {}).find((t) => t.name === effectiveDisplayTable)
    );
  }, [assembledModelQuery.data?.data_model?.tables, effectiveDisplayTable]);

  const captionHeaderLabel = useMemo(() => {
    if (!effectiveDisplayTable) return "Record ID";
    if (!tableDef || !tableDef.caption_field) {
      return "Record ID";
    }
    const fields = tableDef.fields ?? {};
    const fieldDef =
      fields[tableDef.caption_field] ??
      Object.values(fields).find(
        (f) => f.name === tableDef.caption_field || f.id === tableDef.caption_field
      );
    if (!fieldDef) {
      return tableDef.caption_field;
    }
    return fieldDef.alias && fieldDef.alias !== fieldDef.name
      ? `${fieldDef.alias} (${fieldDef.name})`
      : fieldDef.name;
  }, [tableDef, effectiveDisplayTable]);

  // Optional live fetching of record display values
  const [fetchRecordFields, setFetchRecordFields] = useState<boolean>(false);
  const display = useDecisionDisplay(tenantId, session, decisions, effectiveDisplayTable, fetchRecordFields);

  // Column visibility selection (per table)
  const [hiddenColumnIds, setHiddenColumnIds] = useState<Record<string, string[]>>({});

  const availableColumns = useMemo(() => {
    if (!effectiveDisplayTable) return [];
    return display.allColumns[effectiveDisplayTable] ?? display.columns ?? [];
  }, [display.allColumns, display.columns, effectiveDisplayTable]);

  const getFilterTypeLabel = useCallback((filterType: string) => filterType, []);
  const getFilterValueLabel = useCallback((_filterType: string, rawValue: string) => rawValue || "Any", []);

  const hiddenForActiveTable = useMemo(() => {
    return hiddenColumnIds[effectiveDisplayTable] ?? [];
  }, [hiddenColumnIds, effectiveDisplayTable]);

  const visibleColumns = useMemo(() => {
    if (!fetchRecordFields) return [];
    return availableColumns.filter((col) => !hiddenForActiveTable.includes(col.id));
  }, [fetchRecordFields, availableColumns, hiddenForActiveTable]);

  const outcomeFilterItems = ["Approve", "Block and Review", "Decline", "Review"];
  const filterItems = ["Scenario", "Trigger object", "Object ID", "Outcome", "Score (min)", "Score (max)"];

  const toggleColumnVisibility = (columnId: string) => {
    setHiddenColumnIds((prev) => {
      const current = prev[effectiveDisplayTable] ?? [];
      const updated = current.includes(columnId)
        ? current.filter((id) => id !== columnId)
        : [...current, columnId];
      return { ...prev, [effectiveDisplayTable]: updated };
    });
  };

  const showAllColumns = () => {
    setHiddenColumnIds((prev) => ({ ...prev, [effectiveDisplayTable]: [] }));
  };

  const hideAllColumns = () => {
    setHiddenColumnIds((prev) => ({
      ...prev,
      [effectiveDisplayTable]: availableColumns.map((c) => c.id),
    }));
  };

  const selection = useDecisionSelection(tenantId, session, decisions, JSON.stringify(decisionsQueryKey), decisionsQuery.isFetching);
  const [creating, setCreating] = useState<{ scope: string; decisions: Decision[] } | null>(null);
  const selectionScope = JSON.stringify([tenantId, session, decisionsQueryKey]);
  const pagination = decisionsQuery.data?.pagination;
  const isInitialDecisionLoad = decisionsQuery.isLoading && !decisionsQuery.data;
  const isDecisionListRefreshing = decisionsQuery.isFetching && !isInitialDecisionLoad;
  const isDecisionCountRefreshing =
    decisionsCountQuery.isFetching && !decisionsCountQuery.isLoading;
  const canGoPrevious = pageOffset > 0;
  const totalRecords = decisionsCountQuery.data?.count;
  const totalPages = totalRecords != null ? Math.ceil(totalRecords / pageSize) : 0;
  const canGoNext = pagination?.has_more ?? false;
  const currentPage = Math.floor(pageOffset / pageSize) + 1;
  const paginationTokens = totalPages > 0 ? buildDecisionPaginationTokens(currentPage, totalPages) : [];
  const pageRangeLabel =
    decisions.length > 0
      ? `${pageOffset + 1}-${pageOffset + decisions.length}`
      : "0-0";

  useEffect(() => {
    if (!tenantId || !pagination?.has_more) {
      return;
    }

    const nextOffset = pageOffset + pageSize;
    void queryClient.prefetchQuery({
      queryKey: [
        "decision-engine",
        "decisions",
        tenantId,
        pageSize,
        nextOffset,
        decisionListFilters.scenario_id ?? "",
        decisionListFilters.object_type ?? "",
        decisionListFilters.object_id ?? "",
        decisionListFilters.outcome ?? "",
        decisionListFilters.search ?? "",
      ],
      queryFn: () =>
        decisionEngineApi.listDecisions(tenantId, {
          ...decisionListFilters,
          limit: pageSize,
          offset: nextOffset,
        }),
      staleTime: 30_000,
    });
  }, [decisionListFilters, pageOffset, pageSize, pagination?.has_more, queryClient, tenantId]);

  function upsertFilter(type: string, value: string) {
    setPageOffset(0);
    setSelectedFilters((current) => {
      const existing = current.find((item) => item.type === type);
      if (!existing) {
        return [...current, { type, value }];
      }

      return current.map((item) => (item.type === type ? { ...item, value } : item));
    });
  }

  function removeFilter(type: string) {
    setPageOffset(0);
    setSelectedFilters((current) => current.filter((item) => item.type !== type));
    setActiveFilterMenu((current) => (current === type ? null : current));
  }

  function availableFilterItems() {
    return filterItems.filter(
      (item) => !selectedFilters.some((selected) => selected.type === item)
    );
  }

  return (
    <>
      <div className="space-y-4 min-w-0 max-w-full">
        {/* Top Control Toolbar */}
        <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          {/* Left: Search input & Filters toggle */}
          <div className="flex flex-1 items-center gap-2 max-w-xl">
            <div className="relative flex-1">
              <Search className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-slate-400" />
              <Input
                value={searchTerm}
                onChange={(event) => {
                  setPageOffset(0);
                  setSearchTerm(event.target.value);
                }}
                placeholder="Search by decision, object, or scenario..."
                className="h-10 rounded-xl border-slate-200 bg-white pl-10 text-[13.5px] shadow-none focus:border-slate-300"
              />
            </div>

            <div ref={filtersContainerRef} className="relative">
              <Button
                variant="outline"
                type="button"
                onClick={() => {
                  setFiltersOpen((current) => !current);
                  setActiveFilterMenu(null);
                  setColumnPickerOpen(false);
                  setFilterSearchQuery("");
                }}
                className={cn(
                  "h-10 rounded-xl border-slate-200 bg-white px-3.5 text-[13.5px] shadow-none gap-2 text-slate-700 hover:bg-slate-50",
                  filtersOpen && "border-slate-300 bg-slate-50",
                  selectedFilters.length > 0 && "text-[#1f4f96] border-blue-200 bg-blue-50/50"
                )}
              >
                <Filter className="size-4 text-slate-500" />
                <span>Filters</span>
                {selectedFilters.length > 0 && (
                  <span className="flex size-5 items-center justify-center rounded-full bg-[#1f4f96] text-[11px] font-semibold text-white">
                    {selectedFilters.length}
                  </span>
                )}
              </Button>

              {filtersOpen && (
                <div className="absolute left-0 top-12 z-30 w-72 rounded-xl border border-slate-200 bg-white p-3 shadow-[0_18px_30px_rgba(15,23,42,0.12)]">
                  <div className="flex items-center justify-between border-b border-slate-100 pb-2 mb-2">
                    <span className="text-xs font-semibold text-slate-900">Add filter</span>
                    <button
                      type="button"
                      onClick={() => {
                        setFiltersOpen(false);
                        setFilterSearchQuery("");
                      }}
                      className="text-xs text-slate-400 hover:text-slate-700 font-medium"
                    >
                      Cancel
                    </button>
                  </div>

                  <div className="relative mb-2">
                    <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-slate-400" />
                    <Input
                      autoFocus
                      placeholder="Search filters or fields..."
                      value={filterSearchQuery}
                      onChange={(e) => setFilterSearchQuery(e.target.value)}
                      className="h-8 pl-8 pr-7 text-xs rounded-lg border-slate-200 shadow-none focus:border-blue-400"
                    />
                    {filterSearchQuery && (
                      <button
                        type="button"
                        onClick={() => setFilterSearchQuery("")}
                        className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600 text-xs font-bold"
                      >
                        ×
                      </button>
                    )}
                  </div>

                  <div className="max-h-64 overflow-y-auto space-y-1 pr-0.5">
                    {(() => {
                      const q = filterSearchQuery.trim().toLowerCase();
                      const availableFilters = filterItems
                        .filter((item) => !selectedFilters.some((s) => s.type === item))
                        .filter((item) => !q || item.toLowerCase().includes(q));

                      if (availableFilters.length === 0) {
                        return (
                          <p className="px-2 py-4 text-xs text-slate-400 text-center">
                            {filterSearchQuery
                              ? `No filters match "${filterSearchQuery}".`
                              : "All filters active."}
                          </p>
                        );
                      }

                      return (
                        <div className="space-y-0.5">
                          {availableFilters.map((item) => (
                            <button
                              key={item}
                              type="button"
                              onClick={() => {
                                const val =
                                  item === "Outcome"
                                    ? "Approve"
                                    : item === "Scenario"
                                      ? scenarioFilterOptions[0] ?? ""
                                      : item === "Trigger object"
                                        ? availableTables[0] ?? ""
                                        : item === "Score (min)"
                                          ? "70"
                                          : item === "Score (max)"
                                            ? "100"
                                            : "";
                                upsertFilter(item, val);
                                setFiltersOpen(false);
                                setFilterSearchQuery("");
                                setActiveFilterMenu(item);
                              }}
                              className="flex w-full items-center rounded-lg px-2.5 py-1.5 text-left text-xs font-medium text-slate-800 hover:bg-slate-100/70 transition-colors"
                            >
                              {item}
                            </button>
                          ))}
                        </div>
                      );
                    })()}
                  </div>

                  <div className="mt-2 border-t border-slate-100 pt-2 flex justify-end">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setFiltersOpen(false);
                        setFilterSearchQuery("");
                      }}
                      className="h-7 text-xs text-slate-600"
                    >
                      Cancel
                    </Button>
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* Right: Actions & View controls */}
          <div className="flex flex-wrap items-center gap-2">
            {selection.selected.length > 0 && (
              <div className="flex items-center gap-2 rounded-xl border border-blue-200 bg-blue-50/80 px-3 py-1.5">
                <span className="text-xs font-semibold text-[#1f4f96]">
                  {selection.selected.length} selected
                </span>
                <Button
                  size="sm"
                  disabled={decisionsQuery.isFetching}
                  onClick={() => setCreating({ scope: selectionScope, decisions: selection.selected })}
                  className="h-7 rounded-lg bg-[#1f4f96] px-3 text-xs font-medium text-white shadow-none transition-colors hover:bg-[#163f79]"
                >
                  <Plus className="size-3.5 mr-1" />
                  Add to Case
                </Button>
                <button
                  type="button"
                  onClick={selection.clear}
                  className="text-xs text-slate-500 hover:text-slate-800 transition-colors ml-1 underline"
                >
                  Clear
                </button>
              </div>
            )}

            <Button
              type="button"
              variant="outline"
              onClick={() => setFetchRecordFields((prev) => !prev)}
              className={cn(
                "h-10 rounded-xl px-3.5 text-[13.5px] shadow-none border-slate-200 transition-colors gap-2",
                fetchRecordFields
                  ? "bg-blue-50 text-[#1f4f96] border-blue-300 font-medium"
                  : "bg-white text-slate-700 hover:bg-slate-50"
              )}
            >
              <span
                className={cn(
                  "size-2 rounded-full",
                  fetchRecordFields ? "bg-blue-600 animate-pulse" : "bg-slate-300"
                )}
              />
              <span>{fetchRecordFields ? "Live fields: ON" : "Fetch record fields"}</span>
            </Button>

            <div ref={columnPickerContainerRef} className="relative">
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setColumnPickerOpen((prev) => !prev);
                  setFiltersOpen(false);
                  setActiveFilterMenu(null);
                  setColumnSearchQuery("");
                }}
                className={cn(
                  "h-10 rounded-xl border-slate-200 bg-white px-3.5 text-[13.5px] shadow-none text-slate-700 hover:bg-slate-50 gap-2",
                  columnPickerOpen && "border-slate-300 bg-slate-50"
                )}
              >
                <Sliders className="size-4 text-slate-500" />
                <span>Columns</span>
                {availableColumns.length > 0 && (
                  <span className="rounded-md bg-slate-100 px-1.5 py-0.5 text-[11px] font-medium text-slate-600 border border-slate-200/60">
                    {visibleColumns.length}/{availableColumns.length}
                  </span>
                )}
              </Button>

              {columnPickerOpen && (
                <div className="absolute right-0 top-12 z-30 w-72 rounded-xl border border-slate-200 bg-white p-3 shadow-[0_18px_30px_rgba(15,23,42,0.12)]">
                  <div className="flex items-center justify-between border-b border-slate-100 pb-2 mb-2">
                    <span className="text-xs font-semibold text-slate-900">
                      {effectiveDisplayTable ? `${effectiveDisplayTable.replace(/_/g, " ")} fields` : "Columns"}
                    </span>
                    <button
                      type="button"
                      onClick={() => {
                        setColumnPickerOpen(false);
                        setColumnSearchQuery("");
                      }}
                      className="text-xs text-slate-400 hover:text-slate-700 font-medium"
                    >
                      Cancel
                    </button>
                  </div>

                  <div className="relative mb-2">
                    <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-slate-400" />
                    <Input
                      autoFocus
                      placeholder="Search columns..."
                      value={columnSearchQuery}
                      onChange={(e) => setColumnSearchQuery(e.target.value)}
                      className="h-8 pl-8 pr-7 text-xs rounded-lg border-slate-200 shadow-none focus:border-blue-400"
                    />
                    {columnSearchQuery && (
                      <button
                        type="button"
                        onClick={() => setColumnSearchQuery("")}
                        className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600 text-xs font-bold"
                      >
                        ×
                      </button>
                    )}
                  </div>

                  <div className="flex items-center justify-between text-xs pb-1.5 mb-1 px-0.5 text-slate-500">
                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        onClick={showAllColumns}
                        className="text-[#1f4f96] hover:underline font-medium"
                      >
                        Select all
                      </button>
                      <span className="text-slate-300">|</span>
                      <button
                        type="button"
                        onClick={hideAllColumns}
                        className="text-slate-500 hover:underline"
                      >
                        Deselect all
                      </button>
                    </div>
                    <span className="text-[11px] text-slate-400">
                      {visibleColumns.length}/{availableColumns.length}
                    </span>
                  </div>

                  {!fetchRecordFields && (
                    <div className="text-[11px] text-amber-800 bg-amber-50 border border-amber-200/60 rounded-lg p-2 mb-2">
                      Click <strong>&ldquo;Fetch record fields&rdquo;</strong> to load live record values into these columns.
                    </div>
                  )}

                  {availableColumns.length === 0 ? (
                    <p className="text-xs text-slate-400 py-3 text-center">
                      No display columns configured for {effectiveDisplayTable || "this table"}.
                    </p>
                  ) : (
                    <div className="max-h-56 overflow-y-auto space-y-1 pr-1">
                      {availableColumns
                        .filter((col) => {
                          const cq = columnSearchQuery.trim().toLowerCase();
                          return (
                            !cq ||
                            (col.label && col.label.toLowerCase().includes(cq)) ||
                            col.id.toLowerCase().includes(cq)
                          );
                        })
                        .map((col) => {
                          const isChecked = !hiddenForActiveTable.includes(col.id);
                          return (
                            <label
                              key={col.id}
                              className="flex items-center gap-2.5 rounded-lg px-2 py-1.5 text-xs text-slate-800 hover:bg-slate-50 cursor-pointer select-none"
                            >
                              <input
                                type="checkbox"
                                checked={isChecked}
                                onChange={() => toggleColumnVisibility(col.id)}
                                className="size-4 accent-[#1f4f96] rounded"
                              />
                              <span className="truncate font-medium">{col.label}</span>
                            </label>
                          );
                        })}
                    </div>
                  )}

                  <div className="mt-2 border-t border-slate-100 pt-2 flex items-center justify-between">
                    <span className="text-[11px] text-slate-400">
                      {visibleColumns.length} visible
                    </span>
                    <div className="flex items-center gap-1.5">
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          setColumnPickerOpen(false);
                          setColumnSearchQuery("");
                        }}
                        className="h-7 text-xs text-slate-600"
                      >
                        Cancel
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        onClick={() => {
                          setColumnPickerOpen(false);
                          setColumnSearchQuery("");
                        }}
                        className="h-7 text-xs bg-[#1f4f96] text-white hover:bg-[#163f79]"
                      >
                        Done
                      </Button>
                    </div>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Active Filters row */}
        {selectedFilters.length > 0 && (
          <div className="flex flex-wrap items-center gap-2 pt-1">
            {selectedFilters.map((filter) => {
              const typeLabel = getFilterTypeLabel(filter.type);
              const valueLabel = getFilterValueLabel(filter.type, filter.value);

              return (
                <div
                  key={filter.type}
                  className="relative"
                  ref={activeFilterMenu === filter.type ? activeFilterContainerRef : undefined}
                >
                  <div className="inline-flex items-center rounded-xl border border-slate-200 bg-white px-2.5 py-1 text-xs text-slate-800 shadow-xs">
                    <button
                      type="button"
                      onClick={() => {
                        setFiltersOpen(false);
                        setColumnPickerOpen(false);
                        setActiveFilterMenu((current) =>
                          current === filter.type ? null : filter.type
                        );
                      }}
                      className="flex items-center gap-1.5 font-medium hover:text-[#1f4f96]"
                    >
                      <span className="text-slate-500 font-normal">{typeLabel}:</span>
                      <span className="font-semibold text-slate-800 truncate max-w-[160px]">{valueLabel}</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => removeFilter(filter.type)}
                      className="ml-2 text-slate-400 hover:text-slate-700 font-bold"
                    >
                      ×
                    </button>
                  </div>

                  {activeFilterMenu === filter.type && (
                    <div className="absolute left-0 top-10 z-30 w-72 rounded-xl border border-slate-200 bg-white p-2 shadow-[0_18px_30px_rgba(15,23,42,0.12)]">
                      <div className="flex items-center justify-between border-b border-slate-100 pb-2 mb-1 px-1">
                        <span className="text-xs font-semibold text-slate-800">{typeLabel}</span>
                        <button
                          type="button"
                          onClick={() => setActiveFilterMenu(null)}
                          className="text-xs text-slate-400 hover:text-slate-700 px-1 py-0.5 rounded"
                        >
                          Cancel
                        </button>
                      </div>
                      {filter.type === "Outcome" ? (
                        <div className="space-y-1 p-1">
                          {outcomeFilterItems.map((item) => (
                            <button
                              key={item}
                              type="button"
                              onClick={() => {
                                upsertFilter(filter.type, item);
                                setActiveFilterMenu(null);
                              }}
                              className="flex w-full items-center justify-between rounded-lg px-2.5 py-2 text-left text-xs text-slate-900 hover:bg-slate-50"
                            >
                              <span
                                className={cn(
                                  "rounded-full px-2.5 py-1 text-xs font-medium",
                                  item === "Approve"
                                    ? "bg-emerald-100 text-emerald-700"
                                    : item === "Block and Review"
                                      ? "bg-orange-100 text-orange-700"
                                      : item === "Decline"
                                        ? "bg-rose-100 text-rose-700"
                                        : "bg-amber-100 text-amber-700"
                                )}
                              >
                                {item}
                              </span>
                              {filter.value === item && <span className="text-[#1f4f96] font-bold">✓</span>}
                            </button>
                          ))}
                        </div>
                      ) : filter.type === "Scenario" ? (
                        <div className="max-h-60 overflow-y-auto space-y-1 p-1">
                          {scenarioFilterOptions.map((item) => (
                            <button
                              key={item}
                              type="button"
                              onClick={() => {
                                upsertFilter(filter.type, item);
                                setActiveFilterMenu(null);
                              }}
                              className="flex w-full items-center justify-between rounded-lg px-2.5 py-1.5 text-left text-xs text-slate-900 hover:bg-slate-50"
                            >
                              <span className="truncate">{item}</span>
                              {filter.value === item && <span className="text-[#1f4f96] font-bold">✓</span>}
                            </button>
                          ))}
                        </div>
                      ) : filter.type === "Trigger object" ? (
                        <div className="space-y-1 p-1">
                          {availableTables.map((item) => (
                            <button
                              key={item}
                              type="button"
                              onClick={() => {
                                upsertFilter(filter.type, item);
                                setActiveFilterMenu(null);
                              }}
                              className="flex w-full items-center justify-between rounded-lg px-2.5 py-1.5 text-left text-xs text-slate-900 hover:bg-slate-50"
                            >
                              <span>{item}</span>
                              {filter.value === item && <span className="text-[#1f4f96] font-bold">✓</span>}
                            </button>
                          ))}
                        </div>
                      ) : filter.type.startsWith("Score") ? (
                        <div className="p-2 space-y-2">
                          <label className="text-xs font-semibold text-slate-700">
                            {filter.type} threshold:
                          </label>
                          <Input
                            type="number"
                            min={0}
                            max={100}
                            placeholder="e.g. 80"
                            value={filter.value}
                            onChange={(event) => upsertFilter(filter.type, event.target.value)}
                            className="h-9 rounded-lg border-slate-200 text-xs shadow-none"
                          />
                        </div>
                      ) : (
                        <div className="p-2 space-y-2">
                          <label className="text-xs font-semibold text-slate-700">
                            {filter.type} value:
                          </label>
                          <Input
                            aria-label={`${filter.type} filter value`}
                            value={filter.value}
                            onChange={(event) => upsertFilter(filter.type, event.target.value)}
                            className="h-9 rounded-lg border-slate-200 text-xs shadow-none"
                          />
                        </div>
                      )}

                    <div className="mt-2 border-t border-slate-100 pt-2 flex items-center justify-between px-1">
                      <button
                        type="button"
                        onClick={() => removeFilter(filter.type)}
                        className="text-xs text-rose-600 hover:text-rose-800 font-medium px-1 py-0.5 rounded hover:bg-rose-50"
                      >
                        Remove
                      </button>
                      <div className="flex items-center gap-1.5">
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          onClick={() => setActiveFilterMenu(null)}
                          className="h-7 text-xs px-2.5 rounded-lg border-slate-200 hover:bg-slate-50"
                        >
                          Cancel
                        </Button>
                        <Button
                          type="button"
                          size="sm"
                          onClick={() => setActiveFilterMenu(null)}
                          className="h-7 text-xs px-2.5 rounded-lg bg-[#1f4f96] text-white hover:bg-[#1a427f]"
                        >
                          Apply
                        </Button>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            );
          })}

            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setPageOffset(0);
                setSelectedFilters([]);
                setActiveFilterMenu(null);
              }}
              className="h-8 rounded-lg px-2.5 text-xs text-slate-500 hover:text-slate-800"
            >
              Clear all
            </Button>
          </div>
        )}

        {creating?.scope === selectionScope && <CreateDecisionCaseModal tenant={tenantId} session={session} decisions={creating.decisions} scenarioNames={scenarioNameById} onClose={created => { setCreating(null); if (created) selection.clear(); }} />}
        {selection.identity.isError && <p role="alert" className="text-sm text-amber-800">Case actions are unavailable for this session. <button className="underline" onClick={() => void selection.identity.refetch()}>Retry case access</button></p>}
        {display.query.isError && <p role="alert" className="text-sm text-amber-800">Source display values are unavailable. <button className="underline" onClick={() => void display.query.refetch()}>Retry display values</button></p>}
        {selection.memberships.isError && <p role="alert" className="text-sm text-red-700">Could not check case membership. <button className="underline" onClick={() => void selection.memberships.refetch()}>Retry case lookup</button></p>}
        {isInitialDecisionLoad ? (
          <Card className="rounded-xl border border-slate-200 shadow-none">
            <CardContent className="flex items-center gap-2 p-6 text-sm text-slate-600">
              <Loader2 className="size-4 animate-spin text-slate-400" />
              Loading decisions...
            </CardContent>
          </Card>
        ) : decisionsQuery.isError ? (
          <Card className="rounded-xl border border-red-200 bg-red-50 shadow-none">
            <CardContent className="p-6 text-sm text-red-700">
              {decisionsQuery.error instanceof Error
                ? decisionsQuery.error.message
                : "Failed to load decisions."}
            </CardContent>
          </Card>
        ) : decisions.length === 0 ? (
          <Card className="rounded-xl border border-slate-200 shadow-none">
            <CardContent className="flex flex-col items-center justify-center gap-2 p-8 text-center text-sm text-slate-600">
              <p>No decisions matched the current search and filters.</p>
              {(selectedFilters.length > 0 || searchTerm) && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setSelectedFilters([]);
                    setSearchTerm("");
                    setPageOffset(0);
                  }}
                  className="mt-2 text-xs text-[#1f4f96] border-blue-200 hover:bg-blue-50"
                >
                  Clear all filters and search
                </Button>
              )}
            </CardContent>
          </Card>
        ) : (
          <Card className="relative overflow-hidden rounded-xl border border-slate-200 shadow-none w-full min-w-0 max-w-full">
            {isDecisionListRefreshing ? (
              <>
                <div className="absolute inset-0 z-20 bg-white/70 backdrop-blur-[2px]" />
                <div className="absolute inset-0 z-30 flex items-center justify-center">
                  <div className="inline-flex items-center gap-2 rounded-full border border-slate-200 bg-white px-4 py-2 text-[13px] font-medium text-slate-700 shadow-sm">
                    <Loader2 className="size-4 animate-spin text-[#2d63b8]" />
                    Loading new page...
                  </div>
                </div>
              </>
            ) : null}
            <CardContent className="p-0 overflow-hidden w-full min-w-0 max-w-full">
              <div
                className={cn(
                  "overflow-x-auto w-full max-w-full transition-opacity",
                  isDecisionListRefreshing && "pointer-events-none select-none opacity-25"
                )}
              >
                <table className="w-full text-left border-collapse" style={{ minWidth: "max-content" }}>
                  <thead>
                    <tr className="border-b border-slate-200 bg-white text-[13px] font-semibold text-slate-950">
                      <th className="px-4 py-3 whitespace-nowrap"><SelectPageCheckbox checked={selection.eligible.length > 0 && selection.selectedIDs.length === selection.eligible.length} mixed={selection.selectedIDs.length > 0 && selection.selectedIDs.length < selection.eligible.length} disabled={!selection.eligible.length || decisionsQuery.isFetching} onChange={selection.all} /></th>
                      <th className="px-4 py-3 whitespace-nowrap">Date</th>
                      <th className="px-4 py-3 whitespace-nowrap">Scenario</th>
                      <th className="px-4 py-3 whitespace-nowrap">Live version</th>
                      <th className="px-4 py-3 whitespace-nowrap">Trigger object</th>
                      <th className="px-4 py-3 whitespace-nowrap">
                        {captionHeaderLabel}
                      </th>
                      {visibleColumns.map((column) => (
                        <th key={column.id} className="whitespace-nowrap px-4 py-3 font-semibold text-slate-900 min-w-[130px]">
                          {column.label}
                        </th>
                      ))}
                      <th className="px-4 py-3 whitespace-nowrap">Case</th>
                      <th className="px-4 py-3 whitespace-nowrap">Score</th>
                      <th className="px-4 py-3 whitespace-nowrap">Outcome</th>
                    </tr>
                  </thead>
                  <tbody>
                    {decisions.map((item) => (
                      <tr
                        key={item.id}
                        className="border-b border-slate-100 text-[14px] text-slate-950 last:border-b-0 hover:bg-slate-50/50"
                      >
                        <td className="px-4 py-3 whitespace-nowrap"><input type="checkbox" className="size-4 accent-blue-700" aria-label={`Select decision ${item.id}`} checked={selection.selectedIDs.includes(item.id)} disabled={!selection.eligible.includes(item.id) || decisionsQuery.isFetching} onChange={() => selection.toggle(item.id)} /></td>
                        <td className="px-4 py-3 whitespace-nowrap text-slate-600">
                          {formatDecisionDate(item.created_at)}
                        </td>
                        <td className="px-4 py-3 whitespace-nowrap">
                          <Link
                            href={`/detection/decisions/${item.id}?fromTab=Decisions`}
                            className="font-medium text-slate-950 transition hover:text-[#2d63b8]"
                          >
                            {scenarioNameById.get(item.scenario_id) ?? item.scenario_id}
                          </Link>
                        </td>
                        <td className="px-4 py-3 whitespace-nowrap text-slate-600">
                          {liveVersionByScenarioId.get(item.scenario_id) ?? "-"}
                        </td>
                        <td className="px-4 py-3 whitespace-nowrap">
                          <TablePill>{item.object_type}</TablePill>
                        </td>
                        <td className="max-w-xs truncate px-4 py-3 whitespace-nowrap" title={item.object_id}>
                          {fetchRecordFields ? (display.record(item)?.caption || item.object_id) : item.object_id}
                          {fetchRecordFields && display.record(item)?.found === false && (
                            <span className="block text-xs text-slate-400">Unavailable</span>
                          )}
                        </td>
                        {visibleColumns.map((column) => (
                          <td key={column.id} className="whitespace-nowrap px-4 py-3 text-slate-700 min-w-[130px]">
                            {fetchRecordFields ? displayValue(display.record(item)?.values[column.id]) : "—"}
                          </td>
                        ))}
                        <td className="px-4 py-3 whitespace-nowrap text-sm">{selection.cell(item.id)}</td>
                        <td className="px-4 py-3 whitespace-nowrap">{item.score}</td>
                        <td className="px-4 py-3 whitespace-nowrap">
                          <span
                            className={cn(
                              "inline-flex rounded-full px-3 py-1.5 text-[13px] font-medium",
                              outcomeBadgeClass(item.outcome)
                            )}
                          >
                            {formatDecisionOutcome(item.outcome)}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {pagination ? (
                <div className="flex flex-col gap-3 border-t border-slate-200 px-4 py-3 text-[13px] text-slate-600 sm:flex-row sm:items-center sm:justify-between">
                  <div className="flex flex-wrap items-center gap-3">
                    <div>
                      Showing {pageRangeLabel}
                      {totalRecords != null
                        ? ` of ${totalRecords}`
                        : decisionsCountQuery.isFetching
                          ? " of ..."
                          : ""}
                      {debouncedSearchTerm || selectedFilters.length > 0
                        ? " matching current filters"
                        : ""}
                      {isDecisionCountRefreshing ? (
                        <span className="inline-flex items-center gap-1 text-slate-500 ml-2">
                          <Loader2 className="size-3 animate-spin" />
                          Updating total...
                        </span>
                      ) : null}
                    </div>
                    <span className="text-slate-300 hidden md:inline">|</span>
                    <label className="flex items-center gap-1.5 text-xs text-slate-600">
                      <span>Rows per page:</span>
                      <select
                        value={pageSize}
                        onChange={(event) => {
                          setPageOffset(0);
                          setPageSize(Number(event.target.value));
                        }}
                        className="rounded-lg border border-slate-200 bg-white px-2 py-1 text-xs text-slate-700 shadow-none outline-none focus:border-slate-300 cursor-pointer"
                      >
                        {DECISIONS_PAGE_SIZE_OPTIONS.map((option) => (
                          <option key={option} value={option}>
                            {option}
                          </option>
                        ))}
                      </select>
                    </label>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <Button
                      variant="outline"
                      disabled={!canGoPrevious || isDecisionListRefreshing}
                      onClick={() =>
                        setPageOffset((current) =>
                          Math.max(0, current - pageSize)
                        )
                      }
                      className="h-9 rounded-xl border-slate-200 bg-white px-3 text-[13px] shadow-none"
                    >
                      <ChevronLeft className="size-4" />
                    </Button>
                    <div className="flex items-center gap-1">
                      {paginationTokens.map((token) =>
                        token.type === "ellipsis" ? (
                          <div
                            key={token.key}
                            className="flex h-9 min-w-9 items-center justify-center px-1 text-[13px] text-slate-400"
                          >
                            ...
                          </div>
                        ) : (
                          <Button
                            key={token.page}
                            variant="outline"
                            onClick={() => setPageOffset((token.page - 1) * pageSize)}
                            disabled={token.page === currentPage || isDecisionListRefreshing}
                            className={cn(
                              "h-9 min-w-9 rounded-xl border px-3 text-[13px] shadow-none",
                              token.page === currentPage
                                ? "border-[#2d63b8] bg-[#2d63b8] text-white hover:bg-[#2d63b8]"
                                : "border-slate-200 bg-white text-slate-700"
                            )}
                          >
                            {token.page}
                          </Button>
                        )
                      )}
                    </div>
                    <Button
                      variant="outline"
                      disabled={!canGoNext || isDecisionListRefreshing}
                      onClick={() => setPageOffset(pageOffset + pageSize)}
                      className="h-9 rounded-xl border-slate-200 bg-white px-3 text-[13px] shadow-none"
                    >
                      <ChevronRight className="size-4" />
                    </Button>
                  </div>
                </div>
              ) : null}
            </CardContent>
          </Card>
        )}
      </div>

    </>
  );
}

function DecisionsView({ hasScenarios }: { hasScenarios: boolean }) {
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [newFilterOpen, setNewFilterOpen] = useState(false);
  const [activeFilterMenu, setActiveFilterMenu] = useState<string | null>(null);
  const [selectedFilters, setSelectedFilters] = useState<
    Array<{
      type: string;
      value: string | boolean;
    }>
  >([]);
  const filterItems = [
    "Date",
    "Scenario",
    "Trigger object",
    "Object ID",
    "Outcome",
    "Inbox",
    "Presence of a case",
    "Pivot value",
    "Scheduled execution",
  ];
  const outcomeFilterItems = [
    "Approve",
    "Block and Review",
    "Manually approved",
    "Manually declined",
    "Decline",
    "Review",
  ];

  function upsertFilter(type: string, value: string | boolean) {
    setSelectedFilters((current) => {
      const existing = current.find((item) => item.type === type);
      if (!existing) {
        return [...current, { type, value }];
      }

      return current.map((item) => (item.type === type ? { ...item, value } : item));
    });
  }

  function removeFilter(type: string) {
    setSelectedFilters((current) => current.filter((item) => item.type !== type));
    setActiveFilterMenu((current) => (current === type ? null : current));
  }

  function availableFilterItems() {
    return filterItems.filter(
      (item) => !selectedFilters.some((selected) => selected.type === item)
    );
  }

  function filterLabel(value: string | boolean) {
    if (typeof value === "boolean") {
      return value ? "Enabled" : "Disabled";
    }

    return value;
  }

  if (!hasScenarios) {
    return (
      <Card className="rounded-xl border border-slate-200 shadow-none">
        <CardContent className="p-6 text-sm text-slate-600">
          No decisions created yet because there are no scenarios configured.
        </CardContent>
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
        <div className="flex w-full max-w-md gap-1.5">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-4 top-1/2 size-5 -translate-y-1/2 text-slate-500" />
            <Input
              placeholder="Search by id"
              className="h-10 rounded-xl border-slate-200 pl-11 text-[14px] shadow-none focus:border-slate-300"
            />
          </div>
          <Button className="h-10 rounded-xl bg-[#6b7280] px-4 text-[14px] shadow-none hover:bg-[#5b6473]">
            Search
          </Button>
        </div>

        <div className="relative flex gap-3">
          <Button
            variant="outline"
            onClick={() => {
              setFiltersOpen((current) => !current);
              setActiveFilterMenu(null);
            }}
            className="h-10 rounded-xl border-slate-200 bg-white px-4 text-[14px] shadow-none"
          >
            <Filter className="size-4" />
            Filters
          </Button>
          <Button className="h-10 rounded-xl bg-[#6b7280] px-4 text-[14px] shadow-none hover:bg-[#5b6473]">
            <Plus className="size-4" />
            Add to Case
          </Button>
          {filtersOpen ? (
            <div className="absolute right-[152px] top-12 z-10 w-[260px] rounded-xl border border-slate-200 bg-white p-2 shadow-[0_18px_30px_rgba(15,23,42,0.08)]">
              {filterItems.map((item) => (
                <button
                  key={item}
                  type="button"
                  onClick={() => {
                    if (item === "Presence of a case") {
                      upsertFilter(item, true);
                    } else if (item === "Outcome") {
                      upsertFilter(item, "Approve");
                    } else {
                      upsertFilter(item, "Selected");
                    }
                    setFiltersOpen(false);
                  }}
                  className="flex w-full items-center rounded-lg px-3 py-2.5 text-left text-[14px] text-slate-950 hover:bg-slate-50"
                >
                  {item}
                </button>
              ))}
            </div>
          ) : null}
        </div>
      </div>

      {selectedFilters.length > 0 ? (
        <div className="flex flex-wrap items-center gap-3">
          {selectedFilters.map((filter) => (
            <div key={filter.type} className="relative">
              <button
                type="button"
                onClick={() =>
                  setActiveFilterMenu((current) =>
                    current === filter.type ? null : filter.type
                  )
                }
                className="flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-3.5 py-2.5 text-[14px] text-[#1f4f96]"
              >
                <span className="font-medium">{filter.type}</span>
                <span className="text-slate-500">{filterLabel(filter.value)}</span>
                <span className="text-slate-500">×</span>
              </button>

              {activeFilterMenu === filter.type ? (
                <div className="absolute left-0 top-12 z-10 w-[260px] rounded-xl border border-slate-200 bg-white p-2 shadow-[0_18px_30px_rgba(15,23,42,0.08)]">
                  {filter.type === "Presence of a case" ? (
                    <div className="flex items-center justify-between rounded-lg px-3 py-3 text-[14px] text-slate-950">
                      <span>Presence of a case</span>
                      <button
                        type="button"
                        onClick={() => upsertFilter(filter.type, !(filter.value as boolean))}
                        className={cn(
                          "relative inline-flex h-8 w-14 items-center rounded-full transition",
                          filter.value ? "bg-[#1f4f96]" : "bg-slate-200"
                        )}
                      >
                        <span
                          className={cn(
                            "inline-block size-6 rounded-full bg-white transition",
                            filter.value ? "translate-x-7" : "translate-x-1"
                          )}
                        />
                      </button>
                    </div>
                  ) : filter.type === "Outcome" ? (
                    <div className="space-y-2 p-2">
                      <div className="h-12 rounded-lg border border-slate-200" />
                      {outcomeFilterItems.map((item) => (
                        <button
                          key={item}
                          type="button"
                          onClick={() => upsertFilter(filter.type, item)}
                          className="flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-left text-[14px] text-slate-950 hover:bg-slate-50"
                        >
                          <span
                            className={cn(
                              "rounded-full px-3 py-1.5",
                              item.includes("Approve")
                                ? "bg-emerald-100 text-emerald-700"
                                : item.includes("Block")
                                  ? "bg-orange-100 text-orange-700"
                                  : item.includes("Decline")
                                    ? "bg-rose-100 text-rose-700"
                                    : "bg-amber-100 text-amber-700"
                            )}
                          >
                            {item}
                          </span>
                          {filter.value === item ? <span className="text-[#1f4f96]">✓</span> : null}
                        </button>
                      ))}
                    </div>
                  ) : (
                    <div className="space-y-2 p-2">
                      <Input
                        value={String(filter.value)}
                        onChange={(event) => upsertFilter(filter.type, event.target.value)}
                        className="h-10 rounded-lg border-slate-200 text-[14px] shadow-none"
                      />
                    </div>
                  )}
                  <div className="mt-2 border-t border-slate-100 pt-2">
                    <button
                      type="button"
                      onClick={() => removeFilter(filter.type)}
                      className="w-full rounded-lg px-3 py-2 text-left text-[14px] text-rose-700 hover:bg-rose-50"
                    >
                      Remove filter
                    </button>
                  </div>
                </div>
              ) : null}
            </div>
          ))}
          <Button
            variant="outline"
            onClick={() => setNewFilterOpen((current) => !current)}
            className="h-10 rounded-xl border-slate-200 bg-white px-4 text-[14px] shadow-none"
          >
            <Plus className="size-4" />
            New Filter
          </Button>
          <Button
            variant="ghost"
            onClick={() => {
              setSelectedFilters([]);
              setActiveFilterMenu(null);
              setNewFilterOpen(false);
            }}
            className="h-10 rounded-xl px-4 text-[14px]"
          >
            Clear filters
          </Button>
          {newFilterOpen ? (
            <div className="relative">
              <div className="absolute left-0 top-2 z-10 w-[260px] rounded-xl border border-slate-200 bg-white p-2 shadow-[0_18px_30px_rgba(15,23,42,0.08)]">
                {availableFilterItems()
                  .map((item) => (
                    <button
                      key={item}
                      type="button"
                      onClick={() => {
                        if (item === "Presence of a case") {
                          upsertFilter(item, true);
                        } else if (item === "Outcome") {
                          upsertFilter(item, "Approve");
                        } else {
                          upsertFilter(item, "Selected");
                        }
                        setNewFilterOpen(false);
                      }}
                      className="flex w-full items-center rounded-lg px-3 py-2.5 text-left text-[14px] text-slate-950 hover:bg-slate-50"
                    >
                      {item}
                    </button>
                  ))}
              </div>
            </div>
          ) : null}
        </div>
      ) : null}

      <Card className="overflow-hidden rounded-xl border border-slate-200 shadow-none">
        <CardContent className="p-0">
          <div className="overflow-x-auto">
            <table className="min-w-full text-left">
              <thead>
                <tr className="border-b border-slate-200 bg-white text-[13px] font-semibold text-slate-950">
                  <th className="w-[64px] px-4 py-3">
                    <div className="size-7 rounded-lg border border-[#2d63b8]" />
                  </th>
                  <th className="px-4 py-3">Date</th>
                  <th className="px-4 py-3">Scenario</th>
                  <th className="px-4 py-3">Trigger object</th>
                  <th className="px-4 py-3">Case</th>
                  <th className="px-4 py-3">Score</th>
                  <th className="px-4 py-3">Outcome</th>
                </tr>
              </thead>
              <tbody />
            </table>
          </div>
        </CardContent>
      </Card>

      <div className="flex justify-end gap-1.5">
        <button
          type="button"
          className="inline-flex size-8 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-slate-300"
        >
          <ChevronLeft className="size-4" />
        </button>
        <button
          type="button"
          className="inline-flex size-8 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-slate-300"
        >
          <ChevronRight className="size-4" />
        </button>
      </div>
    </div>
  );
}

function ScenarioModal({
  isOpen,
  name,
  description,
  trigger,
  triggerOptions,
  setName,
  setDescription,
  setTrigger,
  onClose,
  onSave,
  isSaving,
  errorMessage,
}: {
  isOpen: boolean;
  name: string;
  description: string;
  trigger: string;
  triggerOptions: string[];
  setName: (value: string) => void;
  setDescription: (value: string) => void;
  setTrigger: (value: string) => void;
  onClose: () => void;
  onSave: () => void;
  isSaving: boolean;
  errorMessage?: string | null;
}) {
  if (!isOpen) {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-500/30 p-6 backdrop-blur-sm">
      <div className="w-full max-w-[560px] overflow-hidden rounded-2xl bg-white shadow-[0_18px_50px_rgba(15,23,42,0.18)]">
        <div className="border-b border-slate-200 px-5 py-5 text-center">
          <h2 className="text-[18px] font-semibold text-slate-950">New Scenario</h2>
        </div>

        <div className="space-y-4 px-5 py-5">
          <div className="rounded-lg border border-slate-200 bg-white px-4 py-3">
            <div className="flex items-start gap-3 text-[15px] leading-6 text-slate-950">
              <Lightbulb className="mt-1 size-4 shrink-0" />
              <p>
                A scenario identifies a certain risk type, triggered by a specific event{" "}
                <span className="font-semibold text-[#1f4f96]">(learn more)</span>
              </p>
            </div>
          </div>

          <div className="space-y-2">
            <label className="text-[15px] font-medium text-slate-950">Name</label>
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="Add a name"
              className="h-10 rounded-lg border-slate-200 text-[14px] shadow-none"
            />
          </div>

          <div className="space-y-2">
            <label className="text-[15px] font-medium text-slate-950">Description</label>
            <Input
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="Add a description"
              className="h-10 rounded-lg border-slate-200 text-[14px] shadow-none"
            />
          </div>

          <div className="space-y-2">
            <label className="flex items-center gap-2 text-[15px] font-medium text-slate-950">
              Trigger Object
              <Info className="size-4 text-slate-300" />
            </label>
            <div className="relative">
              <select
                value={trigger}
                onChange={(event) => setTrigger(event.target.value)}
                className="h-10 w-full appearance-none rounded-lg border border-slate-200 bg-white px-4 text-center text-[14px] text-slate-950 outline-none"
              >
                {triggerOptions.map((option) => (
                  <option key={option} value={option}>
                    {option}
                  </option>
                ))}
              </select>
              <ChevronDown className="pointer-events-none absolute right-4 top-1/2 size-4 -translate-y-1/2 text-slate-950" />
            </div>
          </div>

          {errorMessage ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {errorMessage}
            </div>
          ) : null}
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
            onClick={onSave}
            disabled={isSaving || !name.trim() || !trigger}
            className="h-10 flex-1 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
          >
            {isSaving ? "Saving..." : "Save"}
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}

function SimpleScenarioModal({
  isOpen,
  title,
  children,
  confirmLabel,
  confirmClassName,
  onClose,
  onConfirm,
  isPending = false,
  confirmDisabled = false,
}: {
  isOpen: boolean;
  title: string;
  children: ReactNode;
  confirmLabel: string;
  confirmClassName?: string;
  onClose: () => void;
  onConfirm: () => void;
  isPending?: boolean;
  confirmDisabled?: boolean;
}) {
  if (!isOpen) {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-500/30 p-6 backdrop-blur-sm">
      <div className="w-full max-w-[640px] overflow-hidden rounded-2xl bg-white shadow-[0_18px_50px_rgba(15,23,42,0.18)]">
        <div className="border-b border-slate-200 px-5 py-5 text-center">
          <h2 className="text-[18px] font-semibold text-slate-950">{title}</h2>
        </div>
        <div className="space-y-4 px-5 py-5">{children}</div>
        <div className="flex gap-3 border-t border-slate-200 px-5 py-4">
          <Button
            variant="outline"
            onClick={onClose}
            className="h-10 flex-1 rounded-xl border-slate-200 px-4 text-[14px] shadow-none"
          >
            Cancel
          </Button>
          <Button
            onClick={onConfirm}
            disabled={isPending || confirmDisabled}
            className={cn(
              "h-10 flex-1 rounded-xl px-4 text-[14px] shadow-none",
              confirmClassName ?? "bg-[#1f4f96] hover:bg-[#163f79]"
            )}
          >
            {isPending ? "Saving..." : confirmLabel}
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}

function NewListModal({
  isOpen,
  name,
  description,
  type,
  setName,
  setDescription,
  setType,
  onClose,
  onSave,
}: {
  isOpen: boolean;
  name: string;
  description: string;
  type: string;
  setName: (value: string) => void;
  setDescription: (value: string) => void;
  setType: (value: string) => void;
  onClose: () => void;
  onSave: () => void;
}) {
  if (!isOpen) {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-500/30 p-6 backdrop-blur-sm">
      <div className="w-full max-w-[640px] overflow-hidden rounded-2xl bg-white shadow-[0_18px_50px_rgba(15,23,42,0.18)]">
        <div className="border-b border-slate-200 px-5 py-5 text-center">
          <h2 className="text-[18px] font-semibold text-slate-950">New List</h2>
        </div>
        <div className="space-y-4 px-5 py-5">
          <div className="space-y-2">
            <label className="text-[15px] font-medium text-slate-950">Name</label>
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="Add a name to your list"
              className="h-12 rounded-lg border-slate-200 text-[14px] shadow-none"
            />
          </div>
          <div className="space-y-2">
            <label className="text-[15px] font-medium text-slate-950">Description</label>
            <Input
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="Add a description"
              className="h-12 rounded-lg border-slate-200 text-[14px] shadow-none"
            />
          </div>
          <div className="space-y-2">
            <label className="text-[15px] font-medium text-slate-950">Type of list</label>
            <div className="relative">
              <select
                value={type}
                onChange={(event) => setType(event.target.value)}
                className="h-12 w-full appearance-none rounded-lg border border-slate-200 bg-white px-4 text-[14px] text-slate-950 outline-none"
              >
                <option value="ip_subnet">IP addresses and subnets</option>
                <option value="generic_text">Generic text</option>
                <option value="uuid">UUID values</option>
              </select>
              <ChevronDown className="pointer-events-none absolute right-4 top-1/2 size-4 -translate-y-1/2 text-slate-950" />
            </div>
          </div>
        </div>
        <div className="flex justify-end gap-3 border-t border-slate-200 px-5 py-4">
          <Button
            variant="outline"
            onClick={onClose}
            className="h-10 rounded-xl border-slate-200 px-4 text-[14px] shadow-none"
          >
            Cancel
          </Button>
          <Button
            onClick={onSave}
            disabled={!name.trim()}
            className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
          >
            Create new list
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}

function DetectionPageContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { tenant: tenantId } = useContext(DetectionIdentity);
  const queryClient = useQueryClient();
  const pushToast = useToastStore((state) => state.pushToast);
  const activeTab = parseDetectionTab(searchParams.get("tab"));
  const guideStep = searchParams.get("guide");
  const requestedAction = searchParams.get("action");
  const initialOutcome = searchParams.get("outcome") ?? undefined;
  const [createModalOpen, setCreateModalOpen] = useState(
    requestedAction === "new-scenario"
  );
  const [createListModalOpen, setCreateListModalOpen] = useState(
    requestedAction === "new-list"
  );
  const [editModalOpen, setEditModalOpen] = useState(false);
  const [duplicateModalOpen, setDuplicateModalOpen] = useState(false);
  const [archiveModalOpen, setArchiveModalOpen] = useState(false);
  const [selectedScenario, setSelectedScenario] = useState<DetectionScenario | null>(
    null
  );
  const [scenarioName, setScenarioName] = useState("");
  const [scenarioDescription, setScenarioDescription] = useState("");
  const [scenarioTrigger, setScenarioTrigger] = useState("");
  const [duplicateName, setDuplicateName] = useState("");
  const [listName, setListName] = useState("");
  const [listDescription, setListDescription] = useState("");
  const [listType, setListType] = useState("generic_text");

  const assembledModelQuery = useAssembledDataModelQuery(tenantId);
  const triggerOptions = useMemo(() => {
    return Object.keys(assembledModelQuery.data?.data_model.tables ?? {});
  }, [assembledModelQuery.data]);
  const effectiveScenarioTrigger = scenarioTrigger || triggerOptions[0] || "";

  const scenariosQuery = useQuery({
    queryKey: scenarioQueryKey(tenantId),
    queryFn: () => decisionEngineApi.listScenarios(tenantId),
    enabled: Boolean(tenantId),
  });
  const customListsQuery = useQuery({
    queryKey: ["decision-engine", "custom-lists", tenantId],
    queryFn: () => decisionEngineApi.listCustomLists(tenantId),
    enabled: Boolean(tenantId),
  });
  const customListEntriesQuery = useQuery({
    queryKey: ["decision-engine", "custom-list-entries", tenantId],
    queryFn: () => decisionEngineApi.listCustomListEntries(tenantId),
    enabled: Boolean(tenantId),
  });

  const createScenarioMutation = useMutation({
    mutationFn: async () => {
      const { scenario } = await decisionEngineApi.createScenario(tenantId, {
        name: scenarioName.trim(),
        description: scenarioDescription.trim(),
        trigger_object_type: effectiveScenarioTrigger,
      });

      try {
        await decisionEngineApi.createIteration(tenantId, scenario.id);
        return { scenario, iterationCreated: true as const };
      } catch (iterationError) {
        return {
          scenario,
          iterationCreated: false as const,
          iterationError,
        };
      }
    },
    onSuccess: ({ scenario, iterationCreated, iterationError }) => {
      void queryClient.invalidateQueries({
        queryKey: scenarioQueryKey(tenantId),
      });
      setCreateModalOpen(false);
      setScenarioName("");
      setScenarioDescription("");
      setScenarioTrigger(triggerOptions[0] ?? "");

      if (!iterationCreated) {
        pushToast({
          title: "Scenario created without a draft",
          description:
            iterationError instanceof Error
              ? iterationError.message
              : "The initial draft iteration could not be created automatically.",
          variant: "error",
        });
      }

      router.push(`/detection/${scenario.id}/edit`);
    },
  });

  const updateScenarioMutation = useMutation({
    mutationFn: () => {
      if (!selectedScenario) {
        throw new Error("No scenario selected.");
      }

      return decisionEngineApi.updateScenario(tenantId, selectedScenario.id, {
        name: scenarioName.trim(),
        description: scenarioDescription.trim(),
        trigger_object_type: scenarioTrigger,
      });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: scenarioQueryKey(tenantId),
      });
      setEditModalOpen(false);
      setSelectedScenario(null);
    },
  });

  const duplicateScenarioMutation = useMutation({
    mutationFn: async () => {
      if (!selectedScenario) {
        throw new Error("No scenario selected.");
      }

      const { scenario } = await decisionEngineApi.createScenario(tenantId, {
        name: duplicateName.trim() || `Copy of ${selectedScenario.name}`,
        description: selectedScenario.description,
        trigger_object_type: selectedScenario.trigger,
      });

      try {
        await decisionEngineApi.createIteration(tenantId, scenario.id);
        return { scenario, iterationCreated: true as const };
      } catch (iterationError) {
        return {
          scenario,
          iterationCreated: false as const,
          iterationError,
        };
      }
    },
    onSuccess: ({ scenario, iterationCreated, iterationError }) => {
      void queryClient.invalidateQueries({
        queryKey: scenarioQueryKey(tenantId),
      });
      setDuplicateModalOpen(false);
      setSelectedScenario(null);
      setDuplicateName("");

      if (!iterationCreated) {
        pushToast({
          title: "Scenario duplicated without a draft",
          description:
            iterationError instanceof Error
              ? iterationError.message
              : "The initial draft iteration could not be created automatically.",
          variant: "error",
        });
      }

      router.push(`/detection/${scenario.id}/edit`);
    },
  });
  const createCustomListMutation = useMutation({
    mutationFn: () =>
      decisionEngineApi.createCustomList(tenantId, {
        name: listName.trim(),
        description: listDescription.trim(),
        kind: listType,
      }),
    onSuccess: async ({ custom_list }) => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-lists", tenantId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "custom-list-entries", tenantId],
      });
      setCreateListModalOpen(false);
      router.push(`/detection/lists/${custom_list.id}`);
    },
  });

  const scenarios = useMemo(
    () => (scenariosQuery.data?.scenarios ?? []).map(adaptScenario),
    [scenariosQuery.data]
  );
  const listEntryCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const item of customListEntriesQuery.data?.custom_list_entries ?? []) {
      if (!item.list_id) {
        continue;
      }
      counts.set(item.list_id, (counts.get(item.list_id) ?? 0) + 1);
    }
    return counts;
  }, [customListEntriesQuery.data?.custom_list_entries]);
  const lists = useMemo(
    () => adaptCustomLists(customListsQuery.data?.custom_lists ?? [], listEntryCounts),
    [customListsQuery.data?.custom_lists, listEntryCounts]
  );

  const createActionLabel = useMemo(() => {
    if (activeTab === "Lists") return "New List";
    return "New Scenario";
  }, [activeTab]);

  function handleTabChange(tab: DetectionTab) {
    const params = new URLSearchParams(searchParams.toString());
    if (tab === "Scenarios") {
      params.delete("tab");
    } else {
      params.set("tab", tab);
    }
    const query = params.toString();
    router.replace(`/detection${query ? `?${query}` : ""}`, { scroll: false });
  }

  function openScenarioModal() {
    setScenarioName("");
    setScenarioDescription("");
    setScenarioTrigger(triggerOptions[0] ?? "");
    setCreateModalOpen(true);
  }

  function openListModal() {
    setListName("");
    setListDescription("");
    setListType("generic_text");
    setCreateListModalOpen(true);
  }

  function openEditModal(scenario: DetectionScenario) {
    setSelectedScenario(scenario);
    setScenarioName(scenario.name);
    setScenarioDescription(scenario.description);
    setScenarioTrigger(scenario.trigger);
    setEditModalOpen(true);
  }

  function openDuplicateModal(scenario: DetectionScenario) {
    setSelectedScenario(scenario);
    setDuplicateName(`Copy of ${scenario.name}`);
    setDuplicateModalOpen(true);
  }

  function openArchiveModal(scenario: DetectionScenario) {
    setSelectedScenario(scenario);
    setArchiveModalOpen(true);
  }

  function handleSaveScenario() {
    if (!tenantId || !scenarioName.trim() || !effectiveScenarioTrigger) {
      return;
    }

    createScenarioMutation.mutate();
  }

  function handleSaveList() {
    if (!tenantId || !listName.trim()) {
      return;
    }
    createCustomListMutation.mutate();
  }

  function finishNavigationTour() {
    markFraudManagerNavigationTourCompleted(tenantId);
    router.push("/dashboard");
  }

  return (
    <>
      <div className="space-y-5 min-w-0 max-w-full">
        <PageHeader
          activeTab={activeTab}
          onTabChange={handleTabChange}
          action={activeTab === "Decisions" ? null : (
            <Button
              variant="accent"
              size="lg"
              onClick={
                activeTab === "Scenarios"
                  ? openScenarioModal
                  : activeTab === "Lists"
                    ? openListModal
                    : undefined
              }
              className="h-10 self-start rounded-xl bg-[#1f4f96] px-5 text-[14px] shadow-none hover:bg-[#163f79]"
            >
              <Plus className="size-4" />
              {createActionLabel}
            </Button>
          )}
        />

        {activeTab === "Lists" && guideStep === "lists" ? (
          <NavigationGuideBanner
            step="lists"
            onContinue={() => router.push("/detection?tab=Decisions&guide=decisions")}
            onSkip={finishNavigationTour}
          />
        ) : null}

        {activeTab === "Decisions" && guideStep === "decisions" ? (
          <NavigationGuideBanner
            step="decisions"
            onContinue={finishNavigationTour}
            onSkip={finishNavigationTour}
          />
        ) : null}

        {activeTab === "Scenarios" ? (
          <>
            <InfoBanner />
            {!tenantId ? (
              <Card className="rounded-xl border border-amber-200 bg-amber-50 shadow-none">
                <CardContent className="p-4 text-sm text-amber-800">
                  Sign in with an active tenant to load scenarios.
                </CardContent>
              </Card>
            ) : scenariosQuery.isLoading ? (
              <Card className="rounded-xl border border-slate-200 shadow-none">
                <CardContent className="p-4 text-sm text-slate-600">
                  Loading scenarios...
                </CardContent>
              </Card>
            ) : scenariosQuery.isError ? (
              <Card className="rounded-xl border border-red-200 bg-red-50 shadow-none">
                <CardContent className="p-4 text-sm text-red-700">
                  {scenariosQuery.error instanceof Error
                    ? scenariosQuery.error.message
                    : "Failed to load scenarios."}
                </CardContent>
              </Card>
            ) : (
              <ScenariosTable
                scenarios={scenarios}
                onEdit={openEditModal}
                onDuplicate={openDuplicateModal}
                onArchive={openArchiveModal}
              />
            )}
          </>
        ) : null}

        {activeTab === "Lists" ? (
          !tenantId ? (
            <Card className="rounded-xl border border-amber-200 bg-amber-50 shadow-none">
              <CardContent className="p-4 text-sm text-amber-800">
                Sign in with an active tenant to load lists.
              </CardContent>
            </Card>
          ) : customListsQuery.isLoading || customListEntriesQuery.isLoading ? (
            <Card className="rounded-xl border border-slate-200 shadow-none">
              <CardContent className="p-4 text-sm text-slate-600">Loading lists...</CardContent>
            </Card>
          ) : customListsQuery.isError || customListEntriesQuery.isError ? (
            <Card className="rounded-xl border border-red-200 bg-red-50 shadow-none">
              <CardContent className="p-4 text-sm text-red-700">
                {customListsQuery.error instanceof Error
                  ? customListsQuery.error.message
                  : customListEntriesQuery.error instanceof Error
                    ? customListEntriesQuery.error.message
                    : "Failed to load lists."}
              </CardContent>
            </Card>
          ) : (
            <ListsTable lists={lists} />
          )
        ) : null}
        {/* {activeTab === "Analytics" ? <AnalyticsView /> : null} */}
        {activeTab === "Decisions" ? (
          <LiveDecisionsView
            tenantId={tenantId}
            scenarios={scenarios}
            initialOutcome={initialOutcome}
          />
        ) : null}
      </div>

      <ScenarioModal
        isOpen={createModalOpen}
        name={scenarioName}
        description={scenarioDescription}
        trigger={effectiveScenarioTrigger}
        triggerOptions={triggerOptions}
        setName={setScenarioName}
        setDescription={setScenarioDescription}
        setTrigger={setScenarioTrigger}
        onClose={() => setCreateModalOpen(false)}
        onSave={handleSaveScenario}
        isSaving={createScenarioMutation.isPending}
        errorMessage={
          createScenarioMutation.error instanceof Error
            ? createScenarioMutation.error.message
            : null
        }
      />

      <SimpleScenarioModal
        isOpen={editModalOpen}
        title="Edit Scenario"
        confirmLabel="Save"
        onClose={() => {
          setEditModalOpen(false);
          setSelectedScenario(null);
        }}
        onConfirm={() => {
          if (!tenantId || !selectedScenario || !scenarioName.trim() || !scenarioTrigger) {
            return;
          }

          updateScenarioMutation.mutate();
        }}
        isPending={updateScenarioMutation.isPending}
        confirmDisabled={!scenarioName.trim() || !scenarioTrigger}
      >
        <div className="space-y-2">
          <label className="text-[15px] font-medium text-slate-950">Name</label>
          <Input
            value={scenarioName}
            onChange={(event) => setScenarioName(event.target.value)}
            className="h-12 rounded-lg border-slate-200 text-[14px] shadow-none"
          />
        </div>
        <div className="space-y-2">
          <label className="text-[15px] font-medium text-slate-950">Description</label>
          <Input
            value={scenarioDescription}
            onChange={(event) => setScenarioDescription(event.target.value)}
            className="h-12 rounded-lg border-slate-200 text-[14px] shadow-none"
          />
        </div>
      </SimpleScenarioModal>

      <SimpleScenarioModal
        isOpen={duplicateModalOpen}
        title="Duplicate"
        confirmLabel="Copy"
        onClose={() => {
          setDuplicateModalOpen(false);
          setSelectedScenario(null);
          setDuplicateName("");
        }}
        onConfirm={() => {
          if (!tenantId || !selectedScenario) {
            return;
          }

          duplicateScenarioMutation.mutate();
        }}
        isPending={duplicateScenarioMutation.isPending}
      >
        <p className="text-[15px] leading-8 text-slate-600">
          Create a copy of the scenario &quot;{selectedScenario?.name}&quot; with all its
          rules in their latest version, and its configuration.
        </p>
        <div className="space-y-2">
          <label className="text-[15px] font-medium text-slate-950">Name (optional)</label>
          <Input
            value={duplicateName}
            onChange={(event) => setDuplicateName(event.target.value)}
            className="h-12 rounded-lg border-slate-200 text-[14px] shadow-none"
          />
        </div>
      </SimpleScenarioModal>

      <SimpleScenarioModal
        isOpen={archiveModalOpen}
        title="Archive"
        confirmLabel="Archive"
        confirmClassName="bg-[#dd3719] hover:bg-[#c43014]"
        onClose={() => {
          setArchiveModalOpen(false);
          setSelectedScenario(null);
        }}
        onConfirm={() => {
          setArchiveModalOpen(false);
          setSelectedScenario(null);
        }}
      >
        <p className="text-[15px] leading-8 text-slate-600">
          Are you sure you want to archive the scenario &quot;{selectedScenario?.name}&quot;?
          You can unarchive it later.
        </p>
      </SimpleScenarioModal>

      <NewListModal
        isOpen={createListModalOpen}
        name={listName}
        description={listDescription}
        type={listType}
        setName={setListName}
        setDescription={setListDescription}
        setType={setListType}
        onClose={() => setCreateListModalOpen(false)}
        onSave={handleSaveList}
      />
    </>
  );
}

export default function DetectionWorkspace({ tenant, session }: { tenant: string; session: string }) {
  return (
    <DetectionIdentity.Provider value={{ tenant, session }}>
    <Suspense fallback={<div className="p-6 text-sm text-slate-600">Loading detection workspace...</div>}>
      <DetectionPageContent key={`${tenant}:${session}`} />
    </Suspense>
    </DetectionIdentity.Provider>
  );
}
