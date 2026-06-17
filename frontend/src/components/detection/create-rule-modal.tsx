"use client";

import { useState } from "react";
import { createPortal } from "react-dom";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  type ASTNodeDTO,
  type CreateRuleRequest,
  type Rule,
} from "@/lib/decision-engine-api";

export type RuleOperatorOption = {
  label: string;
  value: string;
  usesList?: boolean;
};

export const fallbackRuleOperators: RuleOperatorOption[] = [
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

export function extractPayloadFieldNames(nodes: ASTNodeDTO[]) {
  return nodes
    .map((node) => {
      const fieldNode = node.children?.[0];
      return typeof fieldNode?.constant === "string" ? fieldNode.constant : null;
    })
    .filter((value): value is string => Boolean(value))
    .sort((left, right) => left.localeCompare(right));
}

function slugifyStableRuleId(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function buildRuleFormula(
  fieldName: string,
  operator: string,
  valueType: "string" | "number" | "boolean",
  rawValue: string
) {
  const fieldRef = {
    function: "field_ref",
    named_children: {
      field: { constant: fieldName },
    },
  };

  const constantValue =
    valueType === "number"
      ? Number(rawValue)
      : valueType === "boolean"
        ? rawValue === "true"
        : rawValue;

  if (operator === "in") {
    const items = rawValue
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean)
      .map((item) =>
        valueType === "number"
          ? Number(item)
          : valueType === "boolean"
            ? item === "true"
            : item
      );

    return {
      function: operator,
      children: [fieldRef, { constant: items }],
    };
  }

  return {
    function: operator,
    children: [fieldRef, { constant: constantValue }],
  };
}

function parseSimpleRuleFormula(formula: unknown): {
  fieldName: string;
  operator: string;
  valueType: "string" | "number" | "boolean";
  value: string;
} | null {
  if (!formula || typeof formula !== "object" || Array.isArray(formula)) {
    return null;
  }

  const node = formula as {
    function?: string;
    children?: Array<{
      function?: string;
      named_children?: { field?: { constant?: unknown } };
      constant?: unknown;
    }>;
  };

  const operator = node.function;
  const children = node.children;
  if (!operator || !children || children.length !== 2) {
    return null;
  }

  const left = children[0];
  const right = children[1];
  const fieldName = left?.named_children?.field?.constant;
  if (left?.function !== "field_ref" || typeof fieldName !== "string") {
    return null;
  }

  const constant = right?.constant;
  if (operator === "in") {
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
    return {
      fieldName,
      operator,
      valueType,
      value: constant.map((item) => String(item)).join(", "),
    };
  }

  if (
    typeof constant !== "string" &&
    typeof constant !== "number" &&
    typeof constant !== "boolean"
  ) {
    return null;
  }

  return {
    fieldName,
    operator,
    valueType:
      typeof constant === "number"
        ? "number"
        : typeof constant === "boolean"
          ? "boolean"
          : "string",
    value: String(constant),
  };
}

export function CreateRuleModal({
  isOpen,
  onClose,
  isDraft,
  isSaving,
  fieldOptions,
  operatorOptions,
  initialRule,
  onSubmit,
}: {
  isOpen: boolean;
  onClose: () => void;
  isDraft: boolean;
  isSaving: boolean;
  fieldOptions: string[];
  operatorOptions: RuleOperatorOption[];
  initialRule?: Rule | null;
  onSubmit: (payload: CreateRuleRequest) => void;
}) {
  const parsedFormula = initialRule ? parseSimpleRuleFormula(initialRule.formula) : null;
  const [name, setName] = useState(initialRule?.name ?? "");
  const [description, setDescription] = useState(initialRule?.description ?? "");
  const [ruleGroup, setRuleGroup] = useState(initialRule?.rule_group ?? "");
  const [fieldName, setFieldName] = useState(parsedFormula?.fieldName ?? "");
  const [operator, setOperator] = useState(parsedFormula?.operator ?? "eq");
  const [valueType, setValueType] = useState<"string" | "number" | "boolean">(
    parsedFormula?.valueType ?? "string"
  );
  const [value, setValue] = useState(parsedFormula?.value ?? "");
  const [scoreModifier, setScoreModifier] = useState(
    initialRule ? String(initialRule.score_modifier) : "10"
  );
  const [snoozeGroupId, setSnoozeGroupId] = useState(initialRule?.snooze_group_id ?? "");

  if (!isOpen) {
    return null;
  }

  const effectiveFieldName = fieldName || fieldOptions[0] || "";
  const selectedOperator =
    operatorOptions.find((option) => option.value === operator) ?? operatorOptions[0];
  const stableRuleID = initialRule?.stable_rule_id ?? slugifyStableRuleId(name);
  const canSubmit =
    isDraft &&
    (!initialRule || Boolean(parsedFormula)) &&
    name.trim() &&
    description.trim() &&
    ruleGroup.trim() &&
    effectiveFieldName.trim() &&
    value.trim() &&
    stableRuleID &&
    !Number.isNaN(Number(scoreModifier));

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/20 p-6 backdrop-blur-sm">
      <div className="w-full max-w-[760px] overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-[0_22px_60px_rgba(15,23,42,0.18)]">
        <div className="border-b border-slate-200 px-6 py-5">
          <div className="flex items-center justify-between gap-4">
            <div>
              <h2 className="text-[18px] font-semibold text-slate-950">
                {initialRule ? "Edit Rule" : "Create Rule"}
              </h2>
              <p className="mt-1 text-[13px] text-slate-500">
                Build a score rule against the trigger record fields.
              </p>
            </div>
            <div className="rounded-full border border-slate-200 bg-slate-50 px-3 py-1 text-[12px] font-medium text-slate-600">
              Score-based
            </div>
          </div>
        </div>
        {initialRule && !parsedFormula ? (
          <div className="border-b border-amber-200 bg-amber-50 px-6 py-3 text-[13px] text-amber-800">
            This rule uses a formula shape the inline editor does not support yet.
          </div>
        ) : null}
        <div className="grid gap-6 px-6 py-6 lg:grid-cols-[1.3fr_0.9fr]">
          <div className="space-y-4">
            <label className="space-y-2 text-[13px] text-slate-700">
              <span>Rule name</span>
              <Input
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="High value payment"
                className="h-11 rounded-2xl border-slate-200 shadow-none"
              />
            </label>
            <label className="space-y-2 text-[13px] text-slate-700">
              <span>Description</span>
              <textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                placeholder="Checks whether the payment amount is above the configured threshold."
                className="min-h-[108px] w-full rounded-2xl border border-slate-200 px-3 py-3 text-[14px] text-slate-950 outline-none"
              />
            </label>
            <div className="grid gap-4 md:grid-cols-2">
              <label className="space-y-2 text-[13px] text-slate-700">
                <span>Rule group</span>
                <Input
                  value={ruleGroup}
                  onChange={(event) => setRuleGroup(event.target.value)}
                  placeholder="Payments"
                  className="h-11 rounded-2xl border-slate-200 shadow-none"
                />
              </label>
              <label className="space-y-2 text-[13px] text-slate-700">
                <span>Score modifier</span>
                <Input
                  value={scoreModifier}
                  onChange={(event) => setScoreModifier(event.target.value)}
                  inputMode="numeric"
                  className="h-11 rounded-2xl border-slate-200 shadow-none"
                />
              </label>
            </div>
            <div className="rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
              <div className="mb-3 text-[13px] font-medium text-slate-900">Condition</div>
              <div className="grid gap-4 md:grid-cols-2">
                <label className="space-y-2 text-[13px] text-slate-700">
                  <span>Field</span>
                  <select
                    value={effectiveFieldName}
                    onChange={(event) => setFieldName(event.target.value)}
                    className="h-11 w-full rounded-2xl border border-slate-200 bg-white px-3 text-[14px] text-slate-950 outline-none"
                  >
                    <option value="">Select a field</option>
                    {fieldOptions.map((option) => (
                      <option key={option} value={option}>
                        {option}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="space-y-2 text-[13px] text-slate-700">
                  <span>Operator</span>
                  <select
                    value={operator}
                    onChange={(event) => setOperator(event.target.value)}
                    className="h-11 w-full rounded-2xl border border-slate-200 bg-white px-3 text-[14px] text-slate-950 outline-none"
                  >
                    {operatorOptions.map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="space-y-2 text-[13px] text-slate-700">
                  <span>Value type</span>
                  <select
                    value={valueType}
                    onChange={(event) =>
                      setValueType(event.target.value as "string" | "number" | "boolean")
                    }
                    className="h-11 w-full rounded-2xl border border-slate-200 bg-white px-3 text-[14px] text-slate-950 outline-none"
                  >
                    <option value="string">String</option>
                    <option value="number">Number</option>
                    <option value="boolean">Boolean</option>
                  </select>
                </label>
                <label className="space-y-2 text-[13px] text-slate-700">
                  <span>{selectedOperator?.usesList ? "Values" : "Value"}</span>
                  <Input
                    value={value}
                    onChange={(event) => setValue(event.target.value)}
                    placeholder={selectedOperator?.usesList ? "review, decline" : "1000"}
                    className="h-11 rounded-2xl border-slate-200 bg-white shadow-none"
                  />
                </label>
              </div>
            </div>
          </div>
          <div className="space-y-4">
            <div className="rounded-2xl border border-slate-200 bg-white p-4">
              <div className="text-[13px] font-medium text-slate-900">Rule identity</div>
              <div className="mt-3 space-y-3">
                <label className="space-y-2 text-[13px] text-slate-700">
                  <span>Snooze group ID</span>
                  <Input
                    value={snoozeGroupId}
                    onChange={(event) => setSnoozeGroupId(event.target.value)}
                    placeholder="Optional"
                    className="h-11 rounded-2xl border-slate-200 shadow-none"
                  />
                </label>
                <div className="space-y-2 text-[13px] text-slate-700">
                  <span>Stable rule ID</span>
                  <div className="rounded-2xl border border-slate-200 bg-slate-50 px-3 py-3 text-[14px] text-slate-700">
                    {stableRuleID || "Generated from rule name"}
                  </div>
                </div>
              </div>
            </div>
            <div className="rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
              <div className="text-[13px] font-medium text-slate-900">Preview</div>
              <div className="mt-3 space-y-2 text-[13px] text-slate-600">
                <p>
                  <span className="font-medium text-slate-900">Field:</span>{" "}
                  {effectiveFieldName || "Not selected"}
                </p>
                <p>
                  <span className="font-medium text-slate-900">Operator:</span>{" "}
                  {selectedOperator?.label ?? "Not selected"}
                </p>
                <p>
                  <span className="font-medium text-slate-900">Value:</span>{" "}
                  {value || "Not set"}
                </p>
                <p>
                  <span className="font-medium text-slate-900">Score:</span>{" "}
                  {scoreModifier || "0"}
                </p>
              </div>
            </div>
            {!isDraft ? (
              <div className="rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-[13px] text-amber-800">
                Create a draft iteration before adding new rules.
              </div>
            ) : null}
          </div>
        </div>
        <div className="flex gap-3 border-t border-slate-200 px-6 py-4">
          <Button
            variant="outline"
            onClick={onClose}
            className="h-10 flex-1 rounded-2xl border-slate-200 px-4 text-[14px] shadow-none"
          >
            Cancel
          </Button>
          <Button
            disabled={!canSubmit || isSaving}
            onClick={() =>
              onSubmit({
                display_order: 0,
                name: name.trim(),
                description: description.trim(),
                formula: buildRuleFormula(
                  effectiveFieldName,
                  operator,
                  valueType,
                  value.trim()
                ),
                score_modifier: Number(scoreModifier),
                rule_group: ruleGroup.trim(),
                snooze_group_id: snoozeGroupId.trim() || null,
                stable_rule_id: stableRuleID,
              })
            }
            className="h-10 flex-1 rounded-2xl bg-[#1f4f96] px-4 text-[14px] shadow-none hover:bg-[#163f79]"
          >
            {isSaving ? "Saving..." : initialRule ? "Save rule" : "Create rule"}
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}
