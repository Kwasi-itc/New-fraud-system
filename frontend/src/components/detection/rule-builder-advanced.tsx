"use client";

import { Plus, Trash2 } from "lucide-react";

import { RuleOperandSelector } from "@/components/detection/rule-operand-selector";
import { Button } from "@/components/ui/button";
import {
  createAdvancedRuleCondition,
  createAdvancedRuleGroup,
  getRuleOperatorOption,
  isUnaryRuleOperator,
  type AdvancedRuleCondition,
  type AdvancedRuleConditionGroup,
  type RuleAccessorOption,
  type RuleOperatorOption,
} from "@/lib/rule-builder";
import { cn } from "@/lib/utils";

function updateConditionInGroups(
  groups: AdvancedRuleConditionGroup[],
  groupId: string,
  conditionId: string,
  updater: (condition: AdvancedRuleCondition) => AdvancedRuleCondition
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

export function RuleBuilderAdvanced({
  groups,
  onChange,
  accessorOptions,
  operatorOptions,
  triggerObjectType,
  disabled = false,
}: {
  groups: AdvancedRuleConditionGroup[];
  onChange: (groups: AdvancedRuleConditionGroup[]) => void;
  accessorOptions: RuleAccessorOption[];
  operatorOptions: RuleOperatorOption[];
  triggerObjectType: string;
  disabled?: boolean;
}) {
  const accessorSelectorOptions = accessorOptions.map((option) => ({
    value: option.id,
    label: option.label,
    meta: option.meta,
    keywords: [option.kind],
  }));

  const operatorSelectorOptions = operatorOptions.map((option) => ({
    value: option.value,
    label: option.label,
    meta: option.unary
      ? "Uses only the left operand"
      : option.usesList
        ? "Accepts a comma-separated list"
        : "Comparison function",
    keywords: option.keywords,
  }));

  const valueTypeOptions = [
    { value: "string", label: "String", meta: "Text constant" },
    { value: "number", label: "Number", meta: "Numeric constant" },
    { value: "boolean", label: "Boolean", meta: "true or false" },
  ];

  const firstAccessorId = accessorOptions[0]?.id ?? "";

  function updateCondition(
    groupId: string,
    conditionId: string,
    updater: (condition: AdvancedRuleCondition) => AdvancedRuleCondition
  ) {
    onChange(updateConditionInGroups(groups, groupId, conditionId, updater));
  }

  function addCondition(groupId: string) {
    onChange(
      groups.map((group) =>
        group.id === groupId
          ? {
              ...group,
              conditions: [...group.conditions, createAdvancedRuleCondition(firstAccessorId)],
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

    onChange(nextGroups.length > 0 ? nextGroups : [createAdvancedRuleGroup(firstAccessorId)]);
  }

  function addGroup() {
    onChange([...groups, createAdvancedRuleGroup(firstAccessorId)]);
  }

  function removeGroup(groupId: string) {
    const nextGroups = groups.filter((group) => group.id !== groupId);
    onChange(nextGroups.length > 0 ? nextGroups : [createAdvancedRuleGroup(firstAccessorId)]);
  }

  return (
    <div className="space-y-4">
      <div className="text-[13px] text-slate-600">
        Advanced mode exposes payload and related database accessors for <span className="font-medium text-slate-900">{triggerObjectType || "the trigger object"}</span>.
      </div>

      <div className="inline-flex min-h-10 min-w-10 w-fit items-center justify-center rounded-sm bg-slate-100 px-3 py-2 text-[13px] font-semibold text-[#365fa3]">
        {triggerObjectType || "Trigger"}
      </div>
      <div className="text-[12px] text-slate-500">
        Use this mode when the right side of a comparison should be another accessor instead of a constant.
      </div>
      <div className="flex flex-col gap-4">
        <div />
      </div>

      {groups.map((group, groupIndex) => (
        <div key={group.id} className="space-y-3">
          {groupIndex > 0 ? (
            <div className="grid grid-cols-[40px_1fr] items-center gap-2">
              <div className="inline-flex h-10 items-center justify-center rounded-sm bg-slate-100 px-3 text-[12px] font-semibold uppercase text-slate-500">
                or
              </div>
              <div className="h-px bg-slate-200" />
            </div>
          ) : null}

          <div className="grid grid-cols-[40px_1fr_max-content] gap-2">
            <div />
            <div className="space-y-3">
              {group.conditions.map((condition, conditionIndex) => {
                const selectedOperator = getRuleOperatorOption(condition.operator);
                const requiresRightOperand = !isUnaryRuleOperator(condition.operator);

                return (
                  <div
                    key={condition.id}
                    className="grid grid-cols-[40px_1fr_max-content] gap-2"
                  >
                    <div className="inline-flex h-10 items-center justify-center rounded-sm bg-slate-100 px-3 text-[12px] font-semibold uppercase text-slate-500">
                      {conditionIndex === 0 ? "if" : "and"}
                    </div>
                    <div className="flex flex-col gap-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <RuleOperandSelector
                          className="min-w-[260px] max-w-[360px]"
                          disabled={disabled}
                          value={condition.leftAccessorId}
                          prefix="fx"
                          options={accessorSelectorOptions}
                          placeholder="Select a left operand"
                          searchPlaceholder="Search payload and database accessors"
                          emptyLabel="No accessors matched your search."
                          onChange={(nextValue) =>
                            updateCondition(group.id, condition.id, (current) => ({
                              ...current,
                              leftAccessorId: nextValue,
                            }))
                          }
                        />
                        <RuleOperandSelector
                          className="min-w-[150px] max-w-[190px]"
                          disabled={disabled}
                          value={condition.operator}
                          prefix={condition.operator === "gt" ? ">" : condition.operator === "lt" ? "<" : condition.operator === "gte" ? ">=" : condition.operator === "lte" ? "<=" : condition.operator === "eq" ? "=" : condition.operator === "neq" ? "!=" : "..."}
                          options={operatorSelectorOptions}
                          placeholder="Select an operator"
                          searchPlaceholder="Search operators"
                          emptyLabel="No operators matched your search."
                          onChange={(nextValue) =>
                            updateCondition(group.id, condition.id, (current) => ({
                              ...current,
                              operator: nextValue as AdvancedRuleCondition["operator"],
                            }))
                          }
                        />
                      </div>

                      {requiresRightOperand ? (
                        <div className="flex flex-wrap items-center gap-2">
                          <div className="flex flex-wrap items-center gap-2">
                            <button
                              type="button"
                              disabled={disabled}
                              onClick={() =>
                                updateCondition(group.id, condition.id, (current) => ({
                                  ...current,
                                  rightOperand:
                                    current.rightOperand.mode === "constant"
                                      ? current.rightOperand
                                      : {
                                          mode: "constant",
                                          value: "",
                                          valueType: "string",
                                        },
                                }))
                              }
                              className={cn(
                                "rounded-sm border px-3 py-2 text-[12px] font-medium transition",
                                condition.rightOperand.mode === "constant"
                                  ? "border-[#365fa3] bg-[#eef3ff] text-[#365fa3]"
                                  : "border-slate-300 bg-white text-slate-600 hover:bg-slate-50"
                              )}
                            >
                              Constant
                            </button>
                            <button
                              type="button"
                              disabled={disabled}
                              onClick={() =>
                                updateCondition(group.id, condition.id, (current) => ({
                                  ...current,
                                  rightOperand:
                                    current.rightOperand.mode === "accessor"
                                      ? current.rightOperand
                                      : {
                                          mode: "accessor",
                                          accessorId: firstAccessorId,
                                        },
                                }))
                              }
                              className={cn(
                                "rounded-sm border px-3 py-2 text-[12px] font-medium transition",
                                condition.rightOperand.mode === "accessor"
                                  ? "border-[#365fa3] bg-[#eef3ff] text-[#365fa3]"
                                  : "border-slate-300 bg-white text-slate-600 hover:bg-slate-50"
                              )}
                            >
                              Accessor
                            </button>
                          </div>

                          {condition.rightOperand.mode === "accessor" ? (
                            <RuleOperandSelector
                              className="min-w-[260px] max-w-[360px] flex-1"
                              disabled={disabled}
                              value={condition.rightOperand.accessorId}
                              prefix="fx"
                              options={accessorSelectorOptions}
                              placeholder="Select a right operand"
                              searchPlaceholder="Search payload and database accessors"
                              emptyLabel="No accessors matched your search."
                              onChange={(nextValue) =>
                                updateCondition(group.id, condition.id, (current) => ({
                                  ...current,
                                  rightOperand: {
                                    mode: "accessor",
                                    accessorId: nextValue,
                                  },
                                }))
                              }
                            />
                          ) : (
                            <div className="flex flex-1 flex-wrap items-center gap-2">
                              <RuleOperandSelector
                                className="min-w-[120px] max-w-[150px]"
                                disabled={disabled}
                                value={condition.rightOperand.valueType}
                                prefix={condition.rightOperand.valueType === "number" ? "#" : condition.rightOperand.valueType === "boolean" ? "?" : "Tt"}
                                options={valueTypeOptions}
                                placeholder="Value type"
                                searchPlaceholder="Search value types"
                                emptyLabel="No value types matched your search."
                                onChange={(nextValue) =>
                                  updateCondition(group.id, condition.id, (current) => ({
                                    ...current,
                                    rightOperand: {
                                      mode: "constant",
                                      value: current.rightOperand.mode === "constant"
                                        ? current.rightOperand.value
                                        : "",
                                      valueType: nextValue as "string" | "number" | "boolean",
                                    },
                                  }))
                                }
                              />
                              <div className="min-w-[220px] flex-1">
                                <input
                                  disabled={disabled}
                                  value={condition.rightOperand.value}
                                  onChange={(event) =>
                                    updateCondition(group.id, condition.id, (current) => ({
                                      ...current,
                                      rightOperand:
                                        current.rightOperand.mode === "constant"
                                          ? {
                                              ...current.rightOperand,
                                              value: event.target.value,
                                            }
                                          : current.rightOperand,
                                    }))
                                  }
                                  placeholder={
                                    selectedOperator?.usesList ? "review, decline" : "Enter a value"
                                  }
                                  className="h-10 w-full rounded-sm border border-slate-300 bg-white px-3 text-[14px] text-slate-950 outline-none transition placeholder:text-slate-400 focus:border-[#365fa3] disabled:cursor-not-allowed disabled:opacity-50"
                                />
                              </div>
                            </div>
                          )}
                        </div>
                      ) : (
                        <div className="inline-flex h-10 items-center rounded-sm border border-slate-200 bg-slate-50 px-3 text-[12px] font-medium text-slate-500">
                          No right operand required
                        </div>
                      )}
                    </div>
                    <div className="flex h-10 items-center justify-center">
                      <button
                        type="button"
                        disabled={disabled}
                        onClick={() => removeCondition(group.id, condition.id)}
                        className="inline-flex size-8 shrink-0 items-center justify-center rounded-sm border border-slate-300 bg-white text-slate-400 transition hover:border-red-200 hover:bg-red-50 hover:text-red-700 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        <Trash2 className="size-4" />
                      </button>
                    </div>
                  </div>
                );
              })}
              <Button
                type="button"
                variant="outline"
                disabled={disabled}
                onClick={() => addCondition(group.id)}
                className="h-10 w-fit rounded-sm border-[#2d63b8] px-4 text-[14px] text-[#1f4f96] shadow-none hover:bg-[#eef3ff]"
              >
                <Plus className="size-4" />
                Condition
              </Button>
            </div>
            <div className="flex h-10 items-center justify-center">
              {groups.length > 1 ? (
                <button
                  type="button"
                  disabled={disabled}
                  onClick={() => removeGroup(group.id)}
                  className="inline-flex size-8 items-center justify-center rounded-sm border border-slate-300 bg-white text-slate-400 transition hover:border-red-200 hover:bg-red-50 hover:text-red-700 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <Trash2 className="size-4" />
                </button>
              ) : null}
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
            "h-10 rounded-sm border-slate-200 px-4 text-[14px] shadow-none",
            "hover:border-[#2d63b8] hover:bg-[#eef3ff] hover:text-[#1f4f96]"
          )}
        >
          <Plus className="size-4" />
          Or
        </Button>
      </div>
    </div>
  );
}
