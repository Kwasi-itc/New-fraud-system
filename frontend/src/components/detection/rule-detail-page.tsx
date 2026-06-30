"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Copy,
  Info,
  MoreHorizontal,
  Save,
  Sparkles,
  Trash2,
} from "lucide-react";

import { RuleBuilderAdvanced } from "@/components/detection/rule-builder-advanced";
import { RuleBuilderExpression } from "@/components/detection/rule-builder-expression";
import { RuleBuilderSimple } from "@/components/detection/rule-builder-simple";
import { RuleGroupPicker } from "@/components/detection/rule-group-picker";
import { RuleValidationPanel } from "@/components/detection/rule-validation-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useAssembledDataModelQuery } from "@/lib/data-model-query";
import {
  decisionEngineApi,
  type Rule,
  type ValidationResult,
} from "@/lib/decision-engine-api";
import {
  compileAdvancedConditionGroupsToAst,
  compileConditionGroupsToAst,
  compileExpressionRuleNodeToAst,
  createAdvancedRuleGroup,
  createExpressionOperator,
  createSimpleRuleGroup,
  extractAccessorOptions,
  extractPayloadFieldNames,
  isExpressionRuleNodeComplete,
  isUnaryRuleOperator,
  simpleRuleOperatorOptions,
  slugifyStableRuleId,
  summarizeRuleFormula,
  tryParseAstToAdvancedConditionGroups,
  tryParseAstToConditionGroups,
  tryParseAstToExpressionRuleNode,
  type AdvancedRuleConditionGroup,
  type ExpressionRuleNode,
  type RuleAccessorOption,
  type SimpleRuleConditionGroup,
} from "@/lib/rule-builder";
import { cn } from "@/lib/utils";
import { useToastStore } from "@/stores/toast-store";

function coerceScoreModifier(value: string) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function formatIterationLabel(version: number, isLive = false) {
  return isLive ? `V${version} Live` : `Draft V${version}`;
}

function formatRuleSummaryForDisplay(summary: string) {
  return summary
    .replace(/\)\s+and\s+\(/g, ")\nAND\n(")
    .replace(/\)\s+or\s+\(/g, ")\nOR\n(")
    .replace(/\s+and\s+/g, "\nAND\n")
    .replace(/\s+or\s+/g, "\nOR\n");
}

