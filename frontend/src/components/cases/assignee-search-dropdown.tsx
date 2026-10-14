"use client";

import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, User, X } from "lucide-react";
import { labelClass } from "./shared";
import { cn } from "@/lib/utils";

export function AssigneeSearchDropdown({
  label = "Assignee ID",
  name = "assignee_id",
  defaultValue = "",
  value,
  onChange,
  users = [],
  currentUserId,
  disabled = false,
  className,
}: {
  label?: string;
  name?: string;
  defaultValue?: string;
  value?: string;
  onChange?: (val: string) => void;
  users?: string[];
  currentUserId?: string;
  disabled?: boolean;
  className?: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const [isOpen, setIsOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState(value ?? defaultValue ?? "");

  // Sync state if controlled value or defaultValue changes
  useEffect(() => {
    if (value !== undefined) {
      setSearchQuery(value);
    } else if (defaultValue !== undefined) {
      setSearchQuery(defaultValue);
    }
  }, [value, defaultValue]);

  // Handle clicking outside to dismiss the dropdown
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (
        containerRef.current &&
        !containerRef.current.contains(event.target as Node)
      ) {
        setIsOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  // Consolidate unique list of users
  const allUserIds = Array.from(
    new Set([
      ...(currentUserId ? [currentUserId] : []),
      ...users,
      ...(searchQuery.trim() ? [searchQuery.trim()] : []),
    ])
  ).filter(Boolean);

  // Filter based on search query
  const queryLower = searchQuery.toLowerCase().trim();
  const filteredUsers = allUserIds.filter((id) =>
    id.toLowerCase().includes(queryLower)
  );

  function handleSelect(id: string) {
    setSearchQuery(id);
    onChange?.(id);
    setIsOpen(false);
    if (inputRef.current) {
      inputRef.current.value = id;
      // Trigger native input and change events for form tracking
      inputRef.current.dispatchEvent(new Event("input", { bubbles: true }));
      inputRef.current.dispatchEvent(new Event("change", { bubbles: true }));
    }
  }

  function handleClear() {
    setSearchQuery("");
    onChange?.("");
    setIsOpen(false);
    if (inputRef.current) {
      inputRef.current.value = "";
      inputRef.current.dispatchEvent(new Event("input", { bubbles: true }));
      inputRef.current.dispatchEvent(new Event("change", { bubbles: true }));
    }
  }

  return (
    <div ref={containerRef} className={cn("relative space-y-1.5", className)}>
      <label className={cn(labelClass, "mb-0")}>
        <span>{label}</span>
      </label>

      <div className="relative">
        <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-slate-400">
          <User className="size-4" />
        </div>

        <input
          ref={inputRef}
          type="text"
          id={name}
          name={name}
          aria-label={label}
          disabled={disabled}
          value={searchQuery}
          placeholder="Search or pick user…"
          autoComplete="off"
          onFocus={() => setIsOpen(true)}
          onChange={(e) => {
            const next = e.target.value;
            setSearchQuery(next);
            onChange?.(next);
            setIsOpen(true);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              setIsOpen(false);
            } else if (e.key === "ArrowDown" && !isOpen) {
              setIsOpen(true);
            }
          }}
          className="h-10 w-full rounded-xl border border-slate-300 bg-white pl-9 pr-10 text-sm font-normal normal-case text-slate-900 placeholder:text-slate-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 disabled:cursor-not-allowed disabled:bg-slate-50"
        />

        <div className="absolute inset-y-0 right-0 flex items-center pr-2">
          {searchQuery && !disabled && (
            <button
              type="button"
              onClick={handleClear}
              className="mr-1 rounded-md p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-600 transition"
              aria-label="Clear assignee"
            >
              <X className="size-3.5" />
            </button>
          )}
          <button
            type="button"
            disabled={disabled}
            onClick={() => setIsOpen((prev) => !prev)}
            className="rounded-md p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-600 transition"
            aria-label="Toggle user dropdown"
            tabIndex={-1}
          >
            <ChevronDown
              className={cn("size-4 transition-transform", isOpen ? "rotate-180" : "")}
            />
          </button>
        </div>
      </div>

      {/* Popover Dropdown */}
      {isOpen && (
        <div className="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-xl border border-slate-200 bg-white p-1 shadow-lg shadow-slate-950/10 backdrop-blur-xs">
          {/* Quick Clear / Unassign Item */}
          <button
            type="button"
            onClick={handleClear}
            className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-left text-sm font-medium text-slate-600 hover:bg-slate-50 transition"
          >
            <span className="flex items-center gap-2">
              <span className="italic">Unassigned / Clear</span>
            </span>
            {!searchQuery && <Check className="size-4 text-blue-600" />}
          </button>

          {/* User List */}
          {filteredUsers.length > 0 ? (
            <div className="border-t border-slate-100 pt-1">
              {filteredUsers.map((userId) => {
                const isCurrent = userId === currentUserId;
                const isSelected = userId === searchQuery;
                return (
                  <button
                    key={userId}
                    type="button"
                    onClick={() => handleSelect(userId)}
                    className={cn(
                      "flex w-full items-center justify-between rounded-lg px-3 py-2 text-left text-sm transition",
                      isSelected
                        ? "bg-blue-50/70 font-semibold text-blue-900"
                        : "text-slate-900 hover:bg-slate-50"
                    )}
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <User className="size-4 shrink-0 text-slate-400" />
                      <span className="truncate">{userId}</span>
                      {isCurrent && (
                        <span className="rounded-full bg-blue-100 px-2 py-0.5 text-sm font-semibold text-blue-700">
                          You
                        </span>
                      )}
                    </div>
                    {isSelected && (
                      <Check className="size-4 shrink-0 text-blue-600" />
                    )}
                  </button>
                );
              })}
            </div>
          ) : searchQuery.trim() ? (
            <div className="border-t border-slate-100 pt-1">
              <button
                type="button"
                onClick={() => handleSelect(searchQuery.trim())}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm text-slate-700 hover:bg-slate-50 transition"
              >
                <User className="size-4 text-slate-400 shrink-0" />
                <span>
                  Use ID: <strong className="text-slate-900">{searchQuery.trim()}</strong>
                </span>
              </button>
            </div>
          ) : (
            <div className="p-3 text-center text-sm text-slate-500 italic">
              No users available
            </div>
          )}
        </div>
      )}
    </div>
  );
}
