"use client";

import { type ReactNode, useMemo, useState } from "react";
import { Plus } from "lucide-react";

import { ConditionSelectorRow } from "@/components/detection/condition-selector-row";
import {
  createDateFunctionDraft,
  DateFunctionModal,
  type DateFunctionDraft,
  type DateFunctionType,
} from "@/components/detection/date-function-modal";
import {
  AGGREGATOR_OPTIONS,
  FunctionVariableModal,
  type FunctionVariableDraft,
  type FunctionVariableTableFieldOption,
} from "@/components/detection/function-variable-modal";
import { RuleBuilderExpression } from "@/components/detection/rule-builder-expression";
import {
  buildCustomListOperandSources,
  buildFieldOperandSources,
  buildFunctionOperandSources,
  buildLiteralSearchOptions,
  decodeLiteralSelection,
} from "@/components/detection/rule-operand-sources";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  buildAggregatorAst,
  createFunctionOperand,
  createExpressionLeaf,
  createExpressionOperator,
  createSimpleRuleCondition,
  createSimpleRuleGroup,
  getRuleOperatorOption,
  isExpressionRuleNodeComplete,
  isUnaryRuleOperator,
  type AggregatorOperator,
  type ExpressionRuleNode,
  type RuleAccessorOption,
  type RuleOperatorOption,
  type SimpleRuleCondition,
  type SimpleRuleConditionGroup,
  type SimpleRuleFunctionOperand,
} from "@/lib/rule-builder";

type AggregatorModalState = {
  side: "left" | "right";
  groupId: string;
  conditionId: string;
} & FunctionVariableDraft;

type DateFunctionModalState = {
  side: "left" | "right";
  groupId: string;
  conditionId: string;
} & DateFunctionDraft;

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

function buildRightExpressionSeed(
  condition: SimpleRuleCondition
): ExpressionRuleNode {
  const baseLeft =
    condition.rightMode === "field"
      ? createExpressionLeaf({
          mode: "accessor",
          accessorId: condition.right,
        })
      : condition.rightMode === "custom_list"
        ? createExpressionLeaf({
            mode: "custom_list",
            value: condition.right,
            valueType: "string",
          })
        : createExpressionLeaf({
            mode: "constant",
            value: condition.right,
            valueType: condition.valueType,
          });

  return createExpressionOperator({
    children: [baseLeft, createExpressionLeaf()],
  });
}

