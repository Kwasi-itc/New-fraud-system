"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Shield, X, FolderPlus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  CaseApiError,
  writeCase,
  type CaseRecord,
  type CaseSession,
  type Inbox,
} from "@/lib/case-manager-api";
import { caseKeys, useCaseQuery } from "@/lib/case-manager-query";
import type { Decision } from "@/lib/decision-engine-api";
import { StatusPill } from "./structured-data-viewer";
import { cn } from "@/lib/utils";

type Membership = {
  decision_id: string;
  state: "unlinked" | "linked" | "unavailable";
  case_id?: string;
  case_name?: string;
  case_status?: string;
};
type CreateRequest = {
  operation_id: string;
  name: string;
  inbox_id: string;
  type: "decision";
  decision_ids: string[];
};

export function useDecisionSelection(
  tenant: string,
  session: string,
  decisions: Decision[],
  scope: string,
  loading: boolean
) {
  const identity = useCaseQuery<CaseSession>(tenant, session, "case-session");
  const ids = decisions.map((d) => d.id);
  const memberships = useQuery({
    queryKey: [...caseKeys.scope(tenant, session), "decision-memberships", ids],
    queryFn: () =>
      writeCase<{ decisions: Membership[] }>(tenant, "decision-case-memberships", {
        decision_ids: ids,
      }),
    enabled: identity.isSuccess && ids.length > 0 && !loading,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  });
  const key = JSON.stringify([tenant, session, scope]);
  const [selection, setSelection] = useState<{ key: string; ids: string[] }>({
    key,
    ids: [],
  });
  if (selection.key !== key) setSelection({ key, ids: [] });
  const byID = new Map(
    memberships.data?.decisions.map((item) => [item.decision_id, item])
  );
  const eligible =
    loading || memberships.isError || !identity.isSuccess
      ? []
      : ids.filter((id) => byID.get(id)?.state === "unlinked");
  const selectedIDs =
    selection.key === key ? selection.ids.filter((id) => eligible.includes(id)) : [];
  const select = (next: string[]) => setSelection({ key, ids: next });

  return {
    identity,
    memberships,
    selected: decisions.filter((d) => selectedIDs.includes(d.id)),
    eligible,
    selectedIDs,
    clear: () => select([]),
    toggle: (id: string) =>
      select(
        selectedIDs.includes(id)
          ? selectedIDs.filter((value) => value !== id)
          : [...selectedIDs, id]
      ),
    all: () => select(selectedIDs.length === eligible.length ? [] : eligible),
    cell: (id: string) => {
      const entry = byID.get(id);
      if (!identity.isSuccess || memberships.isError)
        return <span className="text-slate-400 text-sm">Case access unavailable</span>;
      if (!entry) return <span className="text-slate-400 text-sm">Checking…</span>;
      if (entry.state === "linked")
        return (
          <Link
            className="text-sm font-medium text-blue-700 hover:underline"
            href={`/cases/${entry.case_id}?${new URLSearchParams({ tenant })}`}
          >
            {entry.case_name} · {entry.case_status}
          </Link>
        );
      return (
        <span className="text-sm text-slate-500">
          {entry.state === "unlinked" ? "No case" : "Unavailable"}
        </span>
      );
    },
  };
}

export function SelectPageCheckbox({
  checked,
  mixed,
  disabled,
  onChange,
  ariaLabel = "Select eligible decisions on this page",
}: {
  checked: boolean;
  mixed: boolean;
  disabled: boolean;
  onChange: () => void;
  ariaLabel?: string;
}) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = mixed;
  }, [mixed]);
  return (
    <input
      ref={ref}
      type="checkbox"
      className="size-4 rounded border-slate-300 accent-blue-700 text-blue-600 focus:ring-blue-500 cursor-pointer"
      aria-label={ariaLabel}
      checked={checked}
      disabled={disabled}
      onChange={onChange}
    />
  );
}

