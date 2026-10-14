"use client";

import { useEffect, useRef, useState } from "react";
import { CheckCircle2, AlertCircle, X, Layers } from "lucide-react";
import type { CaseRecord, Inbox } from "@/lib/case-manager-api";
import { useCaseWrite } from "@/lib/case-manager-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ClosingFindings, Problem, fieldClass, labelClass } from "./shared";
import { AssigneeSearchDropdown } from "./assignee-search-dropdown";
import { cn } from "@/lib/utils";

type Result = { case_id: string; success: boolean; replayed: boolean; error?: string };

export function BulkActionsModal({
  tenant,
  session,
  cases,
  selectedCaseIds = [],
  inboxes,
  onClose,
  onRemoveCaseId,
  onClearSelection,
}: {
  tenant: string;
  session: string;
  cases: CaseRecord[];
  selectedCaseIds?: string[];
  inboxes: Inbox[];
  onClose: () => void;
  onRemoveCaseId?: (id: string) => void;
  onClearSelection?: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const write = useCaseWrite(tenant, session);
  const [action, setAction] = useState("assign");
  const [pending, setPending] = useState<Record<string, unknown>>();
  const [results, setResults] = useState<Result[]>();
  const [internalSelectedIds, setInternalSelectedIds] = useState<string[]>(
    selectedCaseIds.length > 0 ? selectedCaseIds : cases.slice(0, 100).map((c) => c.id)
  );

  useEffect(() => {
    if (selectedCaseIds.length > 0) {
      setInternalSelectedIds(selectedCaseIds);
    }
  }, [selectedCaseIds]);

  useEffect(() => {
    const el = dialog.current;
    const previous = document.activeElement as HTMLElement | null;
    el?.showModal();
    return () => {
      el?.close();
      previous?.focus();
    };
  }, []);

  async function run(payload: Record<string, unknown>) {
    setPending(payload);
    setResults(undefined);
    try {
      const response = (await write.mutateAsync({
        path: "case-bulk",
        payload,
      })) as { results: Result[] };
      setResults(response.results);
    } catch {
      /* Keep the operation ID for safe retry; errors appear in Problem component. */
    }
  }

  const selectedCases = internalSelectedIds.map(
    (id) =>
      cases.find((c) => c.id === id) ??
      ({ id, name: id, status: "active" } as unknown as CaseRecord)
  );
  const activeInboxes = inboxes.filter((i) => i.status === "active");

  function handleRemove(id: string) {
    setInternalSelectedIds((prev) => prev.filter((item) => item !== id));
    onRemoveCaseId?.(id);
  }

  function handleClearAll() {
    setInternalSelectedIds([]);
    onClearSelection?.();
  }

  return (
    <dialog
      ref={dialog}
      aria-labelledby="bulk-actions-modal-title"
      aria-describedby="bulk-actions-modal-description"
      onCancel={(event) => {
        event.preventDefault();
        if (!write.isPending) onClose();
      }}
      className="m-auto w-[min(44rem,calc(100vw-2rem))] max-h-[90vh] overflow-y-auto rounded-2xl border border-slate-200 bg-white p-0 shadow-2xl backdrop:bg-slate-950/40 backdrop:backdrop-blur-xs"
    >
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          const submittedCaseIds = data.getAll("cases").map(String);
          const finalCaseIds = submittedCaseIds.length > 0 ? submittedCaseIds : internalSelectedIds;
          void run({
            operation_id: crypto.randomUUID(),
            case_ids: finalCaseIds,
            action,
            assignee: data.get("assignee") || null,
            inbox_id: data.get("inbox") || null,
            outcome: data.get("outcome") || "unset",
            comment: data.get("comment") || "",
          });
        }}
      >
        {/* Header */}
        <div className="flex items-start justify-between border-b border-slate-100 px-6 py-5">
          <div className="flex items-center gap-3">
            <div className="flex size-10 items-center justify-center rounded-xl bg-blue-50 text-blue-700">
              <Layers className="size-5" />
            </div>
            <div>
              <h2
                id="bulk-actions-modal-title"
                className="text-xl font-bold tracking-tight text-slate-900"
              >
                Bulk actions
              </h2>
              <p
                id="bulk-actions-modal-description"
                className="mt-0.5 text-sm text-slate-500"
              >
                Apply an action across selected cases. Each case is verified independently.
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            disabled={write.isPending}
            className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700 transition"
            aria-label="Close dialog"
          >
            <X className="size-5" />
          </button>
        </div>

        {/* Form Body */}
        <div className="space-y-5 p-6">
          <Problem error={write.error} />

          {/* Selected Cases Section */}
          <div>
            <div className="mb-2 flex items-center justify-between">
              <span className="text-sm font-semibold uppercase tracking-wider text-slate-700">
                {internalSelectedIds.length} selected {internalSelectedIds.length === 1 ? "case" : "cases"}
              </span>
              {internalSelectedIds.length > 0 && (
                <button
                  type="button"
                  onClick={handleClearAll}
                  className="text-sm font-semibold text-blue-700 hover:underline"
                >
                  Clear all
                </button>
              )}
            </div>

            {/* Accessible and keyboard selectable cases listbox */}
            <label className="block">
              <span className="sr-only">Selected cases</span>
              <select
                name="cases"
                multiple
                required
                aria-label="Selected cases"
                size={Math.min(5, Math.max(2, internalSelectedIds.length || 1))}
                value={internalSelectedIds}
                onChange={(e) => {
                  const selectedValues = Array.from(e.target.selectedOptions, (opt) => opt.value);
                  setInternalSelectedIds(selectedValues);
                }}
                className={cn(fieldClass, "h-auto py-2 text-sm font-normal normal-case rounded-xl")}
                disabled={write.isPending}
              >
                {selectedCases.map((item) => (
                  <option key={item.id} value={item.id} className="py-1 px-2 text-sm">
                    {item.name} ({item.status})
                  </option>
                ))}
              </select>
            </label>
          </div>

          {/* Action Configuration */}
          <div className="space-y-4 rounded-xl border border-slate-200/80 bg-slate-50/50 p-4">
            <label className={labelClass}>
              <span>Bulk action</span>
              <select
                value={action}
                disabled={write.isPending}
                onChange={(event) => setAction(event.target.value)}
                className={cn(fieldClass, "font-normal normal-case text-sm rounded-xl bg-white")}
              >
                <option value="assign">Assign / unassign</option>
                <option value="move">Move</option>
                <option value="close">Close</option>
                <option value="reopen">Reopen</option>
              </select>
            </label>

            {action === "assign" && (
              <AssigneeSearchDropdown
                label="Assignee subject (blank unassigns)"
                name="assignee"
                users={Array.from(
                  new Set(
                    cases.map((c) => c.assigned_to).filter(Boolean) as string[]
                  )
                )}
                disabled={write.isPending}
              />
            )}

            {action === "move" && (
              <label className={labelClass}>
                <span>Destination inbox</span>
                <select
                  name="inbox"
                  required
                  disabled={write.isPending}
                  className={cn(fieldClass, "font-normal normal-case text-sm rounded-xl bg-white")}
                >
                  <option value="">Select destination inbox</option>
                  {activeInboxes.map((i) => (
                    <option value={i.id} key={i.id}>
                      {i.name}
                    </option>
                  ))}
                </select>
              </label>
            )}

            {action === "close" && (
              <div className="space-y-4">
                <label className={labelClass}>
                  <span>Bulk closing outcome</span>
                  <select
                    name="outcome"
                    required
                    disabled={write.isPending}
                    className={cn(fieldClass, "font-normal normal-case text-sm rounded-xl bg-white")}
                  >
                    <option value="false_positive">False positive</option>
                    <option value="valuable_alert">Valuable alert</option>
                    <option value="confirmed_risk">Confirmed risk</option>
                  </select>
                </label>
                <ClosingFindings label="Bulk closing findings" />
              </div>
            )}
          </div>

          {/* Results Reporting */}
          {results && (
            <ul
              aria-live="polite"
              className="mt-3 space-y-2 rounded-xl border border-slate-200 bg-slate-50/70 p-4 text-sm"
            >
              {results.map((result) => (
                <li
                  key={result.case_id}
                  className={cn(
                    "flex items-center gap-2 text-sm",
                    result.success ? "text-slate-800" : "font-medium text-rose-700"
                  )}
                >
                  {result.success ? (
                    <CheckCircle2 className="size-4 shrink-0 text-emerald-600" />
                  ) : (
                    <AlertCircle className="size-4 shrink-0 text-rose-600" />
                  )}
                  <span>
                    {cases.find((c) => c.id === result.case_id)?.name ?? result.case_id}:{" "}
                    {result.success
                      ? result.replayed
                        ? "Already completed"
                        : "Completed"
                      : result.error}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>

        {/* Modal Footer */}
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-slate-100 bg-slate-50/70 px-6 py-4">
          <div>
            {pending && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={write.isPending}
                onClick={() => void run(pending)}
                className="text-sm rounded-xl"
              >
                Retry same operation
              </Button>
            )}
          </div>
          <div className="flex items-center gap-3">
            <Button
              type="button"
              variant="outline"
              disabled={write.isPending}
              onClick={onClose}
              className="text-sm rounded-xl"
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={write.isPending || !internalSelectedIds.length}
              className="text-sm font-semibold rounded-xl bg-blue-600 hover:bg-blue-700 text-white shadow-none px-4"
            >
              {write.isPending ? "Applying…" : "Apply bulk action"}
            </Button>
          </div>
        </div>
      </form>
    </dialog>
  );
}

export { BulkActionsModal as BulkActions };
