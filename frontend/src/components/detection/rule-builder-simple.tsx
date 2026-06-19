"use client";

import { Plus, Trash2 } from "lucide-react";

import { RuleOperandSelector } from "@/components/detection/rule-operand-selector";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  createSimpleRuleCondition,
  createSimpleRuleGroup,
  getRuleOperatorOption,
  isUnaryRuleOperator,
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
  customListOptions,
  triggerObjectType,
  disabled = false,
}: {
  groups: SimpleRuleConditionGroup[];
  onChange: (groups: SimpleRuleConditionGroup[]) => void;
  fieldOptions: string[];
  operatorOptions: RuleOperatorOption[];
  customListOptions: Array<{ id: string; name: string }>;
  triggerObjectType: string;
  disabled?: boolean;
}) {
  const fieldSelectorOptions = fieldOptions.map((fieldOption) => ({
    value: fieldOption,
    label: fieldOption,
    keywords: [triggerObjectType],
  }));
  const fieldDiscoveryGroups = [
    {
      id: "fields",
      label: "Fields",
      children: [
        {
          id: `fields-${triggerObjectType || "trigger"}`,
          label: `From ${triggerObjectType || "trigger"}`,
          options: fieldSelectorOptions,
        },
      ],
    },
  ];

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

  function updateCondition(
    groupId: string,
    conditionId: string,
    field: keyof Pick<SimpleRuleCondition, "left" | "operator" | "right" | "valueType">,
    value: string
  ) {
    onChange(
      updateConditionInGroups(groups, groupId, conditionId, (condition) => {
        if (field === "operator") {
          const nextOperator = getRuleOperatorOption(value);
          const nextRightMode =
            nextOperator?.value === "in" || nextOperator?.value === "IsNotInList"
              ? condition.rightMode
              : "constant";

          return {
            ...condition,
            [field]: value as SimpleRuleCondition["operator"],
            rightMode: nextRightMode,
          };
        }

        return {
          ...condition,
          [field]: value,
        };
      })
    );
  }

  function updateRightOperand(groupId: string, conditionId: string, value: string) {
    onChange(
      updateConditionInGroups(groups, groupId, conditionId, (condition) => ({
        ...condition,
        right: value.startsWith("custom-list:") ? value.replace(/^custom-list:/, "") : value,
        rightMode: value.startsWith("custom-list:") ? "custom_list" : "constant",
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
    return Boolean(
      condition.left.trim() &&
        condition.operator &&
        (isUnaryRuleOperator(condition.operator) || condition.right.trim())
    );
  }

  function missingCount(condition: SimpleRuleCondition) {
    const requiredValues = isUnaryRuleOperator(condition.operator)
      ? [condition.left.trim(), condition.operator]
      : [condition.left.trim(), condition.operator, condition.right.trim()];

    return requiredValues.filter(Boolean).length;
  }

  return (
    <div className="space-y-4">
      {groups.map((group, groupIndex) => (
        <div key={group.id} className="space-y-3">
          <div className="rounded-xl border border-slate-200 bg-white p-6">
            <div className="grid grid-cols-[max-content_minmax(0,1fr)_max-content] items-start gap-3">
              <div className="pt-1 text-[18px] text-slate-700">
                {(group.openBefore ?? 0) > 0 ? "(".repeat(group.openBefore ?? 0) : ""}
              </div>
              <div className="space-y-4">
              {group.conditions.map((condition, conditionIndex) => {
                const complete = isConditionComplete(condition);
                const selectedOperator = getRuleOperatorOption(condition.operator);
                const requiresRightOperand = !selectedOperator?.unary;
                const totalRequired = requiresRightOperand ? 3 : 2;
                const required = totalRequired - missingCount(condition);
                const leftGroups = [
                  ...fieldDiscoveryGroups,
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

                return (
                  <div
                    key={condition.id}
                    className="grid grid-cols-[48px_minmax(0,1fr)_40px] gap-3"
                  >
                    <div className="inline-flex h-12 items-center justify-center rounded-sm bg-slate-50 px-3 text-[16px] font-medium text-slate-600">
                      {groupIndex > 0 && conditionIndex === 0
                        ? "or"
                        : conditionIndex === 0
                          ? "if"
                          : "and"}
                    </div>
                    <div className="space-y-2">
                      <div className="flex flex-wrap items-start gap-3">
                      <RuleOperandSelector
                        className="min-w-[220px] max-w-[280px]"
                        disabled={disabled}
                        value={condition.left}
                        invalid={!condition.left.trim()}
                        prefix={condition.valueType === "number" ? "#" : "Tt"}
                        options={fieldSelectorOptions}
                        groups={leftGroups}
                        placeholder="Select an operand..."
                        searchPlaceholder="Select or create an operand"
                        emptyLabel="No fields matched your search."
                        onChange={(nextValue) =>
                          updateCondition(group.id, condition.id, "left", nextValue)
                        }
                      />
                      <RuleOperandSelector
                        className="min-w-[150px] max-w-[190px]"
                        disabled={disabled}
                        value={condition.operator}
                        invalid={!condition.operator}
                        prefix={null}
                        options={operatorSelectorOptions}
                        placeholder="..."
                        searchPlaceholder="Search operators"
                        emptyLabel="No operators matched your search."
                        onChange={(nextValue) =>
                          updateCondition(
                            group.id,
                            condition.id,
                            "operator",
                            nextValue as SimpleRuleCondition["operator"]
                          )
                        }
                      />
                      {requiresRightOperand ? (
                      <RuleOperandSelector
                        className="min-w-[220px] max-w-[280px]"
                        disabled={disabled}
                        value={
                          condition.rightMode === "custom_list"
                              ? `custom-list:${condition.right}`
                              : condition.right
                          }
                          invalid={!condition.right.trim()}
                          prefix={condition.valueType === "number" ? "#" : condition.valueType === "boolean" ? "?" : "Tt"}
                        options={[
                          ...fieldSelectorOptions.map((option) => ({
                            ...option,
                            value: option.label,
                          })),
                            ...((!selectedOperator || selectedOperator.usesList)
                              ? customListSelectorOptions
                              : []),
                        ]}
                        groups={[
                            {
                              id: "fields",
                              label: "Fields",
                              children: [
                                {
                                  id: `fields-${triggerObjectType || "trigger"}-right`,
                                  label: `From ${triggerObjectType || "trigger"}`,
                                  options: fieldSelectorOptions.map((option) => ({
                                    ...option,
                                    value: option.label,
                                  })),
                                },
                              ],
                            },
                            ...((!selectedOperator || selectedOperator.usesList) &&
                            customListSelectorOptions.length > 0
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
                          ]}
                          placeholder="Select an operand..."
                          searchPlaceholder="Select or create an operand"
                          emptyLabel="No operands matched your search."
                          searchOptionsBuilder={(searchValue) => {
                            if (condition.valueType === "number") {
                              return Number.isFinite(Number(searchValue))
                                ? [
                                    {
                                      value: searchValue,
                                      label: searchValue,
                                      meta: selectedOperator?.usesList ? "Number list" : "Number",
                                    },
                                  ]
                                : [];
                            }

                            if (condition.valueType === "boolean") {
                              const normalized = searchValue.toLowerCase();
                              return ["true", "false"]
                                .filter((candidate) => candidate.includes(normalized))
                                .map((candidate) => ({
                                  value: candidate,
                                  label: candidate,
                                  meta: selectedOperator?.usesList ? "Boolean list" : "Boolean",
                                }));
                            }

                            return [
                              {
                                value: searchValue,
                                label: searchValue,
                                meta: selectedOperator?.usesList ? "String list" : "String",
                              },
                            ];
                          }}
                          onChange={(nextValue) =>
                            updateRightOperand(group.id, condition.id, nextValue)
                          }
                        />
                      ) : null}
                      </div>
                      {!complete ? (
                        <div className="inline-flex rounded-md bg-[#ffe7de] px-3 py-2 text-[12px] font-medium text-[#ec5a2e]">
                          {required} required
                        </div>
                      ) : null}
                    </div>
                    <div className="flex h-12 items-center justify-center">
                      <button
                        type="button"
                        disabled={disabled}
                        onClick={() => removeCondition(group.id, condition.id)}
                        className="inline-flex size-7 shrink-0 items-center justify-center rounded-sm border border-slate-200 bg-white text-slate-400 transition hover:border-red-200 hover:bg-red-50 hover:text-red-700 disabled:cursor-not-allowed disabled:opacity-50"
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
  );
}
