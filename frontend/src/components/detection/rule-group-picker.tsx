"use client";

import { useMemo, useState } from "react";
import { Check, Pencil, Plus, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

export function RuleGroupPicker({
  selectedRuleGroup,
  ruleGroups,
  disabled = false,
  onChange,
}: {
  selectedRuleGroup?: string;
  ruleGroups: string[];
  disabled?: boolean;
  onChange?: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [draftValue, setDraftValue] = useState("");

  const finalRuleGroups = useMemo(() => {
    return [...new Set([selectedRuleGroup, ...ruleGroups].filter(Boolean))].sort((a, b) =>
      a.localeCompare(b)
    ) as string[];
  }, [ruleGroups, selectedRuleGroup]);

  return (
    <div className="relative">
      <div className="flex items-center gap-2">
        {selectedRuleGroup ? (
          <span className="inline-flex items-center gap-2 rounded-full bg-[#eef3ff] px-3 py-1 text-[12px] font-medium text-[#365fa3]">
            {selectedRuleGroup}
            {!disabled ? (
              <button
                type="button"
                onClick={() => onChange?.("")}
                className="text-[#365fa3] transition hover:opacity-70"
              >
                <X className="size-3.5" />
              </button>
            ) : null}
          </span>
        ) : null}
        <Button
          type="button"
          variant="outline"
          disabled={disabled}
          onClick={() => setOpen((current) => !current)}
          className={cn(
            "h-9 rounded-xl border-slate-200 px-3 text-[13px] shadow-none",
            selectedRuleGroup ? "w-9 p-0" : "gap-2"
          )}
        >
          {selectedRuleGroup ? <Pencil className="size-4" /> : <Plus className="size-4" />}
          {!selectedRuleGroup ? "Add group" : null}
        </Button>
      </div>

      {open ? (
        <div className="absolute left-0 top-full z-20 mt-2 w-[320px] rounded-xl border border-slate-200 bg-white p-3 shadow-[0_18px_50px_rgba(15,23,42,0.12)]">
          <div className="space-y-3">
            <Input
              value={draftValue}
              onChange={(event) => setDraftValue(event.target.value)}
              placeholder="Create or search a rule group"
              className="h-10 rounded-xl border-slate-200 shadow-none"
            />

            {draftValue.trim() && !finalRuleGroups.includes(draftValue.trim()) ? (
              <button
                type="button"
                onClick={() => {
                  onChange?.(draftValue.trim());
                  setDraftValue("");
                  setOpen(false);
                }}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2.5 text-left text-[13px] text-slate-950 hover:bg-slate-50"
              >
                <Plus className="size-4 text-slate-500" />
                <span>Create group &quot;{draftValue.trim()}&quot;</span>
              </button>
            ) : null}

            <div className="space-y-1">
              {finalRuleGroups.length > 0 ? (
                finalRuleGroups.map((group) => {
                  const isSelected = selectedRuleGroup === group;

                  return (
                    <button
                      key={group}
                      type="button"
                      onClick={() => {
                        onChange?.(group);
                        setDraftValue("");
                        setOpen(false);
                      }}
                      className={cn(
                        "flex w-full items-center justify-between rounded-lg px-3 py-2.5 text-left text-[13px]",
                        isSelected
                          ? "bg-[#eef3ff] text-[#365fa3]"
                          : "text-slate-950 hover:bg-slate-50"
                      )}
                    >
                      <span className="inline-flex rounded-full bg-[#eef3ff] px-2.5 py-1 text-[12px] font-medium text-[#365fa3]">
                        {group}
                      </span>
                      {isSelected ? <Check className="size-4" /> : null}
                    </button>
                  );
                })
              ) : (
                <div className="rounded-lg border border-dashed border-slate-200 px-3 py-4 text-[13px] text-slate-500">
                  No rule groups yet.
                </div>
              )}
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
