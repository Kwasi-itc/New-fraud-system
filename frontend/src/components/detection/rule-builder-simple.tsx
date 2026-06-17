"use client";

import { Plus, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  createSimpleRuleCondition,
  createSimpleRuleGroup,
  type RuleOperatorOption,
  type SimpleRuleCondition,
  type SimpleRuleConditionGroup,
} from "@/lib/rule-builder";

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

export function RuleBuilderSimple({
  groups,
  onChange,
  fieldOptions,
  operatorOptions,
  disabled = false,
}: {
  groups: SimpleRuleConditionGroup[];
  onChange: (groups: SimpleRuleConditionGroup[]) => void;
  fieldOptions: string[];
  operatorOptions: RuleOperatorOption[];
  disabled?: boolean;
}) {
  function updateCondition(
    groupId: string,
    conditionId: string,
    field: keyof Pick<SimpleRuleCondition, "left" | "operator" | "right" | "valueType">,
    value: string
  ) {
    onChange(
      updateConditionInGroups(groups, groupId, conditionId, (condition) => ({
        ...condition,
        [field]: value,
      }))
    );
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

  function removeGroup(groupId: string) {
    const nextGroups = groups.filter((group) => group.id !== groupId);
    onChange(nextGroups.length > 0 ? nextGroups : [createSimpleRuleGroup()]);
  }

  return (
    <div className="space-y-4">
      {groups.map((group, groupIndex) => (
        <div key={group.id} className="space-y-3">
          {groupIndex > 0 ? (
            <div className="flex justify-center">
              <div className="rounded-full bg-slate-100 px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.16em] text-slate-500">
                Or
              </div>
            </div>
          ) : null}

          <div className="rounded-xl border border-slate-200 p-4">
            <div className="space-y-3">
              {group.conditions.map((condition, conditionIndex) => {
                const selectedOperator = operatorOptions.find(
                  (option) => option.value === condition.operator
                );

                return (
                  <div key={condition.id} className="space-y-2.5">
                    <div className="overflow-x-auto pb-1">
                      <div className="flex min-w-[760px] items-center gap-2.5">
                        <span className="rounded-md bg-slate-50 px-3 py-2.5 text-[14px] font-medium text-slate-600">
                          {conditionIndex === 0 ? "if" : "and"}
                        </span>
                        <select
                          disabled={disabled}
                          value={condition.left}
                          onChange={(event) =>
                            updateCondition(group.id, condition.id, "left", event.target.value)
                          }
                          className="h-11 w-[250px] rounded-xl border border-slate-200 bg-white px-3 text-[14px] text-slate-950 outline-none"
                        >
                          <option value="">Select a field</option>
                          {fieldOptions.map((fieldOption) => (
                            <option key={fieldOption} value={fieldOption}>
                              {fieldOption}
                            </option>
                          ))}
                        </select>
                        <select
                          disabled={disabled}
                          value={condition.operator}
                          onChange={(event) =>
                            updateCondition(group.id, condition.id, "operator", event.target.value)
                          }
                          className="h-11 w-[170px] rounded-xl border border-slate-200 bg-white px-3 text-[14px] text-slate-950 outline-none"
                        >
                          {operatorOptions.map((operatorOption) => (
                            <option key={operatorOption.value} value={operatorOption.value}>
                              {operatorOption.label}
                            </option>
                          ))}
                        </select>
                        <select
                          disabled={disabled}
                          value={condition.valueType}
                          onChange={(event) =>
                            updateCondition(group.id, condition.id, "valueType", event.target.value)
                          }
                          className="h-11 w-[130px] rounded-xl border border-slate-200 bg-white px-3 text-[14px] text-slate-950 outline-none"
                        >
                          <option value="string">String</option>
                          <option value="number">Number</option>
                          <option value="boolean">Boolean</option>
                        </select>
                        <input
                          disabled={disabled}
                          value={condition.right}
                          onChange={(event) =>
                            updateCondition(group.id, condition.id, "right", event.target.value)
                          }
                          placeholder={
                            selectedOperator?.usesList ? "review, decline" : "Enter a value"
                          }
                          className="h-11 min-w-[180px] flex-1 rounded-xl border border-slate-200 px-3 text-[14px] text-slate-950 outline-none"
                        />
                        <button
                          type="button"
                          disabled={disabled}
                          onClick={() => removeCondition(group.id, condition.id)}
                          className="inline-flex size-8 shrink-0 items-center justify-center rounded-lg border border-slate-200 text-slate-400 disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          <Trash2 className="size-4" />
                        </button>
                      </div>
                    </div>
                    <div className="inline-flex rounded-md bg-slate-100 px-3 py-1 text-[12px] text-slate-600">
                      {[condition.left, condition.operator, condition.right].filter(Boolean).length} / 3
                      {" "}filled
                    </div>
                  </div>
                );
              })}
            </div>

            <div className="mt-4 flex items-center gap-3">
              <Button
                type="button"
                variant="outline"
                disabled={disabled}
                onClick={() => addCondition(group.id)}
                className="h-10 rounded-xl border-[#2d63b8] px-4 text-[14px] text-[#1f4f96] shadow-none"
              >
                <Plus className="size-4" />
                Condition
              </Button>
              {groups.length > 1 ? (
                <Button
                  type="button"
                  variant="outline"
                  disabled={disabled}
                  onClick={() => removeGroup(group.id)}
                  className="h-10 rounded-xl border-red-200 px-4 text-[14px] text-red-700 shadow-none"
                >
                  <Trash2 className="size-4" />
                  Group
                </Button>
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
          className="h-10 rounded-xl border-slate-200 px-4 text-[14px] shadow-none"
        >
          <Plus className="size-4" />
          Or group
        </Button>
      </div>
    </div>
  );
}
