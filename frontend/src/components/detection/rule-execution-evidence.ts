import type { JSONValue, RuleEvaluationNode } from "@/lib/decision-engine-api";

export function nodeName(node: RuleEvaluationNode | undefined) {
  return node?.function?.trim().toLowerCase().replace(/_/g, "") ?? "";
}

export function humanField(value: string) {
  return value.replace(/_/g, " ");
}

export function displayValue(value: JSONValue | undefined): string {
  if (value === undefined) return "Not recorded";
  if (value === null) return "null";
  if (value === true) return "Yes";
  if (value === false) return "No";
  return typeof value === "string" ? value : JSON.stringify(value);
}

function namedString(node: RuleEvaluationNode | undefined, name: string) {
  const child = node?.named_children?.[name];
  const value = child?.constant ?? child?.return_value;
  return typeof value === "string" ? value : undefined;
}

const operators: Record<string, string> = {
  eq: "=", neq: "!=", gt: ">", gte: "≥", lt: "<", lte: "≤",
  multiply: "×", divide: "÷", add: "+", subtract: "−",
  contains: "contains", in: "in", startswith: "starts with", endswith: "ends with",
};
export function operatorLabel(name: string) { return operators[name] ?? name; }

export function expression(node: RuleEvaluationNode): string {
  const name = nodeName(node);
  const children = node.children ?? [];
  if (!name || name === "constant") return displayValue(node.constant);
  if (name === "fieldref") return humanField(namedString(node, "field") ?? "field");
  if (name === "relatedfield") {
    const path = namedString(node, "path");
    const field = namedString(node, "field");
    return [path && humanField(path), field && humanField(field)].filter(Boolean).join(" · ") || "Related field";
  }
  if (name === "timeadd") {
    const anchor = node.named_children?.timestampField;
    const sign = namedString(node, "sign");
    const amount = namedString(node, "duration");
    if (anchor && amount && (sign === "+" || sign === "-")) return `${expression(anchor)} ${sign === "-" ? "−" : "+"} ${amount}`;
  }
  if (name === "aggregator") {
    const aggregate = namedString(node, "aggregator") ?? "Aggregate";
    const field = namedString(node, "fieldName");
    const table = namedString(node, "tableName");
    const label: Record<string, string> = { COUNT: "Count", COUNT_DISTINCT: "Distinct count", SUM: "Sum", AVG: "Average" };
    return `${label[aggregate.toUpperCase()] ?? aggregate}${field ? ` of ${humanField(field)}` : ""}${table ? ` (${humanField(table)})` : ""}`;
  }
  if (name === "not" && children[0]) return `NOT (${expression(children[0])})`;
  if (operators[name] && children.length === 2) {
    return `${expression(children[0])} ${operators[name]} ${expression(children[1])}`;
  }
  return `${node.function ?? "Expression"}${children.length ? `(${children.map(expression).join(", ")})` : ""}`;
}

export function aggregateNodes(node: RuleEvaluationNode): RuleEvaluationNode[] {
  if (nodeName(node) === "aggregator") return [node];
  return (node.children ?? []).flatMap(aggregateNodes);
}

type Filter = { field: string; operator: string; value: RuleEvaluationNode };
type Bound = Filter & { timestamp: number };
type Window = { field: string; summary: string; boundary: string; filters: Filter[] };

function timestamp(value: JSONValue | undefined): number | undefined {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/i.test(value)) return undefined;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function relative(node: RuleEvaluationNode) {
  const name = nodeName(node);
  const anchor = name === "timeadd" ? node.named_children?.timestampField : node;
  if (nodeName(anchor) !== "fieldref") return null;
  const field = namedString(anchor, "field");
  const value = timestamp(node.return_value);
  const referenceValue = timestamp(anchor?.return_value);
  if (!field || value === undefined || referenceValue === undefined) return null;
  return {
    key: `${field}:${referenceValue}`,
    reference: `the evaluated record's ${humanField(field)}`,
    offset: value - referenceValue,
  };
}

function duration(milliseconds: number) {
  for (const [unit, size] of [["day", 86_400_000], ["hour", 3_600_000], ["minute", 60_000], ["second", 1_000]] as const) {
    if (milliseconds > 0 && milliseconds % size === 0) {
      const count = milliseconds / size;
      return `${count} ${unit}${count === 1 ? "" : "s"}`;
    }
  }
  return null;
}

