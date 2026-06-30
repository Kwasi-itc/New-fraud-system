"use client";

import { type ReactNode } from "react";
import { Trash2 } from "lucide-react";

import { RuleOperandSelector } from "@/components/detection/rule-operand-selector";

type SelectorOption = {
  value: string;
  label: string;
  keywords?: string[];
  meta?: string;
  sideLabel?: string;
  isAction?: boolean;
  onSelectAction?: () => void;
};

type SelectorGroup = {
  id: string;
  label: string;
  options?: SelectorOption[];
  children?: SelectorGroup[];
  count?: number;
};

type SelectorConfig = {
  value: string;
  options: SelectorOption[];
  groups?: SelectorGroup[];
  actions?: Array<{ id: string; label: string; onSelect: () => void }>;
  panelPosition?: "bottom" | "top";
  placeholder: string;
  searchPlaceholder: string;
  emptyLabel: string;
  invalid?: boolean;
  prefix?: ReactNode;
  selectedMeta?: string;
  className?: string;
  searchOptionsBuilder?: (search: string) => SelectorOption[];
  onChange: (value: string) => void;
};

export function ConditionSelectorRow({
  prefixLabel,
  leftSelector,
  operatorSelector,
  rightSelector,
  rightContent,
  onRemove,
  disabled = false,
  className,
  prefixClassName,
  removeButtonClassName,
}: {
  prefixLabel: ReactNode;
  leftSelector: SelectorConfig;
  operatorSelector: SelectorConfig;
  rightSelector?: SelectorConfig | null;
  rightContent?: ReactNode;
  onRemove: () => void;
  disabled?: boolean;
  className?: string;
  prefixClassName?: string;
  removeButtonClassName?: string;
}) {
  return (
    <div className={className ?? "flex flex-wrap items-center gap-2.5 text-[14px]"}>
      <span
        className={
          prefixClassName ??
          "inline-flex h-12 items-center justify-center rounded-sm bg-slate-50 px-3 text-[16px] font-medium text-slate-600"
        }
      >
        {prefixLabel}
      </span>
      <RuleOperandSelector
        className={leftSelector.className ?? "min-w-[220px] max-w-[280px]"}
        disabled={disabled}
        value={leftSelector.value}
        options={leftSelector.options}
        groups={leftSelector.groups}
        placeholder={leftSelector.placeholder}
        searchPlaceholder={leftSelector.searchPlaceholder}
        emptyLabel={leftSelector.emptyLabel}
        invalid={leftSelector.invalid}
        prefix={leftSelector.prefix}
        selectedMeta={leftSelector.selectedMeta}
        actions={leftSelector.actions}
        panelPosition={leftSelector.panelPosition}
        searchOptionsBuilder={leftSelector.searchOptionsBuilder}
        onChange={leftSelector.onChange}
      />
      <RuleOperandSelector
        className={operatorSelector.className ?? "min-w-[120px] max-w-[170px]"}
        disabled={disabled}
        value={operatorSelector.value}
        options={operatorSelector.options}
        groups={operatorSelector.groups}
        placeholder={operatorSelector.placeholder}
        searchPlaceholder={operatorSelector.searchPlaceholder}
        emptyLabel={operatorSelector.emptyLabel}
        invalid={operatorSelector.invalid}
        prefix={operatorSelector.prefix}
        selectedMeta={operatorSelector.selectedMeta}
        actions={operatorSelector.actions}
        panelPosition={operatorSelector.panelPosition}
        searchOptionsBuilder={operatorSelector.searchOptionsBuilder}
        onChange={operatorSelector.onChange}
      />
      {rightContent ? (
        rightContent
      ) : rightSelector ? (
        <RuleOperandSelector
          className={rightSelector.className ?? "min-w-[220px] max-w-[280px]"}
          disabled={disabled}
          value={rightSelector.value}
          options={rightSelector.options}
          groups={rightSelector.groups}
          placeholder={rightSelector.placeholder}
          searchPlaceholder={rightSelector.searchPlaceholder}
          emptyLabel={rightSelector.emptyLabel}
          invalid={rightSelector.invalid}
          prefix={rightSelector.prefix}
          selectedMeta={rightSelector.selectedMeta}
          actions={rightSelector.actions}
          panelPosition={rightSelector.panelPosition}
          searchOptionsBuilder={rightSelector.searchOptionsBuilder}
          onChange={rightSelector.onChange}
        />
      ) : null}
      <button
        type="button"
        disabled={disabled}
        onClick={onRemove}
        className={
          removeButtonClassName ??
          "inline-flex size-7 shrink-0 items-center justify-center rounded-sm border border-slate-200 bg-white text-slate-400 transition hover:border-red-200 hover:bg-red-50 hover:text-red-700 disabled:cursor-not-allowed disabled:opacity-50"
        }
      >
        <Trash2 className="size-3.5" />
      </button>
    </div>
  );
}
