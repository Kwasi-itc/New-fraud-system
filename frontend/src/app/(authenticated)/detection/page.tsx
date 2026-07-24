"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type ReactNode, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Copy,
  Eye,
  Filter,
  Info,
  Lightbulb,
  Pencil,
  Plus,
  Search,
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
import { useToastStore } from "@/stores/toast-store";
import { cn } from "@/lib/utils";

const tabs = [
  "Scenarios",
  "Lists",
  // "Analytics",
  "Decisions",
] as const;

type DetectionTab = (typeof tabs)[number];

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

const DECISIONS_PAGE_SIZE = 25;

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

function LiveDecisionsView({
  tenantId,
  scenarios,
}: {
  tenantId: string;
  scenarios: DetectionScenario[];
}) {
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [newFilterOpen, setNewFilterOpen] = useState(false);
  const [activeFilterMenu, setActiveFilterMenu] = useState<string | null>(null);
  const [searchTerm, setSearchTerm] = useState("");
  const [pageOffset, setPageOffset] = useState(0);
  const [selectedFilters, setSelectedFilters] = useState<Array<{ type: string; value: string }>>(
    []
  );
  const filterItems = ["Scenario", "Trigger object", "Object ID", "Outcome"];
  const outcomeFilterItems = ["Approve", "Block and Review", "Decline", "Review"];
  const selectedScenarioFilter = selectedFilters.find((item) => item.type === "Scenario")?.value;
  const selectedObjectTypeFilter = selectedFilters.find(
    (item) => item.type === "Trigger object"
  )?.value;
  const selectedObjectIDFilter = selectedFilters.find((item) => item.type === "Object ID")?.value;
  const selectedOutcomeFilter = selectedFilters.find((item) => item.type === "Outcome")?.value;
  const trimmedSearchTerm = searchTerm.trim();
  const decisionsQuery = useQuery({
    queryKey: [
      "decision-engine",
      "decisions",
      tenantId,
      pageOffset,
      trimmedSearchTerm,
      selectedScenarioFilter ?? "",
      selectedObjectTypeFilter ?? "",
      selectedObjectIDFilter ?? "",
      selectedOutcomeFilter ?? "",
    ],
    queryFn: () =>
      decisionEngineApi.listDecisions(tenantId, {
        scenario_id: scenarioIdByName.get(selectedScenarioFilter ?? "") ?? undefined,
        object_type: selectedObjectTypeFilter || undefined,
        object_id: selectedObjectIDFilter || undefined,
        outcome: selectedOutcomeFilter
          ? outcomeFilterToApiValue(selectedOutcomeFilter)
          : undefined,
        search: trimmedSearchTerm || undefined,
        limit: DECISIONS_PAGE_SIZE,
        offset: pageOffset,
      }),
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
  const scenarioNameById = useMemo(
    () => new Map(scenarios.map((scenario) => [scenario.id, scenario.name])),
    [scenarios]
  );
  const scenarioIdByName = new Map(scenarios.map((scenario) => [scenario.name, scenario.id]));
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
  const pagination = decisionsQuery.data?.pagination;
  const canGoPrevious = pageOffset > 0;
  const totalRecords = pagination?.total_count ?? 0;
  const totalPages = pagination?.total_pages ?? 0;
  const canGoNext = pagination ? pageOffset + DECISIONS_PAGE_SIZE < totalRecords : false;
  const currentPage = Math.floor(pageOffset / DECISIONS_PAGE_SIZE) + 1;
  const paginationTokens = totalPages > 0 ? buildDecisionPaginationTokens(currentPage, totalPages) : [];
  const pageRangeLabel =
    decisionsQuery.data?.decisions?.length && pagination
      ? `${pagination.offset + 1}-${pagination.offset + decisionsQuery.data.decisions.length}`
      : "0-0";

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

  if (scenarios.length === 0) {
    return (
      <Card className="rounded-xl border border-slate-200 shadow-none">
        <CardContent className="p-6 text-sm text-slate-600">
          No decisions created yet because there are no scenarios configured.
        </CardContent>
      </Card>
    );
  }

  return (
    <>
      <div className="space-y-4">
        <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex w-full max-w-md gap-1.5">
            <div className="relative flex-1">
              <Search className="pointer-events-none absolute left-4 top-1/2 size-5 -translate-y-1/2 text-slate-500" />
              <Input
                value={searchTerm}
                onChange={(event) => {
                  setPageOffset(0);
                  setSearchTerm(event.target.value);
                }}
                placeholder="Search by decision, object, or scenario"
                className="h-10 rounded-xl border-slate-200 pl-11 text-[14px] shadow-none focus:border-slate-300"
              />
            </div>
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
            <Button
              disabled
              className="h-10 rounded-xl bg-[#6b7280] px-4 text-[14px] shadow-none hover:bg-[#5b6473]"
            >
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
                      upsertFilter(
                        item,
                        item === "Outcome"
                          ? "Approve"
                          : item === "Scenario"
                            ? scenarioFilterOptions[0] ?? ""
                            : ""
                      );
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
                  <span className="text-slate-500">{filter.value || "Any"}</span>
                  <span className="text-slate-500">×</span>
                </button>

                {activeFilterMenu === filter.type ? (
                  <div className="absolute left-0 top-12 z-10 w-[280px] rounded-xl border border-slate-200 bg-white p-2 shadow-[0_18px_30px_rgba(15,23,42,0.08)]">
                    {filter.type === "Outcome" ? (
                      <div className="space-y-2 p-2">
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
                            {filter.value === item ? <span className="text-[#1f4f96]">✓</span> : null}
                          </button>
                        ))}
                      </div>
                    ) : filter.type === "Scenario" ? (
                      <div className="space-y-1 p-2">
                        {scenarioFilterOptions.map((item) => (
                          <button
                            key={item}
                            type="button"
                            onClick={() => upsertFilter(filter.type, item)}
                            className="flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-left text-[14px] text-slate-950 hover:bg-slate-50"
                          >
                            <span>{item}</span>
                            {filter.value === item ? <span className="text-[#1f4f96]">✓</span> : null}
                          </button>
                        ))}
                      </div>
                    ) : (
                      <div className="space-y-2 p-2">
                        <Input
                          value={filter.value}
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
                setPageOffset(0);
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
                  {availableFilterItems().map((item) => (
                    <button
                      key={item}
                      type="button"
                      onClick={() => {
                        upsertFilter(
                          item,
                          item === "Outcome"
                            ? "Approve"
                            : item === "Scenario"
                              ? scenarioFilterOptions[0] ?? ""
                              : ""
                        );
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

        {decisionsQuery.isLoading ? (
          <Card className="rounded-xl border border-slate-200 shadow-none">
            <CardContent className="p-6 text-sm text-slate-600">Loading decisions...</CardContent>
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
            <CardContent className="p-6 text-sm text-slate-600">
              No decisions matched the current search and filters.
            </CardContent>
          </Card>
        ) : (
          <Card className="overflow-hidden rounded-xl border border-slate-200 shadow-none">
            <CardContent className="p-0">
              <div className="overflow-x-auto">
                <table className="min-w-full text-left">
                  <thead>
                    <tr className="border-b border-slate-200 bg-white text-[13px] font-semibold text-slate-950">
                      <th className="px-4 py-3">Date</th>
                      <th className="px-4 py-3">Scenario</th>
                      <th className="px-4 py-3">Live version</th>
                      <th className="px-4 py-3">Trigger object</th>
                      <th className="px-4 py-3">Case</th>
                      <th className="px-4 py-3">Score</th>
                      <th className="px-4 py-3">Outcome</th>
                    </tr>
                  </thead>
                  <tbody>
                    {decisions.map((item) => (
                      <tr
                        key={item.id}
                        className="border-b border-slate-100 text-[14px] text-slate-950 last:border-b-0"
                      >
                        <td className="px-4 py-3 text-slate-600">
                          {formatDecisionDate(item.created_at)}
                        </td>
                        <td className="px-4 py-3">
                          <Link
                            href={`/detection/decisions/${item.id}`}
                            className="font-medium text-slate-950 transition hover:text-[#2d63b8]"
                          >
                            {scenarioNameById.get(item.scenario_id) ?? item.scenario_id}
                          </Link>
                        </td>
                        <td className="px-4 py-3 text-slate-600">
                          {liveVersionByScenarioId.get(item.scenario_id) ?? "-"}
                        </td>
                        <td className="px-4 py-3">
                          <TablePill>{item.object_type}</TablePill>
                        </td>
                        <td className="px-4 py-3 font-medium text-slate-400">-</td>
                        <td className="px-4 py-3">{item.score}</td>
                        <td className="px-4 py-3">
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
                  <div>
                    Showing {pageRangeLabel}
                    {pagination ? ` of ${totalRecords}` : ""}
                    {trimmedSearchTerm || selectedFilters.length > 0
                      ? " matching current filters"
                      : ""}
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <Button
                      variant="outline"
                      disabled={!canGoPrevious}
                      onClick={() =>
                        setPageOffset((current) =>
                          Math.max(0, current - DECISIONS_PAGE_SIZE)
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
                            onClick={() => setPageOffset((token.page - 1) * DECISIONS_PAGE_SIZE)}
                            disabled={token.page === currentPage}
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
                      disabled={!canGoNext}
                      onClick={() =>
                        setPageOffset(pageOffset + DECISIONS_PAGE_SIZE)
                      }
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

export default function DetectionPage() {
  const router = useRouter();
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const queryClient = useQueryClient();
  const pushToast = useToastStore((state) => state.pushToast);
  const [activeTab, setActiveTab] = useState<DetectionTab>("Scenarios");
  const [createModalOpen, setCreateModalOpen] = useState(false);
  const [createListModalOpen, setCreateListModalOpen] = useState(false);
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
        trigger_object_type: scenarioTrigger,
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
    if (!tenantId || !scenarioName.trim() || !scenarioTrigger) {
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

  return (
    <>
      <div className="space-y-5">
        <PageHeader
          activeTab={activeTab}
          onTabChange={setActiveTab}
          action={
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
          }
        />

        {activeTab === "Scenarios" ? (
          <>
            <InfoBanner />
            {!tenantId ? (
              <Card className="rounded-xl border border-amber-200 bg-amber-50 shadow-none">
                <CardContent className="p-4 text-sm text-amber-800">
                  Set `NEXT_PUBLIC_DATA_MODEL_TENANT_ID` to load scenarios for a tenant.
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
                Set `NEXT_PUBLIC_DATA_MODEL_TENANT_ID` to load lists for a tenant.
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
          <LiveDecisionsView tenantId={tenantId} scenarios={scenarios} />
        ) : null}
      </div>

      <ScenarioModal
        isOpen={createModalOpen}
        name={scenarioName}
        description={scenarioDescription}
        trigger={scenarioTrigger}
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
