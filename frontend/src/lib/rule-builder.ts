export type SimpleValueType = "string" | "number" | "boolean";

export type SupportedRuleOperator =
  | "eq"
  | "neq"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "contains"
  | "starts_with"
  | "ends_with"
  | "in";

export type RuleOperatorOption = {
  label: string;
  value: SupportedRuleOperator;
  usesList?: boolean;
};

export const simpleRuleOperatorOptions: RuleOperatorOption[] = [
  { label: "Equals", value: "eq" },
  { label: "Not equal", value: "neq" },
  { label: "Greater than", value: "gt" },
  { label: "Greater or equal", value: "gte" },
  { label: "Less than", value: "lt" },
  { label: "Less or equal", value: "lte" },
  { label: "Contains", value: "contains" },
  { label: "Starts with", value: "starts_with" },
  { label: "Ends with", value: "ends_with" },
  { label: "In list", value: "in", usesList: true },
];

export type SimpleRuleCondition = {
  id: string;
  left: string;
  operator: SupportedRuleOperator;
  right: string;
  valueType: SimpleValueType;
};

export type SimpleRuleConditionGroup = {
  id: string;
  conditions: SimpleRuleCondition[];
};

type ASTNodeLike = {
  function?: string;
  name?: string;
  constant?: unknown;
  children?: ASTNodeLike[];
  named_children?: Record<string, ASTNodeLike>;
};

function getNodeFunction(node: ASTNodeLike | null | undefined) {
  return node?.function ?? node?.name;
}

export function createSimpleRuleCondition(
  overrides: Partial<SimpleRuleCondition> = {}
): SimpleRuleCondition {
  return {
    id: globalThis.crypto?.randomUUID?.() ?? `condition-${Date.now()}-${Math.random()}`,
    left: "",
    operator: "eq",
    right: "",
    valueType: "string",
    ...overrides,
  };
}

export function createSimpleRuleGroup(
  overrides: Partial<SimpleRuleConditionGroup> = {}
): SimpleRuleConditionGroup {
  return {
    id: globalThis.crypto?.randomUUID?.() ?? `group-${Date.now()}-${Math.random()}`,
    conditions: [createSimpleRuleCondition()],
    ...overrides,
  };
}

export function slugifyStableRuleId(value: string) {
  const slug = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");

  return slug || "new-rule";
}

export function buildFieldRef(fieldName: string) {
  return {
    function: "field_ref",
    named_children: {
      field: { constant: fieldName },
    },
  };
}

function normalizeConstantValue(
  rawValue: string,
  valueType: SimpleValueType,
  usesList: boolean
) {
  if (usesList) {
    return rawValue
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean)
      .map((item) => normalizeConstantValue(item, valueType, false));
  }

  if (valueType === "number") {
    return Number(rawValue);
  }

  if (valueType === "boolean") {
    return rawValue === "true";
  }

  return rawValue;
}

export function compileConditionToAst(condition: SimpleRuleCondition) {
  const operator = simpleRuleOperatorOptions.find(
    (option) => option.value === condition.operator
  );

  return {
    function: condition.operator,
    children: [
      buildFieldRef(condition.left),
      {
        constant: normalizeConstantValue(
          condition.right,
          condition.valueType,
          Boolean(operator?.usesList)
        ),
      },
    ],
  };
}

export function compileConditionGroupsToAst(groups: SimpleRuleConditionGroup[]) {
  const compiledGroups = groups
    .map((group) => {
      const compiledConditions = group.conditions
        .filter((condition) => condition.left.trim() && condition.right.trim())
        .map((condition) => compileConditionToAst(condition));

      if (compiledConditions.length === 0) {
        return null;
      }

      if (compiledConditions.length === 1) {
        return compiledConditions[0];
      }

      return {
        function: "and",
        children: compiledConditions,
      };
    })
    .filter((group): group is NonNullable<typeof group> => Boolean(group));

  if (compiledGroups.length === 0) {
    return { constant: true };
  }

  if (compiledGroups.length === 1) {
    return compiledGroups[0];
  }

  return {
    function: "or",
    children: compiledGroups,
  };
}

function parsePrimitiveValue(constant: unknown): {
  value: string;
  valueType: SimpleValueType;
} | null {
  if (typeof constant === "string") {
    return { value: constant, valueType: "string" };
  }

  if (typeof constant === "number") {
    return { value: String(constant), valueType: "number" };
  }

  if (typeof constant === "boolean") {
    return { value: String(constant), valueType: "boolean" };
  }

  return null;
}

function parseConditionNode(node: ASTNodeLike): SimpleRuleCondition | null {
  const nodeFunction = getNodeFunction(node);
  const operator = simpleRuleOperatorOptions.find(
    (option) => option.value === nodeFunction
  );

  if (!operator) {
    return null;
  }

  const children = node.children ?? [];
  if (children.length !== 2) {
    return null;
  }

  const fieldRef = children[0];
  const fieldName = fieldRef?.named_children?.field?.constant;
  if (getNodeFunction(fieldRef) !== "field_ref" || typeof fieldName !== "string") {
    return null;
  }

  const constant = children[1]?.constant;
  if (operator.usesList) {
    if (!Array.isArray(constant)) {
      return null;
    }

    const first = constant[0];
    const valueType =
      typeof first === "number"
        ? "number"
        : typeof first === "boolean"
          ? "boolean"
          : "string";

    return createSimpleRuleCondition({
      left: fieldName,
      operator: operator.value,
      right: constant.map((item) => String(item)).join(", "),
      valueType,
    });
  }

  const parsedValue = parsePrimitiveValue(constant);
  if (!parsedValue) {
    return null;
  }

  return createSimpleRuleCondition({
    left: fieldName,
    operator: operator.value,
    right: parsedValue.value,
    valueType: parsedValue.valueType,
  });
}

function parseGroupNode(node: ASTNodeLike): SimpleRuleConditionGroup | null {
  const nodeFunction = getNodeFunction(node);

  if (nodeFunction === "and") {
    const parsedConditions = (node.children ?? [])
      .map((child) => parseConditionNode(child))
      .filter((condition): condition is SimpleRuleCondition => Boolean(condition));

    if (parsedConditions.length !== (node.children ?? []).length || parsedConditions.length === 0) {
      return null;
    }

    return createSimpleRuleGroup({
      conditions: parsedConditions,
    });
  }

  const singleCondition = parseConditionNode(node);
  if (!singleCondition) {
    return null;
  }

  return createSimpleRuleGroup({
    conditions: [singleCondition],
  });
}

export function tryParseAstToConditionGroups(ast: unknown) {
  if (!ast || typeof ast !== "object" || Array.isArray(ast)) {
    return null;
  }

  const node = ast as ASTNodeLike;
  const nodeFunction = getNodeFunction(node);

  if (nodeFunction === "or") {
    const groups = (node.children ?? [])
      .map((child) => parseGroupNode(child))
      .filter((group): group is SimpleRuleConditionGroup => Boolean(group));

    if (groups.length !== (node.children ?? []).length || groups.length === 0) {
      return null;
    }

    return groups;
  }

  const singleGroup = parseGroupNode(node);
  if (!singleGroup) {
    return null;
  }

  return [singleGroup];
}
