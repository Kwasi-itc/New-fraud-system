"use client";

import { useMemo, useState } from "react";
import { Plus } from "lucide-react";

import {
  buildCustomListOperandSources,
  buildFieldOperandSources,
  buildFunctionOperandSources,
  buildLiteralSearchOptions,
  decodeLiteralSelection,
} from "@/components/detection/rule-operand-sources";
import {
  RuleOperandSelector,
  type OperandOption,
  type OperandOptionGroup,
} from "@/components/detection/rule-operand-selector";
import {
  createExpressionLeaf,
  createExpressionOperator,
  createFunctionOperand,
  getRuleOperatorOption,
  isExpressionRuleNodeComplete,
  isUnaryRuleOperator,
  type ExpressionRuleNode,
  type ExpressionLeafMode,
  type RuleAccessorOption,
  type RuleOperatorOption,
  type SimpleRuleFunctionOperand,
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

function collectFunctionOperands(node: ExpressionRuleNode): SimpleRuleFunctionOperand[] {
  if (node.kind === "leaf") {
    return node.mode === "function" && node.functionOperand ? [node.functionOperand] : [];
  }

  return node.children.flatMap((child) => collectFunctionOperands(child));
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
  compact = false,
  accessorOptions,
  operatorOptions,
  customListOptions,
  functionOperands,
  triggerObjectType,
  operandOptionsOverride,
  operandGroupsOverride,
  disabled,
  onChange,
}: {
  node: ExpressionRuleNode;
  root?: boolean;
  compact?: boolean;
  accessorOptions: RuleAccessorOption[];
  operatorOptions: RuleOperatorOption[];
  customListOptions: Array<{ id: string; name: string }>;
  functionOperands: SimpleRuleFunctionOperand[];
  triggerObjectType: string;
  operandOptionsOverride?: OperandOption[];
  operandGroupsOverride?: OperandOptionGroup[];
  disabled?: boolean;
  onChange: (node: ExpressionRuleNode) => void;
}) {
  const { fieldSelectorOptions, fieldDiscoveryGroups } = useMemo(
    () => buildFieldOperandSources({ accessorOptions, triggerObjectType }),
    [accessorOptions, triggerObjectType]
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
  const { customListSelectorOptions, customListDiscoveryGroups } = useMemo(
    () => buildCustomListOperandSources(customListOptions),
    [customListOptions]
  );
  const { functionSelectorOptions, functionDiscoveryGroups, functionLookup } = useMemo(
    () => buildFunctionOperandSources(functionOperands),
    [functionOperands]
  );
  const operandOptions = useMemo(
    () =>
      operandOptionsOverride ?? [
        ...fieldSelectorOptions,
        ...functionSelectorOptions,
        ...customListSelectorOptions,
      ],
    [
      customListSelectorOptions,
      fieldSelectorOptions,
      functionSelectorOptions,
      operandOptionsOverride,
    ]
  );
  const operandGroups = useMemo(
    () =>
      operandGroupsOverride ?? [
        ...fieldDiscoveryGroups,
        ...customListDiscoveryGroups,
        ...functionDiscoveryGroups,
      ],
    [
      customListDiscoveryGroups,
      fieldDiscoveryGroups,
      functionDiscoveryGroups,
      operandGroupsOverride,
    ]
  );

  const updateChild = (childId: string, updater: (child: ExpressionRuleNode) => ExpressionRuleNode) => {
    onChange(mapNode(node, childId, updater));
  };

  if (node.kind === "leaf") {
    const selectorValue =
      node.mode === "accessor"
        ? node.accessorId
        : node.mode === "custom_list"
          ? `custom-list:${node.value}`
          : node.mode === "function" && node.functionOperand
            ? `function:${node.functionOperand.id}`
          : `literal:${node.valueType}:${node.value}`;
    const literalSelectedOption =
      node.mode === "constant"
        ? {
            value: selectorValue,
            label: node.valueType === "string" ? `"${node.value}"` : node.value,
            meta:
              node.valueType === "number"
                ? "Number"
                : node.valueType === "boolean"
                  ? "Boolean"
                  : "String",
          }
        : null;
    const functionSelectedOption =
      node.mode === "function" && node.functionOperand
        ? {
            value: selectorValue,
            label: node.functionOperand.label,
            meta: node.functionOperand.meta,
          }
        : null;
    const selectedMeta =
      node.mode === "function"
        ? node.functionOperand?.meta
        : node.mode === "custom_list"
          ? "Custom list"
          : node.mode === "constant"
            ? node.valueType === "number"
              ? "Number"
              : node.valueType === "boolean"
                ? "Boolean"
                : "String"
            : undefined;
    const selectedPrefix =
      node.mode === "function"
        ? "fx"
        : node.mode === "custom_list"
          ? "[]"
          : node.mode === "constant"
            ? node.valueType === "number"
              ? "#"
              : node.valueType === "boolean"
                ? "?"
                : "Tt"
            : "Tt";

    const handleLeafSelectorChange = (value: string) => {
      const literalSelection = decodeLiteralSelection(value);
      if (literalSelection) {
        onChange({
          ...node,
          mode: "constant",
          value: literalSelection.rawValue,
          valueType: literalSelection.valueType,
        });
        return;
      }

      if (value.startsWith("custom-list:")) {
        onChange({
          ...node,
          mode: "custom_list",
          value: value.replace(/^custom-list:/, ""),
          valueType: "string",
        });
        return;
      }

      if (value.startsWith("function:")) {
        const selectedFunction = functionLookup.get(value);
        if (!selectedFunction) {
          return;
        }

        onChange({
          ...node,
          mode: "function",
          functionOperand: selectedFunction,
          valueType: selectedFunction.valueType,
        });
        return;
      }

      onChange({
        ...node,
        mode: "accessor",
        accessorId: value,
      });
    };

    const selector = (
      <RuleOperandSelector
        className={compact ? "min-w-[170px] max-w-[220px] shrink-0" : "min-w-[260px] max-w-[360px]"}
        disabled={disabled}
        value={selectorValue}
        prefix={selectedPrefix}
        invalid={
          node.mode === "accessor"
            ? !node.accessorId.trim()
            : node.mode === "function"
              ? !node.functionOperand
              : !node.value.trim()
        }
        selectedMeta={selectedMeta}
        options={[
          ...operandOptions,
          ...(functionSelectedOption ? [functionSelectedOption] : []),
          ...(literalSelectedOption ? [literalSelectedOption] : []),
        ]}
        groups={operandGroups}
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
        searchPlaceholder="Select or create an operand"
        emptyLabel="No operands matched your search."
        searchOptionsBuilder={(searchValue) => buildLiteralSearchOptions(searchValue)}
        onChange={handleLeafSelectorChange}
      />
    );

    if (compact) {
      return selector;
    }

    const valueTypeOptions = [
      { value: "string", label: "String" },
      { value: "number", label: "Number" },
      { value: "boolean", label: "Boolean" },
    ];

    return (
      <div className="flex flex-wrap items-start gap-2">
        {selector}
        {node.mode === "constant" ? (
          <div className="flex flex-wrap items-center gap-2">
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
          </div>
        ) : null}
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
          compact={compact}
          accessorOptions={accessorOptions}
          operatorOptions={operatorOptions}
          customListOptions={customListOptions}
          functionOperands={functionOperands}
          triggerObjectType={triggerObjectType}
          operandOptionsOverride={operandOptionsOverride}
          operandGroupsOverride={operandGroupsOverride}
          disabled={disabled}
          onChange={(child) => updateChild(children[0]!.id, () => child)}
        />
      <RuleOperandSelector
        className={compact ? "min-w-[110px] max-w-[140px] shrink-0" : "min-w-[150px] max-w-[190px]"}
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
          compact={compact}
          accessorOptions={accessorOptions}
          operatorOptions={operatorOptions}
          customListOptions={customListOptions}
          functionOperands={functionOperands}
          triggerObjectType={triggerObjectType}
          operandOptionsOverride={operandOptionsOverride}
          operandGroupsOverride={operandGroupsOverride}
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
    <>
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
    </>
  );
}

export function RuleBuilderExpression({
  root,
  onChange,
  accessorOptions,
  operatorOptions,
  customListOptions,
  functionOperands = [],
  triggerObjectType = "trigger",
  operandOptionsOverride,
  operandGroupsOverride,
  disabled = false,
  compact = false,
}: {
  root: ExpressionRuleNode;
  onChange: (root: ExpressionRuleNode) => void;
  accessorOptions: RuleAccessorOption[];
  operatorOptions: RuleOperatorOption[];
  customListOptions: Array<{ id: string; name: string }>;
  functionOperands?: SimpleRuleFunctionOperand[];
  triggerObjectType?: string;
  operandOptionsOverride?: OperandOption[];
  operandGroupsOverride?: OperandOptionGroup[];
  disabled?: boolean;
  compact?: boolean;
}) {
  const availableFunctionOperands = useMemo(() => {
    const directFunctions = [
      createFunctionOperand({ ast: { function: "TimeNow" } }),
      createFunctionOperand({ ast: { function: "record_risk_level" } }),
    ];
    const items = new Map<string, SimpleRuleFunctionOperand>();
    [...functionOperands, ...collectFunctionOperands(root), ...directFunctions].forEach((operand) => {
      items.set(operand.id, operand);
    });
    return [...items.values()].sort((left, right) => left.label.localeCompare(right.label));
  }, [functionOperands, root]);

  if (compact) {
    return (
      <div className="inline-flex max-w-full flex-wrap items-center gap-2 align-middle">
        <ExpressionNodeEditor
          compact
          node={root}
          accessorOptions={accessorOptions}
          operatorOptions={operatorOptions}
          customListOptions={customListOptions}
          functionOperands={availableFunctionOperands}
          triggerObjectType={triggerObjectType}
          operandOptionsOverride={operandOptionsOverride}
          operandGroupsOverride={operandGroupsOverride}
          disabled={disabled}
          onChange={onChange}
        />
      </div>
    );
  }

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
          functionOperands={availableFunctionOperands}
          triggerObjectType={triggerObjectType}
          operandOptionsOverride={operandOptionsOverride}
          operandGroupsOverride={operandGroupsOverride}
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