function RuleEditorContent({
  tenantId,
  scenarioId,
  scenarioName,
  triggerObjectType,
  iterationId,
  iterationVersion,
  isEditable,
  currentRule,
  fieldOptions,
  accessorOptions,
  operatorOptions,
  customListOptions,
  tableFieldOptions,
  ruleGroups,
  validation,
  isValidating,
}: {
  tenantId: string;
  scenarioId: string;
  scenarioName: string;
  triggerObjectType: string;
  iterationId: string;
  iterationVersion: number;
  isEditable: boolean;
  currentRule: Rule;
  fieldOptions: string[];
  accessorOptions: RuleAccessorOption[];
  operatorOptions: typeof simpleRuleOperatorOptions;
  customListOptions: Array<{ id: string; name: string }>;
  tableFieldOptions: Array<{ tableName: string; fieldName: string; label: string }>;
  ruleGroups: string[];
  validation?: ValidationResult;
  isValidating: boolean;
}) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const pushToast = useToastStore((state) => state.pushToast);
  const [name, setName] = useState(currentRule.name);
  const [description, setDescription] = useState(currentRule.description);
  const [ruleGroup, setRuleGroup] = useState(currentRule.rule_group);
  const [scoreModifier, setScoreModifier] = useState(String(currentRule.score_modifier));
  const [stableRuleId, setStableRuleId] = useState(currentRule.stable_rule_id);
  const [snoozeGroupId, setSnoozeGroupId] = useState(currentRule.snooze_group_id ?? "");
  const [actionsOpen, setActionsOpen] = useState(false);

  const parsedGroups = useMemo(
    () => tryParseAstToConditionGroups(currentRule.formula, accessorOptions),
    [accessorOptions, currentRule.formula]
  );
  const parsedAdvancedGroups = useMemo(
    () => tryParseAstToAdvancedConditionGroups(currentRule.formula, accessorOptions),
    [accessorOptions, currentRule.formula]
  );
  const parsedExpressionRoot = useMemo(
    () => tryParseAstToExpressionRuleNode(currentRule.formula, accessorOptions),
    [accessorOptions, currentRule.formula]
  );
  const [editorMode] = useState<"simple" | "advanced" | "expression">(() =>
    parsedGroups ? "simple" : parsedAdvancedGroups ? "advanced" : parsedExpressionRoot ? "expression" : "simple"
  );
  const [conditionGroups, setConditionGroups] = useState<SimpleRuleConditionGroup[]>(
    parsedGroups ?? [createSimpleRuleGroup()]
  );
  const [advancedConditionGroups, setAdvancedConditionGroups] = useState<AdvancedRuleConditionGroup[]>(
    parsedAdvancedGroups ?? [createAdvancedRuleGroup(accessorOptions[0]?.id ?? "")]
  );
  const [expressionRoot, setExpressionRoot] = useState<ExpressionRuleNode>(
    parsedExpressionRoot ?? createExpressionOperator()
  );
  const hasUnsupportedFormula =
    parsedGroups === null && parsedAdvancedGroups === null && parsedExpressionRoot === null;
  const ruleSummary = useMemo(
    () => summarizeRuleFormula(currentRule.formula),
    [currentRule.formula]
  );
  const formattedRuleSummary = useMemo(
    () => (ruleSummary ? formatRuleSummaryForDisplay(ruleSummary) : null),
    [ruleSummary]
  );

  useEffect(() => {
    setConditionGroups((current) => {
      const nextGroups = parsedGroups ?? [createSimpleRuleGroup()];
      return JSON.stringify(current) === JSON.stringify(nextGroups) ? current : nextGroups;
    });
  }, [currentRule.id, parsedGroups]);

  useEffect(() => {
    setAdvancedConditionGroups((current) => {
      const nextGroups =
        parsedAdvancedGroups ?? [createAdvancedRuleGroup(accessorOptions[0]?.id ?? "")];
      return JSON.stringify(current) === JSON.stringify(nextGroups) ? current : nextGroups;
    });
  }, [accessorOptions, currentRule.id, parsedAdvancedGroups]);

  useEffect(() => {
    setExpressionRoot((current) => {
      const nextRoot = parsedExpressionRoot ?? createExpressionOperator();
      return JSON.stringify(current) === JSON.stringify(nextRoot) ? current : nextRoot;
    });
  }, [currentRule.id, parsedExpressionRoot]);

  const updateRuleMutation = useMutation({
    mutationFn: async () =>
      decisionEngineApi.updateRule(tenantId, scenarioId, iterationId, currentRule.id, {
        display_order: currentRule.display_order,
        name: name.trim(),
        description: description.trim(),
        formula: hasUnsupportedFormula
          ? currentRule.formula
          : editorMode === "expression"
            ? compileExpressionRuleNodeToAst(expressionRoot, accessorOptions)
          : editorMode === "advanced"
            ? compileAdvancedConditionGroupsToAst(advancedConditionGroups, accessorOptions)
            : compileConditionGroupsToAst(conditionGroups, accessorOptions),
        score_modifier: coerceScoreModifier(scoreModifier),
        rule_group: ruleGroup.trim(),
        snooze_group_id: snoozeGroupId.trim() || null,
        stable_rule_id: stableRuleId.trim() || slugifyStableRuleId(name),
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "rules", tenantId, scenarioId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "validation", tenantId, scenarioId],
      });
      pushToast({
        title: "Rule saved",
        description: `${name.trim() || "Rule"} was updated.`,
        variant: "success",
      });
    },
    onError: (error) => {
      pushToast({
        title: "Failed to save rule",
        description: error instanceof Error ? error.message : "The rule could not be saved.",
        variant: "error",
      });
    },
  });

  const deleteRuleMutation = useMutation({
    mutationFn: async () =>
      decisionEngineApi.deleteRule(tenantId, scenarioId, iterationId, currentRule.id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "rules", tenantId, scenarioId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "validation", tenantId, scenarioId],
      });
      pushToast({
        title: "Rule deleted",
        description: `${currentRule.name} was removed.`,
        variant: "success",
      });
      router.push(`/detection/${scenarioId}/edit`);
    },
    onError: (error) => {
      pushToast({
        title: "Failed to delete rule",
        description: error instanceof Error ? error.message : "The rule could not be deleted.",
        variant: "error",
      });
    },
  });

  const saveDisabled =
    !isEditable ||
    updateRuleMutation.isPending ||
    !name.trim() ||
    !stableRuleId.trim() ||
    (!hasUnsupportedFormula &&
      (editorMode === "expression"
        ? !isExpressionRuleNodeComplete(expressionRoot)
        : editorMode === "advanced"
        ? !advancedConditionGroups.some((group) =>
            group.conditions.some((condition) => {
              if (!condition.leftAccessorId.trim()) {
                return false;
              }

              if (isUnaryRuleOperator(condition.operator)) {
                return true;
              }

              if (condition.rightOperand.mode === "accessor") {
                return Boolean(condition.rightOperand.accessorId.trim());
              }

              return condition.rightOperand.value.trim().length > 0;
            })
          )
        : !conditionGroups.some((group) =>
            group.conditions.some(
              (condition) =>
                ((condition.leftMode === "function" && condition.leftFunction) ||
                  condition.left.trim()) &&
                (isUnaryRuleOperator(condition.operator) ||
                  (condition.rightExpression &&
                    isExpressionRuleNodeComplete(condition.rightExpression)) ||
                  (condition.rightMode === "function" && condition.rightFunction) ||
                  condition.right.trim())
            )
          )));

  return (
    <div className="mx-auto w-full max-w-[1280px] px-4 sm:px-6 xl:px-8">
      <div className="flex flex-col gap-8">
        <div className="sticky top-0 z-10 bg-white/95 pb-4 pt-2 backdrop-blur supports-[backdrop-filter]:bg-white/88">
          <div className="flex flex-wrap items-center justify-between gap-4 border-b border-slate-200 pb-4">
            <div className="flex min-w-0 flex-1 items-center gap-3">
              <Link
                href={`/detection/${scenarioId}/edit`}
                className="inline-flex size-9 shrink-0 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-900"
              >
                <ArrowLeft className="size-4" />
              </Link>
              <div className="min-w-0 flex-1">
                <Input
                  value={name}
                  disabled={!isEditable}
                  onChange={(event) => {
                    const nextName = event.target.value;
                    setName(nextName);
                    if (!stableRuleId || stableRuleId === slugifyStableRuleId(name)) {
                      setStableRuleId(slugifyStableRuleId(nextName));
                    }
                  }}
                  className="h-11 border-none bg-transparent px-0 text-[1.4rem] font-medium tracking-tight text-slate-950 shadow-none focus-visible:ring-0"
                />
                <div className="mt-1 flex flex-wrap items-center gap-2 text-[12px] text-slate-500">
                  <span>{scenarioName}</span>
                  <span className="text-slate-300">/</span>
                  <span>{formatIterationLabel(iterationVersion)}</span>
                  <span className="text-slate-300">/</span>
                  <span>{currentRule.id}</span>
                </div>
              </div>
            </div>

            <div className="flex items-center gap-2">
              <div className="relative">
                <Button
                  type="button"
                  variant="outline"
                  disabled={!isEditable}
                  onClick={() => setActionsOpen((current) => !current)}
                  className="h-10 w-10 rounded-xl border-slate-200 p-0 shadow-none"
                >
                  <MoreHorizontal className="size-4" />
                </Button>
                {actionsOpen ? (
                  <div className="absolute right-0 top-full z-20 mt-2 w-[190px] rounded-xl border border-slate-200 bg-white p-1.5 shadow-[0_18px_50px_rgba(15,23,42,0.12)]">
                    <button
                      type="button"
                      onClick={() => {
                        setActionsOpen(false);
                        void navigator?.clipboard?.writeText(currentRule.id);
                      }}
                      className="flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-left text-[13px] text-slate-950 hover:bg-slate-50"
                    >
                      <Copy className="size-4" />
                      Copy rule ID
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setActionsOpen(false);
                        if (
                          typeof window !== "undefined" &&
                          window.confirm(`Delete rule "${currentRule.name}"?`)
                        ) {
                          void deleteRuleMutation.mutateAsync();
                        }
                      }}
                      className="flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-left text-[13px] text-red-700 hover:bg-red-50"
                    >
                      <Trash2 className="size-4" />
                      Delete rule
                    </button>
                  </div>
                ) : null}
              </div>

              <Button
                onClick={() => void updateRuleMutation.mutateAsync()}
                disabled={saveDisabled}
                className="h-10 rounded-xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
              >
                <Save className="size-4" />
                {updateRuleMutation.isPending ? "Saving..." : "Save"}
              </Button>
            </div>
          </div>
        </div>

        <div className="space-y-6">
          <div className="max-w-4xl space-y-4 border-b border-slate-200 pb-6">
            <textarea
              value={description}
              disabled={!isEditable}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="Add a description..."
              rows={3}
              className="w-full resize-none border-none bg-transparent px-0 text-[15px] font-normal text-slate-900 outline-none placeholder:text-slate-400"
            />

            <div className="w-fit">
              <RuleGroupPicker
                selectedRuleGroup={ruleGroup}
                ruleGroups={ruleGroups}
                onChange={isEditable ? setRuleGroup : () => {}}
              />
            </div>
          </div>

          <div className="space-y-3">
            <span className="text-[15px] font-medium text-slate-950">Settings</span>
            <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]">
              <div className="space-y-5">
                <Card className="rounded-xl border border-slate-200 shadow-none">
                  <CardContent className="space-y-4 p-6">
                    {hasUnsupportedFormula ? (
                      <div className="space-y-4">
                        <div className="overflow-hidden rounded-xl border border-[#d7e7fb] bg-[#f7fbff]">
                          <div className="border-b border-[#d7e7fb] px-4 py-3">
                            <div className="text-[13px] font-medium text-[#2d63b8]">
                              Rule logic
                            </div>
                            <div className="mt-1 text-[12px] text-slate-500">
                              Displayed in readable form for this rule shape.
                            </div>
                          </div>
                          <pre className="whitespace-pre-wrap break-words px-4 py-4 font-mono text-[13px] leading-6 text-slate-800">
                            {formattedRuleSummary ??
                              "A readable summary is not available for this rule formula."}
                          </pre>
                        </div>
                      </div>
                    ) : editorMode === "advanced" ? (
                      <RuleBuilderAdvanced
                        groups={advancedConditionGroups}
                        onChange={setAdvancedConditionGroups}
                        accessorOptions={accessorOptions}
                        operatorOptions={operatorOptions}
                        triggerObjectType={triggerObjectType}
                        disabled={!isEditable}
                      />
                    ) : editorMode === "expression" ? (
                      <RuleBuilderExpression
                        root={expressionRoot}
                        onChange={setExpressionRoot}
                        accessorOptions={accessorOptions}
                        operatorOptions={operatorOptions}
                        customListOptions={customListOptions}
                        triggerObjectType={triggerObjectType}
                        disabled={!isEditable}
                      />
                    ) : (
                      <RuleBuilderSimple
                        groups={conditionGroups}
                        onChange={setConditionGroups}
                        accessorOptions={accessorOptions}
                        operatorOptions={operatorOptions}
                        customListOptions={customListOptions}
                        triggerObjectType={triggerObjectType}
                        tableFieldOptions={tableFieldOptions}
                        disabled={!isEditable}
                      />
                    )}
                  </CardContent>
                </Card>

                <Card className="max-w-3xl rounded-xl border border-slate-200 shadow-none">
                  <CardContent className="flex flex-wrap items-center gap-3 p-5">
                    <span className="inline-flex rounded-sm bg-slate-100 px-3 py-2 text-[13px] font-medium text-slate-700">
                      then, change the alert score by
                    </span>
                    <Input
                      value={scoreModifier}
                      disabled={!isEditable}
                      onChange={(event) => setScoreModifier(event.target.value)}
                      inputMode="numeric"
                      className="h-10 w-[140px] rounded-sm border-slate-200 bg-slate-50 shadow-none"
                    />
                  </CardContent>
                </Card>

                {updateRuleMutation.error instanceof Error ? (
                  <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-[13px] text-red-700">
                    {updateRuleMutation.error.message}
                  </div>
                ) : null}
                {deleteRuleMutation.error instanceof Error ? (
                  <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-[13px] text-red-700">
                    {deleteRuleMutation.error.message}
                  </div>
                ) : null}
              </div>

              <div className="space-y-5">
                <RuleValidationPanel
                  validation={validation}
                  ruleId={currentRule.id}
                  isLoading={isValidating}
                />

                <Card className="rounded-xl border border-slate-200 shadow-none">
                  <CardContent className="space-y-3 p-5">
                    <div className="flex items-center justify-between">
                      <div className="text-[13px] font-medium text-slate-900">Iteration</div>
                      <Badge className="rounded-full border-slate-200 bg-slate-50 px-2.5 py-0.5 text-[12px] font-medium tracking-normal normal-case text-slate-700">
                        {formatIterationLabel(iterationVersion)}
                      </Badge>
                    </div>
                    <div className="space-y-2 text-[13px] text-slate-600">
                      <p>
                        <span className="font-medium text-slate-900">Scenario:</span>{" "}
                        {scenarioName}
                      </p>
                      <p>
                        <span className="font-medium text-slate-900">Trigger object:</span>{" "}
                        {triggerObjectType}
                      </p>
                      <p>
                        <span className="font-medium text-slate-900">Fields:</span>{" "}
                        {fieldOptions.length}
                      </p>
                      <p>
                        <span className="font-medium text-slate-900">Accessors:</span>{" "}
                        {accessorOptions.length}
                      </p>
                      <p>
                        <span className="font-medium text-slate-900">Operators:</span>{" "}
                        {operatorOptions.length}
                      </p>
                    </div>
                  </CardContent>
                </Card>

                <Card className="rounded-xl border border-slate-200 shadow-none">
                  <CardContent className="space-y-3 p-5">
                    <div className="flex items-center gap-2 text-[13px] font-medium text-slate-900">
                      <Info className="size-4 text-slate-500" />
                      Metadata
                    </div>
                    <div className="space-y-3">
                      <label className="space-y-1.5 text-[13px] text-slate-700">
                        <span>Stable rule ID</span>
                        <Input
                          value={stableRuleId}
                          disabled={!isEditable}
                          onChange={(event) => setStableRuleId(event.target.value)}
                          placeholder="high-value-payment"
                          className="h-10 rounded-xl border-slate-200 shadow-none"
                        />
                      </label>
                      <label className="space-y-1.5 text-[13px] text-slate-700">
                        <span>Snooze group ID</span>
                        <Input
                          value={snoozeGroupId}
                          disabled={!isEditable}
                          onChange={(event) => setSnoozeGroupId(event.target.value)}
                          placeholder="Optional"
                          className="h-10 rounded-xl border-slate-200 shadow-none"
                        />
                      </label>
                    </div>
                  </CardContent>
                </Card>

              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

