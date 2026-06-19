"use client";

import { useMemo, useState } from "react";
import { Plus } from "lucide-react";

import { RuleOperandSelector } from "@/components/detection/rule-operand-selector";
import {
  createExpressionLeaf,
  createExpressionOperator,
  getRuleOperatorOption,
  isExpressionRuleNodeComplete,
  isUnaryRuleOperator,
  type ExpressionRuleNode,
  type ExpressionLeafMode,
  type RuleAccessorOption,
  type RuleOperatorOption,
  type SimpleValueType,
} from "@/lib/rule-builder";
import { cn } from "@/lib/utils";

function mapNode(
  node: ExpressionRuleNode,
  targetId: string,
  updater: (node: ExpressionRuleNode) => ExpressionRuleNode
): ExpressionRuleNode {
  if (node.id === targetId) {
    return updater(node);
  }

  if (node.kind === "leaf") {
    return node;
  }

  return {
    ...node,
    children: node.children.map((child) => mapNode(child, targetId, updater)),
  };
}

function adjustOperatorChildren(node: Extract<ExpressionRuleNode, { kind: "operator" }>) {
  const arity = isUnaryRuleOperator(node.operator) ? 1 : 2;
  const nextChildren = [...node.children];

  if (arity === 1) {
    return {
      ...node,
      children: nextChildren.slice(0, 1),
    };
  }

  while (nextChildren.length < 2) {
    nextChildren.push(createExpressionLeaf());
  }

  return {
    ...node,
    children: nextChildren.slice(0, 2),
  };
}

function BracketMenu({
  label,
  onAddNesting,
  onRemoveNesting,
  onSwapOperands,
  unary = false,
}: {
  label: "(" | ")";
  onAddNesting: () => void;
  onRemoveNesting: () => void;
  onSwapOperands?: () => void;
  unary?: boolean;
}) {
  const [open, setOpen] = useState(false);

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        className="inline-flex h-10 items-center justify-center rounded-sm border border-slate-300 bg-white px-2 text-[18px] text-slate-700 transition hover:bg-slate-50"
      >
        {label}
      </button>
      {open ? (
        <div className="absolute left-0 top-full z-20 mt-1 min-w-[180px] rounded-sm border border-slate-300 bg-white p-1 shadow-[0_18px_50px_rgba(15,23,42,0.12)]">
          {!unary && onSwapOperands ? (
            <button
              type="button"
              onClick={() => {
                onSwapOperands();
                setOpen(false);
              }}
              className="block w-full rounded-sm px-3 py-2 text-left text-[13px] text-slate-800 transition hover:bg-slate-50"
            >
              Swap operands
            </button>
          ) : null}
          <button
            type="button"
            onClick={() => {
              onAddNesting();
              setOpen(false);
            }}
            className="block w-full rounded-sm px-3 py-2 text-left text-[13px] text-slate-800 transition hover:bg-slate-50"
          >
            Add right nesting
          </button>
          <button
            type="button"
            onClick={() => {
              onRemoveNesting();
              setOpen(false);
            }}
            className="block w-full rounded-sm px-3 py-2 text-left text-[13px] text-red-700 transition hover:bg-red-50"
          >
            Remove nesting
          </button>
        </div>
      ) : null}
    </div>
  );
}

