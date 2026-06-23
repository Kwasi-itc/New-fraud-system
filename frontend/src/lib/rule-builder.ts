import type { ASTNodeDTO, JSONValue } from "@/lib/decision-engine-api";

export type SimpleValueType = "string" | "number" | "boolean";

export type AggregatorOperator =
  | "AVG"
  | "COUNT"
  | "COUNT_DISTINCT"
  | "MAX"
  | "MIN"
  | "SUM"
  | "STDDEV"
  | "PCTILE"
  | "MEDIAN";

export type SimpleRuleFunctionOperand = {
  id: string;
  label: string;
  meta: string;
  valueType: SimpleValueType;
  ast: RuleAstNode;
};

export type SupportedRuleOperator =
  | "eq"
  | "neq"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "add"
  | "subtract"
  | "multiply"
  | "divide"
  | "contains"
  | "starts_with"
  | "ends_with"
  | "in"
  | "IsNotInList"
  | "StringNotContain"
  | "ContainsAnyOf"
  | "ContainsNoneOf"
  | "IsEmpty"
  | "IsNotEmpty";

export type RuleOperatorOption = {
  label: string;
  value: SupportedRuleOperator;
  usesList?: boolean;
  unary?: boolean;
  keywords?: string[];
  aliases?: string[];
};

export const simpleRuleOperatorOptions: RuleOperatorOption[] = [
  { label: "=", value: "eq", keywords: ["="], aliases: ["="] },
  { label: "!=", value: "neq", keywords: ["!=", "≠"], aliases: ["!=", "≠"] },
  { label: "<", value: "lt", keywords: ["<"], aliases: ["<"] },
  { label: "<=", value: "lte", keywords: ["<=", "≤"], aliases: ["<=", "≤"] },
  { label: ">", value: "gt", keywords: [">"], aliases: [">"] },
  { label: ">=", value: "gte", keywords: [">=", "≥"], aliases: [">=", "≥"] },
  { label: "+", value: "add", keywords: ["add", "+"], aliases: ["+"] },
  { label: "-", value: "subtract", keywords: ["subtract", "-"], aliases: ["-"] },
  { label: "*", value: "multiply", keywords: ["multiply", "*"], aliases: ["*"] },
  { label: "/", value: "divide", keywords: ["divide", "/"], aliases: ["/"] },
  { label: "Is in list", value: "in", usesList: true, keywords: ["IsInList", "in list"], aliases: ["IsInList"] },
  { label: "Is not in list", value: "IsNotInList", usesList: true },
  { label: "Contains", value: "contains", keywords: ["StringContains"], aliases: ["StringContains"] },
  { label: "Does not contain", value: "StringNotContain" },
  { label: "Starts with", value: "starts_with", keywords: ["StringStartsWith"], aliases: ["StringStartsWith"] },
  { label: "Ends with", value: "ends_with", keywords: ["StringEndsWith"], aliases: ["StringEndsWith"] },
  { label: "Contains any of", value: "ContainsAnyOf", usesList: true },
  { label: "Contains none of", value: "ContainsNoneOf", usesList: true },
  { label: "Is empty", value: "IsEmpty", unary: true },
  { label: "Is not empty", value: "IsNotEmpty", unary: true },
];

export function getRuleOperatorOption(operator: string | null | undefined) {
  if (!operator) {
    return null;
  }

  return (
    simpleRuleOperatorOptions.find(
      (option) =>
        option.value === operator ||
        option.aliases?.some((alias) => alias.toLowerCase() === operator.toLowerCase())
    ) ?? null
  );
}

export function isUnaryRuleOperator(
  operator: SupportedRuleOperator | string | null | undefined
) {
  return Boolean(getRuleOperatorOption(operator)?.unary);
}

function parseOperatorNodeName(
  operatorName: string | undefined
): SupportedRuleOperator | null {
  return getRuleOperatorOption(operatorName)?.value ?? null;
}

export type SimpleRuleCondition = {
  id: string;
  left: string;
  leftMode?: "field" | "function" | "custom_list" | "constant";
  leftFunction?: SimpleRuleFunctionOperand | null;
  operator: SupportedRuleOperator | "";
  right: string;
  rightMode?: "constant" | "custom_list" | "function" | "field";
  rightFunction?: SimpleRuleFunctionOperand | null;
  valueType: SimpleValueType;
};

export type SimpleRuleConditionGroup = {
  id: string;
  conditions: SimpleRuleCondition[];
  openBefore?: number;
  closeAfter?: number;
};

export type RuleAccessorKind = "payload" | "database";

export type RuleAccessorOption = {
  id: string;
  kind: RuleAccessorKind;
  label: string;
  meta: string;
  astNode: RuleAstNode;
};

export type AdvancedRuleOperand =
  | {
      mode: "constant";
      value: string;
      valueType: SimpleValueType;
    }
  | {
      mode: "accessor";
      accessorId: string;
    };

export type AdvancedRuleCondition = {
  id: string;
  leftAccessorId: string;
  operator: SupportedRuleOperator | "";
  rightOperand: AdvancedRuleOperand;
};

export type AdvancedRuleConditionGroup = {
  id: string;
  conditions: AdvancedRuleCondition[];
};

export type ExpressionLeafMode = "accessor" | "constant" | "custom_list";

export type ExpressionRuleNode =
  | {
      id: string;
      kind: "operator";
      operator: SupportedRuleOperator | "";
      children: ExpressionRuleNode[];
    }
  | {
      id: string;
      kind: "leaf";
      mode: ExpressionLeafMode;
      accessorId: string;
      value: string;
      valueType: SimpleValueType;
    };

export type RuleAstNode = {
  function?: string;
  name?: string;
  constant?: JSONValue;
  children?: RuleAstNode[];
  named_children?: Record<string, RuleAstNode>;
};

