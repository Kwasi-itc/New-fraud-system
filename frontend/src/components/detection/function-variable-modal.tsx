"use client";

import { createPortal } from "react-dom";
import { Plus, X } from "lucide-react";

import { ConditionSelectorRow } from "@/components/detection/condition-selector-row";
import { RuleOperandSelector } from "@/components/detection/rule-operand-selector";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { type AggregatorOperator } from "@/lib/rule-builder";

export const AGGREGATOR_OPTIONS: Array<{
  value: AggregatorOperator;
  label: string;
  helper?: string;
}> = [
  { value: "AVG", label: "Average" },
  { value: "COUNT", label: "Count" },
  { value: "COUNT_DISTINCT", label: "Count distinct" },
  { value: "MAX", label: "Max" },
  { value: "MIN", label: "Min" },
  { value: "SUM", label: "Sum" },
  {
    value: "STDDEV",
    label: "Standard deviation",
    helper: "Heavier to compute on large result sets.",
  },
  {
    value: "PCTILE",
    label: "Percentile",
    helper: "Requires a percentile value and can be expensive on large result sets.",
  },
  {
    value: "MEDIAN",
    label: "Median",
    helper: "Heavier to compute on large result sets.",
  },
];

export type FunctionVariableTableFieldOption = {
  tableName: string;
  fieldName: string;
  label: string;
};

export type FunctionVariableFilterDraft = {
  id: string;
  fieldKey: string;
  operator: "=" | "!=" | ">" | ">=" | "<" | "<=";
  rightValue: string;
  rightMode: "literal" | "field";
};

export function createFunctionVariableFilterDraft(): FunctionVariableFilterDraft {
  return {
    id: globalThis.crypto?.randomUUID?.() ?? `filter-${Date.now()}-${Math.random()}`,
    fieldKey: "",
    operator: "=",
    rightValue: "",
    rightMode: "literal",
  };
}

function decodeFunctionVariableLiteralSelection(value: string): string {
  if (value.startsWith("literal:number:")) {
    return value.replace(/^literal:number:/, "");
  }

  if (value.startsWith("literal:boolean:")) {
    return value.replace(/^literal:boolean:/, "");
  }

  if (value.startsWith("literal:string:")) {
    return value.replace(/^literal:string:/, "");
  }

  return value;
}

function buildFunctionVariableLiteralSearchOptions(searchValue: string) {
  const normalized = searchValue.toLowerCase();
  const literalOptions: Array<{
    value: string;
    label: string;
    meta: string;
    sideLabel: string;
  }> = [];

  if (searchValue.trim().length > 0 && Number.isFinite(Number(searchValue))) {
    literalOptions.push({
      value: `literal:number:${searchValue}`,
      label: searchValue,
      meta: "Number",
      sideLabel: "Use number",
    });
  }

  literalOptions.push({
    value: `literal:string:${searchValue}`,
    label: `"${searchValue}"`,
    meta: "String",
    sideLabel: "Use string",
  });

  if ("true".includes(normalized) || "false".includes(normalized)) {
    ["true", "false"]
      .filter((candidate) => candidate.includes(normalized))
      .forEach((candidate) => {
        literalOptions.push({
          value: `literal:boolean:${candidate}`,
          label: candidate,
          meta: "Boolean",
          sideLabel: "Use boolean",
        });
      });
  }

  return literalOptions;
}

export type FunctionVariableDraft = {
  aggregator: AggregatorOperator;
  variableName: string;
  fieldKey: string;
  percentile: string;
  filters: FunctionVariableFilterDraft[];
};

