"use client";

import { useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { Info, Plus, X } from "lucide-react";

import { ConditionSelectorRow } from "@/components/detection/condition-selector-row";
import { RuleOperandSelector } from "@/components/detection/rule-operand-selector";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import {
  buildAggregatorAst,
  createFunctionOperand,
  createSimpleRuleCondition,
  createSimpleRuleGroup,
  getRuleOperatorOption,
  isUnaryRuleOperator,
  type AggregatorOperator,
  type RuleAccessorOption,
  type RuleOperatorOption,
  type SimpleRuleCondition,
  type SimpleRuleConditionGroup,
  type SimpleRuleFunctionOperand,
} from "@/lib/rule-builder";

const AGGREGATOR_OPTIONS: Array<{
  value: AggregatorOperator;
  label: string;
  helper?: string;
}> = [
  { value: "AVG", label: "Average" },
  { value: "COUNT", label: "Count" },
  { value: "COUNT_DISTINCT", label: "Count distinct" },
  { value: "MAX", label: "Max" },
  { value: "MIN", label: "Min" },
  { value: "SUM", label: "Sum" },
  {
    value: "STDDEV",
    label: "Standard deviation",
    helper: "Heavier to compute on large result sets.",
  },
  {
    value: "PCTILE",
    label: "Percentile",
    helper: "Requires a percentile value and can be expensive on large result sets.",
  },
  {
    value: "MEDIAN",
    label: "Median",
    helper: "Heavier to compute on large result sets.",
  },
];

type TableFieldOption = {
  tableName: string;
  fieldName: string;
  label: string;
};

type AggregatorModalState = {
  side: "left" | "right";
  groupId: string;
  conditionId: string;
  aggregator: AggregatorOperator;
  variableName: string;
  fieldKey: string;
  percentile: string;
};

function isLiteralNumberValue(value: string) {
  return value.trim().length > 0 && Number.isFinite(Number(value));
}

function decodeLiteralSelection(value: string): {
  rawValue: string;
  valueType: "string" | "number" | "boolean";
} | null {
  if (value.startsWith("literal:string:")) {
    return {
      rawValue: value.replace(/^literal:string:/, ""),
      valueType: "string",
    };
  }

  if (value.startsWith("literal:number:")) {
    return {
      rawValue: value.replace(/^literal:number:/, ""),
      valueType: "number",
    };
  }

  if (value.startsWith("literal:boolean:")) {
    return {
      rawValue: value.replace(/^literal:boolean:/, ""),
      valueType: "boolean",
    };
  }

  return null;
}

function buildLiteralSearchOptions(searchValue: string, usesList = false) {
  const normalized = searchValue.toLowerCase();
  const literalOptions: Array<{
    value: string;
    label: string;
    meta: string;
    sideLabel: string;
  }> = [];

  if (isLiteralNumberValue(searchValue)) {
    literalOptions.push({
      value: `literal:number:${searchValue}`,
      label: searchValue,
      meta: usesList ? "Number list" : "Number",
      sideLabel: "Use number",
    });
  }

  literalOptions.push({
    value: `literal:string:${searchValue}`,
    label: `"${searchValue}"`,
    meta: usesList ? "String list" : "String",
    sideLabel: "Use string",
  });

  if ("true".includes(normalized) || "false".includes(normalized)) {
    ["true", "false"]
      .filter((candidate) => candidate.includes(normalized))
      .forEach((candidate) => {
        literalOptions.push({
          value: `literal:boolean:${candidate}`,
          label: candidate,
          meta: usesList ? "Boolean list" : "Boolean",
          sideLabel: "Use boolean",
        });
      });
  }

  return literalOptions;
}

function updateConditionInGroups(
  groups: SimpleRuleConditionGroup[],
  groupId: string,
  conditionId: string,
  updater: (condition: SimpleRuleCondition) => SimpleRuleCondition
) {
  return groups.map((group) =>
    group.id === groupId
      ? {
          ...group,
          conditions: group.conditions.map((condition) =>
            condition.id === conditionId ? updater(condition) : condition
          ),
        }
      : group
  );
}

function FunctionVariableModal({
  draft,
  onClose,
  onChange,
  onSave,
  tableFieldOptions,
}: {
  draft: AggregatorModalState;
  onClose: () => void;
  onChange: (draft: AggregatorModalState) => void;
  onSave: (draft: AggregatorModalState) => void;
  tableFieldOptions: TableFieldOption[];
}) {
  const fieldOptions = tableFieldOptions.map((option) => ({
    value: `${option.tableName}::${option.fieldName}`,
    label: option.fieldName,
    meta: option.tableName,
    keywords: [option.tableName, option.fieldName],
  }));
  const fieldGroups = Object.entries(
    fieldOptions.reduce<Record<string, typeof fieldOptions>>((acc, option) => {
      const tableName = option.meta ?? "records";
      acc[tableName] = acc[tableName] ?? [];
      acc[tableName]!.push(option);
      return acc;
    }, {})
  )
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([tableName, options]) => ({
      id: `table-${tableName}`,
      label: tableName,
      children: [
        {
          id: `table-${tableName}-fields`,
          label: tableName,
          options,
        },
      ],
    }));

  const selectedAggregator = AGGREGATOR_OPTIONS.find(
    (option) => option.value === draft.aggregator
  );
  const needsPercentile = draft.aggregator === "PCTILE";
  const percentileValue = Number(draft.percentile);
  const hasValidPercentile = !needsPercentile || Number.isFinite(percentileValue);
  const canSave = Boolean(draft.fieldKey && hasValidPercentile);

  if (typeof document === "undefined") {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/38 p-4">
      <div className="w-full max-w-[920px] rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgba(15,23,42,0.14)]">
        <div className="relative border-b border-slate-200 px-6 py-5 text-center">
          <h3 className="text-[1.65rem] font-semibold tracking-tight text-slate-950">
            Create a variable
          </h3>
          <div className="mt-1 text-[13px] text-slate-500">From Marble database</div>
          <button
            type="button"
            onClick={onClose}
            className="absolute right-4 top-4 rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-900"
            aria-label="Close create variable modal"
          >
            <X className="size-4" />
          </button>
        </div>

        <div className="space-y-6 px-6 py-6">
          <div className="rounded-xl border border-slate-200 px-4 py-3 text-[14px] text-slate-700">
            Computes aggregates on your ingested data.
          </div>

          <label className="block space-y-2">
            <span className="text-[15px] font-medium text-slate-900">Variable name</span>
            <Input
              value={draft.variableName}
              onChange={(event) =>
                onChange({
                  ...draft,
                  variableName: event.target.value,
                })
              }
              placeholder="Edit the name of your variable"
              className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
            />
          </label>

          <div
            className={cn(
              "grid gap-5",
              needsPercentile
                ? "md:grid-cols-[220px_140px_minmax(0,1fr)]"
                : "md:grid-cols-[220px_minmax(0,1fr)]"
            )}
          >
            <label className="block space-y-2">
              <span className="text-[15px] font-medium text-slate-900">Function</span>
              <select
                value={draft.aggregator}
                onChange={(event) =>
                  onChange({
                    ...draft,
                    aggregator: event.target.value as AggregatorOperator,
                  })
                }
                className="flex h-11 w-full rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-900 outline-none focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
              >
                {AGGREGATOR_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>

            {needsPercentile ? (
              <label className="block space-y-2">
                <span className="text-[15px] font-medium text-slate-900">Percentile</span>
                <Input
                  value={draft.percentile}
                  onChange={(event) =>
                    onChange({
                      ...draft,
                      percentile: event.target.value,
                    })
                  }
                  inputMode="decimal"
                  placeholder="50"
                  className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                />
              </label>
            ) : null}

            <label className="block space-y-2">
              <span className="text-[15px] font-medium text-slate-900">Object field</span>
              <RuleOperandSelector
                value={draft.fieldKey}
                options={fieldOptions}
                groups={fieldGroups}
                panelPosition="top"
                placeholder="Select a field..."
                searchPlaceholder="Search fields"
                emptyLabel="No fields matched your search."
                onChange={(value) =>
                  onChange({
                    ...draft,
                    fieldKey: value,
                  })
                }
              />
            </label>
          </div>

          {selectedAggregator?.helper ? (
            <div className="rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
              {selectedAggregator.helper}
            </div>
          ) : null}

          <div className="space-y-3">
            <div className="text-[15px] font-medium text-slate-900">Filters</div>
            <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-4 text-[14px] text-slate-700">
              <div className="flex items-start gap-2">
                <Info className="mt-0.5 size-4 shrink-0 text-slate-500" />
                <span>Filter authoring for aggregator variables is the next pass. This first slice saves the variable and field selection end to end.</span>
              </div>
            </div>
          </div>
        </div>

        <div className="flex items-center justify-end gap-3 border-t border-slate-200 px-6 py-5">
          <Button
            variant="outline"
            type="button"
            onClick={onClose}
            className="h-9 rounded-lg border-slate-200 px-4 text-sm shadow-none hover:translate-y-0"
          >
            Cancel
          </Button>
          <Button
            type="button"
            onClick={() => onSave(draft)}
            disabled={!canSave}
            className="h-9 rounded-lg bg-[#2563eb] px-4 text-sm text-white shadow-none hover:translate-y-0 hover:bg-[#1d4ed8]"
          >
            Save
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}

export function RuleBuilderSimple({
  groups,
  onChange,
  accessorOptions,
  operatorOptions,
  customListOptions,
  triggerObjectType,
  tableFieldOptions,
  disabled = false,
}: {
  groups: SimpleRuleConditionGroup[];
  onChange: (groups: SimpleRuleConditionGroup[]) => void;
  accessorOptions: RuleAccessorOption[];
  operatorOptions: RuleOperatorOption[];
  customListOptions: Array<{ id: string; name: string }>;
  triggerObjectType: string;
  tableFieldOptions: TableFieldOption[];
  disabled?: boolean;
}) {
  const [aggregatorModal, setAggregatorModal] = useState<AggregatorModalState | null>(null);

  const fieldSelectorOptions = accessorOptions.map((option) => ({
    value: option.id,
    label: option.label,
    meta: option.meta,
    keywords: [option.meta, option.label],
  }));
  const fieldDiscoveryGroups = useMemo(() => {
    const payloadOptions = fieldSelectorOptions.filter((option) =>
      option.value.startsWith("payload:")
    );
    const databaseOptionGroups = new Map<string, typeof fieldSelectorOptions>();

    fieldSelectorOptions
      .filter((option) => option.value.startsWith("database:"))
      .forEach((option) => {
        const accessor = accessorOptions.find((item) => item.id === option.value);
        const path =
          accessor?.astNode.named_children?.path?.constant &&
          Array.isArray(accessor.astNode.named_children.path.constant)
            ? accessor.astNode.named_children.path.constant
                .filter((item): item is string => typeof item === "string")
            : [];
        const tableName =
          typeof accessor?.astNode.named_children?.tableName?.constant === "string"
            ? accessor.astNode.named_children.tableName.constant
            : triggerObjectType;
        const groupLabel =
          path.length > 0 ? `${tableName}_${path.join("_")}` : `From ${tableName}`;
        const current = databaseOptionGroups.get(groupLabel) ?? [];
        current.push(option);
        databaseOptionGroups.set(groupLabel, current);
      });

    return [
      {
        id: "fields",
        label: "Fields",
        children: [
          ...(payloadOptions.length > 0
            ? [
                {
                  id: `fields-${triggerObjectType || "trigger"}`,
                  label: `From ${triggerObjectType || "trigger"}`,
                  options: payloadOptions,
                },
              ]
            : []),
          ...[...databaseOptionGroups.entries()]
            .sort(([left], [right]) => left.localeCompare(right))
            .map(([label, options]) => ({
              id: `fields-${label}`,
              label,
              options,
            })),
        ],
      },
    ];
  }, [accessorOptions, fieldSelectorOptions, triggerObjectType]);

  const operatorSelectorOptions = operatorOptions.map((operatorOption) => ({
    value: operatorOption.value,
    label: operatorOption.label,
    keywords: operatorOption.keywords,
  }));
  const customListSelectorOptions = customListOptions.map((customList) => ({
    value: `custom-list:${customList.id}`,
    label: customList.name,
    meta: "Custom list",
    keywords: ["list", "custom list"],
  }));

  const functionOperands = useMemo(() => {
    const items = new Map<string, SimpleRuleFunctionOperand>();

    groups.forEach((group) => {
      group.conditions.forEach((condition) => {
        if (condition.leftFunction) {
          items.set(condition.leftFunction.id, condition.leftFunction);
        }
        if (condition.rightFunction) {
          items.set(condition.rightFunction.id, condition.rightFunction);
        }
      });
    });

    return [...items.values()].sort((left, right) => left.label.localeCompare(right.label));
  }, [groups]);

  const functionLookup = useMemo(
    () => new Map(functionOperands.map((operand) => [`function:${operand.id}`, operand])),
    [functionOperands]
  );
  const functionSelectorOptions = functionOperands.map((operand) => ({
    value: `function:${operand.id}`,
    label: operand.label,
    meta: operand.meta,
    keywords: ["function", "variable", operand.label],
  }));

  function updateCondition(
    groupId: string,
    conditionId: string,
    updater: (condition: SimpleRuleCondition) => SimpleRuleCondition
  ) {
    onChange(updateConditionInGroups(groups, groupId, conditionId, updater));
  }

  function updateOperator(groupId: string, conditionId: string, value: string) {
    updateCondition(groupId, conditionId, (condition) => {
      return {
        ...condition,
        operator: value as SimpleRuleCondition["operator"],
      };
    });
  }

  function updateLeftOperand(groupId: string, conditionId: string, value: string) {
    const selectedFunction = functionLookup.get(value);
    if (selectedFunction) {
      updateCondition(groupId, conditionId, (condition) => ({
        ...condition,
        left: "",
        leftMode: "function",
        leftFunction: selectedFunction,
        valueType: selectedFunction.valueType,
      }));
      return;
    }

    if (value.startsWith("custom-list:")) {
      updateCondition(groupId, conditionId, (condition) => ({
        ...condition,
        left: value.replace(/^custom-list:/, ""),
        leftMode: "custom_list",
        leftFunction: null,
        valueType: "string",
      }));
      return;
    }

    const literalSelection = decodeLiteralSelection(value);
    if (literalSelection) {
      updateCondition(groupId, conditionId, (condition) => ({
        ...condition,
        left: literalSelection.rawValue,
        leftMode: "constant",
        leftFunction: null,
        valueType: literalSelection.valueType,
      }));
      return;
    }

    updateCondition(groupId, conditionId, (condition) => ({
      ...condition,
      left: value,
      leftMode: "field",
      leftFunction: null,
      valueType: "string",
    }));
  }

  function updateRightOperand(groupId: string, conditionId: string, value: string) {
    const selectedFunction = functionLookup.get(value);
    if (selectedFunction) {
      updateCondition(groupId, conditionId, (condition) => ({
        ...condition,
        right: "",
        rightMode: "function",
        rightFunction: selectedFunction,
        valueType: selectedFunction.valueType,
      }));
      return;
    }

    const literalSelection = decodeLiteralSelection(value);
    if (literalSelection) {
      updateCondition(groupId, conditionId, (condition) => ({
        ...condition,
        right: literalSelection.rawValue,
        rightMode: "constant",
        rightFunction: null,
        valueType: literalSelection.valueType,
      }));
      return;
    }

    const isFieldSelection = fieldSelectorOptions.some((option) => option.value === value);
    updateCondition(groupId, conditionId, (condition) => ({
      ...condition,
      right: isFieldSelection
        ? value
        : value.startsWith("custom-list:")
          ? value.replace(/^custom-list:/, "")
          : value,
      rightMode: isFieldSelection ? "field" : value.startsWith("custom-list:") ? "custom_list" : "constant",
      rightFunction: null,
      valueType: isFieldSelection || value.startsWith("custom-list:") ? "string" : condition.valueType,
    }));
  }

  function applyFunctionOperand(
    groupId: string,
    conditionId: string,
    side: "left" | "right",
    operand: SimpleRuleFunctionOperand
  ) {
    updateCondition(groupId, conditionId, (condition) => {
      if (side === "left") {
        return {
          ...condition,
          left: "",
          leftMode: "function",
          leftFunction: operand,
          valueType: operand.valueType,
        };
      }

      return {
        ...condition,
        right: "",
        rightMode: "function",
        rightFunction: operand,
        valueType: operand.valueType,
      };
    });
  }

  function openAggregatorVariableModal(
    groupId: string,
    conditionId: string,
    side: "left" | "right",
    aggregator: AggregatorOperator
  ) {
    setAggregatorModal({
      side,
      groupId,
      conditionId,
      aggregator,
      variableName: "",
      fieldKey: "",
      percentile: "50",
    });
  }

  function saveAggregatorVariable(draft: AggregatorModalState) {
    const [tableName = "", fieldName = ""] = draft.fieldKey.split("::");
    if (!tableName || !fieldName) {
      setAggregatorModal(draft);
      return;
    }

    if (draft.aggregator === "PCTILE" && !Number.isFinite(Number(draft.percentile))) {
      setAggregatorModal(draft);
      return;
    }

    const operand = createFunctionOperand({
      ast: buildAggregatorAst({
        aggregator: draft.aggregator,
        tableName,
        fieldName,
        label: draft.variableName.trim() || `${draft.aggregator.toLowerCase()}_${fieldName}`,
        percentile:
          draft.aggregator === "PCTILE" ? Number(draft.percentile) : undefined,
      }),
      label: draft.variableName.trim(),
      meta: `Aggregation on ${tableName}`,
    });

    applyFunctionOperand(draft.groupId, draft.conditionId, draft.side, operand);
    setAggregatorModal(null);
  }

  function addCondition(groupId: string) {
    onChange(
      groups.map((group) =>
        group.id === groupId
          ? {
              ...group,
              conditions: [...group.conditions, createSimpleRuleCondition()],
            }
          : group
      )
    );
  }

  function removeCondition(groupId: string, conditionId: string) {
    const nextGroups = groups
      .map((group) =>
        group.id === groupId
          ? {
              ...group,
              conditions: group.conditions.filter((condition) => condition.id !== conditionId),
            }
          : group
      )
      .filter((group) => group.conditions.length > 0);

    onChange(nextGroups.length > 0 ? nextGroups : [createSimpleRuleGroup()]);
  }

  function addGroup() {
    onChange([...groups, createSimpleRuleGroup()]);
  }

  function wrapGroup(groupId: string) {
    const groupIndex = groups.findIndex((group) => group.id === groupId);
    if (groupIndex === -1) {
      return;
    }

    const nextGroups = [...groups];
    const currentGroup = nextGroups[groupIndex]!;
    nextGroups[groupIndex] = {
      ...currentGroup,
      openBefore: (currentGroup.openBefore ?? 0) + 1,
    };
    nextGroups.splice(
      groupIndex + 1,
      0,
      createSimpleRuleGroup({
        closeAfter: 1,
      })
    );
    onChange(nextGroups);
  }

  function unwrapGroup(groupId: string) {
    const groupIndex = groups.findIndex((group) => group.id === groupId);
    if (groupIndex === -1) {
      return;
    }

    const currentGroup = groups[groupIndex]!;
    if ((currentGroup.openBefore ?? 0) <= 0) {
      return;
    }

    let balance = currentGroup.openBefore ?? 0;
    let closingIndex = groupIndex;

    for (let index = groupIndex; index < groups.length; index += 1) {
      if (index > groupIndex) {
        balance += groups[index]!.openBefore ?? 0;
      }
      balance -= groups[index]!.closeAfter ?? 0;
      if (balance <= 0) {
        closingIndex = index;
        break;
      }
    }

    onChange(
      groups.map((group, index) => {
        if (index === groupIndex) {
          return {
            ...group,
            openBefore: Math.max(0, (group.openBefore ?? 0) - 1),
          };
        }

        if (index === closingIndex) {
          return {
            ...group,
            closeAfter: Math.max(0, (group.closeAfter ?? 0) - 1),
          };
        }

        return group;
      })
    );
  }

  function isConditionComplete(condition: SimpleRuleCondition) {
    const hasLeftOperand = Boolean(
      (condition.leftMode === "function" && condition.leftFunction) || condition.left.trim()
    );
    const hasRightOperand = Boolean(
      (condition.rightMode === "function" && condition.rightFunction) || condition.right.trim()
    );

    return Boolean(
      hasLeftOperand &&
        condition.operator &&
        (isUnaryRuleOperator(condition.operator) || hasRightOperand)
    );
  }

  function isConditionStarted(condition: SimpleRuleCondition) {
    return Boolean(
      (condition.leftMode === "function" && condition.leftFunction) ||
        (condition.rightMode === "function" && condition.rightFunction) ||
        condition.left.trim() ||
        condition.operator ||
        condition.right.trim()
    );
  }

  function missingCount(condition: SimpleRuleCondition) {
    const requiredValues = isUnaryRuleOperator(condition.operator)
      ? [
          (condition.leftMode === "function" && condition.leftFunction) || condition.left.trim(),
          condition.operator,
        ]
      : [
          (condition.leftMode === "function" && condition.leftFunction) || condition.left.trim(),
          condition.operator,
          (condition.rightMode === "function" && condition.rightFunction) || condition.right.trim(),
        ];

    return requiredValues.filter(Boolean).length;
  }

  return (
    <>
      <div className="space-y-4">
        {groups.map((group, groupIndex) => (
          <div key={group.id} className="space-y-3">
            {groupIndex > 0 ? (
              <div className="flex justify-start">
                <div className="inline-flex h-12 items-center justify-center rounded-sm bg-slate-50 px-3 text-[16px] font-medium text-slate-600">
                  or
                </div>
              </div>
            ) : null}
            <div className="rounded-xl border border-slate-200 bg-white p-6">
              <div className="grid grid-cols-[max-content_minmax(0,1fr)_max-content] items-start gap-3">
                <div className="pt-1 text-[18px] text-slate-700">
                  {(group.openBefore ?? 0) > 0 ? "(".repeat(group.openBefore ?? 0) : ""}
                </div>
                <div className="space-y-4">
                  {group.conditions.map((condition, conditionIndex) => {
                    const complete = isConditionComplete(condition);
                    const started = isConditionStarted(condition);
                    const selectedOperator = getRuleOperatorOption(condition.operator);
                    const requiresRightOperand = !selectedOperator?.unary;
                    const totalRequired = requiresRightOperand ? 3 : 2;
                    const required = totalRequired - missingCount(condition);
                    const leftValue =
                      condition.leftMode === "function" && condition.leftFunction
                        ? `function:${condition.leftFunction.id}`
                        : condition.leftMode === "custom_list"
                          ? `custom-list:${condition.left}`
                        : condition.left;
                    const leftSelectedMeta =
                      condition.leftMode === "function"
                        ? condition.leftFunction?.meta
                        : condition.leftMode === "custom_list"
                          ? "Custom list"
                          : condition.leftMode === "constant"
                            ? condition.valueType === "number"
                              ? "Number"
                              : condition.valueType === "boolean"
                                ? "Boolean"
                                : "String"
                          : condition.left.trim()
                            ? "Field"
                            : undefined;
                    const rightValue =
                      condition.rightMode === "function" && condition.rightFunction
                        ? `function:${condition.rightFunction.id}`
                        : condition.rightMode === "custom_list"
                          ? `custom-list:${condition.right}`
                          : condition.right;
                    const rightSelectedMeta =
                      condition.rightMode === "function"
                        ? condition.rightFunction?.meta
                        : condition.rightMode === "custom_list"
                          ? "Custom list"
                          : condition.right.trim()
                            ? selectedOperator?.usesList
                              ? condition.valueType === "number"
                                ? "Number list"
                                : condition.valueType === "boolean"
                                  ? "Boolean list"
                                  : "String list"
                              : condition.valueType === "number"
                                ? "Number"
                                : condition.valueType === "boolean"
                                  ? "Boolean"
                                  : "String"
                            : undefined;

                    const leftGroups = [
                      ...fieldDiscoveryGroups,
                      ...(customListSelectorOptions.length > 0
                        ? [
                            {
                              id: "custom-lists-left",
                              label: "Lists",
                              children: [
                                {
                                  id: "custom-lists-left-items",
                                  label: "Lists",
                                  options: customListSelectorOptions,
                                },
                              ],
                            },
                          ]
                        : []),
                      {
                        id: "functions",
                        label: "Functions",
                        children: [
                          ...(functionSelectorOptions.length > 0
                            ? [
                                {
                                  id: `functions-existing-${group.id}-${condition.id}`,
                                  label: "Variables",
                                  options: functionSelectorOptions,
                                },
                              ]
                            : []),
                          {
                            id: `functions-aggregations-${group.id}-${condition.id}`,
                            label: "Create a variable",
                            options: AGGREGATOR_OPTIONS.map((option) => ({
                              value: `aggregator:${option.value}:${group.id}:${condition.id}:left`,
                              label: option.label,
                              meta: option.helper ?? "Aggregation",
                              isAction: true,
                              onSelectAction: () =>
                                openAggregatorVariableModal(
                                  group.id,
                                  condition.id,
                                  "left",
                                  option.value
                                ),
                            })),
                          },
                          {
                            id: `functions-direct-${group.id}-${condition.id}`,
                            label: "Direct functions",
                            options: [
                              {
                                value: `direct-time-now-${group.id}-${condition.id}`,
                                label: "Current time",
                                meta: "Function",
                                isAction: true,
                                onSelectAction: () =>
                                  applyFunctionOperand(
                                    group.id,
                                    condition.id,
                                    "left",
                                    createFunctionOperand({
                                      ast: { function: "TimeNow" },
                                    })
                                  ),
                              },
                              {
                                value: `direct-risk-level-${group.id}-${condition.id}`,
                                label: "Record risk level",
                                meta: "Platform function",
                                isAction: true,
                                onSelectAction: () =>
                                  applyFunctionOperand(
                                    group.id,
                                    condition.id,
                                    "left",
                                    createFunctionOperand({
                                      ast: { function: "record_risk_level" },
                                    })
                                  ),
                              },
                            ],
                          },
                        ],
                      },
                      {
                        id: "modeling",
                        label: "Modeling",
                        children: [
                          {
                            id: `modeling-open-bracket-${group.id}-${condition.id}`,
                            label: "Modeling",
                            options: [
                              {
                                value: `modeling-open-bracket-${group.id}-${condition.id}`,
                                label: "Open bracket",
                                meta: "Wrap this block in brackets",
                                isAction: true,
                                onSelectAction: () => wrapGroup(group.id),
                              },
                              ...((group.openBefore ?? 0) > 0
                                ? [
                                    {
                                      value: `modeling-remove-bracket-${group.id}-${condition.id}`,
                                      label: "Remove bracket",
                                      meta: "Unwrap this block",
                                      isAction: true,
                                      onSelectAction: () => unwrapGroup(group.id),
                                    },
                                  ]
                                : []),
                            ],
                          },
                        ],
                      },
                    ];

                    const rightGroups = [
                      {
                        id: "fields",
                        label: "Fields",
                        children: fieldDiscoveryGroups[0]?.children ?? [],
                      },
                      {
                        id: "functions-right",
                        label: "Functions",
                        children: [
                          ...(functionSelectorOptions.length > 0
                            ? [
                                {
                                  id: `functions-existing-right-${group.id}-${condition.id}`,
                                  label: "Variables",
                                  options: functionSelectorOptions,
                                },
                              ]
                            : []),
                          {
                            id: `functions-aggregations-right-${group.id}-${condition.id}`,
                            label: "Create a variable",
                            options: AGGREGATOR_OPTIONS.map((option) => ({
                              value: `aggregator:${option.value}:${group.id}:${condition.id}:right`,
                              label: option.label,
                              meta: option.helper ?? "Aggregation",
                              isAction: true,
                              onSelectAction: () =>
                                openAggregatorVariableModal(
                                  group.id,
                                  condition.id,
                                  "right",
                                  option.value
                                ),
                            })),
                          },
                          {
                            id: `functions-direct-right-${group.id}-${condition.id}`,
                            label: "Direct functions",
                            options: [
                              {
                                value: `direct-time-now-right-${group.id}-${condition.id}`,
                                label: "Current time",
                                meta: "Function",
                                isAction: true,
                                onSelectAction: () =>
                                  applyFunctionOperand(
                                    group.id,
                                    condition.id,
                                    "right",
                                    createFunctionOperand({
                                      ast: { function: "TimeNow" },
                                    })
                                  ),
                              },
                              {
                                value: `direct-risk-level-right-${group.id}-${condition.id}`,
                                label: "Record risk level",
                                meta: "Platform function",
                                isAction: true,
                                onSelectAction: () =>
                                  applyFunctionOperand(
                                    group.id,
                                    condition.id,
                                    "right",
                                    createFunctionOperand({
                                      ast: { function: "record_risk_level" },
                                    })
                                  ),
                              },
                            ],
                          },
                        ],
                      },
                      ...(customListSelectorOptions.length > 0
                        ? [
                            {
                              id: "custom-lists",
                              label: "Lists",
                              children: [
                                {
                                  id: "custom-lists-items",
                                  label: "Lists",
                                  options: customListSelectorOptions,
                                },
                              ],
                            },
                          ]
                        : []),
                    ];

                    return (
                      <div
                        key={condition.id}
                        className="space-y-2"
                      >
                        <ConditionSelectorRow
                          prefixLabel={conditionIndex === 0 ? "if" : "and"}
                          className="flex flex-wrap items-start gap-3 text-[14px]"
                          leftSelector={{
                            value: leftValue,
                            invalid:
                              started &&
                              !(
                                (condition.leftMode === "function" && condition.leftFunction) ||
                                condition.left.trim()
                              ),
                            selectedMeta: leftSelectedMeta,
                            prefix:
                              condition.leftMode === "function"
                                ? "fx"
                                : condition.leftMode === "custom_list"
                                  ? "[]"
                                  : condition.valueType === "number"
                                    ? "#"
                                    : "Tt",
                            options: [
                              ...fieldSelectorOptions,
                              ...functionSelectorOptions,
                              ...customListSelectorOptions,
                            ],
                            groups: leftGroups,
                            placeholder: "Select an operand...",
                            searchPlaceholder: "Select or create an operand",
                            emptyLabel: "No operands matched your search.",
                            searchOptionsBuilder: (searchValue) =>
                              buildLiteralSearchOptions(searchValue),
                            onChange: (nextValue) =>
                              updateLeftOperand(group.id, condition.id, nextValue),
                          }}
                          operatorSelector={{
                            value: condition.operator,
                            invalid: started && !condition.operator,
                            prefix: null,
                            className: "min-w-[150px] max-w-[190px]",
                            options: operatorSelectorOptions,
                            placeholder: "...",
                            searchPlaceholder: "Search operators",
                            emptyLabel: "No operators matched your search.",
                            onChange: (nextValue) =>
                              updateOperator(
                                group.id,
                                condition.id,
                                nextValue as SimpleRuleCondition["operator"]
                              ),
                          }}
                          rightSelector={
                            requiresRightOperand
                              ? {
                                  value: rightValue,
                                  invalid:
                                    started &&
                                    !(
                                      (condition.rightMode === "function" &&
                                        condition.rightFunction) ||
                                      condition.right.trim()
                                    ),
                                  selectedMeta: rightSelectedMeta,
                                  prefix:
                                    condition.rightMode === "function"
                                      ? "fx"
                                      : condition.rightMode === "custom_list"
                                        ? "[]"
                                        : condition.valueType === "number"
                                          ? "#"
                                          : condition.valueType === "boolean"
                                            ? "?"
                                            : "Tt",
                                  options: [
                                    ...fieldSelectorOptions,
                                    ...functionSelectorOptions,
                                    ...customListSelectorOptions,
                                  ],
                                  groups: rightGroups,
                                  placeholder: "Select an operand...",
                                  searchPlaceholder: "Select or create an operand",
                                  emptyLabel: "No operands matched your search.",
                                  searchOptionsBuilder: (searchValue) =>
                                    buildLiteralSearchOptions(
                                      searchValue,
                                      Boolean(selectedOperator?.usesList)
                                    ),
                                  onChange: (nextValue) =>
                                    updateRightOperand(group.id, condition.id, nextValue),
                                }
                              : null
                          }
                          disabled={disabled}
                          onRemove={() => removeCondition(group.id, condition.id)}
                        />
                        {started && !complete ? (
                          <div className="inline-flex rounded-md bg-[#ffe7de] px-3 py-2 text-[12px] font-medium text-[#ec5a2e]">
                            {required} required
                          </div>
                        ) : null}
                      </div>
                    );
                  })}

                  <Button
                    type="button"
                    variant="outline"
                    disabled={disabled}
                    onClick={() => addCondition(group.id)}
                    className="h-11 w-fit rounded-md border-[#2d63b8] px-5 text-[14px] font-medium text-[#1f4f96] shadow-none hover:bg-[#eef3ff]"
                  >
                    <Plus className="size-4" />
                    Condition
                  </Button>
                </div>
                <div className="pt-1 text-[18px] text-slate-700">
                  {(group.closeAfter ?? 0) > 0 ? ")".repeat(group.closeAfter ?? 0) : ""}
                </div>
              </div>
            </div>
          </div>
        ))}

        <div className="flex justify-start">
          <Button
            type="button"
            variant="outline"
            disabled={disabled}
            onClick={addGroup}
            className={cn(
              "h-11 rounded-md border-[#2d63b8] px-5 text-[14px] font-medium text-[#1f4f96] shadow-none",
              "hover:border-[#2d63b8] hover:bg-[#eef3ff] hover:text-[#1f4f96]"
            )}
          >
            <Plus className="size-4" />
            Group
          </Button>
        </div>
      </div>

      {aggregatorModal ? (
        <FunctionVariableModal
          draft={aggregatorModal}
          onClose={() => setAggregatorModal(null)}
          onChange={setAggregatorModal}
          onSave={saveAggregatorVariable}
          tableFieldOptions={tableFieldOptions}
        />
      ) : null}
    </>
  );
}
