"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Ellipsis, Info, Lightbulb, Pencil, Plus, Save, Search, Trash2 } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { decisionEngineApi } from "@/lib/decision-engine-api";
import { cn } from "@/lib/utils";

const richOperatorOptions = [
  "+",
  "-",
  "×",
  "÷",
  "is in",
  "is not in",
  "contains",
  "does not contain",
  "starts with",
  "ends with",
  "contains any of",
  "does not contain any of",
  "is empty",
  "is not empty",
] as const;
const normalizedOperatorOptions = [
  "=",
  ">",
  "<",
  ">=",
  "<=",
  "+",
  "-",
  "*",
  "/",
  "is in",
  "is not in",
  "contains",
  "does not contain",
  "starts with",
  "ends with",
  "contains any of",
  "does not contain any of",
  "is empty",
  "is not empty",
] as const;
void richOperatorOptions;
const operandSections = [
  {
    title: "Fields",
    count: 54,
    groups: [
      { label: "From transactions", count: 22, items: ["transaction.amount", "transaction.currency", "transaction.direction"] },
      { label: "transactions_accounts", count: 8, items: ["transactions_accounts.country", "transactions_accounts.balance"] },
      { label: "accounts_companies", count: 12, items: ["accounts_companies.name", "accounts_companies.country"] },
      { label: "transactions_companies", count: 12, items: ["transactions_companies.name", "transactions_companies.industry"] },
    ],
  },
  {
    title: "Lists",
    count: 6,
    groups: [{ label: "Lists", count: 6, items: ["High_risk_MCC", "Countries Watchlist"] }],
  },
  {
    title: "Client risk",
    count: 1,
    groups: [{ label: "Client risk", count: 1, items: ["clientRiskScore"] }],
  },
  {
    title: "Functions",
    count: 15,
    groups: [{ label: "Functions", count: 15, items: ["balanceAverage", "count", "sum"] }],
  },
  {
    title: "Modeling",
    count: 1,
    groups: [{ label: "Modeling", count: 1, items: ["modeledRiskLevel"] }],
  },
] as const;
const screeningOutcomeOptions = ["Block and Review", "Decline", "Review"] as const;
const screeningEntityOptions = ["Any type", "Person", "Organization", "Vehicle"] as const;
const sanctionListGroups = [
  "United Nations",
  "Africa",
  "Asia",
  "Europe",
  "North America",
  "Oceania",
  "South America",
  "Others",
] as const;

const ruleContent = {
  "merchant-risk-mcc-codes": {
    title: "Merchant risk : MCC codes",
    description:
      "Check if the Merchant code provided by the transaction is an increased risk (gambling, tobacco...)",
    ruleGroup: null,
    settings: [
      ["if", "Tt", "category", "is in", "High_risk_MCC"],
      ["then, change the alert score by", "20"],
    ],
  },
  "check-transaction-value": {
    title: "Check transaction value",
    description: "check if the value of the transaction is over 1000EUR",
    ruleGroup: "Amount",
    settings: [
      ["if", "#", "value", ">", "1,000"],
      ["and", "Tt", "transactions_accounts.acco...", "=", '"BERLIN"'],
      ["then, change the alert score by", "10"],
    ],
  },
} as const;

type RuleCondition = {
  id: string;
  prefix: "if" | "and";
  left: string;
  operator: string;
  right: string;
};

type RuleConditionGroup = {
  id: string;
  conditions: RuleCondition[];
};

type SelectorTarget = {
  groupId: string;
  conditionId: string;
  field: "left" | "right" | "operator";
};

function SelectorTrigger({
  value,
  placeholder,
  onClick,
  className,
}: {
  value: string;
  placeholder: string;
  onClick: () => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "inline-flex h-10 items-center justify-between rounded-md border bg-white px-3 text-[14px] outline-none",
        value ? "text-slate-900" : "text-slate-400",
        className
      )}
    >
      <span className="truncate">{value || placeholder}</span>
      <span className="ml-3 shrink-0 text-slate-700">⌄</span>
    </button>
  );
}

