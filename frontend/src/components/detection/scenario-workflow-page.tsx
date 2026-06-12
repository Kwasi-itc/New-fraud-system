"use client";

import Link from "next/link";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  CircleSlash2,
  Info,
  Lightbulb,
  Plus,
  Trash2,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { decisionEngineApi } from "@/lib/decision-engine-api";
import { cn } from "@/lib/utils";

const conditionTemplates = [
  {
    label: "Always applies",
    description: "The action will always be executed",
  },
  {
    label: "Never applies",
    description: "The action will never be executed",
  },
  {
    label: "Outcome is in",
    description: "The action will be executed if the decision outcome is in this list of values",
  },
  {
    label: "Payload field condition",
    description: "The action will be executed if the payload evaluates to true",
  },
] as const;

const actionTemplates = [
  {
    label: "Do nothing",
    description: "No action will be taken",
    icon: CircleSlash2,
  },
  {
    label: "Create case",
    description: "Create a new case in the specified inbox",
    icon: Plus,
  },
  {
    label: "Add to case if possible",
    description: "Add to existing case or create new one",
    icon: Plus,
  },
] as const;

type ConditionTemplate = (typeof conditionTemplates)[number]["label"];
type ActionTemplate = (typeof actionTemplates)[number]["label"];

type WorkflowRuleDraft = {
  id: string;
  name: string;
  conditions: ConditionTemplate[];
  action: ActionTemplate;
  inbox: string;
  title: string;
  tags: string[];
};

const workflowTagOptions = ["Approved", "Review Required", "High Risk"] as const;
const workflowInboxOptions = ["AML Compliance", "Fraud Ops", "Risk Review"] as const;

function ConditionMenu({
  onSelect,
}: {
  onSelect: (condition: ConditionTemplate) => void;
}) {
  return (
    <div className="absolute left-0 top-full z-20 mt-2 w-[320px] rounded-2xl border border-slate-200 bg-white p-2 shadow-[0_20px_48px_rgba(15,23,42,0.14)]">
      {conditionTemplates.map((item) => (
        <button
          key={item.label}
          type="button"
          onClick={() => onSelect(item.label)}
          className="flex w-full flex-col rounded-xl px-3.5 py-3 text-left transition hover:bg-slate-50"
        >
          <span className="text-[15px] font-medium text-slate-950">{item.label}</span>
          <span className="mt-1 text-[13px] leading-5 text-slate-500">{item.description}</span>
        </button>
      ))}
    </div>
  );
}

function ActionMenu({
  onSelect,
}: {
  onSelect: (action: ActionTemplate) => void;
}) {
  return (
    <div className="absolute right-0 top-full z-20 mt-2 w-[340px] rounded-2xl border border-slate-200 bg-white p-2 shadow-[0_20px_48px_rgba(15,23,42,0.14)]">
      {actionTemplates.map((item) => {
        const Icon = item.icon;
        return (
          <button
            key={item.label}
            type="button"
            onClick={() => onSelect(item.label)}
            className="flex w-full flex-col rounded-xl px-3.5 py-3 text-left transition hover:bg-slate-50"
          >
            <span className="flex items-center gap-2 text-[15px] font-medium text-slate-950">
              <Icon className="size-4 text-slate-500" />
              {item.label}
            </span>
            <span className="mt-1 text-[13px] leading-5 text-slate-500">{item.description}</span>
          </button>
        );
      })}
    </div>
  );
}