export function FunctionVariableModal({
  draft,
  onClose,
  onChange,
  onSave,
  tableFieldOptions,
}: {
  draft: FunctionVariableDraft;
  onClose: () => void;
  onChange: (draft: FunctionVariableDraft) => void;
  onSave: (draft: FunctionVariableDraft) => void;
  tableFieldOptions: FunctionVariableTableFieldOption[];
}) {
  const fieldOptions = tableFieldOptions.map((option) => ({
    value: `${option.tableName}::${option.fieldName}`,
    label: option.fieldName,
    meta: option.tableName,
    keywords: [option.tableName, option.fieldName],
  }));
  const fieldGroups = Object.entries(
    fieldOptions.reduce<Record<string, typeof fieldOptions>>((acc, option) => {
      const tableName = option.meta ?? "records";
      acc[tableName] = acc[tableName] ?? [];
      acc[tableName]!.push(option);
      return acc;
    }, {})
  )
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([tableName, options]) => ({
      id: `table-${tableName}`,
      label: tableName,
      children: [
        {
          id: `table-${tableName}-fields`,
          label: tableName,
          options,
        },
      ],
    }));

  const selectedAggregator = AGGREGATOR_OPTIONS.find(
    (option) => option.value === draft.aggregator
  );
  const needsPercentile = draft.aggregator === "PCTILE";
  const percentileValue = Number(draft.percentile);
  const hasValidPercentile = !needsPercentile || Number.isFinite(percentileValue);
  const hasValidFilters = draft.filters.every(
    (filter) => Boolean(filter.fieldKey) && filter.rightValue.trim().length > 0
  );
  const canSave = Boolean(draft.fieldKey && hasValidPercentile && hasValidFilters);
  const filterOperatorOptions = [
    { value: "=", label: "=" },
    { value: "!=", label: "!=" },
    { value: ">", label: ">" },
    { value: ">=", label: ">=" },
    { value: "<", label: "<" },
    { value: "<=", label: "<=" },
  ];

  if (typeof document === "undefined") {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/38 p-4">
      <div className="w-full max-w-[920px] rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgba(15,23,42,0.14)]">
        <div className="relative border-b border-slate-200 px-6 py-5 text-center">
          <h3 className="text-[1.65rem] font-semibold tracking-tight text-slate-950">
            Create a variable
          </h3>
          <div className="mt-1 text-[13px] text-slate-500">From Marble database</div>
          <button
            type="button"
            onClick={onClose}
            className="absolute right-4 top-4 rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-900"
            aria-label="Close create variable modal"
          >
            <X className="size-4" />
          </button>
        </div>

        <div className="space-y-6 px-6 py-6">
          <div className="rounded-xl border border-slate-200 px-4 py-3 text-[14px] text-slate-700">
            Computes aggregates on your ingested data.
          </div>

          <label className="block space-y-2">
            <span className="text-[15px] font-medium text-slate-900">Variable name</span>
            <Input
              value={draft.variableName}
              onChange={(event) =>
                onChange({
                  ...draft,
                  variableName: event.target.value,
                })
              }
              placeholder="Edit the name of your variable"
              className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
            />
          </label>

          <div
            className={cn(
              "grid gap-5",
              needsPercentile
                ? "md:grid-cols-[220px_140px_minmax(0,1fr)]"
                : "md:grid-cols-[220px_minmax(0,1fr)]"
            )}
          >
            <label className="block space-y-2">
              <span className="text-[15px] font-medium text-slate-900">Function</span>
              <select
                value={draft.aggregator}
                onChange={(event) =>
                  onChange({
                    ...draft,
                    aggregator: event.target.value as AggregatorOperator,
                  })
                }
                className="flex h-11 w-full rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-900 outline-none focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
              >
                {AGGREGATOR_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>

            {needsPercentile ? (
              <label className="block space-y-2">
                <span className="text-[15px] font-medium text-slate-900">Percentile</span>
                <Input
                  value={draft.percentile}
                  onChange={(event) =>
                    onChange({
                      ...draft,
                      percentile: event.target.value,
                    })
                  }
                  inputMode="decimal"
                  placeholder="50"
                  className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                />
              </label>
            ) : null}

            <label className="block space-y-2">
              <span className="text-[15px] font-medium text-slate-900">Object field</span>
              <RuleOperandSelector
                value={draft.fieldKey}
                options={fieldOptions}
                groups={fieldGroups}
                panelPosition="top"
                placeholder="Select a field..."
                searchPlaceholder="Search fields"
                emptyLabel="No fields matched your search."
                onChange={(value) =>
                  onChange({
                    ...draft,
                    fieldKey: value,
                  })
                }
              />
            </label>
          </div>

          {selectedAggregator?.helper ? (
            <div className="rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
              {selectedAggregator.helper}
            </div>
          ) : null}

          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div className="text-[15px] font-medium text-slate-900">Filters</div>
              <Button
                type="button"
                variant="outline"
                onClick={() =>
                  onChange({
                    ...draft,
                    filters: [...draft.filters, createFunctionVariableFilterDraft()],
                  })
                }
                className="h-8 rounded-lg border-slate-200 px-3 text-sm shadow-none hover:translate-y-0"
              >
                <Plus className="size-4" />
                Add filter
              </Button>
            </div>

            {draft.filters.length === 0 ? (
              <div className="rounded-md border border-dashed border-slate-200 bg-slate-50 px-4 py-4 text-[14px] text-slate-600">
                No filters added. The variable will aggregate across all matching records.
              </div>
            ) : (
              <div className="space-y-3">
                {draft.filters.map((filter, index) => (
                  <div key={filter.id} className="rounded-md border border-slate-200 bg-slate-50 px-4 py-4">
                    <ConditionSelectorRow
                      prefixLabel={index === 0 ? "where" : "and"}
                      className="flex flex-wrap items-start gap-3 text-[14px]"
                      leftSelector={{
                        value: filter.fieldKey,
                        options: fieldOptions,
                        groups: fieldGroups,
                        panelPosition: "top",
                        placeholder: "Select a field...",
                        searchPlaceholder: "Search fields",
                        emptyLabel: "No fields matched your search.",
                        invalid: !filter.fieldKey,
                        prefix: "Tt",
                        onChange: (value) =>
                          onChange({
                            ...draft,
                            filters: draft.filters.map((item) =>
                              item.id === filter.id ? { ...item, fieldKey: value } : item
                            ),
                          }),
                      }}
                      operatorSelector={{
                        value: filter.operator,
                        options: filterOperatorOptions,
                        panelPosition: "top",
                        placeholder: "Select an operator...",
                        searchPlaceholder: "Search operators",
                        emptyLabel: "No operators matched your search.",
                        invalid: !filter.operator,
                        onChange: (value) =>
                          onChange({
                            ...draft,
                            filters: draft.filters.map((item) =>
                              item.id === filter.id
                                ? {
                                    ...item,
                                    operator: value as FunctionVariableFilterDraft["operator"],
                                  }
                                : item
                            ),
                          }),
                      }}
                      rightSelector={{
                        value: filter.rightValue,
                        panelPosition: "top",
                        placeholder: "Select or enter a value...",
                        searchPlaceholder: "Enter a filter value",
                        emptyLabel: "No values matched your search.",
                        invalid: !filter.rightValue.trim(),
                        prefix:
                          filter.rightMode === "field"
                            ? "Tt"
                            : filter.rightValue.trim().length > 0
                              ? "#"
                              : "Tt",
                        options: fieldOptions,
                        groups: fieldGroups,
                        searchOptionsBuilder: (searchValue) =>
                          buildFunctionVariableLiteralSearchOptions(searchValue),
                        onChange: (value) =>
                          onChange({
                            ...draft,
                            filters: draft.filters.map((item) =>
                              item.id === filter.id
                                ? {
                                    ...item,
                                    rightValue: value.startsWith("literal:")
                                      ? decodeFunctionVariableLiteralSelection(value)
                                      : value,
                                    rightMode: value.startsWith("literal:") ? "literal" : "field",
                                  }
                                : item
                            ),
                          }),
                      }}
                      onRemove={() =>
                        onChange({
                          ...draft,
                          filters: draft.filters.filter((item) => item.id !== filter.id),
                        })
                      }
                    />
                  </div>
                ))}
              </div>
            )}
          </div>

        </div>

        <div className="flex items-center justify-end gap-3 border-t border-slate-200 px-6 py-5">
          <Button
            variant="outline"
            type="button"
            onClick={onClose}
            className="h-9 rounded-lg border-slate-200 px-4 text-sm shadow-none hover:translate-y-0"
          >
            Cancel
          </Button>
          <Button
            type="button"
            onClick={() => onSave(draft)}
            disabled={!canSave}
            className="h-9 rounded-lg bg-[#2563eb] px-4 text-sm text-white shadow-none hover:translate-y-0 hover:bg-[#1d4ed8]"
          >
            Save
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}