type RuleConstantScalar = string | number | boolean;
type RuleConstantValue = RuleConstantScalar | RuleConstantScalar[];

type ASTNodeLike = RuleAstNode;

function getNodeFunction(node: ASTNodeLike | null | undefined) {
	return node?.function ?? node?.name;
}

function isEmptyRuleFormulaNode(node: ASTNodeLike | null | undefined) {
	return (
		Boolean(node) &&
		getNodeFunction(node) === undefined &&
		node?.constant === true &&
		(node.children?.length ?? 0) === 0 &&
		Object.keys(node.named_children ?? {}).length === 0
	);
}

function randomId(prefix: string) {
  return globalThis.crypto?.randomUUID?.() ?? `${prefix}-${Date.now()}-${Math.random()}`;
}

export function createSimpleRuleCondition(
  overrides: Partial<SimpleRuleCondition> = {}
): SimpleRuleCondition {
  const { leftFunction: _leftFunction, rightFunction: _rightFunction, ...rest } = overrides;
  const normalizedLeftFunction = normalizeFunctionOperand(overrides.leftFunction);
  const normalizedRightFunction = normalizeFunctionOperand(overrides.rightFunction);

  return {
    id: randomId("condition"),
    left: "",
    leftMode: "field",
    leftFunction: normalizedLeftFunction,
    operator: "",
    right: "",
    rightMode: "constant",
    rightFunction: normalizedRightFunction,
    valueType: "string",
    ...rest,
  };
}

export function createSimpleRuleGroup(
  overrides: Partial<SimpleRuleConditionGroup> = {}
): SimpleRuleConditionGroup {
  return {
    id: randomId("group"),
    conditions: [createSimpleRuleCondition()],
    openBefore: 0,
    closeAfter: 0,
    ...overrides,
  };
}

export function createAdvancedRuleCondition(
  accessorId = "",
  overrides: Partial<AdvancedRuleCondition> = {}
): AdvancedRuleCondition {
  return {
    id: randomId("advanced-condition"),
    leftAccessorId: accessorId,
    operator: "",
    rightOperand: {
      mode: "constant",
      value: "",
      valueType: "string",
    },
    ...overrides,
  };
}

export function createAdvancedRuleGroup(
  accessorId = "",
  overrides: Partial<AdvancedRuleConditionGroup> = {}
): AdvancedRuleConditionGroup {
  return {
    id: randomId("advanced-group"),
    conditions: [createAdvancedRuleCondition(accessorId)],
    ...overrides,
  };
}

export function createExpressionLeaf(
  overrides: Partial<Extract<ExpressionRuleNode, { kind: "leaf" }>> = {}
): Extract<ExpressionRuleNode, { kind: "leaf" }> {
  return {
    id: randomId("expression-leaf"),
    kind: "leaf",
    mode: "accessor",
    accessorId: "",
    value: "",
    valueType: "string",
    ...overrides,
  };
}