function ExpressionNodeEditor({
  node,
  root = false,
  accessorOptions,
  operatorOptions,
  customListOptions,
  disabled,
  onChange,
}: {
  node: ExpressionRuleNode;
  root?: boolean;
  accessorOptions: RuleAccessorOption[];
  operatorOptions: RuleOperatorOption[];
  customListOptions: Array<{ id: string; name: string }>;
  disabled?: boolean;
  onChange: (node: ExpressionRuleNode) => void;
}) {
  const accessorSelectorOptions = useMemo(
    () =>
      accessorOptions.map((option) => ({
        value: option.id,
        label: option.label,
        meta: option.meta,
        keywords: [option.kind],
      })),
    [accessorOptions]
  );
  const accessorGroups = useMemo(
    () => [
      {
        id: "payload",
        label: "Field",
        options: accessorSelectorOptions.filter((option) => option.keywords?.includes("payload")),
      },
      {
        id: "database",
        label: "Related",
        options: accessorSelectorOptions.filter((option) => option.keywords?.includes("database")),
      },
    ],
    [accessorSelectorOptions]
  );
  const operatorSelectorOptions = useMemo(
    () =>
      operatorOptions.map((option) => ({
        value: option.value,
        label: option.label,
        keywords: option.keywords,
      })),
    [operatorOptions]
  );
  const customListSelectorOptions = useMemo(
    () =>
      customListOptions.map((item) => ({
        value: item.id,
        label: item.name,
        meta: "Custom list",
      })),
    [customListOptions]
  );

  const updateChild = (childId: string, updater: (child: ExpressionRuleNode) => ExpressionRuleNode) => {
    onChange(mapNode(node, childId, updater));
  };

  if (node.kind === "leaf") {
    const valueTypeOptions = [
      { value: "string", label: "String" },
      { value: "number", label: "Number" },
      { value: "boolean", label: "Boolean" },
    ];

    return (
      <div className="flex flex-wrap items-center gap-2">
        <div className="inline-flex rounded-sm border border-slate-300 bg-white p-1">
          {[
            { value: "accessor", label: "Field" },
            { value: "constant", label: "Value" },
            { value: "custom_list", label: "List" },
          ].map((option) => (
            <button
              key={option.value}
              type="button"
                              disabled={disabled}
                              onClick={() =>
                                onChange({
                                  ...node,
                                  mode: option.value as ExpressionLeafMode,
                                })
                              }
              className={cn(
                "rounded-sm px-3 py-2 text-[12px] font-medium transition",
                node.mode === option.value
                  ? "bg-[#eef3ff] text-[#365fa3]"
                  : "text-slate-600 hover:bg-slate-50"
              )}
            >
              {option.label}
            </button>
          ))}
        </div>

        {node.mode === "accessor" ? (
          <RuleOperandSelector
            className="min-w-[260px] max-w-[360px]"
            disabled={disabled}
            value={node.accessorId}
            prefix="fx"
            invalid={!node.accessorId.trim()}
            options={accessorSelectorOptions}
            groups={accessorGroups}
            actions={[
              {
                id: `nest-${node.id}`,
                label: "Open bracket",
                onSelect: () =>
                  onChange(
                    createExpressionOperator({
                      children: [node, createExpressionLeaf()],
                    })
                  ),
              },
            ]}
            placeholder="Select an operand..."
            searchPlaceholder="Search operands"
            emptyLabel="No operands matched your search."
            onChange={(value) => onChange({ ...node, accessorId: value })}
          />
        ) : node.mode === "custom_list" ? (
          <RuleOperandSelector
            className="min-w-[220px] max-w-[320px]"
            disabled={disabled}
            value={node.value}
            prefix="[]"
            invalid={!node.value.trim()}
            options={customListSelectorOptions}
            groups={[
              {
                id: "lists",
                label: "Lists",
                options: customListSelectorOptions,
              },
            ]}
            actions={[
              {
                id: `nest-${node.id}`,
                label: "Open bracket",
                onSelect: () =>
                  onChange(
                    createExpressionOperator({
                      children: [node, createExpressionLeaf()],
                    })
                  ),
              },
            ]}
            placeholder="Select a list..."
            searchPlaceholder="Search lists"
            emptyLabel="No lists matched your search."
            onChange={(value) => onChange({ ...node, value })}
          />
        ) : (
          <>
            <RuleOperandSelector
              className="min-w-[120px] max-w-[150px]"
              disabled={disabled}
              value={node.valueType}
              prefix={node.valueType === "number" ? "#" : node.valueType === "boolean" ? "?" : "Tt"}
              options={valueTypeOptions}
              placeholder="Value type"
              searchPlaceholder="Search value types"
              emptyLabel="No value types matched your search."
              onChange={(value) => onChange({ ...node, valueType: value as SimpleValueType })}
            />
            <div className="min-w-[220px]">
              <input
                disabled={disabled}
                value={node.value}
                onChange={(event) => onChange({ ...node, value: event.target.value })}
                placeholder="Enter a value"
                className="h-10 w-full rounded-sm border border-slate-300 bg-white px-3 text-[14px] text-slate-950 outline-none transition placeholder:text-slate-400 focus:border-[#365fa3] disabled:cursor-not-allowed disabled:opacity-50"
              />
            </div>
            <button
              type="button"
              disabled={disabled}
              onClick={() =>
                onChange(
                  createExpressionOperator({
                    children: [node, createExpressionLeaf()],
                  })
                )
              }
              className="inline-flex h-10 items-center rounded-sm border border-slate-300 bg-white px-3 text-[13px] font-medium text-slate-700 transition hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Open bracket
            </button>
          </>
        )}
      </div>
    );
  }

  const selectedOperator = getRuleOperatorOption(node.operator);
  const unary = Boolean(selectedOperator?.unary);
  const children = adjustOperatorChildren(node).children;
  const isComplete = isExpressionRuleNodeComplete(adjustOperatorChildren(node));

  const content = (
    <>
      <ExpressionNodeEditor
        node={children[0] ?? createExpressionLeaf()}
        accessorOptions={accessorOptions}
        operatorOptions={operatorOptions}
        customListOptions={customListOptions}
        disabled={disabled}
        onChange={(child) => updateChild(children[0]!.id, () => child)}
      />
      <RuleOperandSelector
        className="min-w-[150px] max-w-[190px]"
        disabled={disabled}
        value={node.operator}
        invalid={!node.operator}
        prefix={null}
        options={operatorSelectorOptions}
        placeholder="Select operator"
        searchPlaceholder="Search operators"
        emptyLabel="No operators matched your search."
        onChange={(value) =>
          onChange(
            adjustOperatorChildren({
              ...node,
              operator: value as RuleOperatorOption["value"],
            })
          )
        }
      />
      {!unary ? (
        <ExpressionNodeEditor
          node={children[1] ?? createExpressionLeaf()}
          accessorOptions={accessorOptions}
          operatorOptions={operatorOptions}
          customListOptions={customListOptions}
          disabled={disabled}
          onChange={(child) => updateChild(children[1]!.id, () => child)}
        />
      ) : null}
    </>
  );

  if (root) {
    return (
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">{content}</div>
        {!isComplete ? (
          <div className="inline-flex rounded-md bg-[#ffe7de] px-3 py-2 text-[12px] font-medium text-[#ec5a2e]">
            Complete the expression before saving.
          </div>
        ) : null}
      </div>
    );
  }

  return (
    <div className="inline-flex flex-wrap items-center gap-2">
      <BracketMenu
        label="("
        unary={unary}
        onAddNesting={() =>
          onChange(
            createExpressionOperator({
              children: [node, createExpressionLeaf()],
            })
          )
        }
        onRemoveNesting={() => onChange(children[0] ?? createExpressionLeaf())}
        onSwapOperands={
          unary
            ? undefined
            : () =>
                onChange({
                  ...node,
                  children: [children[1] ?? createExpressionLeaf(), children[0] ?? createExpressionLeaf()],
                })
        }
      />
      {content}
      <BracketMenu
        label=")"
        unary={unary}
        onAddNesting={() =>
          onChange(
            createExpressionOperator({
              children: [node, createExpressionLeaf()],
            })
          )
        }
        onRemoveNesting={() => onChange(children[0] ?? createExpressionLeaf())}
        onSwapOperands={
          unary
            ? undefined
            : () =>
                onChange({
                  ...node,
                  children: [children[1] ?? createExpressionLeaf(), children[0] ?? createExpressionLeaf()],
                })
        }
      />
    </div>
  );
}

export function RuleBuilderExpression({
  root,
  onChange,
  accessorOptions,
  operatorOptions,
  customListOptions,
  disabled = false,
}: {
  root: ExpressionRuleNode;
  onChange: (root: ExpressionRuleNode) => void;
  accessorOptions: RuleAccessorOption[];
  operatorOptions: RuleOperatorOption[];
  customListOptions: Array<{ id: string; name: string }>;
  disabled?: boolean;
}) {
  return (
    <div className="space-y-4">
      <div className="text-[13px] text-slate-600">
        Expression mode supports nested brackets and arithmetic operators like <span className="font-medium text-slate-900">+, -, *, /</span>.
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <ExpressionNodeEditor
          root
          node={root}
          accessorOptions={accessorOptions}
          operatorOptions={operatorOptions}
          customListOptions={customListOptions}
          disabled={disabled}
          onChange={onChange}
        />
        <button
          type="button"
          disabled={disabled}
          onClick={() => onChange(createExpressionOperator())}
          className="inline-flex h-10 items-center rounded-sm border border-slate-300 bg-white px-3 text-[13px] font-medium text-slate-700 transition hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Plus className="mr-2 size-4" />
          Reset expression
        </button>
      </div>
    </div>
  );
}