export function ScenarioWorkflowPage({ scenarioId }: { scenarioId: string }) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
  const queryClient = useQueryClient();
  const [rules, setRules] = useState<WorkflowRuleDraft[]>(() => {
    if (typeof window !== "undefined") {
      const saved = window.localStorage.getItem(`workflow-draft:${scenarioId}`);
      if (saved) {
        try {
          const parsed = JSON.parse(saved) as WorkflowRuleDraft[];
          if (Array.isArray(parsed) && parsed.length > 0) {
            return parsed;
          }
        } catch {
          window.localStorage.removeItem(`workflow-draft:${scenarioId}`);
        }
      }
    }

    return [
      {
        id: "rule-1",
        name: "New Rule",
        conditions: [],
        action: "Do nothing",
        inbox: "AML Compliance",
        title: "Case %object_id%",
        tags: [],
      },
    ];
  });
  const [conditionMenuFor, setConditionMenuFor] = useState<string | null>(null);
  const [actionMenuFor, setActionMenuFor] = useState<string | null>(null);
  const [saveState, setSaveState] = useState<"idle" | "saved">("idle");

  const scenarioQuery = useQuery({
    queryKey: ["decision-engine", "scenario", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.getScenario(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });

  const workflowRulesQuery = useQuery({
    queryKey: ["decision-engine", "workflow-rules", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.listWorkflowRules(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });

  const persistMutation = useMutation({
    mutationFn: async () => {
      window.localStorage.setItem(`workflow-draft:${scenarioId}`, JSON.stringify(rules));
      return true;
    },
    onSuccess: () => {
      setSaveState("saved");
      window.setTimeout(() => setSaveState("idle"), 1800);
    },
  });

  const createWorkflowRuleMutation = useMutation({
    mutationFn: async () => {
      const newestRule = rules[rules.length - 1];
      if (!newestRule?.name.trim()) {
        throw new Error("Give the last workflow rule a name before saving it.");
      }

      return decisionEngineApi.createWorkflowRule(tenantId, scenarioId, {
        name: newestRule.name.trim(),
        fallthrough: false,
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "workflow-rules", tenantId, scenarioId],
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

  if (scenarioQuery.isLoading) {
    return (
      <Card className="rounded-2xl border border-slate-200 shadow-none">
        <CardContent className="p-5 text-sm text-slate-600">
          Loading workflow...
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

  if (workflowRulesQuery.isError) {
    return (
      <Card className="rounded-2xl border border-red-200 bg-red-50 shadow-none">
        <CardContent className="p-5 text-sm text-red-700">
          {workflowRulesQuery.error instanceof Error
            ? workflowRulesQuery.error.message
            : "Failed to load workflow rules."}
        </CardContent>
      </Card>
    );
  }

  const scenario = scenarioQuery.data.scenario;

  function addRule() {
    setRules((current) => [
      ...current,
      {
        id: `rule-${current.length + 1}`,
        name: `New Rule ${current.length + 1}`,
        conditions: [],
        action: "Do nothing",
        inbox: "AML Compliance",
        title: "Case %object_id%",
        tags: [],
      },
    ]);
  }

  function addCondition(ruleId: string, condition: ConditionTemplate) {
    setRules((current) =>
      current.map((rule) =>
        rule.id === ruleId
          ? { ...rule, conditions: [...rule.conditions, condition] }
          : rule
      )
    );
    setConditionMenuFor(null);
  }

  function removeRule(ruleId: string) {
    setRules((current) => current.filter((rule) => rule.id !== ruleId));
    setConditionMenuFor(null);
    setActionMenuFor(null);
  }

  function updateAction(ruleId: string, action: ActionTemplate) {
    setRules((current) =>
      current.map((rule) => (rule.id === ruleId ? { ...rule, action } : rule))
    );
    setActionMenuFor(null);
  }

  function removeCondition(ruleId: string, conditionIndex: number) {
    setRules((current) =>
      current.map((rule) =>
        rule.id === ruleId
          ? {
              ...rule,
              conditions: rule.conditions.filter((_, index) => index !== conditionIndex),
            }
          : rule
      )
    );
  }

  function updateRuleName(ruleId: string, name: string) {
    setRules((current) =>
      current.map((rule) => (rule.id === ruleId ? { ...rule, name } : rule))
    );
  }

  function updateRuleConfig(
    ruleId: string,
    field: keyof Pick<WorkflowRuleDraft, "inbox" | "title">,
    value: string
  ) {
    setRules((current) =>
      current.map((rule) => (rule.id === ruleId ? { ...rule, [field]: value } : rule))
    );
  }

  function toggleTag(ruleId: string, tag: string) {
    setRules((current) =>
      current.map((rule) =>
        rule.id === ruleId
          ? {
              ...rule,
              tags: rule.tags.includes(tag)
                ? rule.tags.filter((item) => item !== tag)
                : [...rule.tags, tag],
            }
          : rule
      )
    );
  }

  return (
    <div className="mx-auto w-full max-w-[1320px] space-y-5 px-4 sm:px-6 xl:px-8">
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
          <span className="font-semibold text-slate-950">Workflow</span>
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="space-y-2">
          <h1 className="text-[1.7rem] font-semibold tracking-tight text-slate-950">
            Workflow
          </h1>
          <p className="flex items-center gap-2 text-[14px] text-slate-600">
            <Info className="size-4 text-slate-400" />
            Define what should happen after a decision outcome is produced.
          </p>
        </div>
        <div className="flex flex-wrap gap-3">
          <Button
            variant="outline"
            onClick={() => persistMutation.mutate()}
            disabled={persistMutation.isPending}
            className="h-10 rounded-xl border-slate-200 px-4 text-[14px] shadow-none"
          >
            {persistMutation.isPending
              ? "Saving draft..."
              : saveState === "saved"
                ? "Draft saved"
                : "Save draft"}
          </Button>
          <Button
            onClick={() => createWorkflowRuleMutation.mutate()}
            disabled={createWorkflowRuleMutation.isPending}
            className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
          >
            {createWorkflowRuleMutation.isPending ? "Creating..." : "Create API rule"}
          </Button>
        </div>
      </div>

      {createWorkflowRuleMutation.error instanceof Error ? (
        <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-[13px] text-red-700">
          {createWorkflowRuleMutation.error.message}
        </div>
      ) : null}

      <div className="space-y-4">
        {rules.map((rule, index) => {
          const selectedAction =
            actionTemplates.find((item) => item.label === rule.action) ?? actionTemplates[0];
          const ActionIcon = selectedAction.icon;
          const showsCaseConfig =
            rule.action === "Create case" || rule.action === "Add to case if possible";

          return (
            <div key={rule.id} className="space-y-4">
              {index > 0 ? (
                <div className="flex justify-center">
                  <div className="rounded-lg bg-slate-100 px-3 py-1.5 text-[11px] font-semibold uppercase tracking-[0.16em] text-slate-500">
                    Else
                  </div>
                </div>
              ) : null}

              <div className="grid gap-3 xl:grid-cols-[minmax(0,1fr)_240px] xl:items-start">
                <Card className="rounded-2xl border border-slate-200 shadow-none">
                  <CardContent className="space-y-4 p-5">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <Input
                        value={rule.name}
                        onChange={(event) => updateRuleName(rule.id, event.target.value)}
                        className="h-10 max-w-[280px] rounded-xl border-slate-200 text-[15px] font-semibold text-slate-950 shadow-none"
                      />
                      <Button
                        variant="outline"
                        onClick={() => removeRule(rule.id)}
                        className="h-9 rounded-xl border-slate-200 px-3.5 text-[13px] shadow-none"
                      >
                        <Trash2 className="size-4" />
                        Delete
                      </Button>
                    </div>

                    {rule.conditions.length === 0 ? (
                      <div className="rounded-xl border border-slate-200 px-4 py-3.5 text-[14px] text-slate-950">
                        <div className="flex items-start gap-3">
                          <Lightbulb className="mt-0.5 size-4 shrink-0 text-[#1f4f96]" />
                          <span>A rule without conditions is ignored.</span>
                        </div>
                      </div>
                    ) : (
                      <div className="space-y-3">
                        {rule.conditions.map((condition, conditionIndex) => (
                          <div key={`${rule.id}-${condition}-${conditionIndex}`} className="flex flex-wrap items-center gap-3">
                            <span className="w-10 text-[16px] font-semibold text-slate-950">
                              {conditionIndex === 0 ? "If" : "And"}
                            </span>
                            <div className="rounded-full border border-slate-200 bg-white px-3.5 py-1.5 text-[14px] text-slate-950">
                              {condition}
                            </div>
                            <button
                              type="button"
                              onClick={() => removeCondition(rule.id, conditionIndex)}
                              className="inline-flex size-7 items-center justify-center rounded-lg border border-slate-200 text-slate-500 transition hover:bg-slate-50"
                            >
                              <Trash2 className="size-3.5" />
                            </button>
                          </div>
                        ))}
                      </div>
                    )}

                    <div className="relative">
                      <Button
                        variant="outline"
                        onClick={() =>
                          setConditionMenuFor((current) =>
                            current === rule.id ? null : rule.id
                          )
                        }
                        className="h-9 rounded-xl border-slate-200 px-3.5 text-[13px] shadow-none"
                      >
                        <Plus className="size-4" />
                        Add Condition
                      </Button>
                      {conditionMenuFor === rule.id ? (
                        <ConditionMenu onSelect={(condition) => addCondition(rule.id, condition)} />
                      ) : null}
                    </div>
                  </CardContent>
                </Card>

                <div className="space-y-3 xl:pt-4">
                  <div className="flex items-center justify-center gap-2 xl:justify-start">
                    <div className="rounded-lg bg-slate-100 px-3 py-2 text-[11px] font-semibold uppercase tracking-[0.16em] text-slate-500">
                      Then
                    </div>
                    <div className="relative">
                      <button
                        type="button"
                        onClick={() =>
                          setActionMenuFor((current) => (current === rule.id ? null : rule.id))
                        }
                        className={cn(
                          "inline-flex min-h-12 items-center gap-2 rounded-2xl border border-slate-200 bg-white px-4 py-3 text-[14px] font-medium text-slate-950 shadow-none transition hover:border-slate-300"
                        )}
                      >
                        <ActionIcon className="size-4 text-slate-500" />
                        {rule.action}
                      </button>
                      {actionMenuFor === rule.id ? (
                        <ActionMenu onSelect={(action) => updateAction(rule.id, action)} />
                      ) : null}
                    </div>
                  </div>

                  {showsCaseConfig ? (
                    <Card className="rounded-2xl border border-slate-200 shadow-none">
                      <CardContent className="space-y-4 p-5">
                        <div className="inline-flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2 text-[14px] font-medium text-slate-950">
                          <Plus className="size-4" />
                          {rule.action}
                        </div>
                        <div className="space-y-3 text-[14px] text-slate-950">
                          <div className="flex items-center justify-between gap-3">
                            <span className="font-semibold">In</span>
                            <select
                              value={rule.inbox}
                              onChange={(event) => updateRuleConfig(rule.id, "inbox", event.target.value)}
                              className="h-10 min-w-[180px] rounded-xl border border-slate-200 bg-white px-3 outline-none"
                            >
                              {workflowInboxOptions.map((option) => (
                                <option key={option} value={option}>
                                  {option}
                                </option>
                              ))}
                            </select>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="font-semibold">With title</span>
                            <Input
                              value={rule.title}
                              onChange={(event) => updateRuleConfig(rule.id, "title", event.target.value)}
                              className="h-10 min-w-[180px] rounded-xl border-slate-200 text-[14px] shadow-none"
                            />
                          </div>
                          <div className="space-y-2">
                            <div className="flex items-center justify-between gap-3">
                              <span className="font-semibold">With tags</span>
                              <button
                                type="button"
                                className="inline-flex items-center gap-2 rounded-xl border border-slate-200 px-3 py-2 text-[14px]"
                              >
                                <Plus className="size-4" />
                                Add
                              </button>
                            </div>
                            <Input
                              placeholder="Search tags..."
                              className="h-10 rounded-xl border-slate-200 text-[14px] shadow-none"
                            />
                            <div className="flex flex-wrap gap-2">
                              {workflowTagOptions.map((tag) => (
                                <button
                                  key={tag}
                                  type="button"
                                  onClick={() => toggleTag(rule.id, tag)}
                                  className={cn(
                                    "rounded-full border px-3 py-1.5 text-[14px] transition",
                                    rule.tags.includes(tag)
                                      ? "border-[#1f4f96] bg-blue-50 text-[#1f4f96]"
                                      : "border-slate-300 bg-white text-slate-700"
                                  )}
                                >
                                  {tag}
                                </button>
                              ))}
                            </div>
                          </div>
                        </div>
                      </CardContent>
                    </Card>
                  ) : null}
                </div>
              </div>

              {showsCaseConfig ? (
                <div className="flex justify-end gap-3">
                  <Button
                    variant="outline"
                    className="h-10 rounded-xl border-slate-900 px-4 text-[14px] shadow-none"
                  >
                    Cancel
                  </Button>
                  <Button
                    variant="accent"
                    onClick={() => persistMutation.mutate()}
                    className="h-10 rounded-xl bg-[#1f4f96] px-5 text-[14px] shadow-none hover:bg-[#163f79]"
                  >
                    Save
                  </Button>
                </div>
              ) : null}
            </div>
          );
        })}
      </div>

      <div className="flex justify-center pb-2">
        <Button
          variant="accent"
          onClick={addRule}
          className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
        >
          <Plus className="size-4" />
          Create rule
        </Button>
      </div>
    </div>
  );
}