export function createExpressionOperator(
  overrides: Partial<Extract<ExpressionRuleNode, { kind: "operator" }>> = {}
): Extract<ExpressionRuleNode, { kind: "operator" }> {
  return {
    id: randomId("expression-operator"),
    kind: "operator",
    operator: "",
    children: [createExpressionLeaf(), createExpressionLeaf()],
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

export function buildCustomListAccess(listName: string): RuleAstNode {
  return {
    function: "CustomListAccess",
    named_children: {
      customListId: { constant: listName },
    },
  };
}

export function buildAggregatorAst(params: {
  aggregator: AggregatorOperator;
  tableName: string;
  fieldName: string;
  label: string;
  percentile?: number;
}): RuleAstNode {
  return {
    function: "Aggregator",
    named_children: {
      tableName: { constant: params.tableName },
      fieldName: { constant: params.fieldName },
      aggregator: { constant: params.aggregator },
      label: { constant: params.label },
      ...(params.percentile !== undefined
        ? {
            percentile: {
              constant: params.percentile,
            },
          }
        : {}),
    },
  };
}

function getAggregatorDisplayLabel(aggregator: string) {
  switch (aggregator) {
    case "AVG":
      return "Average";
    case "COUNT":
      return "Count";
    case "COUNT_DISTINCT":
      return "Count distinct";
    case "MAX":
      return "Max";
    case "MIN":
      return "Min";
    case "SUM":
      return "Sum";
    case "STDDEV":
      return "Standard deviation";
    case "PCTILE":
      return "Percentile";
    case "MEDIAN":
      return "Median";
    default:
      return aggregator;
  }
}

function buildDefaultFunctionLabel(ast: RuleAstNode) {
  const functionName = getNodeFunction(ast);
  if (functionName === "Aggregator") {
    const aggregator =
      typeof ast.named_children?.aggregator?.constant === "string"
        ? ast.named_children.aggregator.constant
        : "COUNT";
    const fieldName =
      typeof ast.named_children?.fieldName?.constant === "string"
        ? ast.named_children.fieldName.constant
        : "field";

    return `${getAggregatorDisplayLabel(aggregator)} ${fieldName}`;
  }

  if (functionName === "TimeNow") {
    return "Current time";
  }

  if (functionName === "record_risk_level") {
    return "Record risk level";
  }

  return functionName ?? "Function";
}

function buildFunctionMeta(ast: RuleAstNode) {
  const functionName = getNodeFunction(ast);
  if (functionName === "Aggregator") {
    const tableName =
      typeof ast.named_children?.tableName?.constant === "string"
        ? ast.named_children.tableName.constant
        : "records";
    return `Aggregation on ${tableName}`;
  }

  if (functionName === "TimeNow") {
    return "Function";
  }

  if (functionName === "record_risk_level") {
    return "Platform function";
  }

  return "Function";
}

export function createFunctionOperand(params: {
  ast: RuleAstNode;
  label?: string;
  meta?: string;
}): SimpleRuleFunctionOperand {
  const normalized = normalizeAstNode(params.ast);

  return {
    id: randomId("function"),
    label: params.label?.trim() || buildDefaultFunctionLabel(normalized),
    meta: params.meta?.trim() || buildFunctionMeta(normalized),
    valueType: getFunctionValueType(normalized),
    ast: normalized,
  };
}

function normalizeFunctionOperand(
  operand: SimpleRuleFunctionOperand | null | undefined
): SimpleRuleFunctionOperand | null {
  if (!operand) {
    return null;
  }

  return {
    ...operand,
    ast: normalizeAstNode(operand.ast),
  };
}

function getFunctionValueType(ast: RuleAstNode): SimpleValueType {
  const functionName = getNodeFunction(ast);
  if (functionName === "Aggregator") {
    return "number";
  }

  if (functionName === "record_risk_level") {
    return "string";
  }

  return "string";
}

function buildAccessorLookupId(node: ASTNodeLike, kind: RuleAccessorKind) {
  const nodeName = getNodeFunction(node);

  if (kind === "payload") {
    const fieldName =
      typeof node.children?.[0]?.constant === "string"
        ? node.children[0].constant
        : typeof node.named_children?.field?.constant === "string"
          ? node.named_children.field.constant
          : "";
    return `${kind}:${nodeName ?? "payload"}:${fieldName}`;
  }

  const fieldName =
    typeof node.named_children?.fieldName?.constant === "string"
      ? node.named_children.fieldName.constant
      : "";
  const path =
    Array.isArray(node.named_children?.path?.constant)
      ? node.named_children?.path?.constant.join(".")
      : "";
  const tableName =
    typeof node.named_children?.tableName?.constant === "string"
      ? node.named_children.tableName.constant
      : "";

  return `${kind}:${nodeName ?? "database"}:${tableName}:${path}:${fieldName}`;
}

function normalizeAstNode(node: ASTNodeLike): RuleAstNode {
  return {
    ...(getNodeFunction(node) ? { function: getNodeFunction(node) } : {}),
    ...(node.constant !== undefined ? { constant: node.constant } : {}),
    ...(node.children?.length
      ? { children: node.children.map((child) => normalizeAstNode(child)) }
      : {}),
    ...(node.named_children
      ? {
          named_children: Object.fromEntries(
            Object.entries(node.named_children).map(([key, child]) => [key, normalizeAstNode(child)])
          ),
        }
      : {}),
  };
}

function formatPayloadAccessorLabel(node: ASTNodeLike) {
  const fieldName =
    typeof node.children?.[0]?.constant === "string"
      ? node.children[0].constant
      : typeof node.named_children?.field?.constant === "string"
        ? node.named_children.field.constant
        : "Unknown payload field";

  return {
    label: fieldName,
    meta: "Payload field",
  };
}

function formatDatabaseAccessorLabel(node: ASTNodeLike) {
  const fieldName =
    typeof node.named_children?.fieldName?.constant === "string"
      ? node.named_children.fieldName.constant
      : "field";
  const tableName =
    typeof node.named_children?.tableName?.constant === "string"
      ? node.named_children.tableName.constant
      : "record";
  const pathItems = Array.isArray(node.named_children?.path?.constant)
    ? node.named_children.path.constant.filter(
        (item): item is string => typeof item === "string"
      )
    : [];

  return {
    label: pathItems.length > 0 ? `${pathItems.join(" -> ")} -> ${fieldName}` : fieldName,
    meta: pathItems.length > 0 ? `Related field from ${tableName}` : `Field on ${tableName}`,
  };
}

export function extractAccessorOptions(
  payloadAccessors: ASTNodeDTO[],
  databaseAccessors: ASTNodeDTO[]
): RuleAccessorOption[] {
  const payloadOptions = payloadAccessors.map((node) => {
    const normalized = normalizeAstNode(node);
    const formatted = formatPayloadAccessorLabel(normalized);

    return {
      id: buildAccessorLookupId(normalized, "payload"),
      kind: "payload" as const,
      label: formatted.label,
      meta: formatted.meta,
      astNode: normalized,
    };
  });

  const databaseOptions = databaseAccessors.map((node) => {
    const normalized = normalizeAstNode(node);
    const formatted = formatDatabaseAccessorLabel(normalized);

    return {
      id: buildAccessorLookupId(normalized, "database"),
      kind: "database" as const,
      label: formatted.label,
      meta: formatted.meta,
      astNode: normalized,
    };
  });

  return [...payloadOptions, ...databaseOptions].sort((left, right) =>
    left.label.localeCompare(right.label)
  );
}

export function extractPayloadFieldNames(nodes: ASTNodeDTO[]) {
  return nodes
    .map((node) => {
      const normalized = normalizeAstNode(node);
      const formatted = formatPayloadAccessorLabel(normalized);
      return formatted.label;
    })
    .filter((value): value is string => Boolean(value))
    .sort((left, right) => left.localeCompare(right));
}

function normalizeConstantValue(
  rawValue: string,
  valueType: SimpleValueType,
  usesList: boolean
): RuleConstantValue {
  const normalizeScalarValue = (value: string): RuleConstantScalar => {
    if (valueType === "number") {
      return Number(value);
    }

    if (valueType === "boolean") {
      return value === "true";
    }

    return value;
  };

  if (usesList) {
    return rawValue
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean)
      .map((item) => normalizeScalarValue(item));
  }

  return normalizeScalarValue(rawValue);
}

function buildListNode(rawValue: string, valueType: SimpleValueType): RuleAstNode {
  const items = rawValue
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean)
    .map((item) => ({
      constant: normalizeConstantValue(item, valueType, false),
    }));

  return {
    function: "List",
    children: items,
  };
}