function FloatingSelector({
  anchorRef,
  isOpen,
  width,
  preferredHeight,
  children,
}: {
  anchorRef: React.RefObject<HTMLDivElement | null>;
  isOpen: boolean;
  width: number;
  preferredHeight: number;
  children: React.ReactNode;
}) {
  const [position, setPosition] = useState<{ top: number; left: number } | null>(null);

  useEffect(() => {
    if (!isOpen || !anchorRef.current) {
      return;
    }

    const updatePosition = () => {
      const rect = anchorRef.current?.getBoundingClientRect();

      if (!rect) {
        return;
      }

      const viewportWidth = window.innerWidth;
      const viewportHeight = window.innerHeight;
      const spaceBelow = viewportHeight - rect.bottom - 16;
      const spaceAbove = rect.top - 16;
      const openAbove = spaceBelow < preferredHeight && spaceAbove > spaceBelow;

      setPosition({
        top: openAbove ? Math.max(16, rect.top - preferredHeight - 8) : rect.bottom + 8,
        left: Math.min(
          Math.max(16, rect.left),
          Math.max(16, viewportWidth - width - 16)
        ),
      });
    };

    updatePosition();
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);

    return () => {
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [anchorRef, isOpen, preferredHeight, width]);

  if (!isOpen || !position) {
    return null;
  }

  return createPortal(
    <div
      className="fixed z-[120]"
      style={{
        top: position.top,
        left: position.left,
      }}
    >
      {children}
    </div>,
    document.body
  );
}

function OperandSelectorField({
  value,
  placeholder,
  isOpen,
  onOpen,
  search,
  setSearch,
  onSelect,
  className,
}: {
  value: string;
  placeholder: string;
  isOpen: boolean;
  onOpen: () => void;
  search: string;
  setSearch: (value: string) => void;
  onSelect: (value: string) => void;
  className?: string;
}) {
  const triggerRef = useRef<HTMLDivElement | null>(null);

  return (
    <div ref={triggerRef} className="shrink-0">
      <SelectorTrigger
        value={value}
        placeholder={placeholder}
        onClick={onOpen}
        className={className}
      />
      <FloatingSelector
        anchorRef={triggerRef}
        isOpen={isOpen}
        width={480}
        preferredHeight={560}
      >
        <OperandSelectorMenu
          search={search}
          setSearch={setSearch}
          onSelect={onSelect}
        />
      </FloatingSelector>
    </div>
  );
}

function OperandSelectorMenu({
  search,
  setSearch,
  onSelect,
}: {
  search: string;
  setSearch: (value: string) => void;
  onSelect: (value: string) => void;
}) {
  const [expandedGroups, setExpandedGroups] = useState<string[]>([]);
  const lowered = search.toLowerCase();
  const filteredSections = operandSections
    .map((section) => ({
      ...section,
      groups: section.groups
        .map((group) => ({
          ...group,
          items: group.items.filter((item) => item.toLowerCase().includes(lowered)),
        }))
        .filter((group) => group.items.length > 0 || group.label.toLowerCase().includes(lowered)),
    }))
    .filter((section) => section.groups.length > 0 || section.title.toLowerCase().includes(lowered));

  function toggleGroup(groupKey: string) {
    setExpandedGroups((current) =>
      current.includes(groupKey)
        ? current.filter((item) => item !== groupKey)
        : [...current, groupKey]
    );
  }

  return (
    <div className="w-[480px] overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-[0_20px_48px_rgba(15,23,42,0.14)]">
      <div className="border-b border-slate-200 p-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-5 -translate-y-1/2 text-slate-400" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Select or create an operand"
            className="h-12 rounded-xl border-slate-200 pl-10 text-[14px] shadow-none"
          />
        </div>
      </div>
      <div className="max-h-[420px] overflow-y-auto p-3">
        {filteredSections.map((section) => (
          <div key={section.title} className="mb-5 last:mb-0">
            <div className="mb-2 flex items-center gap-2 text-[16px] font-semibold text-slate-950">
              <span>{section.title}</span>
              <span className="text-slate-300">{section.count}</span>
            </div>
            <div className="space-y-1">
              {section.groups.map((group) => {
                const groupKey = `${section.title}-${group.label}`;
                const isExpanded = lowered.length > 0 || expandedGroups.includes(groupKey);

                return (
                  <div key={groupKey} className="space-y-1">
                  <button
                    type="button"
                    onClick={() => toggleGroup(groupKey)}
                    className="flex w-full items-center justify-between rounded-lg px-2 py-2 text-[15px] text-slate-950 hover:bg-slate-50"
                  >
                    <span>
                      {group.label} <span className="text-slate-300">{group.count}</span>
                    </span>
                    <span>›</span>
                  </button>
                  {isExpanded ? group.items.map((item) => (
                    <button
                      key={item}
                      type="button"
                      onClick={() => onSelect(item)}
                      className="flex w-full items-center rounded-lg px-3 py-2 text-left text-[15px] text-slate-800 hover:bg-slate-50"
                    >
                      {item}
                    </button>
                  )) : null}
                </div>
                );
              })}
            </div>
          </div>
        ))}
      </div>
      <div className="flex gap-3 border-t border-slate-200 p-3">
        <Button variant="outline" className="h-10 rounded-xl border-slate-200 px-4 text-[14px] shadow-none">
          Clear
        </Button>
        <Button variant="outline" className="h-10 rounded-xl border-slate-200 px-4 text-[14px] shadow-none">
          Edit
        </Button>
        <Button variant="outline" className="h-10 rounded-xl border-slate-200 px-4 text-[14px] shadow-none">
          Copy
        </Button>
      </div>
    </div>
  );
}

function OperatorSelectorField({
  value,
  placeholder,
  isOpen,
  onOpen,
  search,
  setSearch,
  onSelect,
  className,
}: {
  value: string;
  placeholder: string;
  isOpen: boolean;
  onOpen: () => void;
  search: string;
  setSearch: (value: string) => void;
  onSelect: (value: string) => void;
  className?: string;
}) {
  const triggerRef = useRef<HTMLDivElement | null>(null);

  return (
    <div ref={triggerRef} className="shrink-0">
      <SelectorTrigger
        value={value}
        placeholder={placeholder}
        onClick={onOpen}
        className={className}
      />
      <FloatingSelector
        anchorRef={triggerRef}
        isOpen={isOpen}
        width={320}
        preferredHeight={460}
      >
        <OperatorSelectorMenu
          search={search}
          setSearch={setSearch}
          onSelect={onSelect}
        />
      </FloatingSelector>
    </div>
  );
}

function OperatorSelectorMenu({
  search,
  setSearch,
  onSelect,
}: {
  search: string;
  setSearch: (value: string) => void;
  onSelect: (value: string) => void;
}) {
  const filtered = normalizedOperatorOptions.filter((option) =>
    option.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="w-[320px] overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-[0_20px_48px_rgba(15,23,42,0.14)]">
      <div className="border-b border-slate-200 p-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-5 -translate-y-1/2 text-slate-400" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder=""
            className="h-12 rounded-xl border-slate-200 pl-10 text-[14px] shadow-none"
          />
        </div>
      </div>
      <div className="max-h-[360px] overflow-y-auto p-3">
        {filtered.map((option) => (
          <button
            key={option}
            type="button"
            onClick={() => onSelect(option)}
            className="flex w-full items-center rounded-lg px-3 py-2.5 text-left text-[15px] text-slate-950 hover:bg-slate-50"
          >
            {option}
          </button>
        ))}
      </div>
    </div>
  );
}

function SettingRow({ tokens }: { tokens: string[] }) {
  if (tokens.length === 2) {
    return (
      <div className="flex flex-wrap items-center gap-2.5 text-[14px] text-slate-950">
        <span className="rounded-md bg-slate-50 px-3 py-2.5 text-slate-600">
          {tokens[0]}
        </span>
        <span className="rounded-md bg-slate-50 px-3 py-2.5">{tokens[1]}</span>
      </div>
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-2.5 text-[14px] text-slate-950">
      <span className="rounded-md bg-slate-50 px-3 py-2.5 text-slate-600">
        {tokens[0]}
      </span>
      <span className="rounded-md bg-slate-50 px-2.5 py-2.5 text-slate-600">
        {tokens[1]}
      </span>
      <span className="rounded-md bg-slate-50 px-3 py-2.5">{tokens[2]}</span>
      <span className="rounded-md bg-slate-50 px-3 py-2.5">{tokens[3]}</span>
      <span className="rounded-md bg-slate-50 px-3 py-2.5">{tokens[4]}</span>
    </div>
  );
}

function NewRuleEditor({ scenarioId }: { scenarioId: string }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const queryClient = useQueryClient();
  const [ruleName, setRuleName] = useState("New rule");
  const [description, setDescription] = useState("");
  const [scoreModifier, setScoreModifier] = useState("0");
  const [ruleGroup, setRuleGroup] = useState("");
  const [conditionGroups, setConditionGroups] = useState<RuleConditionGroup[]>([
    {
      id: "group-1",
      conditions: [
        { id: "group-1-condition-1", prefix: "if", left: "", operator: "", right: "" },
        { id: "group-1-condition-2", prefix: "and", left: "", operator: "", right: "" },
      ],
    },
  ]);
  const [saveMessage, setSaveMessage] = useState<string | null>(null);
  const [selectorTarget, setSelectorTarget] = useState<SelectorTarget | null>(null);
  const [selectorSearch, setSelectorSearch] = useState("");

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

  const createRuleMutation = useMutation({
    mutationFn: async () => {
      const iterations = iterationsQuery.data?.iterations ?? [];
      const targetIteration = [...iterations].sort((a, b) => b.version - a.version)[0];

      if (!targetIteration) {
        throw new Error("Create a scenario iteration before adding rules.");
      }

      return decisionEngineApi.createRule(tenantId, scenarioId, targetIteration.id, {
        name: ruleName.trim(),
        description: description.trim(),
        formula: {
          operator: "or",
          groups: conditionGroups.map((group) => ({
            operator: "and",
            conditions: group.conditions.map((condition) => ({
              prefix: condition.prefix,
              left: condition.left,
              operator: condition.operator,
              right: condition.right,
            })),
          })),
        },
        score_modifier: Number(scoreModifier) || 0,
        rule_group: ruleGroup.trim(),
      });
    },
    onSuccess: async () => {
      setSaveMessage("Rule created.");
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "iterations", tenantId, scenarioId],
      });
    },
  });

  function updateCondition(
    groupId: string,
    conditionId: string,
    field: keyof Pick<RuleCondition, "left" | "operator" | "right">,
    value: string
  ) {
    setConditionGroups((current) =>
      current.map((group) =>
        group.id === groupId
          ? {
              ...group,
              conditions: group.conditions.map((condition) =>
                condition.id === conditionId ? { ...condition, [field]: value } : condition
              ),
            }
          : group
      )
    );
  }

  function removeCondition(groupId: string, conditionId: string) {
    setConditionGroups((current) =>
      current
        .map((group) =>
          group.id === groupId
            ? {
                ...group,
                conditions: group.conditions.filter((condition) => condition.id !== conditionId),
              }
            : group
        )
        .filter((group) => group.conditions.length > 0)
    );
  }

  function addCondition(groupId: string) {
    setConditionGroups((current) =>
      current.map((group) =>
        group.id === groupId
          ? {
              ...group,
              conditions: [
                ...group.conditions,
                {
                  id: `${groupId}-condition-${group.conditions.length + 1}`,
                  prefix: "and",
                  left: "",
                  operator: "",
                  right: "",
                },
              ],
            }
          : group
      )
    );
  }

  function addGroup() {
    setConditionGroups((current) => [
      ...current,
      {
        id: `group-${current.length + 1}`,
        conditions: [
          {
            id: `group-${current.length + 1}-condition-1`,
            prefix: "if",
            left: "",
            operator: "",
            right: "",
          },
        ],
      },
    ]);
  }

  function openSelector(target: SelectorTarget) {
    setSelectorTarget(target);
    setSelectorSearch("");
  }

  function closeSelector() {
    setSelectorTarget(null);
    setSelectorSearch("");
  }

  return (
    <div className="mx-auto w-full max-w-[1120px] space-y-5 px-4 sm:px-6 xl:px-8">
      <div className="border-b border-slate-200 pb-3">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex flex-wrap items-center gap-4 text-[15px] text-slate-600">
            <Link
              href={`/detection/${scenarioId}/edit`}
              className="inline-flex size-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"
            >
              <ArrowLeft className="size-4" />
            </Link>
            <span className="font-medium text-slate-700">Detection</span>
            <span>/</span>
            <span className="font-medium text-slate-700">Scenarios</span>
            <span>/</span>
            <span className="font-medium text-slate-700">German - Validate card payouts</span>
            <span className="text-[#1f4f96]">transactions</span>
            <Info className="size-3.5" />
            <span>/</span>
            <span className="font-medium text-slate-700">Draft</span>
            <span>/</span>
            <span className="font-medium text-slate-700">Rules</span>
            <span>/</span>
            <span className="font-semibold text-slate-950">New rule</span>
          </div>

          <Button variant="outline" className="h-10 rounded-xl border-slate-200 px-4 text-[14px] shadow-none">
            Edit
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-3">
          <Input
            value={ruleName}
            onChange={(event) => setRuleName(event.target.value)}
            className="h-11 w-[360px] rounded-xl border-slate-200 text-[1.35rem] font-semibold tracking-tight text-slate-950 shadow-none"
          />
          <Input
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            placeholder="Add a description..."
            className="h-10 w-[360px] rounded-xl border-slate-200 text-[15px] text-slate-700 shadow-none"
          />
          <Input
            value={ruleGroup}
            onChange={(event) => setRuleGroup(event.target.value)}
            placeholder="Add a rule group"
            className="h-10 w-[220px] rounded-xl border-slate-200 text-[14px] text-slate-700 shadow-none"
          />
        </div>

        <div className="flex items-center gap-3">
          <button
            type="button"
            className="inline-flex size-10 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"
          >
            <Ellipsis className="size-5" />
          </button>
          <Button
            onClick={() => createRuleMutation.mutate()}
            disabled={createRuleMutation.isPending || !tenantId}
            className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
          >
            <Save className="size-4" />
            {createRuleMutation.isPending ? "Saving..." : "Save"}
          </Button>
        </div>
      </div>

      {createRuleMutation.error instanceof Error ? (
        <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-[13px] text-red-700">
          {createRuleMutation.error.message}
        </div>
      ) : null}
      {saveMessage ? (
        <div className="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-[13px] text-emerald-700">
          {saveMessage}
        </div>
      ) : null}
      {scenarioQuery.data?.scenario?.name ? (
        <div className="text-[13px] text-slate-500">
          Creating this rule inside{" "}
          <span className="font-medium text-slate-700">{scenarioQuery.data.scenario.name}</span>.
        </div>
      ) : null}

      <div className="border-t border-slate-200 pt-5">
        <h2 className="mb-4 text-[1rem] font-semibold text-slate-950">Settings</h2>

        <Card className="rounded-xl border border-slate-200 shadow-none">
          <CardContent className="space-y-4 p-5">
            {conditionGroups.map((group, groupIndex) => (
              <div key={group.id} className="space-y-4">
                {groupIndex > 0 ? (
                  <div className="flex justify-center">
                    <div className="rounded-full bg-slate-100 px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.16em] text-slate-500">
                      Or
                    </div>
                  </div>
                ) : null}

                <div className="rounded-xl border border-slate-200 p-4">
                  <div className="space-y-3">
                    {group.conditions.map((condition) => (
                      <div key={condition.id} className="space-y-2.5 overflow-visible">
                        <div className="overflow-x-auto overflow-y-visible pb-2">
                          <div className="flex min-w-[780px] items-center gap-2.5 overflow-visible">
                          <span className="rounded-md bg-slate-50 px-3 py-2.5 text-[14px] font-medium text-slate-600">
                            {condition.prefix}
                          </span>
                          <OperandSelectorField
                            value={condition.left}
                            placeholder="Select an operand..."
                            isOpen={
                              selectorTarget?.groupId === group.id &&
                              selectorTarget.conditionId === condition.id &&
                              selectorTarget.field === "left"
                            }
                            onOpen={() =>
                              openSelector({
                                groupId: group.id,
                                conditionId: condition.id,
                                field: "left",
                              })
                            }
                            search={selectorSearch}
                            setSearch={setSelectorSearch}
                            onSelect={(value) => {
                              updateCondition(group.id, condition.id, "left", value);
                              closeSelector();
                            }}
                            className="w-[260px] border-[#ff6b57]"
                          />
                          <OperatorSelectorField
                            value={condition.operator}
                            placeholder="..."
                            isOpen={
                              selectorTarget?.groupId === group.id &&
                              selectorTarget.conditionId === condition.id &&
                              selectorTarget.field === "operator"
                            }
                            onOpen={() =>
                              openSelector({
                                groupId: group.id,
                                conditionId: condition.id,
                                field: "operator",
                              })
                            }
                            search={selectorSearch}
                            setSearch={setSelectorSearch}
                            onSelect={(value) => {
                              updateCondition(group.id, condition.id, "operator", value);
                              closeSelector();
                            }}
                            className="w-[110px] border-[#ff6b57]"
                          />
                          <OperandSelectorField
                            value={condition.right}
                            placeholder="Select an operand..."
                            isOpen={
                              selectorTarget?.groupId === group.id &&
                              selectorTarget.conditionId === condition.id &&
                              selectorTarget.field === "right"
                            }
                            onOpen={() =>
                              openSelector({
                                groupId: group.id,
                                conditionId: condition.id,
                                field: "right",
                              })
                            }
                            search={selectorSearch}
                            setSearch={setSelectorSearch}
                            onSelect={(value) => {
                              updateCondition(group.id, condition.id, "right", value);
                              closeSelector();
                            }}
                            className="w-[260px] border-[#ff6b57]"
                          />
                          <button
                            type="button"
                            onClick={() => removeCondition(group.id, condition.id)}
                            className="inline-flex size-7 shrink-0 items-center justify-center rounded-md border border-slate-200 text-slate-400"
                          >
                            <Trash2 className="size-3.5" />
                          </button>
                        </div>
                        </div>
                        <div className="inline-flex rounded-md bg-[#ffd9d2] px-3 py-1 text-[13px] text-[#dd3719]">
                          {[condition.left, condition.operator, condition.right].filter(Boolean).length} / 3 filled
                        </div>
                      </div>
                    ))}
                  </div>

                  <div className="mt-4">
                    <Button
                      variant="outline"
                      onClick={() => addCondition(group.id)}
                      className="h-10 rounded-xl border-[#2d63b8] px-4 text-[14px] text-[#1f4f96] shadow-none"
                    >
                      <Plus className="size-4" />
                      Condition
                    </Button>
                  </div>
                </div>
              </div>
            ))}

            <div className="flex flex-wrap gap-3">
              <Button
                variant="outline"
                onClick={addGroup}
                className="h-10 rounded-xl border-[#2d63b8] px-4 text-[14px] text-[#1f4f96] shadow-none"
              >
                <Plus className="size-4" />
                Group
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card className="mt-4 rounded-xl border border-slate-200 shadow-none">
          <CardContent className="p-5">
            <div className="flex flex-wrap items-center gap-2.5">
              <span className="rounded-md bg-slate-50 px-3 py-2.5 text-[14px] text-slate-700">
                then, change the alert score by
              </span>
              <Input
                value={scoreModifier}
                onChange={(event) => setScoreModifier(event.target.value)}
                className="h-10 min-w-[180px] rounded-md border-slate-200 text-[14px] text-slate-950 shadow-none"
              />
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function ScreeningRuleEditor({ scenarioId }: { scenarioId: string }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const queryClient = useQueryClient();
  const [name, setName] = useState("Main Screening");
  const [description, setDescription] = useState("");
  const [threshold, setThreshold] = useState("80");
  const [outcomeMenuOpen, setOutcomeMenuOpen] = useState(false);
  const [outcome, setOutcome] = useState<(typeof screeningOutcomeOptions)[number]>(
    "Block and Review"
  );
  const [counterpartyId, setCounterpartyId] = useState("");
  const [entityType, setEntityType] = useState<(typeof screeningEntityOptions)[number]>(
    "Any type"
  );
  const [nameField, setNameField] = useState("");
  const [screeningSearch, setScreeningSearch] = useState("");
  const [typeFilterOpen, setTypeFilterOpen] = useState(false);
  const [excludeCustomList, setExcludeCustomList] = useState(false);
  const [excludeNumbers, setExcludeNumbers] = useState(false);
  const [minCharactersEnabled, setMinCharactersEnabled] = useState(false);
  const [minCharacters, setMinCharacters] = useState("5");
  const [aiRecognitionEnabled, setAiRecognitionEnabled] = useState(false);
  const [selectedLists, setSelectedLists] = useState<string[]>([]);
  const [saveMessage, setSaveMessage] = useState<string | null>(null);

  const createScreeningMutation = useMutation({
    mutationFn: async () =>
      decisionEngineApi.createScreeningConfig(tenantId, scenarioId, {
        name: name.trim(),
        allowed_outcomes: [outcome],
        provider: "screening",
        active: true,
        config_json: {
          description: description.trim(),
          threshold: Number(threshold) || 70,
          counterparty_id: counterpartyId,
          entity_type: entityType,
          name_field: nameField,
          sanction_lists: selectedLists,
          trigger_mode: "all",
          exclude_custom_list: excludeCustomList,
          exclude_numbers: excludeNumbers,
          min_characters: minCharactersEnabled ? Number(minCharacters) || 5 : null,
          ai_name_recognition: aiRecognitionEnabled,
        },
      }),
    onSuccess: async () => {
      setSaveMessage("Screening rule created.");
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "screening-configs", tenantId, scenarioId],
      });
    },
  });

  function toggleList(group: string) {
    setSelectedLists((current) =>
      current.includes(group)
        ? current.filter((item) => item !== group)
        : [...current, group]
      );
  }

  const filteredSanctionGroups = sanctionListGroups.filter((group) =>
    group.toLowerCase().includes(screeningSearch.toLowerCase())
  );

  return (
    <div className="mx-auto w-full max-w-[1180px] space-y-5 px-4 sm:px-6 xl:px-8">
      <div className="border-b border-slate-200 pb-3">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex flex-wrap items-center gap-4 text-[15px] text-slate-600">
            <Link
              href={`/detection/${scenarioId}/edit`}
              className="inline-flex size-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"
            >
              <ArrowLeft className="size-4" />
            </Link>
            <span className="font-medium text-slate-700">Detection</span>
            <span>/</span>
            <span className="font-medium text-slate-700">Scenarios</span>
            <span>/</span>
            <span className="font-medium text-slate-700">German - Validate card payouts</span>
            <span className="text-[#1f4f96]">transactions</span>
            <Info className="size-3.5" />
            <span>/</span>
            <span className="font-medium text-slate-700">Draft</span>
            <span>/</span>
            <span className="font-medium text-slate-700">Rules</span>
            <span>/</span>
            <span className="font-semibold text-slate-950">Screening</span>
          </div>

          <Button variant="outline" className="h-10 rounded-xl border-slate-200 px-4 text-[14px] shadow-none">
            Edit
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-3">
          <Input
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Screening title..."
            className="h-11 w-[360px] rounded-xl border-slate-200 text-[1.35rem] font-semibold tracking-tight text-slate-950 shadow-none"
          />
          <Input
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            placeholder="Add a description..."
            className="h-10 w-[360px] rounded-xl border-slate-200 text-[15px] text-slate-700 shadow-none"
          />
          <div className="inline-flex items-center gap-2 rounded-full border border-[#2d63b8] bg-white px-3 py-1.5 text-[13px] text-[#2d63b8]">
            Screening
            <Pencil className="size-3.5 text-slate-300" />
          </div>
        </div>

        <div className="flex items-center gap-3">
          <Button
            variant="outline"
            className="h-10 rounded-xl border-[#dd3719] bg-white px-4 text-[14px] text-[#dd3719] shadow-none"
          >
            <Trash2 className="size-4" />
            Delete
          </Button>
          <Button
            onClick={() => createScreeningMutation.mutate()}
            disabled={createScreeningMutation.isPending || !tenantId || !name.trim()}
            className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
          >
            <Save className="size-4" />
            {createScreeningMutation.isPending ? "Saving..." : "Save"}
          </Button>
        </div>
      </div>

      {createScreeningMutation.error instanceof Error ? (
        <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-[13px] text-red-700">
          {createScreeningMutation.error.message}
        </div>
      ) : null}
      {saveMessage ? (
        <div className="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-[13px] text-emerald-700">
          {saveMessage}
        </div>
      ) : null}
      {!name.trim() ? (
        <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-[13px] text-red-700">
          Too small: expected string to have &gt;=1 characters
        </div>
      ) : null}

      <Card className="rounded-xl border border-slate-200 shadow-none">
        <CardContent className="space-y-4 p-5">
          <div className="rounded-lg border border-slate-200 px-4 py-3 text-[14px] text-slate-950">
            <div className="flex items-center gap-2">
              <Lightbulb className="size-4 text-[#1f4f96]" />
              <span>
                Determines whether the screening is relevant for each trigger object{" "}
                <span className="font-medium text-[#1f4f96]">(learn more)</span>
              </span>
            </div>
          </div>
          <div className="rounded-lg border border-[#3b82f6] bg-blue-50 px-3.5 py-3 text-[14px] text-[#2563eb]">
            All <span className="font-semibold">transactions</span> will be checked
          </div>
          <div className="flex justify-end">
            <Button variant="outline" className="h-9 rounded-xl border-slate-200 px-4 text-[14px] shadow-none">
              Add trigger condition
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card className="rounded-xl border border-slate-200 shadow-none">
        <CardContent className="space-y-5 p-5">
          <div className="flex flex-wrap items-center gap-3 text-[15px] text-slate-950">
            <span>Considering matchings above</span>
            <Input
              value={threshold}
              onChange={(event) => setThreshold(event.target.value)}
              className="h-10 w-[72px] rounded-xl border-slate-200 text-[15px] shadow-none"
            />
            <span>% (default to 70% if left empty).</span>
          </div>
          <div className="flex flex-wrap items-center gap-3 text-[15px] text-slate-950">
            <span>Force the outcome to</span>
            <div className="relative">
              <button
                type="button"
                onClick={() => setOutcomeMenuOpen((current) => !current)}
                className={cn(
                  "inline-flex h-10 items-center gap-2 rounded-full px-3.5 text-[14px] outline-none",
                  outcome === "Block and Review"
                    ? "bg-orange-100 text-orange-700"
                    : outcome === "Decline"
                      ? "bg-rose-100 text-rose-700"
                      : "bg-amber-100 text-amber-700"
                )}
              >
                {outcome}
                <span>▾</span>
              </button>
              {outcomeMenuOpen ? (
                <div className="absolute left-0 top-12 z-10 w-[260px] rounded-xl border border-slate-200 bg-white p-2 shadow-[0_18px_30px_rgba(15,23,42,0.08)]">
                  <div className="mb-2 h-12 rounded-lg border border-slate-200" />
                  {screeningOutcomeOptions.map((item) => (
                    <button
                      key={item}
                      type="button"
                      onClick={() => {
                        setOutcome(item);
                        setOutcomeMenuOpen(false);
                      }}
                      className="flex w-full items-center rounded-lg px-3 py-2.5 text-left text-[14px] text-slate-950 hover:bg-slate-50"
                    >
                      <span
                        className={cn(
                          "rounded-full px-3 py-1.5",
                          item === "Block and Review"
                            ? "bg-orange-100 text-orange-700"
                            : item === "Decline"
                              ? "bg-rose-100 text-rose-700"
                              : "bg-amber-100 text-amber-700"
                        )}
                      >
                        {item}
                      </span>
                    </button>
                  ))}
                </div>
              ) : null}
            </div>
            <span>if the screening is triggered</span>
          </div>
        </CardContent>
      </Card>

      <div className="space-y-4">
        <div>
          <label className="mb-2 flex items-center gap-2 text-[15px] font-semibold text-slate-950">
            Counterparty ID
            <Info className="size-4 text-slate-400" />
          </label>
          <Card className="rounded-xl border border-slate-200 shadow-none">
            <CardContent className="p-5">
              <select
                value={counterpartyId}
                onChange={(event) => setCounterpartyId(event.target.value)}
                className="h-12 w-full rounded-xl border border-slate-200 bg-white px-4 text-[14px] outline-none"
              >
                <option value="">Select a unique counterpart...</option>
                <option value="transaction.counterparty_id">transaction.counterparty_id</option>
                <option value="transactions_accounts.account_id">transactions_accounts.account_id</option>
              </select>
            </CardContent>
          </Card>
        </div>

        <div>
          <label className="mb-2 block text-[15px] font-semibold text-slate-950">
            Matching settings
          </label>
          <Card className="rounded-xl border border-slate-200 shadow-none">
            <CardContent className="space-y-4 p-5">
              <div className="rounded-lg border border-slate-200 px-4 py-3 text-[14px] text-slate-950">
                <div className="flex items-center gap-2">
                  <Lightbulb className="size-4 text-[#1f4f96]" />
                  <span>Choose information that should be checked.</span>
                </div>
              </div>
              <div className="space-y-2">
                <label className="flex items-center gap-2 text-[14px] text-slate-950">
                  What kind of entity are you screening for?
                  <Info className="size-4 text-slate-400" />
                </label>
                <select
                  value={entityType}
                  onChange={(event) =>
                    setEntityType(event.target.value as (typeof screeningEntityOptions)[number])
                  }
                  className="h-10 w-[220px] rounded-xl border border-slate-200 bg-white px-3 text-[14px] outline-none"
                >
                  {screeningEntityOptions.map((item) => (
                    <option key={item} value={item}>
                      {item}
                    </option>
                  ))}
                </select>
              </div>
              <div className="rounded-xl border border-slate-200 p-4">
                <div className="mb-3 flex items-center gap-2 text-[14px] font-medium text-slate-950">
                  Name
                  <Info className="size-4 text-slate-400" />
                </div>
                <div className="flex flex-wrap items-center gap-3">
                  <span className="text-slate-400">⋮⋮</span>
                  <span className="text-slate-400">+</span>
                  <select
                    value={nameField}
                    onChange={(event) => setNameField(event.target.value)}
                    className="h-10 min-w-[260px] rounded-xl border border-slate-200 bg-white px-3 text-[14px] outline-none"
                  >
                    <option value="">Select the first name or full ...</option>
                    <option value="transaction.full_name">transaction.full_name</option>
                    <option value="transactions_accounts.name">transactions_accounts.name</option>
                    <option value="transaction.company_name">transaction.company_name</option>
                  </select>
                </div>
                <div className="mt-4 space-y-3 text-[14px] text-slate-950">
                  <label className="flex items-center gap-3">
                    <input
                      type="checkbox"
                      checked={excludeCustomList}
                      onChange={(event) => setExcludeCustomList(event.target.checked)}
                      className="size-5 rounded-full border border-slate-300"
                    />
                    <span>Exclude terms listed in a custom list</span>
                  </label>
                  <label className="flex items-center gap-3">
                    <input
                      type="checkbox"
                      checked={excludeNumbers}
                      onChange={(event) => setExcludeNumbers(event.target.checked)}
                      className="size-5 rounded-full border border-slate-300"
                    />
                    <span>Exclude numbers</span>
                  </label>
                  <label className="flex items-center gap-3">
                    <input
                      type="checkbox"
                      checked={minCharactersEnabled}
                      onChange={(event) => setMinCharactersEnabled(event.target.checked)}
                      className="size-5 rounded-full border border-slate-300"
                    />
                    <span>Do not screen if the text contains less than</span>
                    <Input
                      value={minCharacters}
                      onChange={(event) => setMinCharacters(event.target.value)}
                      disabled={!minCharactersEnabled}
                      className="h-10 w-[56px] rounded-xl border-slate-200 text-[14px] shadow-none"
                    />
                    <span>characters</span>
                  </label>
                  <label className="flex items-center gap-3">
                    <input
                      type="checkbox"
                      checked={aiRecognitionEnabled}
                      onChange={(event) => setAiRecognitionEnabled(event.target.checked)}
                      className="size-5 rounded-full border border-slate-300"
                    />
                    <span>
                      Enable AI name recognition{" "}
                      <span className="rounded-full bg-blue-100 px-2 py-0.5 text-[11px] text-[#1f4f96]">
                        beta
                      </span>
                    </span>
                  </label>
                </div>
                <div className="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-[14px] text-slate-800">
                  For effective screening, it is recommended to fill in at least the name or
                  registration number for an organization/vehicle, or the name or ID number for a
                  person.
                </div>
              </div>
            </CardContent>
          </Card>
        </div>

        <div>
          <label className="mb-2 block text-[15px] font-semibold text-slate-950">
            Sanction lists
          </label>
          <Card className="rounded-xl border border-slate-200 shadow-none">
            <CardContent className="space-y-4 p-5">
              <div className="rounded-lg border border-slate-200 px-4 py-3 text-[14px] text-slate-950">
                <div className="flex items-center gap-2">
                  <Lightbulb className="size-4 text-[#1f4f96]" />
                  <span>Select lists that are relevant to the scenario</span>
                </div>
              </div>
              <div className="flex gap-3">
                <Input
                  value={screeningSearch}
                  onChange={(event) => setScreeningSearch(event.target.value)}
                  placeholder="Search for a specific list..."
                  className="h-10 rounded-xl border-slate-200 text-[14px] shadow-none"
                />
                <div className="relative">
                  <Button
                    variant="outline"
                    onClick={() => setTypeFilterOpen((current) => !current)}
                    className="h-10 rounded-xl border-slate-200 px-4 text-[14px] shadow-none"
                  >
                    Type
                  </Button>
                  {typeFilterOpen ? (
                    <div className="absolute right-0 top-12 z-10 w-[180px] rounded-xl border border-slate-200 bg-white p-2 shadow-[0_18px_30px_rgba(15,23,42,0.08)]">
                      {["PEP", "Sanctions", "Watchlists"].map((item) => (
                        <button
                          key={item}
                          type="button"
                          className="flex w-full items-center rounded-lg px-3 py-2.5 text-left text-[14px] text-slate-950 hover:bg-slate-50"
                        >
                          {item}
                        </button>
                      ))}
                    </div>
                  ) : null}
                </div>
              </div>
              <div className="space-y-2">
                {filteredSanctionGroups.map((group) => (
                  <button
                    key={group}
                    type="button"
                    onClick={() => toggleList(group)}
                    className="flex w-full items-center justify-between rounded-xl border border-slate-100 bg-slate-50 px-4 py-4 text-left text-[15px] text-slate-950"
                  >
                    <div className="flex items-center gap-3">
                      <span>›</span>
                      <span>{group}</span>
                    </div>
                    <div className="flex items-center gap-3 text-[13px] text-slate-500">
                      <span>Select all</span>
                      <span
                        className={cn(
                          "inline-flex size-5 rounded-md border",
                          selectedLists.includes(group)
                            ? "border-[#1f4f96] bg-[#1f4f96]"
                            : "border-slate-300 bg-white"
                        )}
                      />
                    </div>
                  </button>
                ))}
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

export function RuleDetailPage({
  scenarioId,
  ruleId,
}: {
  scenarioId: string;
  ruleId: string;
}) {
  if (ruleId === "new") {
    return <NewRuleEditor scenarioId={scenarioId} />;
  }

  if (ruleId === "new-screening") {
    return <ScreeningRuleEditor scenarioId={scenarioId} />;
  }

  const rule =
    ruleContent[ruleId as keyof typeof ruleContent] ?? ruleContent["check-transaction-value"];

  return (
    <div className="mx-auto w-full max-w-[1120px] space-y-6 px-4 sm:px-6 xl:px-8">
      <div className="border-b border-slate-200 pb-3">
        <div className="flex flex-wrap items-center gap-4 text-[15px] text-slate-600">
          <Link
            href={`/detection/${scenarioId}/edit`}
            className="inline-flex size-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"
          >
            <ArrowLeft className="size-4" />
          </Link>
          <span className="font-medium text-slate-700">Detection</span>
          <span>/</span>
          <span className="font-medium text-slate-700">Scenarios</span>
          <span>/</span>
          <span className="font-medium text-slate-700">German - Validate card payouts</span>
          <span className="text-[#1f4f96]">transactions</span>
          <Info className="size-3.5" />
          <span>/</span>
          <span className="font-medium text-slate-700">V1 Live</span>
          <span>/</span>
          <span className="font-medium text-slate-700">Rules</span>
          <span>/</span>
          <span className="font-semibold text-slate-950">{rule.title}</span>
        </div>
      </div>

      <div className="space-y-4">
        <h1 className="text-[1.6rem] font-medium tracking-tight text-slate-950">
          {rule.title}
        </h1>
        <p className="max-w-[920px] text-[14px] leading-7 text-slate-950">
          {rule.description}
        </p>
        <div className="flex flex-wrap items-center gap-3">
          {rule.ruleGroup ? (
            <Badge className="rounded-full border-[#2d63b8] bg-white px-2.5 py-0.5 text-[13px] font-medium tracking-normal normal-case text-[#2d63b8]">
              {rule.ruleGroup}
            </Badge>
          ) : null}
          <button
            type="button"
            className="inline-flex size-8 items-center justify-center rounded-lg border border-slate-200 bg-slate-50 text-slate-300"
          >
            <Pencil className="size-3.5" />
          </button>
        </div>
      </div>

      <div className="border-t border-slate-200 pt-6">
        <h2 className="mb-3 text-[1rem] font-semibold text-slate-950">Settings</h2>

        <Card className="rounded-xl border border-slate-200 shadow-none">
          <CardContent className="space-y-3 p-6">
            {rule.settings.slice(0, -1).map((tokens) => (
              <SettingRow key={tokens.join("-")} tokens={[...tokens]} />
            ))}
          </CardContent>
        </Card>

        <Card className="mt-4 rounded-xl border border-slate-200 shadow-none">
          <CardContent className="p-6">
            <SettingRow tokens={[...rule.settings[rule.settings.length - 1]]} />
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