function boundLabel(bound: Bound) {
  const ref = relative(bound.value);
  const amount = ref && duration(Math.abs(ref.offset));
  if (ref && ref.offset === 0) return ref.reference;
  if (ref && amount) return `${amount} ${ref.offset < 0 ? "before" : "after"} ${ref.reference}`;
  return new Date(bound.timestamp).toISOString();
}

function windowSummary(start: Bound | undefined, end: Bound | undefined) {
  if (!start && end) {
    const offset = relative(end.value)?.offset;
    return `${offset ? (end.operator === "<=" ? "At or earlier than" : "Earlier than") : (end.operator === "<=" ? "On or before" : "Before")} ${boundLabel(end)}`;
  }
  if (!end && start) {
    const offset = relative(start.value)?.offset;
    return `${offset ? (start.operator === ">=" ? "At or later than" : "Later than") : (start.operator === ">=" ? "On or after" : "After")} ${boundLabel(start)}`;
  }
  if (!start || !end) return "Time bounds unavailable";
  if (start.timestamp > end.timestamp || (start.timestamp === end.timestamp && (start.operator === ">" || end.operator === "<"))) {
    return "No time satisfies these bounds";
  }
  const from = relative(start.value), to = relative(end.value);
  if (from && to && from.key === to.key) {
    const first = duration(Math.abs(from.offset)), last = duration(Math.abs(to.offset));
    if (first && from.offset < 0 && to.offset === 0) return `Within the ${first.replace(/^(\d+) (\w+?)s?$/, "$1-$2")} window before ${from.reference}`;
    if (last && from.offset === 0 && to.offset > 0) return `Within the ${last.replace(/^(\d+) (\w+?)s?$/, "$1-$2")} window after ${from.reference}`;
    if (first && last && from.offset < 0 && to.offset < 0) return `Between ${first} and ${last} before ${from.reference}`;
    if (first && last && from.offset > 0 && to.offset > 0) return `Between ${first} and ${last} after ${from.reference}`;
    if (first && last && from.offset < 0 && to.offset > 0) return `Between ${first} before and ${last} after ${from.reference}`;
  }
  return `Between ${boundLabel(start)} and ${boundLabel(end)}`;
}

export function aggregateFilters(node: RuleEvaluationNode): { windows: Window[]; filters: Filter[]; unsupported: boolean } {
  const list = node.named_children?.filters;
  if (!list) return { windows: [], filters: [], unsupported: false };
  if (nodeName(list) !== "list") return { windows: [], filters: [], unsupported: true };
  let unsupported = false;
  const filters = (list.children ?? []).flatMap((filter): Filter[] => {
    if (nodeName(filter) !== "filter") {
      unsupported = true;
      return [];
    }
    const field = namedString(filter, "fieldName");
    const operator = namedString(filter, "operator");
    const value = filter.named_children?.value;
    if (!field || !operator || !value) {
      unsupported = true;
      return [];
    }
    return [{ field, operator, value }];
  });
  if (unsupported) return { windows: [], filters: [], unsupported };
  const timeFields = new Set(filters.filter((filter) => nodeName(filter.value) === "timeadd" && timestamp(filter.value.return_value) !== undefined).map((filter) => filter.field));
  const groups = new Map<string, Bound[]>();
  const other: Filter[] = [];
  for (const filter of filters) {
    const value = timestamp(filter.value.return_value);
    if (timeFields.has(filter.field) && !filter.value.skipped && value !== undefined && [">", ">=", "<", "<="].includes(filter.operator)) {
      groups.set(filter.field, [...(groups.get(filter.field) ?? []), { ...filter, timestamp: value }]);
    } else {
      other.push(filter);
    }
  }
  const windows = [...groups].map(([field, bounds]) => {
    const lower = bounds.filter((b) => b.operator === ">" || b.operator === ">=").sort((a, b) => b.timestamp - a.timestamp || a.operator.length - b.operator.length)[0];
    const upper = bounds.filter((b) => b.operator === "<" || b.operator === "<=").sort((a, b) => a.timestamp - b.timestamp || a.operator.length - b.operator.length)[0];
    const boundary = [
      lower && `Start ${lower.operator === ">=" ? "included" : "excluded"}`,
      upper && `End ${upper.operator === "<=" ? "included" : "excluded"}`,
    ].filter(Boolean).join(" · ");
    return { field, summary: windowSummary(lower, upper), boundary, filters: bounds };
  });
  return { windows, filters: other, unsupported: false };
}