function parseListNode(constantNode: ASTNodeLike | null | undefined) {
  if (!constantNode) {
    return null;
  }

  const nodeFunction = getNodeFunction(constantNode);
  if (nodeFunction === "List" && Array.isArray(constantNode.children)) {
    const values = constantNode.children.map((child) => child.constant);
    const first = values[0];
    const valueType =
      typeof first === "number"
        ? "number"
        : typeof first === "boolean"
          ? "boolean"
          : "string";

    return {
      value: values.map((item) => String(item)).join(", "),
      valueType,
    } satisfies {
      value: string;
      valueType: SimpleValueType;
    };
  }

  if (Array.isArray(constantNode.constant)) {
    const first = constantNode.constant[0];
    const valueType =
      typeof first === "number"
        ? "number"
        : typeof first === "boolean"
          ? "boolean"
          : "string";

    return {
      value: constantNode.constant.map((item) => String(item)).join(", "),
      valueType,
    } satisfies {
      value: string;
      valueType: SimpleValueType;
    };
  }

  return null;
}

function buildSimpleFieldOperandAst(
  operandIdOrField: string,
  accessorLookup: Map<string, RuleAccessorOption>
) {
  const accessor = accessorLookup.get(operandIdOrField);
  if (accessor) {
    return normalizeAstNode(accessor.astNode);
  }

  return buildFieldRef(operandIdOrField);
}

export function compileConditionToAst(
  condition: SimpleRuleCondition,
  accessorLookup: Map<string, RuleAccessorOption> = new Map()
): RuleAstNode {
  if (!condition.operator) {
    return { constant: true };
  }

  const operator = getRuleOperatorOption(condition.operator);
  if (!operator) {
    return { constant: true };
  }

  if (operator.unary) {
    return {
      function: condition.operator,
      children: [
        condition.leftMode === "function" && condition.leftFunction
          ? normalizeAstNode(condition.leftFunction.ast)
          : condition.leftMode === "custom_list"
            ? buildCustomListAccess(condition.left)
            : condition.leftMode === "constant"
              ? {
                  constant: normalizeConstantValue(
                    condition.left,
                    condition.valueType,
                    false
                  ),
                }
          : buildSimpleFieldOperandAst(condition.left, accessorLookup),
      ],
    };
  }

  const rightOperand = operator.value === "ContainsAnyOf" || operator.value === "ContainsNoneOf"
    ? buildListNode(condition.right, condition.valueType)
    : condition.rightMode === "field"
      ? buildSimpleFieldOperandAst(condition.right, accessorLookup)
    : condition.rightMode === "function" && condition.rightFunction
      ? normalizeAstNode(condition.rightFunction.ast)
    : condition.rightMode === "custom_list" &&
        (operator.value === "in" || operator.value === "IsNotInList")
      ? buildCustomListAccess(condition.right)
    : {
        constant: normalizeConstantValue(
          condition.right,
          condition.valueType,
          Boolean(operator.usesList)
        ),
      };

  return {
    function: condition.operator,
    children: [
      condition.leftMode === "function" && condition.leftFunction
        ? normalizeAstNode(condition.leftFunction.ast)
        : condition.leftMode === "custom_list"
          ? buildCustomListAccess(condition.left)
          : condition.leftMode === "constant"
            ? {
                constant: normalizeConstantValue(
                  condition.left,
                  condition.valueType,
                  false
                ),
              }
        : buildSimpleFieldOperandAst(condition.left, accessorLookup),
      rightOperand,
    ],
  };
}

function buildOperandAst(
  operand: AdvancedRuleOperand,
  accessorLookup: Map<string, RuleAccessorOption>,
  operator: RuleOperatorOption | null
): RuleAstNode {
  if (operand.mode === "accessor") {
    return normalizeAstNode(accessorLookup.get(operand.accessorId)?.astNode ?? {});
  }

  if (operator?.value === "ContainsAnyOf" || operator?.value === "ContainsNoneOf") {
    return buildListNode(operand.value, operand.valueType);
  }

  return {
    constant: normalizeConstantValue(
      operand.value,
      operand.valueType,
      Boolean(operator?.usesList)
    ) as RuleAstNode["constant"],
  };
}

