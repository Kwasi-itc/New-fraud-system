"use client";

import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { ArrowLeft, Check, ChevronDown, ChevronRight, Search } from "lucide-react";

import { cn } from "@/lib/utils";

type OperandOption = {
  value: string;
  label: string;
  keywords?: string[];
  meta?: string;
  isAction?: boolean;
  onSelectAction?: () => void;
};

type OperandOptionGroup = {
  id: string;
  label: string;
  options?: OperandOption[];
  children?: OperandOptionGroup[];
  count?: number;
};

type OperandAction = {
  id: string;
  label: string;
  onSelect: () => void;
};

function countGroupOptions(group: OperandOptionGroup): number {
  return (
    group.count ??
    (group.options?.length ?? 0) +
      (group.children?.reduce((sum, child) => sum + countGroupOptions(child), 0) ?? 0)
  );
}

function filterGroups(
  groups: OperandOptionGroup[],
  options: OperandOption[]
): OperandOptionGroup[] {
  const normalized: OperandOptionGroup[] = [];

  for (const group of groups) {
    const normalizedChildren = filterGroups(group.children ?? [], options);
    const normalizedOptions =
      group.options?.filter((option) =>
        option.isAction || options.some((candidate) => candidate.value === option.value)
      ) ?? [];

    if (normalizedChildren.length === 0 && normalizedOptions.length === 0) {
      continue;
    }

    normalized.push({
      ...group,
      children: normalizedChildren,
      options: normalizedOptions,
      count: countGroupOptions({
        ...group,
        children: normalizedChildren,
        options: normalizedOptions,
      }),
    });
  }

  return normalized;
}

function findGroupTrail(groups: OperandOptionGroup[], path: string[]) {
  const trail: OperandOptionGroup[] = [];
  let current = groups;

  for (const segment of path) {
    const next = current.find((group) => group.id === segment);
    if (!next) {
      break;
    }

    trail.push(next);
    current = next.children ?? [];
  }

  return trail;
}

