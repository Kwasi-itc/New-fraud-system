"use client";

import { createPortal } from "react-dom";
import { X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { type RuleAstNode, type SimpleRuleFunctionOperand, summarizeRuleFormula } from "@/lib/rule-builder";

function getNodeFunction(node: RuleAstNode | null | undefined) {
  return node?.function ?? node?.name;
}

function formatIsoDuration(value: string) {
  const timeMatch = /^PT(\d+)([SMH])$/.exec(value);
  if (timeMatch) {
    const amount = timeMatch[1];
    const unit = timeMatch[2] === "S" ? "seconds" : timeMatch[2] === "M" ? "minutes" : "hours";
    return `${amount} ${unit}`;
  }

  const dayMatch = /^P(\d+)D$/.exec(value);
  if (dayMatch) {
    return `${dayMatch[1]} days`;
  }

  return value;
}

function FieldRow({ label, value }: { label: string; value: string | null | undefined }) {
  if (!value) {
    return null;
  }

  return (
    <div className="grid gap-1 sm:grid-cols-[140px_minmax(0,1fr)] sm:gap-3">
      <div className="text-[12px] font-medium uppercase tracking-[0.04em] text-slate-500">
        {label}
      </div>
      <div className="text-[14px] text-slate-950">{value}</div>
    </div>
  );
}

function renderFilterSummary(node: RuleAstNode) {
  const filters = node.named_children?.filters?.children ?? [];
  return filters
    .map((filterNode) => {
      if (getNodeFunction(filterNode) !== "Filter") {
        return null;
      }

      const fieldName =
        typeof filterNode.named_children?.fieldName?.constant === "string"
          ? filterNode.named_children.fieldName.constant
          : "field";
      const operator =
        typeof filterNode.named_children?.operator?.constant === "string"
          ? filterNode.named_children.operator.constant
          : "=";
      const value = summarizeRuleFormula(filterNode.named_children?.value);
      return value ? `${fieldName} ${operator} ${value}` : `${fieldName} ${operator}`;
    })
    .filter((item): item is string => Boolean(item));
}

function FunctionDetails({ operand }: { operand: SimpleRuleFunctionOperand }) {
  const node = operand.ast;
  const functionName = getNodeFunction(node);

  if (functionName === "Aggregator") {
    const aggregator =
      typeof node.named_children?.aggregator?.constant === "string"
        ? node.named_children.aggregator.constant
        : null;
    const tableName =
      typeof node.named_children?.tableName?.constant === "string"
        ? node.named_children.tableName.constant
        : null;
    const fieldName =
      typeof node.named_children?.fieldName?.constant === "string"
        ? node.named_children.fieldName.constant
        : null;
    const percentile =
      typeof node.named_children?.percentile?.constant === "number"
        ? String(node.named_children.percentile.constant)
        : null;
    const filters = renderFilterSummary(node);

    return (
      <div className="space-y-4">
        <FieldRow label="Function" value="Aggregator" />
        <FieldRow label="Aggregator" value={aggregator} />
        <FieldRow label="Table" value={tableName} />
        <FieldRow label="Field" value={fieldName} />
        <FieldRow label="Percentile" value={percentile} />
        {filters.length > 0 ? (
          <div className="space-y-2">
            <div className="text-[12px] font-medium uppercase tracking-[0.04em] text-slate-500">
              Filters
            </div>
            <div className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-[14px] text-slate-950">
              {filters.map((filter) => (
                <div key={filter}>{filter}</div>
              ))}
            </div>
          </div>
        ) : null}
      </div>
    );
  }

  if (functionName === "TimeAdd") {
    const timestamp = summarizeRuleFormula(node.named_children?.timestampField) ?? "timestamp";
    const sign = node.named_children?.sign?.constant === "-" ? "-" : "+";
    const duration =
      typeof node.named_children?.duration?.constant === "string"
        ? formatIsoDuration(node.named_children.duration.constant)
        : null;

    return (
      <div className="space-y-4">
        <FieldRow label="Function" value="TimeAdd" />
        <FieldRow label="Timestamp source" value={timestamp} />
        <FieldRow label="Sign" value={sign} />
        <FieldRow label="Duration" value={duration} />
      </div>
    );
  }

  if (functionName === "TimestampExtract") {
    const timestamp = summarizeRuleFormula(node.named_children?.timestamp) ?? "timestamp";
    const part =
      typeof node.named_children?.part?.constant === "string"
        ? node.named_children.part.constant
        : null;

    return (
      <div className="space-y-4">
        <FieldRow label="Function" value="TimestampExtract" />
        <FieldRow label="Part" value={part} />
        <FieldRow label="Timestamp source" value={timestamp} />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <FieldRow label="Function" value={functionName ?? "Function"} />
      <div className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-[14px] text-slate-950">
        {summarizeRuleFormula(node) ?? "No summary available."}
      </div>
    </div>
  );
}

export function FunctionDetailsModal({
  operand,
  onClose,
}: {
  operand: SimpleRuleFunctionOperand;
  onClose: () => void;
}) {
  if (typeof document === "undefined") {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/38 p-4">
      <div className="w-full max-w-[720px] rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgba(15,23,42,0.14)]">
        <div className="relative border-b border-slate-200 px-6 py-5">
          <h3 className="text-[1.4rem] font-semibold tracking-tight text-slate-950">
            {operand.label}
          </h3>
          <p className="mt-1 text-[14px] text-slate-600">{operand.meta}</p>
          <button
            type="button"
            onClick={onClose}
            className="absolute right-4 top-4 rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-900"
            aria-label="Close function details"
          >
            <X className="size-4" />
          </button>
        </div>
        <div className="space-y-6 px-6 py-6">
          <FunctionDetails operand={operand} />
        </div>
        <div className="flex justify-end border-t border-slate-200 px-6 py-4">
          <Button type="button" variant="outline" onClick={onClose} className="rounded-xl">
            Close
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}
