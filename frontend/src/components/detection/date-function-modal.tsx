"use client";

import { createPortal } from "react-dom";
import { X } from "lucide-react";

import {
  RuleOperandSelector,
  type OperandOption,
  type OperandOptionGroup,
} from "@/components/detection/rule-operand-selector";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  buildTimeAddAst,
  buildTimestampExtractAst,
  createFunctionOperand,
  type RuleAccessorOption,
  type RuleAstNode,
  type SimpleRuleFunctionOperand,
} from "@/lib/rule-builder";

export type DateFunctionType = "TimeAdd" | "TimestampExtract";

export type DateFunctionDraft = {
  type: DateFunctionType;
  sourceValue: string;
  sign: "+" | "-";
  durationAmount: string;
  durationUnit: "seconds" | "minutes" | "hours" | "days";
  extractPart: "year" | "month" | "day_of_month" | "day_of_week" | "hour";
  label: string;
};

const DURATION_UNIT_TO_ISO: Record<DateFunctionDraft["durationUnit"], string> = {
  seconds: "S",
  minutes: "M",
  hours: "H",
  days: "D",
};

function isTimestampFunctionOperand(operand: SimpleRuleFunctionOperand) {
  const functionName = operand.ast.function ?? operand.ast.name;
  return functionName === "TimeNow" || functionName === "TimeAdd" || functionName === "ParseTime";
}

function resolveSourceAst(
  value: string,
  accessorOptions: RuleAccessorOption[],
  functionOperands: SimpleRuleFunctionOperand[]
): RuleAstNode | null {
  if (value.startsWith("function:")) {
    const operand = functionOperands.find((item) => `function:${item.id}` === value);
    return operand?.ast ?? null;
  }

  const accessor = accessorOptions.find((option) => option.id === value);
  return accessor?.astNode ?? null;
}

function buildSourceOptions(
  accessorOptions: RuleAccessorOption[],
  functionOperands: SimpleRuleFunctionOperand[]
) {
  const accessorSelectorOptions: OperandOption[] = accessorOptions.map((option) => ({
    value: option.id,
    label: option.label,
    meta: option.meta,
    keywords: [option.label, option.meta],
  }));
  const functionSelectorOptions: OperandOption[] = functionOperands
    .filter(isTimestampFunctionOperand)
    .map((operand) => ({
      value: `function:${operand.id}`,
      label: operand.label,
      meta: operand.meta,
      keywords: [operand.label, operand.meta],
    }));

  const groups: OperandOptionGroup[] = [
    {
      id: "fields",
      label: "Fields",
      children: [
        {
          id: "fields-all",
          label: "Timestamp source",
          options: accessorSelectorOptions,
        },
      ],
    },
    ...(functionSelectorOptions.length > 0
      ? [
          {
            id: "functions",
            label: "Functions",
            children: [
              {
                id: "functions-existing",
                label: "Existing time functions",
                options: functionSelectorOptions,
              },
            ],
          } satisfies OperandOptionGroup,
        ]
      : []),
  ];

  return {
    options: [...accessorSelectorOptions, ...functionSelectorOptions],
    groups,
  };
}

function buildDurationIso(amount: string, unit: DateFunctionDraft["durationUnit"]) {
  const trimmed = amount.trim();
  const numeric = Number(trimmed);
  if (!Number.isFinite(numeric) || numeric < 0) {
    return null;
  }

  if (unit === "days") {
    return `P${numeric}${DURATION_UNIT_TO_ISO[unit]}`;
  }

  return `PT${numeric}${DURATION_UNIT_TO_ISO[unit]}`;
}

export function createDateFunctionDraft(type: DateFunctionType): DateFunctionDraft {
  return {
    type,
    sourceValue: "",
    sign: "+",
    durationAmount: "1",
    durationUnit: "hours",
    extractPart: "hour",
    label: "",
  };
}