export function CreateDecisionCaseModal({
  tenant,
  session,
  decisions,
  scenarioNames,
  onClose,
}: {
  tenant: string;
  session: string;
  decisions: Decision[];
  scenarioNames: Map<string, string>;
  onClose: (created?: boolean) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const router = useRouter();
  const client = useQueryClient();
  const inboxes = useCaseQuery<{ inboxes: Inbox[] }>(tenant, session, "inboxes");
  const [selected, setSelected] = useState(decisions);
  const [name, setName] = useState("");
  const [inbox, setInbox] = useState("");
  const [request, setRequest] = useState<CreateRequest | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const inFlight = useRef(false);
  const activeInboxes = inboxes.isSuccess
    ? inboxes.data.inboxes.filter((item) => item.status === "active")
    : [];

  useEffect(() => {
    const element = dialog.current;
    const previous = document.activeElement as HTMLElement | null;
    element?.showModal();
    return () => {
      element?.close();
      previous?.focus();
    };
  }, []);

  async function submit() {
    if (inFlight.current) return;
    if (!request && (!name.trim() || !inbox || !selected.length)) return;
    const payload: CreateRequest = request ?? {
      operation_id: crypto.randomUUID(),
      name: name.trim(),
      inbox_id: inbox,
      type: "decision",
      decision_ids: selected.map((d) => d.id),
    };
    inFlight.current = true;
    setRequest(payload);
    setBusy(true);
    setError("");
    try {
      const result = await writeCase<{ case: CaseRecord }>(tenant, "cases", payload);
      await Promise.all([
        client.invalidateQueries({ queryKey: caseKeys.scope(tenant, session) }),
        client.invalidateQueries({
          queryKey: ["decision-engine", "decisions", tenant],
        }),
      ]);
      router.push(`/cases/${result.case.id}?${new URLSearchParams({ tenant })}`);
      onClose(true);
    } catch (cause) {
      const definitive =
        cause instanceof CaseApiError &&
        cause.status >= 400 &&
        cause.status < 500 &&
        cause.status !== 408 &&
        cause.status !== 429;
      setError(
        (cause instanceof Error ? cause.message : "Creation response unavailable.") +
          (definitive
            ? " Review your selection and inbox before trying again."
            : " The case may have been created. Retry the same request to recover its result.")
      );
      if (definitive) {
        setRequest(null);
        void inboxes.refetch();
      }
      void client.invalidateQueries({ queryKey: caseKeys.scope(tenant, session) });
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  return (
    <dialog
      ref={dialog}
      aria-labelledby="create-decision-case-title"
      aria-describedby="create-decision-case-description"
      onCancel={(event) => {
        event.preventDefault();
        if (!busy && !request) onClose();
      }}
      className="m-auto w-[min(42rem,calc(100vw-2rem))] max-h-[90vh] overflow-y-auto rounded-2xl border border-slate-200 bg-white p-0 shadow-2xl backdrop:bg-slate-950/40 backdrop:backdrop-blur-xs"
    >
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <div className="border-b border-slate-100 px-6 py-5">
          <h2 id="create-decision-case-title" className="text-xl font-bold tracking-tight text-slate-900">
            Create case
          </h2>
          <p
            id="create-decision-case-description"
            className="mt-1 text-sm text-slate-500"
          >
            Bring selected decisions into one investigation. Up to 100 decisions per case creation.
          </p>
        </div>

        <div className="space-y-5 p-6">
          {error && (
            <p
              role="alert"
              className="rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800"
            >
              {error}
            </p>
          )}

          {inboxes.isError && (
            <p role="alert" className="text-sm text-rose-700">
              Unable to load inboxes.{" "}
              <button
                type="button"
                className="underline font-semibold"
                onClick={() => void inboxes.refetch()}
              >
                Retry inboxes
              </button>
            </p>
          )}

          <fieldset disabled={busy || !!request} className="space-y-4">
            <label className="block text-sm font-semibold uppercase tracking-wider text-slate-600">
              Case name
              <Input
                autoFocus
                required
                maxLength={300}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Suspicious payment investigation"
                className="mt-1.5 font-normal normal-case"
              />
            </label>

            <label className="block text-sm font-semibold uppercase tracking-wider text-slate-600">
              Inbox
              <select
                required
                value={inbox}
                onChange={(e) => setInbox(e.target.value)}
                className="mt-1.5 block w-full rounded-lg border border-slate-300 bg-white p-2.5 text-sm font-normal normal-case text-slate-900 focus:border-blue-500 focus:outline-hidden"
              >
                <option value="">
                  {inboxes.isPending ? "Loading inboxes…" : "Choose an inbox"}
                </option>
                {activeInboxes.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </select>
            </label>

            {inboxes.isSuccess && !activeInboxes.length && (
              <p className="text-sm text-amber-800">
                No active inboxes are available to your account.
              </p>
            )}

            <div>
              <p className="mb-2 text-sm font-semibold uppercase tracking-wider text-slate-600">
                {selected.length} selected decisions
              </p>
              <ul className="max-h-56 divide-y divide-slate-100 overflow-auto rounded-lg border border-slate-200 bg-slate-50/30">
                {selected.map((d) => (
                  <li
                    key={d.id}
                    className="flex items-start justify-between gap-3 p-3 text-sm"
                  >
                    <div className="min-w-0">
                      <p className="font-semibold text-slate-900">
                        {scenarioNames.get(d.scenario_id) ?? d.scenario_id}
                      </p>
                      <p className="break-all text-slate-600 mt-0.5">
                        {d.object_type}: {d.object_id} · {d.outcome.replaceAll("_", " ")}
                      </p>
                      <p className="break-all font-mono text-sm text-slate-400 mt-0.5">
                        {d.id}
                      </p>
                    </div>
                    <button
                      type="button"
                      aria-label={`Remove decision ${d.id}`}
                      className="text-sm font-semibold text-blue-700 hover:underline shrink-0"
                      onClick={() =>
                        setSelected((items) => items.filter((item) => item.id !== d.id))
                      }
                    >
                      Remove
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          </fieldset>

          <p className="text-sm text-slate-500">
            The case starts as investigating and may be assigned to you under the inbox capacity rules. All decisions must be available; if one cannot be attached, nothing is created.
          </p>
        </div>

        <div className="flex justify-end gap-3 border-t border-slate-100 bg-slate-50/70 px-6 py-4">
          <Button
            type="button"
            variant="outline"
            disabled={busy || !!request}
            onClick={() => onClose()}
            className="text-sm"
          >
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={
              busy ||
              (!request &&
                (!name.trim() ||
                  !selected.length ||
                  !activeInboxes.some((item) => item.id === inbox)))
            }
            className="text-sm font-medium"
          >
            {busy ? "Creating…" : request ? "Retry creation" : "Create case"}
          </Button>
        </div>
      </form>
    </dialog>
  );
}
