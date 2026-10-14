"use client";

import { useId, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, ShieldCheck, UserCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { caseKeys } from "@/lib/case-manager-query";
import { fieldClass, Problem, readable } from "./shared";
import { StatusPill } from "./structured-data-viewer";
import { cn } from "@/lib/utils";

type ReviewState = {
  status: string;
  version: string;
  eligible: boolean;
  machine_outcome?: string;
  actions: string[] ;
};

export function InvestigatorReview({
  tenant,
  session,
  caseId,
  kind,
  resource,
}: {
  tenant: string;
  session: string;
  caseId: string;
  kind: "decisions" | "matches";
  resource: string;
}) {
  const label = useId();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [action, setAction] = useState("");
  const [findings, setFindings] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [saved, setSaved] = useState(false);
  const operation = useRef<{ signature: string; id: string } | null>(null);
  const path = `/api/case-review/${tenant}/${caseId}/${kind}`;

  const query = useQuery<ReviewState>({
    queryKey: [...caseKeys.scope(tenant, session), caseId, "review", kind, resource],
    enabled: open,
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    staleTime: 0,
    gcTime: 0,
    queryFn: async () => {
      const response = await fetch(`${path}?resource_id=${encodeURIComponent(resource)}`, {
        cache: "no-store",
      });
      const data = await response.json();
      if (!response.ok) throw new Error(data.error ?? "Review is unavailable.");
      return data;
    },
  });

  return (
    <div className="space-y-3">
      <Button
        size="sm"
        variant="outline"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        className="gap-1.5 text-sm font-medium border-slate-200 hover:bg-slate-50 transition-colors"
      >
        <UserCheck className="size-3.5 text-blue-600" />
        {kind === "decisions" ? "Review decision" : "Review match"}
      </Button>

      {open && (
        <div className="rounded-xl border border-slate-200 bg-white p-4 space-y-3 shadow-xs">
          <Problem error={query.error} retry={() => void query.refetch()} />
          {query.isPending && (
            <div className="flex items-center gap-2 py-2 text-sm text-slate-500">
              <span className="inline-block size-3 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
              <p role="status">Loading review state…</p>
            </div>
          )}

          {saved && (
            <p
              role="status"
              className="rounded-lg bg-emerald-50 p-2.5 text-sm font-medium text-emerald-800 border border-emerald-200 flex items-center gap-1.5"
            >
              <CheckCircle2 className="size-3.5 text-emerald-600" />
              Review recorded with case history.
            </p>
          )}

          {query.isSuccess && !query.error && (
            <div className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 pb-2">
                <p className="text-sm font-semibold text-slate-800">
                  Review status: {readable(query.data.status)}
                </p>
                {query.data.machine_outcome && (
                  <span className="text-sm text-slate-500">
                    Machine outcome: <strong className="text-slate-800">{readable(query.data.machine_outcome)}</strong>
                  </span>
                )}
              </div>

              {!query.data.eligible ? (
                <p className="text-sm text-slate-500 italic">
                  No first-review action is available. Reviews require an open case in an active inbox and an eligible pending resource.
                </p>
              ) : (
                <form
                  className="space-y-3 pt-1"
                  onSubmit={async (event) => {
                    event.preventDefault();
                    setError(null);
                    if (
                      !action ||
                      !findings.trim() ||
                      new TextEncoder().encode(findings.trim()).length > 10000
                    ) {
                      setError(
                        new Error("Choose an action and enter findings of 1–10000 UTF-8 bytes.")
                      );
                      return;
                    }
                    if (!confirm) {
                      setConfirm(true);
                      return;
                    }
                    const payload = {
                      resource_id: resource,
                      expected_version: query.data.version,
                      action,
                      findings: findings.trim(),
                    };
                    const signature = JSON.stringify(payload);
                    if (operation.current?.signature !== signature)
                      operation.current = { signature, id: crypto.randomUUID() };
                    setBusy(true);
                    try {
                      const response = await fetch(path, {
                        method: "POST",
                        headers: { "Content-Type": "application/json" },
                        body: JSON.stringify({ ...payload, operation_id: operation.current.id }),
                      });
                      const data = await response.json();
                      if (!response.ok) throw new Error(data.error ?? "Review failed.");
                      setSaved(true);
                      setConfirm(false);
                      await client.invalidateQueries({
                        queryKey: caseKeys.scope(tenant, session),
                      });
                    } catch (err) {
                      setError(
                        err instanceof Error
                          ? err
                          : new Error("Review response unavailable. Retry the same operation.")
                      );
                    } finally {
                      setBusy(false);
                    }
                  }}
                >
                  <fieldset disabled={busy} className="space-y-3">
                    <label className="block text-sm font-semibold uppercase tracking-wider text-slate-600" htmlFor={`${label}-action`}>
                      Review action
                    </label>
                    <select
                      id={`${label}-action`}
                      className={cn(fieldClass, "font-normal normal-case")}
                      required
                      value={action}
                      onChange={(event) => {
                        setAction(event.target.value);
                        setConfirm(false);
                      }}
                    >
                      <option value="">Choose an action</option>
                      {query.data.actions.map((value) => (
                        <option key={value} value={value}>
                          {readable(value)}
                        </option>
                      ))}
                    </select>

                    <label className="block text-sm font-semibold uppercase tracking-wider text-slate-600" htmlFor={`${label}-findings`}>
                      Review findings
                    </label>
                    <textarea
                      id={`${label}-findings`}
                      className={cn(fieldClass, "font-normal normal-case")}
                      required
                      rows={3}
                      placeholder="Document human investigation findings and reasoning..."
                      value={findings}
                      onChange={(event) => {
                        setFindings(event.target.value);
                        setConfirm(false);
                      }}
                    />

                    <Problem error={error} />
                    {error && (
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          setConfirm(false);
                          setError(null);
                          void query.refetch();
                        }}
                        className="text-sm"
                      >
                        Refresh review state
                      </Button>
                    )}

                    {confirm && (
                      <p role="status" className="rounded-lg bg-blue-50 p-2.5 text-sm font-medium text-blue-900 border border-blue-200">
                        Confirm {readable(action)}. This first review cannot be edited here.
                      </p>
                    )}

                    <Button type="submit" size="sm" className="text-sm font-medium">
                      {busy ? "Recording…" : confirm ? "Confirm review" : "Check review"}
                    </Button>
                  </fieldset>
                </form>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