export function DateFunctionModal({
  draft,
  onClose,
  onChange,
  onSave,
  accessorOptions,
  functionOperands,
}: {
  draft: DateFunctionDraft;
  onClose: () => void;
  onChange: (draft: DateFunctionDraft) => void;
  onSave: (operand: SimpleRuleFunctionOperand) => void;
  accessorOptions: RuleAccessorOption[];
  functionOperands: SimpleRuleFunctionOperand[];
}) {
  const { options, groups } = buildSourceOptions(accessorOptions, functionOperands);
  const sourceAst = resolveSourceAst(draft.sourceValue, accessorOptions, functionOperands);
  const durationIso =
    draft.type === "TimeAdd"
      ? buildDurationIso(draft.durationAmount, draft.durationUnit)
      : null;
  const sourceError = sourceAst ? null : "Select a timestamp source.";
  const durationError =
    draft.type === "TimeAdd" && !durationIso
      ? "Enter a valid non-negative duration."
      : null;
  const canSave =
    Boolean(sourceAst) &&
    (draft.type === "TimestampExtract" || Boolean(durationIso));

  if (typeof document === "undefined") {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/38 p-4">
      <div className="w-full max-w-[820px] rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgba(15,23,42,0.14)]">
        <div className="relative border-b border-slate-200 px-6 py-5">
          <h3 className="text-[1.4rem] font-semibold tracking-tight text-slate-950">
            {draft.type === "TimeAdd" ? "Create time adjustment" : "Create timestamp extract"}
          </h3>
          <p className="mt-1 text-[14px] text-slate-600">
            {draft.type === "TimeAdd"
              ? "Apply a duration to a timestamp field or another time function."
              : "Extract a time part such as hour or day from a timestamp source."}
          </p>
          <button
            type="button"
            onClick={onClose}
            className="absolute right-4 top-4 rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-900"
            aria-label="Close date function modal"
          >
            <X className="size-4" />
          </button>
        </div>

        <div className="space-y-6 px-6 py-6">
          {draft.type === "TimeAdd" ? (
            <div className="space-y-3">
              <div className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-4">
                <div className="grid gap-3 md:grid-cols-[minmax(0,1.6fr)_88px_110px_150px]">
                  <label className="block space-y-2">
                    <span className="text-[13px] font-medium text-slate-700">Timestamp source</span>
                    <RuleOperandSelector
                      className="min-w-0"
                      value={draft.sourceValue}
                      options={options}
                      groups={groups}
                      prefix="ts"
                      invalid={Boolean(sourceError)}
                      selectedMeta={sourceError ?? undefined}
                      placeholder="Select a timestamp source..."
                      searchPlaceholder="Search timestamp fields or time functions"
                      emptyLabel="No timestamp sources matched your search."
                      onChange={(value) => onChange({ ...draft, sourceValue: value })}
                    />
                  </label>

                  <label className="block space-y-2">
                    <span className="text-[13px] font-medium text-slate-700">Sign</span>
                    <select
                      value={draft.sign}
                      onChange={(event) =>
                        onChange({
                          ...draft,
                          sign: event.target.value as DateFunctionDraft["sign"],
                        })
                      }
                      className="h-10 w-full rounded-sm border border-slate-300 bg-white px-3 text-[14px] font-medium text-slate-900 outline-none transition focus:border-[#365fa3]"
                    >
                      <option value="+">+</option>
                      <option value="-">-</option>
                    </select>
                  </label>

                  <label className="block space-y-2">
                    <span className="text-[13px] font-medium text-slate-700">Amount</span>
                    <Input
                      type="number"
                      min="0"
                      step="1"
                      value={draft.durationAmount}
                      onChange={(event) =>
                        onChange({
                          ...draft,
                          durationAmount: event.target.value,
                        })
                      }
                      inputMode="numeric"
                      placeholder="0"
                      className="h-10 w-full rounded-sm border-slate-300"
                    />
                  </label>

                  <label className="block space-y-2">
                    <span className="text-[13px] font-medium text-slate-700">Unit</span>
                    <select
                      value={draft.durationUnit}
                      onChange={(event) =>
                        onChange({
                          ...draft,
                          durationUnit: event.target.value as DateFunctionDraft["durationUnit"],
                        })
                      }
                      className="h-10 w-full rounded-sm border border-slate-300 bg-white px-3 text-[14px] text-slate-900 outline-none transition focus:border-[#365fa3]"
                    >
                      <option value="seconds">Seconds</option>
                      <option value="minutes">Minutes</option>
                      <option value="hours">Hours</option>
                      <option value="days">Days</option>
                    </select>
                  </label>
                </div>
              </div>

              {sourceError || durationError ? (
                <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-[13px] text-red-700">
                  {[sourceError, durationError].filter(Boolean).join(" ")}
                </div>
              ) : null}
            </div>
          ) : (
            <div className="space-y-3">
              <div className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-4">
                <div className="grid gap-3 md:grid-cols-[180px_minmax(0,1fr)]">
                  <label className="block space-y-2">
                    <span className="text-[13px] font-medium text-slate-700">Part</span>
                    <select
                      value={draft.extractPart}
                      onChange={(event) =>
                        onChange({
                          ...draft,
                          extractPart: event.target.value as DateFunctionDraft["extractPart"],
                        })
                      }
                      className="h-10 w-full rounded-sm border border-slate-300 bg-white px-3 text-[14px] text-slate-900 outline-none transition focus:border-[#365fa3]"
                    >
                      <option value="year">Year</option>
                      <option value="month">Month</option>
                      <option value="day_of_month">Day of month</option>
                      <option value="day_of_week">Day of week</option>
                      <option value="hour">Hour</option>
                    </select>
                  </label>

                  <label className="block space-y-2">
                    <span className="text-[13px] font-medium text-slate-700">Timestamp source</span>
                    <RuleOperandSelector
                      className="min-w-0"
                      value={draft.sourceValue}
                      options={options}
                      groups={groups}
                      prefix="ts"
                      invalid={Boolean(sourceError)}
                      selectedMeta={sourceError ?? undefined}
                      placeholder="Select a timestamp source..."
                      searchPlaceholder="Search timestamp fields or time functions"
                      emptyLabel="No timestamp sources matched your search."
                      onChange={(value) => onChange({ ...draft, sourceValue: value })}
                    />
                  </label>
                </div>
                <div className="mt-3 text-[12px] text-slate-600">
                  Returns a number, which works with numeric comparison operators.
                </div>
              </div>

              {sourceError ? (
                <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-[13px] text-red-700">
                  {sourceError}
                </div>
              ) : null}
            </div>
          )}

          <label className="block space-y-2">
            <span className="text-[15px] font-medium text-slate-900">Label</span>
            <Input
              value={draft.label}
              onChange={(event) => onChange({ ...draft, label: event.target.value })}
              placeholder={
                draft.type === "TimeAdd" ? "recent event window" : "transaction hour"
              }
              className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
            />
          </label>
        </div>

        <div className="flex items-center justify-end gap-3 border-t border-slate-200 px-6 py-4">
          <Button type="button" variant="outline" onClick={onClose} className="rounded-xl">
            Cancel
          </Button>
          <Button
            type="button"
            disabled={!canSave}
            onClick={() => {
              if (!sourceAst) {
                return;
              }

              const ast =
                draft.type === "TimeAdd"
                  ? durationIso
                    ? buildTimeAddAst({
                        timestampAst: sourceAst,
                        duration: durationIso,
                        sign: draft.sign,
                      })
                    : null
                  : buildTimestampExtractAst({
                      timestampAst: sourceAst,
                      part: draft.extractPart,
                    });

              if (!ast) {
                return;
              }

              onSave(
                createFunctionOperand({
                  ast,
                  label: draft.label.trim(),
                })
              );
            }}
            className="rounded-xl bg-[#1f4f96] hover:bg-[#163f79]"
          >
            Save function
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}