export function compileAdvancedConditionGroupsToAst(
  groups: AdvancedRuleConditionGroup[],
  accessorOptions: RuleAccessorOption[]
): RuleAstNode {
  const accessorLookup = new Map(
    accessorOptions.map((option) => [option.id, option])
  );

  const compiledGroups = groups
    .map((group) => {
      const compiledConditions = group.conditions
        .filter((condition) => {
          if (!condition.leftAccessorId.trim()) {
            return false;
          }
          if (!condition.operator) {
            return false;
          }
          if (isUnaryRuleOperator(condition.operator)) {
            return true;
          }

          if (condition.rightOperand.mode === "accessor") {
            return Boolean(condition.rightOperand.accessorId.trim());
          }

          return condition.rightOperand.value.trim().length > 0;
        })
        .map((condition): RuleAstNode | null => {
          const leftOption = accessorLookup.get(condition.leftAccessorId);
          const operator = getRuleOperatorOption(condition.operator);
          if (!leftOption) {
            return null;
          }

          if (operator?.unary) {
            return {
              function: condition.operator,
              children: [normalizeAstNode(leftOption.astNode)],
            };
          }

          return {
            function: condition.operator,
            children: [
              normalizeAstNode(leftOption.astNode),
              buildOperandAst(
                condition.rightOperand,
                accessorLookup,
                condition.rightOperand.mode === "constant" ? operator ?? null : null
              ),
            ],
          };
        })
        .filter((item): item is RuleAstNode => item !== null);

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
    .filter((group): group is RuleAstNode => group !== null);

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

export function compileConditionGroupsToAst(
  groups: SimpleRuleConditionGroup[],
  accessorOptions: RuleAccessorOption[] = []
): RuleAstNode {
  const accessorLookup = new Map(accessorOptions.map((option) => [option.id, option]));
  const compiledGroups = groups
    .map((group) => {
      const compiledConditions = group.conditions
        .filter(
          (condition) =>
            ((condition.leftMode === "function" && condition.leftFunction) ||
              condition.left.trim()) &&
            condition.operator &&
            (isUnaryRuleOperator(condition.operator) ||
              (condition.rightMode === "function" && condition.rightFunction) ||
              condition.right.trim())
        )
        .map((condition) => compileConditionToAst(condition, accessorLookup));

      if (compiledConditions.length === 0) {
        return null;
      }

      return {
        node:
          compiledConditions.length === 1
            ? compiledConditions[0]
            : {
                function: "and",
                children: compiledConditions,
              },
        openBefore: group.openBefore ?? 0,
        closeAfter: group.closeAfter ?? 0,
      };
    })
    .filter(
      (
        group
      ): group is {
        node: RuleAstNode;
        openBefore: number;
        closeAfter: number;
      } => Boolean(group)
    );

  if (compiledGroups.length === 0) {
    return { constant: true };
  }

  const nodeStack: RuleAstNode[] = [];
  const operatorStack: Array<"or" | "("> = [];

  function reduceOr() {
    if (nodeStack.length < 2) {
      return;
    }

    const right = nodeStack.pop();
    const left = nodeStack.pop();
    if (!left || !right) {
      return;
    }

    nodeStack.push({
      function: "or",
      children: [left, right],
    });
  }

  function pushOrOperator() {
    while (operatorStack[operatorStack.length - 1] === "or") {
      operatorStack.pop();
      reduceOr();
    }
    operatorStack.push("or");
  }

  compiledGroups.forEach((group, index) => {
    if (index > 0) {
      pushOrOperator();
    }

    for (let openIndex = 0; openIndex < group.openBefore; openIndex += 1) {
      operatorStack.push("(");
    }

    nodeStack.push(group.node);

    for (let closeIndex = 0; closeIndex < group.closeAfter; closeIndex += 1) {
      while (operatorStack.length > 0 && operatorStack[operatorStack.length - 1] === "or") {
        operatorStack.pop();
        reduceOr();
      }
      if (operatorStack[operatorStack.length - 1] === "(") {
        operatorStack.pop();
      }
    }
  });

  while (operatorStack.length > 0) {
    const operator = operatorStack.pop();
    if (operator === "or") {
      reduceOr();
    }
  }

  return nodeStack[0] ?? { constant: true };
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

function parseRightOperand(
  node: ASTNodeLike,
  accessorLookup: Map<string, RuleAccessorOption>
): AdvancedRuleOperand | null {
  const nodeName = getNodeFunction(node);

  if (nodeName) {
    const payloadId = buildAccessorLookupId(node, "payload");
    const databaseId = buildAccessorLookupId(node, "database");
    const accessorId = accessorLookup.has(payloadId)
      ? payloadId
      : accessorLookup.has(databaseId)
        ? databaseId
        : null;

    if (accessorId) {
      return {
        mode: "accessor",
        accessorId,
      };
    }
  }

  const parsedConstant = parsePrimitiveValue(node.constant);
  if (!parsedConstant) {
    return null;
  }

  return {
    mode: "constant",
    value: parsedConstant.value,
    valueType: parsedConstant.valueType,
  };
}

function parseSimpleFunctionOperand(node: ASTNodeLike): SimpleRuleFunctionOperand | null {
  const functionName = getNodeFunction(node);
  if (!functionName) {
    return null;
  }

  if (functionName === "Aggregator") {
    return createFunctionOperand({
      ast: normalizeAstNode(node),
    });
  }

  if (functionName === "TimeNow" || functionName === "record_risk_level") {
    return createFunctionOperand({
      ast: normalizeAstNode(node),
    });
  }

  return null;
}

function parseSimpleOperandNode(
  node: ASTNodeLike | null | undefined,
  accessorLookup: Map<string, RuleAccessorOption> = new Map()
) {
  if (!node) {
    return null;
  }

  const functionName = getNodeFunction(node);

  const fieldName = node.named_children?.field?.constant;
  if (functionName === "field_ref" && typeof fieldName === "string") {
    const accessorId = buildAccessorLookupId(node, "payload");
    return {
      mode: "field" as const,
      value: accessorLookup.has(accessorId) ? accessorId : fieldName,
    };
  }

  const payloadFieldName = node.children?.[0]?.constant;
  if (functionName === "Payload" && typeof payloadFieldName === "string") {
    const accessorId = buildAccessorLookupId(node, "payload");
    return {
      mode: "field" as const,
      value: accessorLookup.has(accessorId) ? accessorId : payloadFieldName,
    };
  }

  if (functionName === "DatabaseAccess") {
    const accessorId = buildAccessorLookupId(node, "database");
    const fieldName =
      typeof node.named_children?.fieldName?.constant === "string"
        ? node.named_children.fieldName.constant
        : "";

    return {
      mode: "field" as const,
      value: accessorLookup.has(accessorId) ? accessorId : fieldName,
    };
  }

  const functionOperand = parseSimpleFunctionOperand(node);
  if (!functionOperand) {
    const parsedConstant = parsePrimitiveValue(node.constant);
    if (parsedConstant) {
      return {
        mode: "constant" as const,
        value: parsedConstant.value,
        valueType: parsedConstant.valueType,
      };
    }

    if (functionName === "CustomListAccess") {
      const listName = node.named_children?.customListId?.constant;
      if (typeof listName !== "string") {
        return null;
      }

      return {
        mode: "custom_list" as const,
        value: listName,
      };
    }

    return null;
  }

  return {
    mode: "function" as const,
    functionOperand,
  };
}

function parseSimpleRightOperandNode(
  node: ASTNodeLike | null | undefined,
  accessorLookup: Map<string, RuleAccessorOption> = new Map()
) {
  if (!node) {
    return null;
  }

  const parsedFieldOperand = parseSimpleOperandNode(node, accessorLookup);
  if (parsedFieldOperand?.mode === "field") {
    return {
      mode: "field" as const,
      value: parsedFieldOperand.value,
      valueType: "string" as SimpleValueType,
    };
  }

  const functionOperand = parseSimpleFunctionOperand(node);
  if (functionOperand) {
    return {
      mode: "function" as const,
      functionOperand,
      valueType: functionOperand.valueType,
    };
  }

  const parsedValue = parsePrimitiveValue(node.constant);
  if (!parsedValue) {
    return null;
  }

  return {
    mode: "constant" as const,
    value: parsedValue.value,
    valueType: parsedValue.valueType,
  };
}

function parseConditionNode(
  node: ASTNodeLike,
  accessorLookup: Map<string, RuleAccessorOption> = new Map()
): SimpleRuleCondition | null {
  const operator = getRuleOperatorOption(getNodeFunction(node));

  if (!operator) {
    return null;
  }

  const children = node.children ?? [];
  if (operator.unary) {
    if (children.length !== 1) {
      return null;
    }

    const leftOperand = parseSimpleOperandNode(children[0], accessorLookup);
    if (!leftOperand) {
      return null;
    }

    return createSimpleRuleCondition({
      left: leftOperand.mode === "function" ? "" : leftOperand.value,
      leftMode: leftOperand.mode,
      leftFunction: leftOperand.mode === "function" ? leftOperand.functionOperand : null,
      operator: operator.value,
      right: "",
      valueType: leftOperand.mode === "constant" ? leftOperand.valueType : "string",
    });
  }

  if (children.length !== 2) {
    return null;
  }

  const leftOperand = parseSimpleOperandNode(children[0], accessorLookup);
  if (!leftOperand) {
    return null;
  }

  const constant = children[1]?.constant;
  if (operator.usesList) {
    if (getNodeFunction(children[1]) === "CustomListAccess") {
      const listName = children[1]?.named_children?.customListId?.constant;
      if (typeof listName !== "string") {
        return null;
      }

      return createSimpleRuleCondition({
      left: leftOperand.mode === "function" ? "" : leftOperand.value,
      leftMode: leftOperand.mode,
      leftFunction: leftOperand.mode === "function" ? leftOperand.functionOperand : null,
      operator: operator.value,
      right: listName,
      rightMode: "custom_list",
      valueType: leftOperand.mode === "constant" ? leftOperand.valueType : "string",
    });
    }

    const parsedList = parseListNode(children[1]);
    if (!parsedList) {
      return null;
    }

    return createSimpleRuleCondition({
      left: leftOperand.mode === "function" ? "" : leftOperand.value,
      leftMode: leftOperand.mode,
      leftFunction: leftOperand.mode === "function" ? leftOperand.functionOperand : null,
      operator: operator.value,
      right: parsedList.value,
      rightMode: "constant",
      valueType:
        leftOperand.mode === "constant" ? leftOperand.valueType : parsedList.valueType,
    });
  }

  const rightOperand = parseSimpleRightOperandNode(children[1], accessorLookup);
  if (!rightOperand) {
    return null;
  }

  return createSimpleRuleCondition({
    left: leftOperand.mode === "function" ? "" : leftOperand.value,
    leftMode: leftOperand.mode,
    leftFunction: leftOperand.mode === "function" ? leftOperand.functionOperand : null,
    operator: operator.value,
    right:
      rightOperand.mode === "constant" || rightOperand.mode === "field"
        ? rightOperand.value
        : "",
    rightMode: rightOperand.mode,
    rightFunction: rightOperand.mode === "function" ? rightOperand.functionOperand : null,
    valueType:
      leftOperand.mode === "constant" ? leftOperand.valueType : rightOperand.valueType,
  });
}

function parseGroupNode(
  node: ASTNodeLike,
  accessorLookup: Map<string, RuleAccessorOption> = new Map()
): SimpleRuleConditionGroup | null {
  const nodeFunction = getNodeFunction(node);

  if (nodeFunction === "and") {
    const parsedConditions = (node.children ?? [])
      .map((child) => parseConditionNode(child, accessorLookup))
      .filter((condition): condition is SimpleRuleCondition => Boolean(condition));

    if (parsedConditions.length !== (node.children ?? []).length || parsedConditions.length === 0) {
      return null;
    }

    return createSimpleRuleGroup({
      conditions: parsedConditions,
    });
  }

  const singleCondition = parseConditionNode(node, accessorLookup);
  if (!singleCondition) {
    return null;
  }

  return createSimpleRuleGroup({
    conditions: [singleCondition],
  });
}

function flattenConditionGroupsFromOrNode(
  node: ASTNodeLike,
  accessorLookup: Map<string, RuleAccessorOption>,
  wrapped = false
): SimpleRuleConditionGroup[] | null {
  const nodeFunction = getNodeFunction(node);

  if (nodeFunction !== "or") {
    const parsedGroup = parseGroupNode(node, accessorLookup);
    if (!parsedGroup) {
      return null;
    }

    if (wrapped) {
      parsedGroup.openBefore = (parsedGroup.openBefore ?? 0) + 1;
      parsedGroup.closeAfter = (parsedGroup.closeAfter ?? 0) + 1;
    }

    return [parsedGroup];
  }

  const flattenedChildren = (node.children ?? []).map((child) => {
    const normalizedChild = normalizeAstNode(child);
    return flattenConditionGroupsFromOrNode(
      normalizedChild,
      accessorLookup,
      getNodeFunction(normalizedChild) === "or"
    );
  });

  if (flattenedChildren.some((child) => child === null)) {
    return null;
  }

  const groups = flattenedChildren.flatMap((child) => child ?? []);
  if (groups.length === 0) {
    return null;
  }

  if (wrapped) {
    groups[0]!.openBefore = (groups[0]!.openBefore ?? 0) + 1;
    groups[groups.length - 1]!.closeAfter =
      (groups[groups.length - 1]!.closeAfter ?? 0) + 1;
  }

  return groups;
}

export function tryParseAstToConditionGroups(
  ast: unknown,
  accessorOptions: RuleAccessorOption[] = []
) {
  const accessorLookup = new Map(accessorOptions.map((option) => [option.id, option]));
	if (!ast || typeof ast !== "object" || Array.isArray(ast)) {
		return null;
	}

	const node = ast as ASTNodeLike;
	if (isEmptyRuleFormulaNode(node)) {
		return [createSimpleRuleGroup()];
	}
	const nodeFunction = getNodeFunction(node);

  if (nodeFunction === "or") {
    const groups = flattenConditionGroupsFromOrNode(node, accessorLookup, false);
    if (!groups || groups.length === 0) {
      return null;
    }

    return groups;
  }

  const singleGroup = parseGroupNode(node, accessorLookup);
  if (!singleGroup) {
    return null;
  }

  return [singleGroup];
}

export function summarizeRuleFormula(ast: unknown) {
  const groups = tryParseAstToConditionGroups(ast);
  if (!groups) {
    return null;
  }

  const renderedGroups = groups
    .map((group) => {
      const renderedConditions = group.conditions
        .map((condition) => {
          const operator = getRuleOperatorOption(condition.operator);
          if (!operator) {
            return null;
          }

          const left =
            condition.leftMode === "function" && condition.leftFunction
              ? condition.leftFunction.label
              : condition.leftMode === "custom_list"
                ? `List: ${condition.left}`
                : condition.leftMode === "constant"
                  ? condition.valueType === "string"
                    ? `"${condition.left}"`
                    : condition.left
                  : condition.left;

          if (!left) {
            return null;
          }

          if (operator.unary) {
            return `${left} ${operator.label}`;
          }

          const right =
            condition.rightMode === "function" && condition.rightFunction
              ? condition.rightFunction.label
              : condition.rightMode === "custom_list"
                ? `List: ${condition.right}`
                : condition.valueType === "string"
                  ? `"${condition.right}"`
                  : condition.right;

          if (!right) {
            return null;
          }

          return `${left} ${operator.label} ${right}`;
        })
        .filter((item): item is string => Boolean(item));

      if (renderedConditions.length === 0) {
        return null;
      }

      const groupSummary = renderedConditions.join(" and ");
      const opens = "(".repeat(group.openBefore ?? 0);
      const closes = ")".repeat(group.closeAfter ?? 0);
      return `${opens}${groupSummary}${closes}`;
    })
    .filter((item): item is string => Boolean(item));

  if (renderedGroups.length === 0) {
    return null;
  }

  return renderedGroups.join(" or ");
}

function parseAdvancedConditionNode(
  node: ASTNodeLike,
  accessorLookup: Map<string, RuleAccessorOption>
): AdvancedRuleCondition | null {
  const operator = getRuleOperatorOption(getNodeFunction(node));
  if (!operator) {
    return null;
  }

  const children = node.children ?? [];
  if (operator.unary) {
    if (children.length !== 1) {
      return null;
    }

    const leftNode = normalizeAstNode(children[0]!);
    const payloadId = buildAccessorLookupId(leftNode, "payload");
    const databaseId = buildAccessorLookupId(leftNode, "database");
    const leftAccessorId = accessorLookup.has(payloadId)
      ? payloadId
      : accessorLookup.has(databaseId)
        ? databaseId
        : null;

    if (!leftAccessorId) {
      return null;
    }

    return createAdvancedRuleCondition(leftAccessorId, {
      operator: operator.value,
      rightOperand: {
        mode: "constant",
        value: "",
        valueType: "string",
      },
    });
  }

  if (children.length !== 2) {
    return null;
  }

  const leftNode = normalizeAstNode(children[0]!);
  const payloadId = buildAccessorLookupId(leftNode, "payload");
  const databaseId = buildAccessorLookupId(leftNode, "database");
  const leftAccessorId = accessorLookup.has(payloadId)
    ? payloadId
    : accessorLookup.has(databaseId)
      ? databaseId
      : null;

  if (!leftAccessorId) {
    return null;
  }

  if (operator.usesList) {
    const parsedList = parseListNode(children[1]);
    if (parsedList) {
      return createAdvancedRuleCondition(leftAccessorId, {
        operator: operator.value,
        rightOperand: {
          mode: "constant",
          value: parsedList.value,
          valueType: parsedList.valueType,
        },
      });
    }
  }

  const rightOperand = parseRightOperand(normalizeAstNode(children[1]!), accessorLookup);
  if (!rightOperand) {
    return null;
  }

  return createAdvancedRuleCondition(leftAccessorId, {
    operator: operator.value,
    rightOperand,
  });
}

function parseAdvancedGroupNode(
  node: ASTNodeLike,
  accessorLookup: Map<string, RuleAccessorOption>
): AdvancedRuleConditionGroup | null {
  const nodeFunction = getNodeFunction(node);

  if (nodeFunction === "and") {
    const parsedConditions = (node.children ?? [])
      .map((child) => parseAdvancedConditionNode(normalizeAstNode(child), accessorLookup))
      .filter((condition): condition is AdvancedRuleCondition => Boolean(condition));

    if (parsedConditions.length !== (node.children ?? []).length || parsedConditions.length === 0) {
      return null;
    }

    return createAdvancedRuleGroup(parsedConditions[0]?.leftAccessorId ?? "", {
      conditions: parsedConditions,
    });
  }

  const singleCondition = parseAdvancedConditionNode(node, accessorLookup);
  if (!singleCondition) {
    return null;
  }

  return createAdvancedRuleGroup(singleCondition.leftAccessorId, {
    conditions: [singleCondition],
  });
}

export function tryParseAstToAdvancedConditionGroups(
	ast: unknown,
	accessorOptions: RuleAccessorOption[]
) {
	if (!ast || typeof ast !== "object" || Array.isArray(ast)) {
    return null;
  }

	const accessorLookup = new Map(accessorOptions.map((option) => [option.id, option]));
	const node = normalizeAstNode(ast as ASTNodeLike);
	if (isEmptyRuleFormulaNode(node)) {
		return [createAdvancedRuleGroup(accessorOptions[0]?.id ?? "")];
	}
	const nodeFunction = getNodeFunction(node);

  if (nodeFunction === "or") {
    const groups = (node.children ?? [])
      .map((child) => parseAdvancedGroupNode(normalizeAstNode(child), accessorLookup))
      .filter((group): group is AdvancedRuleConditionGroup => Boolean(group));

    if (groups.length !== (node.children ?? []).length || groups.length === 0) {
      return null;
    }

    return groups;
  }

  const singleGroup = parseAdvancedGroupNode(node, accessorLookup);
  if (!singleGroup) {
    return null;
  }

  return [singleGroup];
}

function getExpressionOperatorArity(operator: SupportedRuleOperator | "") {
  return isUnaryRuleOperator(operator) ? 1 : 2;
}

function normalizeAccessorLeaf(
  node: ASTNodeLike,
  accessorLookup: Map<string, RuleAccessorOption>
): Extract<ExpressionRuleNode, { kind: "leaf" }> | null {
  const normalized = normalizeAstNode(node);
  const payloadId = buildAccessorLookupId(normalized, "payload");
  const databaseId = buildAccessorLookupId(normalized, "database");
  const accessorId = accessorLookup.has(payloadId)
    ? payloadId
    : accessorLookup.has(databaseId)
      ? databaseId
      : null;

  if (!accessorId) {
    return null;
  }

  return createExpressionLeaf({
    mode: "accessor",
    accessorId,
  });
}

function parseExpressionLeafNode(
  node: ASTNodeLike,
  accessorLookup: Map<string, RuleAccessorOption>
): Extract<ExpressionRuleNode, { kind: "leaf" }> | null {
  const nodeName = getNodeFunction(node);

  if (nodeName === "CustomListAccess") {
    const listId = node.named_children?.customListId?.constant;
    if (typeof listId !== "string") {
      return null;
    }

    return createExpressionLeaf({
      mode: "custom_list",
      value: listId,
      valueType: "string",
    });
  }

  const accessorLeaf = nodeName ? normalizeAccessorLeaf(node, accessorLookup) : null;
  if (accessorLeaf) {
    return accessorLeaf;
  }

  const parsedValue = parsePrimitiveValue(node.constant);
  if (!parsedValue) {
    return null;
  }

  return createExpressionLeaf({
    mode: "constant",
    value: parsedValue.value,
    valueType: parsedValue.valueType,
  });
}

export function tryParseAstToExpressionRuleNode(
  ast: unknown,
  accessorOptions: RuleAccessorOption[]
): ExpressionRuleNode | null {
  if (!ast || typeof ast !== "object" || Array.isArray(ast)) {
    return null;
  }

  const accessorLookup = new Map(accessorOptions.map((option) => [option.id, option]));
  const node = normalizeAstNode(ast as ASTNodeLike);
  if (isEmptyRuleFormulaNode(node)) {
    return createExpressionOperator();
  }

  const operator = parseOperatorNodeName(getNodeFunction(node));
  if (!operator) {
    return parseExpressionLeafNode(node, accessorLookup);
  }

  const children = (node.children ?? [])
    .map((child) => tryParseAstToExpressionRuleNode(normalizeAstNode(child), accessorOptions))
    .filter((child): child is ExpressionRuleNode => Boolean(child));

  if (children.length !== (node.children ?? []).length) {
    return null;
  }

  const expectedArity = getExpressionOperatorArity(operator);
  if (children.length !== expectedArity) {
    return null;
  }

  return createExpressionOperator({
    operator,
    children,
  });
}

export function isExpressionRuleNodeComplete(node: ExpressionRuleNode): boolean {
  if (node.kind === "leaf") {
    if (node.mode === "accessor") {
      return Boolean(node.accessorId.trim());
    }

    return Boolean(node.value.trim());
  }

  if (!node.operator) {
    return false;
  }

  const expectedArity = getExpressionOperatorArity(node.operator);
  if (node.children.length !== expectedArity) {
    return false;
  }

  return node.children.every((child) => isExpressionRuleNodeComplete(child));
}

function buildExpressionLeafAst(
  node: Extract<ExpressionRuleNode, { kind: "leaf" }>,
  accessorLookup: Map<string, RuleAccessorOption>
): RuleAstNode {
  if (node.mode === "accessor") {
    return normalizeAstNode(accessorLookup.get(node.accessorId)?.astNode ?? {});
  }

  if (node.mode === "custom_list") {
    return buildCustomListAccess(node.value);
  }

  return {
    constant: normalizeConstantValue(node.value, node.valueType, false) as RuleAstNode["constant"],
  };
}

export function compileExpressionRuleNodeToAst(
  node: ExpressionRuleNode,
  accessorOptions: RuleAccessorOption[]
): RuleAstNode {
  const accessorLookup = new Map(accessorOptions.map((option) => [option.id, option]));

  function compile(current: ExpressionRuleNode): RuleAstNode {
    if (current.kind === "leaf") {
      return buildExpressionLeafAst(current, accessorLookup);
    }

    return {
      function: current.operator || "eq",
      children: current.children.map((child) => compile(child)),
    };
  }

  return compile(node);
}