export function RuleOperandSelector({
  value,
  options,
  placeholder,
  searchPlaceholder,
  emptyLabel,
  disabled = false,
  className,
  prefix,
  invalid = false,
  groups,
  actions = [],
  searchOptionsBuilder,
  onChange,
}: {
  value: string;
  options: OperandOption[];
  placeholder: string;
  searchPlaceholder: string;
  emptyLabel: string;
  disabled?: boolean;
  className?: string;
  prefix?: ReactNode;
  invalid?: boolean;
  groups?: OperandOptionGroup[];
  actions?: OperandAction[];
  searchOptionsBuilder?: (search: string) => OperandOption[];
  onChange: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [activeGroupPath, setActiveGroupPath] = useState<string[]>([]);
  const rootRef = useRef<HTMLDivElement | null>(null);

  const selectedOption = useMemo(
    () => options.find((option) => option.value === value) ?? null,
    [options, value]
  );

  const filteredOptions = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase();
    if (!normalizedSearch) {
      return options;
    }

    return options.filter((option) => {
      const haystacks = [
        option.label,
        option.value,
        option.meta ?? "",
        ...(option.keywords ?? []),
      ];

      return haystacks.some((item) => item.toLowerCase().includes(normalizedSearch));
    });
  }, [options, search]);

  const searchOptions = useMemo(() => {
    const normalizedSearch = search.trim();
    if (!normalizedSearch || !searchOptionsBuilder) {
      return [];
    }

    return searchOptionsBuilder(normalizedSearch);
  }, [search, searchOptionsBuilder]);

  const groupedOptions = useMemo(() => {
    if (!groups || groups.length === 0) {
      return [];
    }

    return filterGroups(groups, options);
  }, [groups, options]);

  const activeGroups = useMemo(
    () => findGroupTrail(groupedOptions, activeGroupPath),
    [activeGroupPath, groupedOptions]
  );
  const activeGroup = activeGroups[activeGroups.length - 1] ?? null;
  const discoveryGroups = activeGroup?.children ?? groupedOptions;
  const discoveryOptions = activeGroup?.options ?? [];

  useEffect(() => {
    if (!open) {
      return;
    }

    function handlePointerDown(event: MouseEvent) {
      if (!rootRef.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    }

    function handleEscape(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setOpen(false);
      }
    }

    window.addEventListener("mousedown", handlePointerDown);
    window.addEventListener("keydown", handleEscape);

    return () => {
      window.removeEventListener("mousedown", handlePointerDown);
      window.removeEventListener("keydown", handleEscape);
    };
  }, [open]);

  useEffect(() => {
    if (!open) {
      setSearch("");
      setActiveGroupPath([]);
    }
  }, [open]);

  return (
    <div ref={rootRef} className={cn("relative", className)}>
      <button
        type="button"
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
        className={cn(
          "group flex min-h-10 w-full items-center justify-between gap-2 rounded-sm border bg-white px-2 text-left text-[13px] outline-none transition",
          invalid
            ? "border-[#ff5a36] text-slate-900 hover:border-[#ff5a36]"
            : "border-slate-300 text-slate-900 hover:border-slate-400 hover:bg-slate-50",
          "disabled:cursor-not-allowed disabled:opacity-50",
          open && "border-[#365fa3] bg-[#eef3ff]"
        )}
      >
        <span className="flex min-w-0 items-center gap-2">
          {prefix ? (
            <span className="inline-flex h-7 min-w-7 items-center justify-center rounded-sm bg-slate-100 px-1 text-[12px] font-semibold text-slate-700">
              {prefix}
            </span>
          ) : null}
          <span className="min-w-0">
            {selectedOption ? (
              <span className="block truncate font-medium text-slate-900">
                {selectedOption.label}
              </span>
            ) : (
              <span className="block truncate text-slate-400">{placeholder}</span>
            )}
            {selectedOption?.meta ? (
              <span className="block truncate text-[11px] text-slate-500">
                {selectedOption.meta}
              </span>
            ) : null}
          </span>
        </span>
        <ChevronDown className="size-4 shrink-0 text-slate-400" />
      </button>

      {open ? (
        <div className="absolute left-0 top-full z-30 mt-1 w-full min-w-[260px] rounded-sm border border-slate-300 bg-white p-2 shadow-[0_18px_50px_rgba(15,23,42,0.12)]">
          <div className="mb-2 flex items-center gap-2 border border-slate-200 bg-slate-50 px-3">
            <Search className="size-4 text-slate-400" />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={searchPlaceholder}
              className="h-10 w-full border-none bg-transparent text-[13px] text-slate-900 outline-none placeholder:text-slate-400"
            />
          </div>

          <div className="max-h-72 space-y-1 overflow-y-auto pr-1">
            {search.trim().length === 0 && groupedOptions.length > 0 ? (
              <>
                {activeGroup ? (
                  <button
                    type="button"
                    onClick={() => setActiveGroupPath((current) => current.slice(0, -1))}
                    className="inline-flex min-h-10 items-center gap-2 rounded-sm px-2 text-[13px] font-medium text-slate-600 transition hover:bg-slate-50"
                  >
                    <ArrowLeft className="size-4" />
                    {activeGroup.label}
                  </button>
                ) : null}

                {discoveryGroups.map((group) => (
                  <button
                    key={group.id}
                    type="button"
                    onClick={() => setActiveGroupPath((current) => [...current, group.id])}
                    className="flex min-h-11 w-full items-center justify-between gap-3 rounded-sm px-3 py-2 text-left text-slate-900 transition hover:bg-slate-50"
                  >
                    <span className="flex min-w-0 items-baseline gap-1">
                      <span className="truncate text-[14px] font-semibold">{group.label}</span>
                      <span className="text-[11px] font-medium text-slate-400">
                        {group.count ?? 0}
                      </span>
                    </span>
                    <ChevronRight className="size-4 shrink-0 text-slate-400" />
                  </button>
                ))}

                {discoveryOptions.map((option) => {
                  const isSelected = option.value === value;

                  return (
                    <button
                      key={option.value}
                      type="button"
                      onClick={() => {
                        if (option.isAction) {
                          option.onSelectAction?.();
                        } else {
                          onChange(option.value);
                        }
                        setSearch("");
                        setOpen(false);
                      }}
                      className={cn(
                        "flex w-full items-start justify-between gap-3 rounded-sm px-3 py-2 text-left transition",
                        isSelected
                          ? "bg-[#edf3ff] text-[#204a8c]"
                          : "text-slate-900 hover:bg-slate-50"
                      )}
                    >
                      <span className="min-w-0">
                        <span className="block truncate text-[13px] font-medium">
                          {option.label}
                        </span>
                        {option.meta ? (
                          <span className="block truncate text-[11px] text-slate-500">
                            {option.meta}
                          </span>
                        ) : null}
                      </span>
                      {isSelected ? <Check className="mt-0.5 size-4 shrink-0" /> : null}
                    </button>
                  );
                })}
              </>
            ) : filteredOptions.length > 0 ? (
              <>
                {search.trim().length > 0 ? (
                  <>
                    {searchOptions.length > 0 ? (
                      <div className="space-y-1">
                        {searchOptions.map((option) => (
                          <button
                            key={option.value}
                            type="button"
                            onClick={() => {
                              if (option.isAction) {
                                option.onSelectAction?.();
                              } else {
                                onChange(option.value);
                              }
                              setSearch("");
                              setOpen(false);
                            }}
                            className="flex w-full items-start justify-between gap-3 rounded-sm px-3 py-2 text-left text-slate-900 transition hover:bg-slate-50"
                          >
                            <span className="min-w-0">
                              <span className="block truncate text-[13px] font-medium">
                                {option.label}
                              </span>
                              {option.meta ? (
                                <span className="block truncate text-[11px] text-slate-500">
                                  {option.meta}
                                </span>
                              ) : null}
                            </span>
                          </button>
                        ))}
                      </div>
                    ) : null}
                    <div className="flex min-h-10 items-center gap-1 px-2 py-1">
                      <div className="flex w-full items-baseline gap-1">
                        <div className="text-[14px] font-semibold text-slate-700">Results</div>
                        <div className="text-[11px] font-medium text-slate-400">
                          {filteredOptions.length}
                        </div>
                      </div>
                    </div>
                  </>
                ) : null}
                {filteredOptions.map((option) => {
                  const isSelected = option.value === value;

                  return (
                    <button
                      key={option.value}
                      type="button"
                      onClick={() => {
                        if (option.isAction) {
                          option.onSelectAction?.();
                        } else {
                          onChange(option.value);
                        }
                        setSearch("");
                        setOpen(false);
                      }}
                      className={cn(
                        "flex w-full items-start justify-between gap-3 rounded-sm px-3 py-2 text-left transition",
                        isSelected
                          ? "bg-[#edf3ff] text-[#204a8c]"
                          : "text-slate-900 hover:bg-slate-50"
                      )}
                    >
                      <span className="min-w-0">
                        <span className="block truncate text-[13px] font-medium">
                          {option.label}
                        </span>
                        {option.meta ? (
                          <span className="block truncate text-[11px] text-slate-500">
                            {option.meta}
                          </span>
                        ) : null}
                      </span>
                      {isSelected ? <Check className="mt-0.5 size-4 shrink-0" /> : null}
                    </button>
                  );
                })}
              </>
            ) : (
              <div className="border border-dashed border-slate-200 px-3 py-5 text-center text-[13px] text-slate-500">
                {emptyLabel}
              </div>
            )}
          </div>

          {actions.length > 0 ? (
            <div className="mt-2 flex gap-2 overflow-x-auto border-t border-slate-200 pt-2">
              {actions.map((action) => (
                <button
                  key={action.id}
                  type="button"
                  onClick={() => {
                    action.onSelect();
                    setSearch("");
                    setOpen(false);
                  }}
                  className="inline-flex h-9 items-center rounded-sm border border-slate-300 bg-white px-3 text-[13px] font-medium text-slate-700 transition hover:bg-slate-50"
                >
                  {action.label}
                </button>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