function BracketControl({
  label,
  onAdd,
  onRemove,
  disabled,
  addLabel,
}: {
  label: ReactNode;
  onAdd: () => void;
  onRemove: () => void;
  disabled?: boolean;
  addLabel: string;
}) {
  const [open, setOpen] = useState(false);

  return (
    <div className="relative inline-flex">
      <button
        type="button"
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
        className="inline-flex h-10 items-center justify-center rounded-sm border border-slate-300 bg-white px-2 text-[18px] text-slate-700 transition hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
      >
        {label}
      </button>
      {open ? (
        <div className="absolute left-0 top-full z-20 mt-1 min-w-[180px] rounded-sm border border-slate-300 bg-white p-1 shadow-[0_18px_50px_rgba(15,23,42,0.12)]">
          <button
            type="button"
            disabled={disabled}
            onClick={() => {
              onAdd();
              setOpen(false);
            }}
            className="block w-full rounded-sm px-3 py-2 text-left text-[13px] text-slate-800 transition hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {addLabel}
          </button>
          <button
            type="button"
            disabled={disabled}
            onClick={() => {
              onRemove();
              setOpen(false);
            }}
            className="block w-full rounded-sm px-3 py-2 text-left text-[13px] text-red-700 transition hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-50"
          >
            Remove nesting
          </button>
        </div>
      ) : null}
    </div>
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
  tableFieldOptions: FunctionVariableTableFieldOption[];
  disabled?: boolean;
}) {
  const [aggregatorModal, setAggregatorModal] = useState<AggregatorModalState | null>(null);
  const [dateFunctionModal, setDateFunctionModal] = useState<DateFunctionModalState | null>(null);
  const [createdFunctionOperands, setCreatedFunctionOperands] = useState<
    SimpleRuleFunctionOperand[]
  >([]);

  const { fieldSelectorOptions, fieldDiscoveryGroups } = useMemo(
    () => buildFieldOperandSources({ accessorOptions, triggerObjectType }),
    [accessorOptions, triggerObjectType]
  );

  const operatorSelectorOptions = operatorOptions.map((operatorOption) => ({
    value: operatorOption.value,
    label: operatorOption.label,
    keywords: operatorOption.keywords,
  }));
  const { customListSelectorOptions } = useMemo(
    () => buildCustomListOperandSources(customListOptions),
    [customListOptions]
  );

  const functionOperands = useMemo(() => {
    const items = new Map<string, SimpleRuleFunctionOperand>();

    createdFunctionOperands.forEach((operand) => {
      items.set(operand.id, operand);
    });

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
  }, [createdFunctionOperands, groups]);

  const { functionLookup, functionSelectorOptions } = useMemo(
    () => buildFunctionOperandSources(functionOperands),
    [functionOperands]
  );

  function updateCondition(
    groupId: string,
    conditionId: string,
    updater: (condition: SimpleRuleCondition) => SimpleRuleCondition
  ) {
    onChange(updateConditionInGroups(groups, groupId, conditionId, updater));
  }

  function updateOperator(groupId: string, conditionId: string, value: string) {
    const nextOperator = getRuleOperatorOption(value);
    updateCondition(groupId, conditionId, (condition) => {
      return {
        ...condition,
        operator: value as SimpleRuleCondition["operator"],
        ...(nextOperator?.unary || nextOperator?.usesList
          ? { rightExpression: null }
          : {}),
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
        rightExpression: null,
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
        rightExpression: null,
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
      rightExpression: null,
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
        rightExpression: null,
        valueType: operand.valueType,
      };
    });
  }

  function openRightOperandExpression(groupId: string, conditionId: string) {
    updateCondition(groupId, conditionId, (condition) => ({
      ...condition,
      rightExpression: condition.rightExpression ?? buildRightExpressionSeed(condition),
    }));
  }

  function updateRightOperandExpression(
    groupId: string,
    conditionId: string,
    rightExpression: ExpressionRuleNode
  ) {
    updateCondition(groupId, conditionId, (condition) => ({
      ...condition,
      rightExpression,
      right: "",
      rightFunction: null,
      rightMode: "constant",
    }));
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
      filters: [],
    });
  }

  function openDateFunctionModal(
    groupId: string,
    conditionId: string,
    side: "left" | "right",
    type: DateFunctionType
  ) {
    setDateFunctionModal({
      side,
      groupId,
      conditionId,
      ...createDateFunctionDraft(type),
    });
  }

  function saveDateFunction(operand: SimpleRuleFunctionOperand) {
    if (!dateFunctionModal) {
      return;
    }

    setCreatedFunctionOperands((current) => [...current, operand]);
    applyFunctionOperand(
      dateFunctionModal.groupId,
      dateFunctionModal.conditionId,
      dateFunctionModal.side,
      operand
    );
    setDateFunctionModal(null);
  }

  function saveAggregatorVariable(draft: FunctionVariableDraft) {
    const modalState = aggregatorModal
      ? {
          ...aggregatorModal,
          ...draft,
        }
      : null;
    if (!modalState) {
      return;
    }

    const [tableName = "", fieldName = ""] = modalState.fieldKey.split("::");
    if (!tableName || !fieldName) {
      setAggregatorModal(modalState);
      return;
    }

    if (
      modalState.aggregator === "PCTILE" &&
      !Number.isFinite(Number(modalState.percentile))
    ) {
      setAggregatorModal(modalState);
      return;
    }

    const operand = createFunctionOperand({
      ast: buildAggregatorAst({
        aggregator: modalState.aggregator,
        tableName,
        fieldName,
        label:
          modalState.variableName.trim() ||
          `${modalState.aggregator.toLowerCase()}_${fieldName}`,
        percentile:
          modalState.aggregator === "PCTILE"
            ? Number(modalState.percentile)
            : undefined,
        filters: modalState.filters
          .flatMap((filter) => {
            const [filterTableName = "", filterFieldName = ""] = filter.fieldKey.split("::");
            return filterTableName && filterFieldName
              ? [{
                  tableName: filterTableName,
                  fieldName: filterFieldName,
                  operator: filter.operator,
                  rightMode: filter.rightMode,
                  value:
                    filter.rightMode === "field"
                      ? filter.rightValue.split("::")[1] ?? filter.rightValue
                      : filter.rightValue,
                }]
              : [];
          })
          ,
      }),
      label: modalState.variableName.trim(),
      meta: `Aggregation on ${tableName}`,
    });

    setCreatedFunctionOperands((current) => {
      const next = current.filter((item) => item.id !== operand.id);
      return [...next, operand].sort((left, right) => left.label.localeCompare(right.label));
    });
    applyFunctionOperand(modalState.groupId, modalState.conditionId, modalState.side, operand);
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
    const groupIndex = groups.findIndex((group) => group.id === groupId);
    if (groupIndex === -1) {
      return;
    }

    const nextGroups = groups.map((group, index) =>
      index === groupIndex
        ? {
            ...group,
            conditions: group.conditions.filter((condition) => condition.id !== conditionId),
          }
        : group
    );
    const targetGroup = nextGroups[groupIndex];
    if (!targetGroup) {
      onChange([createSimpleRuleGroup()]);
      return;
    }

    if (targetGroup.conditions.length > 0) {
      onChange(nextGroups);
      return;
    }

    const remainingGroups = nextGroups.filter((_, index) => index !== groupIndex);
    if (remainingGroups.length === 0) {
      onChange([createSimpleRuleGroup()]);
      return;
    }

    const openBefore = targetGroup.openBefore ?? 0;
    const closeAfter = targetGroup.closeAfter ?? 0;

    if (openBefore > 0) {
      const receiverIndex = Math.min(groupIndex, remainingGroups.length - 1);
      const receiver = remainingGroups[receiverIndex];
      if (receiver) {
        remainingGroups[receiverIndex] = {
          ...receiver,
          openBefore: (receiver.openBefore ?? 0) + openBefore,
        };
      }
    }

    if (closeAfter > 0) {
      const receiverIndex = Math.max(0, groupIndex - 1);
      const receiver = remainingGroups[receiverIndex];
      if (receiver) {
        remainingGroups[receiverIndex] = {
          ...receiver,
          closeAfter: (receiver.closeAfter ?? 0) + closeAfter,
        };
      }
    }

    onChange(remainingGroups);
  }

  function addGroup() {
    onChange([...groups, createSimpleRuleGroup()]);
  }

  function getOpenBalanceThroughIndex(groupIndex: number) {
    return groups.slice(0, groupIndex + 1).reduce((balance, group) => {
      return balance + (group.openBefore ?? 0) - (group.closeAfter ?? 0);
    }, 0);
  }

  function moveClosingBracket(fromIndex: number, toIndex: number) {
    onChange(
      groups.map((group, index) => {
        if (index === fromIndex && index === toIndex) {
          return group;
        }

        if (index === fromIndex) {
          return {
            ...group,
            closeAfter: Math.max(0, (group.closeAfter ?? 0) - 1),
          };
        }

        if (index === toIndex) {
          return {
            ...group,
            closeAfter: (group.closeAfter ?? 0) + 1,
          };
        }

        return group;
      })
    );
  }

  function addOpenBracket(groupId: string) {
    const groupIndex = groups.findIndex((group) => group.id === groupId);
    if (groupIndex === -1) {
      return;
    }

    onChange(
      groups.map((group, index) =>
        index === groupIndex
          ? {
              ...group,
              openBefore: (group.openBefore ?? 0) + 1,
              closeAfter: (group.closeAfter ?? 0) + 1,
            }
          : group
      )
    );
  }

  function addCloseBracket(groupId: string) {
    const groupIndex = groups.findIndex((group) => group.id === groupId);
    if (groupIndex === -1) {
      return;
    }

    if (getOpenBalanceThroughIndex(groupIndex) <= 0) {
      for (let index = groupIndex - 1; index >= 0; index -= 1) {
        const matchingCloseIndex = findMatchingCloseIndex(index);
        if (matchingCloseIndex !== -1 && matchingCloseIndex < groupIndex) {
          moveClosingBracket(matchingCloseIndex, groupIndex);
          return;
        }
      }

      return;
    }

    onChange(
      groups.map((group, index) =>
        index === groupIndex
          ? {
              ...group,
              closeAfter: (group.closeAfter ?? 0) + 1,
            }
          : group
      )
    );
  }

  function findMatchingCloseIndex(openGroupIndex: number) {
    const currentGroup = groups[openGroupIndex];
    if (!currentGroup || (currentGroup.openBefore ?? 0) <= 0) {
      return -1;
    }

    let balance = currentGroup.openBefore ?? 0;
    for (let index = openGroupIndex; index < groups.length; index += 1) {
      if (index > openGroupIndex) {
        balance += groups[index]!.openBefore ?? 0;
      }
      balance -= groups[index]!.closeAfter ?? 0;
      if (balance <= 0) {
        return index;
      }
    }

    return -1;
  }

  function canAddCloseBracket(groupIndex: number) {
    if (getOpenBalanceThroughIndex(groupIndex) > 0) {
      return true;
    }

    for (let index = groupIndex - 1; index >= 0; index -= 1) {
      const matchingCloseIndex = findMatchingCloseIndex(index);
      if (matchingCloseIndex !== -1 && matchingCloseIndex < groupIndex) {
        return true;
      }
    }

    return false;
  }

  function findMatchingOpenIndex(closeGroupIndex: number) {
    const currentGroup = groups[closeGroupIndex];
    if (!currentGroup || (currentGroup.closeAfter ?? 0) <= 0) {
      return -1;
    }

    let balance = currentGroup.closeAfter ?? 0;
    for (let index = closeGroupIndex; index >= 0; index -= 1) {
      if (index < closeGroupIndex) {
        balance += groups[index]!.closeAfter ?? 0;
      }
      balance -= groups[index]!.openBefore ?? 0;
      if (balance <= 0) {
        return index;
      }
    }

    return -1;
  }

  function removeNesting(groupId: string) {
    const groupIndex = groups.findIndex((group) => group.id === groupId);
    if (groupIndex === -1) {
      return;
    }

    const currentGroup = groups[groupIndex]!;
    const openingIndex =
      (currentGroup.openBefore ?? 0) > 0 ? groupIndex : findMatchingOpenIndex(groupIndex);
    if (openingIndex === -1) {
      return;
    }

    const closingIndex = findMatchingCloseIndex(openingIndex);
    if (closingIndex === -1) {
      return;
    }

    onChange(
      groups.map((group, index) => {
        if (index === openingIndex && index === closingIndex) {
          return {
            ...group,
            openBefore: Math.max(0, (group.openBefore ?? 0) - 1),
            closeAfter: Math.max(0, (group.closeAfter ?? 0) - 1),
          };
        }

        if (index === openingIndex) {
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

  function buildDateFunctionActions(
    groupId: string,
    conditionId: string,
    side: "left" | "right"
  ) {
    return [
      {
        value: `date-time-add-${groupId}-${conditionId}-${side}`,
        label: "Adjust time",
        isAction: true,
        onSelectAction: () => openDateFunctionModal(groupId, conditionId, side, "TimeAdd"),
      },
      {
        value: `date-extract-${groupId}-${conditionId}-${side}`,
        label: "Extract time part",
        isAction: true,
        onSelectAction: () =>
          openDateFunctionModal(groupId, conditionId, side, "TimestampExtract"),
      },
      {
        value: `direct-time-now-${groupId}-${conditionId}-${side}`,
        label: "Current time",
        isAction: true,
        onSelectAction: () =>
          applyFunctionOperand(
            groupId,
            conditionId,
            side,
            createFunctionOperand({
              ast: { function: "TimeNow" },
            })
          ),
      },
    ];
  }

  function isConditionComplete(condition: SimpleRuleCondition) {
    const hasLeftOperand = Boolean(
      (condition.leftMode === "function" && condition.leftFunction) || condition.left.trim()
    );
    const hasRightOperand = Boolean(
      (condition.rightExpression && isExpressionRuleNodeComplete(condition.rightExpression)) ||
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
      condition.rightExpression ||
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
          (condition.rightExpression && isExpressionRuleNodeComplete(condition.rightExpression)) ||
            (condition.rightMode === "function" && condition.rightFunction) ||
            condition.right.trim(),
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
                      condition.rightExpression
                        ? "Expression"
                        : condition.rightMode === "function"
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
                            label: "Date and platform",
                            options: [
                              ...buildDateFunctionActions(group.id, condition.id, "left"),
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
                                meta: "Start a nested block here",
                                isAction: true,
                                onSelectAction: () => addOpenBracket(group.id),
                              },
                              ...(canAddCloseBracket(groupIndex)
                                ? [
                                    {
                                      value: `modeling-close-bracket-${group.id}-${condition.id}`,
                                      label: "Close bracket",
                                      meta: "End a nested block here",
                                      isAction: true,
                                      onSelectAction: () => addCloseBracket(group.id),
                                    },
                                  ]
                                : []),
                              ...((group.openBefore ?? 0) > 0 || (group.closeAfter ?? 0) > 0
                                ? [
                                    {
                                      value: `modeling-remove-bracket-${group.id}-${condition.id}`,
                                      label: "Remove nesting",
                                      meta: "Remove one bracket level",
                                      isAction: true,
                                      onSelectAction: () => removeNesting(group.id),
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
                            label: "Date and platform",
                            options: [
                              ...buildDateFunctionActions(group.id, condition.id, "right"),
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
                        <div className="flex flex-wrap items-start gap-2">
                          {conditionIndex === 0
                            ? Array.from({ length: group.openBefore ?? 0 }).map((_, bracketIndex) => (
                                <div
                                  key={`${group.id}-open-${bracketIndex}`}
                                  className="pt-1 text-[18px] text-slate-700"
                                >
                                  <BracketControl
                                    label="("
                                    disabled={disabled}
                                    addLabel="Open bracket"
                                    onAdd={() => addOpenBracket(group.id)}
                                    onRemove={() => removeNesting(group.id)}
                                  />
                                </div>
                              ))
                            : null}
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
                                ? condition.rightExpression
                                  ? null
                                  : {
                                      value: rightValue,
                                      invalid:
                                        started &&
                                        !(
                                          (condition.rightExpression &&
                                            isExpressionRuleNodeComplete(condition.rightExpression)) ||
                                          (condition.rightMode === "function" &&
                                            condition.rightFunction) ||
                                          condition.right.trim()
                                        ),
                                      selectedMeta: rightSelectedMeta,
                                      prefix:
                                        condition.rightExpression
                                          ? "()"
                                          : condition.rightMode === "function"
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
                                      actions:
                                        !selectedOperator?.usesList &&
                                        condition.rightMode !== "function" &&
                                        condition.rightMode !== "custom_list"
                                          ? [
                                              {
                                                id: `rhs-expression-${group.id}-${condition.id}`,
                                                label: condition.rightExpression
                                                  ? "Edit bracketed expression"
                                                  : "Open bracket",
                                                onSelect: () =>
                                                  openRightOperandExpression(group.id, condition.id),
                                              },
                                            ]
                                          : undefined,
                                      onChange: (nextValue) =>
                                        updateRightOperand(group.id, condition.id, nextValue),
                                    }
                                : null
                            }
                            rightContent={
                              requiresRightOperand && condition.rightExpression ? (
                                <RuleBuilderExpression
                                  root={condition.rightExpression}
                                  onChange={(nextExpression) =>
                                    updateRightOperandExpression(group.id, condition.id, nextExpression)
                                  }
                                  accessorOptions={accessorOptions}
                                  operatorOptions={operatorOptions}
                                  customListOptions={customListOptions}
                                  functionOperands={functionOperands}
                                  triggerObjectType={triggerObjectType}
                                  operandOptionsOverride={[
                                    ...fieldSelectorOptions,
                                    ...functionSelectorOptions,
                                    ...customListSelectorOptions,
                                  ]}
                                  operandGroupsOverride={rightGroups}
                                  disabled={disabled}
                                  compact
                                />
                              ) : undefined
                            }
                            disabled={disabled}
                            onRemove={() => removeCondition(group.id, condition.id)}
                          />
                        </div>
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
                <div className="flex flex-wrap items-start gap-2 pt-1 text-[18px] text-slate-700">
                  {Array.from({ length: group.closeAfter ?? 0 }).map((_, bracketIndex) => (
                    <BracketControl
                      key={`${group.id}-close-${bracketIndex}`}
                      label=")"
                      disabled={disabled}
                      addLabel="Close bracket"
                      onAdd={() => addCloseBracket(group.id)}
                      onRemove={() => removeNesting(group.id)}
                    />
                  ))}
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
          onChange={(draft) =>
            setAggregatorModal((current) =>
              current
                ? {
                    ...current,
                    ...draft,
                  }
                : null
            )
          }
          onSave={saveAggregatorVariable}
          tableFieldOptions={tableFieldOptions}
        />
      ) : null}

      {dateFunctionModal ? (
        <DateFunctionModal
          draft={dateFunctionModal}
          onClose={() => setDateFunctionModal(null)}
          onChange={(draft) =>
            setDateFunctionModal((current) =>
              current
                ? {
                    ...current,
                    ...draft,
                  }
                : null
            )
          }
          onSave={saveDateFunction}
          accessorOptions={accessorOptions}
          functionOperands={functionOperands}
        />
      ) : null}
    </>
  );
}