export function RuleDetailPage({
  scenarioId,
  ruleId,
  initialIterationId = null,
}: {
  scenarioId: string;
  ruleId: string;
  initialIterationId?: string | null;
}) {
  const tenantId = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID ?? "";
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
  const ruleFunctionsQuery = useQuery({
    queryKey: ["decision-engine", "rule-functions"],
    queryFn: () => decisionEngineApi.listRuleFunctions(),
    enabled: Boolean(tenantId),
  });
  const editorIdentifiersQuery = useQuery({
    queryKey: ["decision-engine", "editor-identifiers", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.listEditorIdentifiers(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });
  const assembledModelQuery = useAssembledDataModelQuery(tenantId);

  const draftIteration = useMemo(() => {
    const draftIterations = (iterationsQuery.data?.iterations ?? []).filter(
      (iteration) => iteration.status === "draft"
    );

    return draftIterations.sort((a, b) => b.version - a.version)[0] ?? null;
  }, [iterationsQuery.data?.iterations]);
  const selectedIteration = useMemo(() => {
    const iterations = iterationsQuery.data?.iterations ?? [];
    if (initialIterationId) {
      return (
        iterations.find((iteration) => iteration.id === initialIterationId) ??
        draftIteration ??
        [...iterations].sort((a, b) => b.version - a.version)[0] ??
        null
      );
    }

    return draftIteration ?? [...iterations].sort((a, b) => b.version - a.version)[0] ?? null;
  }, [draftIteration, initialIterationId, iterationsQuery.data?.iterations]);

  const rulesQuery = useQuery({
    queryKey: ["decision-engine", "rules", tenantId, scenarioId, selectedIteration?.id],
    queryFn: () => decisionEngineApi.listRules(tenantId, scenarioId, selectedIteration!.id),
    enabled: Boolean(tenantId && scenarioId && selectedIteration?.id),
  });
  const fallbackIterations = useMemo(
    () =>
      (iterationsQuery.data?.iterations ?? []).filter(
        (iteration) => iteration.id !== selectedIteration?.id
      ),
    [iterationsQuery.data?.iterations, selectedIteration?.id]
  );
  const allIterationRulesQueries = useQueries({
    queries: fallbackIterations.map((iteration) => ({
        queryKey: ["decision-engine", "rules", tenantId, scenarioId, iteration.id],
        queryFn: () => decisionEngineApi.listRules(tenantId, scenarioId, iteration.id),
        enabled: Boolean(tenantId && scenarioId && iteration.id && ruleId),
      })),
  });
  const ruleGroupsQuery = useQuery({
    queryKey: ["decision-engine", "rule-groups", tenantId, scenarioId],
    queryFn: () => decisionEngineApi.listRuleGroups(tenantId, scenarioId),
    enabled: Boolean(tenantId && scenarioId),
  });
  const validationQuery = useQuery({
    queryKey: ["decision-engine", "validation", tenantId, scenarioId, selectedIteration?.id],
    queryFn: () =>
      decisionEngineApi.validateIteration(tenantId, scenarioId, selectedIteration!.id),
    enabled: Boolean(tenantId && scenarioId && selectedIteration?.id),
  });
  const customListsQuery = useQuery({
    queryKey: ["decision-engine", "custom-lists", tenantId],
    queryFn: () => decisionEngineApi.listCustomLists(tenantId),
    enabled: Boolean(tenantId),
  });

  const fallbackRuleMatch = useMemo(() => {
    for (let index = 0; index < allIterationRulesQueries.length; index += 1) {
      const query = allIterationRulesQueries[index];
      const matchedRule = query.data?.rules.find((rule) => rule.id === ruleId);
      if (matchedRule) {
        return {
          rule: matchedRule,
          iteration: fallbackIterations[index] ?? null,
        };
      }
    }

    return null;
  }, [allIterationRulesQueries, fallbackIterations, ruleId]);
  const currentRule = useMemo(
    () => rulesQuery.data?.rules.find((rule) => rule.id === ruleId) ?? fallbackRuleMatch?.rule ?? null,
    [fallbackRuleMatch?.rule, ruleId, rulesQuery.data?.rules]
  );
  const resolvedIteration = fallbackRuleMatch?.iteration ?? selectedIteration;

  const ruleGroups = useMemo(() => {
    return [...(ruleGroupsQuery.data?.rule_groups ?? [])].sort((a, b) => a.localeCompare(b));
  }, [ruleGroupsQuery.data?.rule_groups]);

  const fieldOptions = useMemo(() => {
    const payloadFields = extractPayloadFieldNames(
      editorIdentifiersQuery.data?.payload_accessors ?? []
    );

    if (payloadFields.length > 0) {
      return [...new Set(payloadFields)].sort((a, b) => a.localeCompare(b));
    }

    const triggerObjectType = scenarioQuery.data?.scenario.trigger_object_type;
    const triggerTable = Object.values(
      assembledModelQuery.data?.data_model.tables ?? {}
    ).find((table) => table.name === triggerObjectType);

    return Object.values(triggerTable?.fields ?? {})
      .map((field) => field.name)
      .sort((a, b) => a.localeCompare(b));
  }, [
    assembledModelQuery.data?.data_model,
    editorIdentifiersQuery.data?.payload_accessors,
    scenarioQuery.data?.scenario,
  ]);

  const accessorOptions = useMemo(
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

  const operatorOptions = useMemo(() => {
    const availableFunctions = new Set(
      (ruleFunctionsQuery.data?.rule_functions ?? []).map((ruleFunction) => ruleFunction.name)
    );

    if (availableFunctions.size === 0) {
      return simpleRuleOperatorOptions;
    }

    return simpleRuleOperatorOptions.filter((option) => availableFunctions.has(option.value));
  }, [ruleFunctionsQuery.data?.rule_functions]);
  const customListOptions = useMemo(
    () =>
      (customListsQuery.data?.custom_lists ?? [])
        .map((item) => ({ id: item.id, name: item.name }))
        .sort((a, b) => a.name.localeCompare(b.name)),
    [customListsQuery.data?.custom_lists]
  );
  const tableFieldOptions = useMemo(
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

  if (!tenantId) {
    return (
      <Card className="rounded-xl border border-amber-200 bg-amber-50 shadow-none">
        <CardContent className="p-5 text-sm text-amber-800">
          Set `NEXT_PUBLIC_DATA_MODEL_TENANT_ID` to load rule authoring.
        </CardContent>
      </Card>
    );
  }

  if (ruleId === "new") {
    return (
      <Card className="rounded-xl border border-amber-200 bg-amber-50 shadow-none">
        <CardContent className="p-5 text-sm text-amber-800">
          Create new rules from the scenario rules list so the backend can provision a draft rule id first.
        </CardContent>
      </Card>
    );
  }

  if (scenarioQuery.isLoading || iterationsQuery.isLoading || rulesQuery.isLoading) {
    return (
      <Card className="rounded-xl border border-slate-200 shadow-none">
        <CardContent className="p-5 text-sm text-slate-600">Loading rule editor...</CardContent>
      </Card>
    );
  }

  if (scenarioQuery.isError) {
    return (
      <Card className="rounded-xl border border-red-200 bg-red-50 shadow-none">
        <CardContent className="p-5 text-sm text-red-700">
          {scenarioQuery.error instanceof Error
            ? scenarioQuery.error.message
            : "Failed to load scenario."}
        </CardContent>
      </Card>
    );
  }

  if (!resolvedIteration) {
    return (
      <Card className="rounded-xl border border-amber-200 bg-amber-50 shadow-none">
        <CardContent className="p-5 text-sm text-amber-800">
          No iteration is available for this rule.
        </CardContent>
      </Card>
    );
  }

  if (rulesQuery.isError || !currentRule) {
    return (
      <Card className="rounded-xl border border-red-200 bg-red-50 shadow-none">
        <CardContent className="p-5 text-sm text-red-700">
          {rulesQuery.error instanceof Error
            ? rulesQuery.error.message
          : "The requested rule could not be found in the selected iteration."}
        </CardContent>
      </Card>
    );
  }

  return (
    <RuleEditorContent
      key={currentRule.id}
      tenantId={tenantId}
      scenarioId={scenarioId}
      scenarioName={scenarioQuery.data?.scenario.name ?? "Scenario"}
      triggerObjectType={scenarioQuery.data?.scenario.trigger_object_type ?? ""}
      iterationId={resolvedIteration.id}
      iterationVersion={resolvedIteration.version}
      isEditable={resolvedIteration.status === "draft"}
      currentRule={currentRule}
      fieldOptions={fieldOptions}
      accessorOptions={accessorOptions}
      operatorOptions={operatorOptions}
      customListOptions={customListOptions}
      tableFieldOptions={tableFieldOptions}
      ruleGroups={ruleGroups}
      validation={validationQuery.data?.validation}
      isValidating={validationQuery.isLoading || validationQuery.isFetching}
    />
  );
}
