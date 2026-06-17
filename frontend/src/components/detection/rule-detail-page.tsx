"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowLeft,
  Copy,
  MoreHorizontal,
  Save,
  Sparkles,
  Trash2,
} from "lucide-react";

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
  compileConditionGroupsToAst,
  createSimpleRuleGroup,
  simpleRuleOperatorOptions,
  slugifyStableRuleId,
  tryParseAstToConditionGroups,
  type SimpleRuleConditionGroup,
} from "@/lib/rule-builder";

function coerceScoreModifier(value: string) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function formatIterationLabel(version: number, isLive = false) {
  return isLive ? `V${version} Live` : `Draft V${version}`;
}

function RuleEditorContent({
  tenantId,
  scenarioId,
  scenarioName,
  triggerObjectType,
  draftIterationId,
  draftIterationVersion,
  currentRule,
  fieldOptions,
  operatorOptions,
  ruleGroups,
  validation,
  isValidating,
  onValidate,
}: {
  tenantId: string;
  scenarioId: string;
  scenarioName: string;
  triggerObjectType: string;
  draftIterationId: string;
  draftIterationVersion: number;
  currentRule: Rule;
  fieldOptions: string[];
  operatorOptions: typeof simpleRuleOperatorOptions;
  ruleGroups: string[];
  validation?: ValidationResult;
  isValidating: boolean;
  onValidate: () => void;
}) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [name, setName] = useState(currentRule.name);
  const [description, setDescription] = useState(currentRule.description);
  const [ruleGroup, setRuleGroup] = useState(currentRule.rule_group);
  const [scoreModifier, setScoreModifier] = useState(String(currentRule.score_modifier));
  const [stableRuleId, setStableRuleId] = useState(currentRule.stable_rule_id);
  const [snoozeGroupId, setSnoozeGroupId] = useState(currentRule.snooze_group_id ?? "");
  const [actionsOpen, setActionsOpen] = useState(false);

  const parsedGroups = tryParseAstToConditionGroups(currentRule.formula);
  const [conditionGroups, setConditionGroups] = useState<SimpleRuleConditionGroup[]>(
    parsedGroups ?? [createSimpleRuleGroup()]
  );
  const hasUnsupportedFormula = parsedGroups === null;

  const updateRuleMutation = useMutation({
    mutationFn: async () =>
      decisionEngineApi.updateRule(tenantId, scenarioId, draftIterationId, currentRule.id, {
        display_order: currentRule.display_order,
        name: name.trim(),
        description: description.trim(),
        formula: hasUnsupportedFormula
          ? currentRule.formula
          : compileConditionGroupsToAst(conditionGroups),
        score_modifier: coerceScoreModifier(scoreModifier),
        rule_group: ruleGroup.trim(),
        snooze_group_id: snoozeGroupId.trim() || null,
        stable_rule_id: stableRuleId.trim() || slugifyStableRuleId(name),
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "rules", tenantId, scenarioId, draftIterationId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "validation", tenantId, scenarioId, draftIterationId],
      });
    },
  });

  const deleteRuleMutation = useMutation({
    mutationFn: async () =>
      decisionEngineApi.deleteRule(tenantId, scenarioId, draftIterationId, currentRule.id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "rules", tenantId, scenarioId, draftIterationId],
      });
      await queryClient.invalidateQueries({
        queryKey: ["decision-engine", "validation", tenantId, scenarioId, draftIterationId],
      });
      router.push(`/detection/${scenarioId}/edit`);
    },
  });

  const saveDisabled =
    updateRuleMutation.isPending ||
    !name.trim() ||
    !stableRuleId.trim() ||
    (!hasUnsupportedFormula &&
      !conditionGroups.some((group) =>
        group.conditions.some((condition) => condition.left.trim() && condition.right.trim())
      ));

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
                  onChange={(event) => {
                    const nextName = event.target.value;
                    setName(nextName);
                    if (!stableRuleId || stableRuleId === slugifyStableRuleId(name)) {
                      setStableRuleId(slugifyStableRuleId(nextName));
                    }
                  }}
                  className="h-11 border-none bg-transparent px-0 text-[1.4rem] font-medium tracking-tight text-slate-950 shadow-none focus-visible:ring-0"
                />
              </div>
            </div>

            <div className="flex items-center gap-2">
              <div className="relative">
                <Button
                  type="button"
                  variant="outline"
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
          <div className="max-w-3xl space-y-4 border-b border-slate-200 pb-6">
            <textarea
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="Add a description..."
              rows={3}
              className="w-full resize-none border-none bg-transparent px-0 text-[15px] font-medium text-slate-700 outline-none placeholder:text-slate-400"
            />

            <RuleGroupPicker
              selectedRuleGroup={ruleGroup}
              ruleGroups={ruleGroups}
              onChange={setRuleGroup}
            />
          </div>

          <div className="space-y-3">
            <span className="text-[14px] font-medium text-slate-950">Formula</span>
            <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]">
              <div className="space-y-5">
                <Card className="rounded-xl border border-slate-200 shadow-none">
                  <CardContent className="space-y-4 p-6">
                    <div className="flex items-start gap-3 rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-[13px] leading-6 text-slate-700">
                      <Sparkles className="mt-0.5 size-4 shrink-0 text-slate-500" />
                      <p>
                        The layout now mirrors the legacy rule workspace. The next parity step is replacing this simplified builder with the richer AST canvas used in `front`.
                      </p>
                    </div>

                    {hasUnsupportedFormula ? (
                      <div className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-[13px] text-amber-800">
                        This rule uses a formula shape the visual editor does not support yet. Metadata can still be updated and the existing formula will be preserved on save.
                      </div>
                    ) : null}

                    <RuleBuilderSimple
                      groups={conditionGroups}
                      onChange={setConditionGroups}
                      fieldOptions={fieldOptions}
                      operatorOptions={operatorOptions}
                      disabled={hasUnsupportedFormula}
                    />
                  </CardContent>
                </Card>

                <Card className="max-w-3xl rounded-xl border border-slate-200 shadow-none">
                  <CardContent className="flex flex-wrap items-center gap-3 p-5">
                    <span className="inline-flex rounded-lg bg-slate-100 px-3 py-2 text-[13px] font-medium text-slate-700">
                      Score
                    </span>
                    <Input
                      value={scoreModifier}
                      onChange={(event) => setScoreModifier(event.target.value)}
                      inputMode="numeric"
                      className="h-10 w-[140px] rounded-xl border-slate-200 shadow-none"
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
                        {formatIterationLabel(draftIterationVersion)}
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
                        <span className="font-medium text-slate-900">Operators:</span>{" "}
                        {operatorOptions.length}
                      </p>
                    </div>
                  </CardContent>
                </Card>

                <Card className="rounded-xl border border-slate-200 shadow-none">
                  <CardContent className="space-y-3 p-5">
                    <div className="text-[13px] font-medium text-slate-900">Metadata</div>
                    <div className="space-y-3">
                      <label className="space-y-1.5 text-[13px] text-slate-700">
                        <span>Stable rule ID</span>
                        <Input
                          value={stableRuleId}
                          onChange={(event) => setStableRuleId(event.target.value)}
                          placeholder="high-value-payment"
                          className="h-10 rounded-xl border-slate-200 shadow-none"
                        />
                      </label>
                      <label className="space-y-1.5 text-[13px] text-slate-700">
                        <span>Snooze group ID</span>
                        <Input
                          value={snoozeGroupId}
                          onChange={(event) => setSnoozeGroupId(event.target.value)}
                          placeholder="Optional"
                          className="h-10 rounded-xl border-slate-200 shadow-none"
                        />
                      </label>
                    </div>
                  </CardContent>
                </Card>

                <Card className="rounded-xl border border-slate-200 shadow-none">
                  <CardContent className="space-y-3 p-5">
                    <div className="flex items-start gap-2 text-[13px] text-slate-700">
                      <AlertTriangle className="mt-0.5 size-4 shrink-0 text-slate-500" />
                      <p>
                        The legacy UI also included AI description/generation and a richer formula editor. Those backend-compatible pieces still need to be rebuilt on top of the standalone service.
                      </p>
                    </div>
                    <Button
                      variant="outline"
                      onClick={onValidate}
                      disabled={isValidating}
                      className="h-10 w-full rounded-xl border-slate-200 px-4 text-[14px] shadow-none"
                    >
                      <Sparkles className="size-4" />
                      {isValidating ? "Validating..." : "Validate iteration"}
                    </Button>
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
}: {
  scenarioId: string;
  ruleId: string;
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

  const rulesQuery = useQuery({
    queryKey: ["decision-engine", "rules", tenantId, scenarioId, draftIteration?.id],
    queryFn: () => decisionEngineApi.listRules(tenantId, scenarioId, draftIteration!.id),
    enabled: Boolean(tenantId && scenarioId && draftIteration?.id),
  });
  const validationQuery = useQuery({
    queryKey: ["decision-engine", "validation", tenantId, scenarioId, draftIteration?.id],
    queryFn: () =>
      decisionEngineApi.validateIteration(tenantId, scenarioId, draftIteration!.id),
    enabled: Boolean(tenantId && scenarioId && draftIteration?.id),
  });

  const currentRule = useMemo(
    () => rulesQuery.data?.rules.find((rule) => rule.id === ruleId) ?? null,
    [ruleId, rulesQuery.data?.rules]
  );

  const ruleGroups = useMemo(() => {
    return [...new Set((rulesQuery.data?.rules ?? []).map((rule) => rule.rule_group).filter(Boolean))]
      .sort((a, b) => a.localeCompare(b));
  }, [rulesQuery.data?.rules]);

  const fieldOptions = useMemo(() => {
    const payloadFields = (editorIdentifiersQuery.data?.payload_accessors ?? [])
      .map((node) => node.children?.[0]?.constant)
      .filter((value): value is string => typeof value === "string");

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

  const operatorOptions = useMemo(() => {
    const availableFunctions = new Set(
      (ruleFunctionsQuery.data?.rule_functions ?? []).map((ruleFunction) => ruleFunction.name)
    );

    if (availableFunctions.size === 0) {
      return simpleRuleOperatorOptions;
    }

    return simpleRuleOperatorOptions.filter((option) => availableFunctions.has(option.value));
  }, [ruleFunctionsQuery.data?.rule_functions]);

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

  if (!draftIteration) {
    return (
      <Card className="rounded-xl border border-amber-200 bg-amber-50 shadow-none">
        <CardContent className="p-5 text-sm text-amber-800">
          Create a draft iteration before editing rules.
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
            : "The requested rule could not be found in the current draft iteration."}
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
      draftIterationId={draftIteration.id}
      draftIterationVersion={draftIteration.version}
      currentRule={currentRule}
      fieldOptions={fieldOptions}
      operatorOptions={operatorOptions}
      ruleGroups={ruleGroups}
      validation={validationQuery.data?.validation}
      isValidating={validationQuery.isLoading || validationQuery.isFetching}
      onValidate={() => void validationQuery.refetch()}
    />
  );
}
