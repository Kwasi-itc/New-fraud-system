"use client";

import type {
  OperandOption,
  OperandOptionGroup,
} from "@/components/detection/rule-operand-selector";
import type {
  RuleAccessorOption,
  SimpleRuleFunctionOperand,
} from "@/lib/rule-builder";

export function isLiteralNumberValue(value: string) {
  return value.trim().length > 0 && Number.isFinite(Number(value));
}

export function decodeLiteralSelection(value: string): {
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

export function buildLiteralSearchOptions(searchValue: string, usesList = false) {
  const normalized = searchValue.toLowerCase();
  const literalOptions: OperandOption[] = [];

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

export function buildFieldOperandSources({
  accessorOptions,
  triggerObjectType,
}: {
  accessorOptions: RuleAccessorOption[];
  triggerObjectType: string;
}) {
  const fieldSelectorOptions: OperandOption[] = accessorOptions.map((option) => ({
    value: option.id,
    label: option.label,
    meta: option.meta,
    keywords: [option.meta, option.label],
  }));

  const payloadOptions = fieldSelectorOptions.filter((option) => option.value.startsWith("payload:"));
  const databaseOptionGroups = new Map<string, OperandOption[]>();

  fieldSelectorOptions
    .filter((option) => option.value.startsWith("database:"))
    .forEach((option) => {
      const accessor = accessorOptions.find((item) => item.id === option.value);
      const path =
        accessor?.astNode.named_children?.path?.constant &&
        Array.isArray(accessor.astNode.named_children.path.constant)
          ? accessor.astNode.named_children.path.constant.filter(
              (item): item is string => typeof item === "string"
            )
          : [];
      const tableName =
        typeof accessor?.astNode.named_children?.tableName?.constant === "string"
          ? accessor.astNode.named_children.tableName.constant
          : triggerObjectType;
      const groupLabel = path.length > 0 ? `${tableName}_${path.join("_")}` : `From ${tableName}`;
      const current = databaseOptionGroups.get(groupLabel) ?? [];
      current.push(option);
      databaseOptionGroups.set(groupLabel, current);
    });

  const fieldDiscoveryGroups: OperandOptionGroup[] = [
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

  return {
    fieldSelectorOptions,
    fieldDiscoveryGroups,
  };
}

export function buildCustomListOperandSources(
  customListOptions: Array<{ id: string; name: string }>
) {
  const customListSelectorOptions: OperandOption[] = customListOptions.map((customList) => ({
    value: `custom-list:${customList.id}`,
    label: customList.name,
    meta: "Custom list",
    keywords: ["list", "custom list"],
  }));

  const customListDiscoveryGroups: OperandOptionGroup[] =
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
      : [];

  return {
    customListSelectorOptions,
    customListDiscoveryGroups,
  };
}

export function buildFunctionOperandSources(functionOperands: SimpleRuleFunctionOperand[]) {
  const functionSelectorOptions: OperandOption[] = functionOperands.map((operand) => ({
    value: `function:${operand.id}`,
    label: operand.label,
    meta: operand.meta,
    keywords: ["function", "variable", operand.label],
  }));

  const functionDiscoveryGroups: OperandOptionGroup[] =
    functionSelectorOptions.length > 0
      ? [
          {
            id: "functions",
            label: "Functions",
            children: [
              {
                id: "functions-existing",
                label: "Variables",
                options: functionSelectorOptions,
              },
            ],
          },
        ]
      : [];

  const functionLookup = new Map<string, SimpleRuleFunctionOperand>(
    functionOperands.map((operand) => [`function:${operand.id}`, operand])
  );

  return {
    functionSelectorOptions,
    functionDiscoveryGroups,
    functionLookup,
  };
}
