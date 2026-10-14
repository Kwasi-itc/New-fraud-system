"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import {
  FilePlus,
  FolderInput,
  Shield,
  Layers,
  ArrowRight,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { CaseRecord } from "@/lib/case-manager-api";
import {
  useCaseContext,
  useCaseQueue,
  useCaseWrite,
} from "@/lib/case-manager-query";
import {
  CaseNav,
  Panel,
  Problem,
  fieldClass,
  labelClass,
  readable,
} from "./shared";
import { Evidence } from "./evidence";
import { RecordExplorer } from "./record-explorer";
import { CopyButton, StatusPill } from "./structured-data-viewer";
import { cn } from "@/lib/utils";

export function DecisionCase({
  tenant,
  session,
  decisionId,
}: {
  tenant: string;
  session: string;
  decisionId: string;
}) {
  const router = useRouter();
  const { identity, inboxes } = useCaseContext(tenant, session);
  const membership = useCaseQueue(
    tenant,
    session,
    `decision_id=${encodeURIComponent(decisionId)}&include_snoozed=true`,
    identity.isSuccess
  );
  const [mode, setMode] = useState<"create" | "attach" | null>(null);
  const [search, setSearch] = useState("");
  const [target, setTarget] = useState("");
  const [confirm, setConfirm] = useState(false);
  const candidates = useCaseQueue(
    tenant,
    session,
    `status=pending&status=investigating&include_snoozed=true&name=${encodeURIComponent(
      search
    )}`,
    identity.isSuccess && mode === "attach"
  );
  const write = useCaseWrite(tenant, session);
  const linked =
    !membership.error && !identity.error
      ? membership.data?.pages.flatMap((page) => page.cases) ?? []
      : [];
  const activeInboxes = inboxes.error
    ? []
    : inboxes.data?.inboxes.filter((inbox) => inbox.status === "active") ?? [];
  const choices = candidates.error
    ? []
    : candidates.data?.pages
        .flatMap((page) => page.cases)
        .filter(
          (item) =>
            item.type === "decision" &&
            activeInboxes.some((inbox) => inbox.id === item.inbox_id)
        ) ?? [];
  const selected = choices.find((item) => item.id === target);
  const detail = (id: string) =>
    `/cases/${encodeURIComponent(id)}?${new URLSearchParams({ tenant })}`;
  const canChoose =
    identity.isSuccess && membership.isSuccess && !membership.error && !linked.length;

  return (
    <div className="space-y-6">
      <CaseNav tenant={tenant} />

      {/* Header Banner */}
      <header className="rounded-2xl border border-slate-200 bg-white p-6 shadow-xs">
        <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-slate-900">
          Decision investigation
        </h1>
        <div className="mt-2 flex flex-wrap items-center gap-2 text-sm">
          <span className="font-mono text-slate-500 font-medium">
            Decision {decisionId}
          </span>
          <CopyButton text={decisionId} />
        </div>
      </header>

      <Problem error={identity.error} retry={() => void identity.refetch()} />
      <Problem error={membership.error} retry={() => void membership.refetch()} />
      <Problem error={write.error} />

      {(identity.isPending || (identity.isSuccess && membership.isPending)) && (
        <div className="flex items-center gap-2 py-8 text-sm text-slate-500">
          <span className="inline-block size-3.5 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
          <p role="status">Finding the case…</p>
        </div>
      )}

      {/* Current Attached Case */}
      {!!linked.length && (
        <Panel title="Current case">
          <div className="space-y-4">
            {linked.map((item) => (
              <div
                key={item.id}
                className="space-y-3 rounded-xl border border-slate-200 bg-slate-50/40 p-4"
              >
                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200/60 pb-2">
                  <div className="flex items-center gap-2">
                    <Shield className="size-4 text-blue-600" />
                    <Link
                      className="text-sm font-semibold text-blue-700 underline"
                      href={detail(item.id)}
                    >
                      {item.name}
                    </Link>
                    <span className="text-sm text-slate-500">· {readable(item.status)}</span>
                  </div>
                  <StatusPill value={item.status} />
                </div>
                <Evidence
                  tenant={tenant}
                  session={session}
                  caseId={item.id}
                  kind="decisions"
                  resource={decisionId}
                />
              </div>
            ))}
            <p className="text-sm text-slate-500">
              This decision belongs to one primary case, including after closure. Open that case to investigate or explicitly transfer it.
            </p>
          </div>
        </Panel>
      )}

      {linked.map((item) => (
        <RecordExplorer
          key={item.id}
          tenant={tenant}
          session={session}
          caseId={item.id}
          decisionId={decisionId}
        />
      ))}

      {/* Intake / Attachment Options if Unattached */}
      {canChoose && (
        <div className="space-y-6">
          <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-xs space-y-4">
            <div>
              <h2 className="text-base font-semibold text-slate-900">
                Decision Intake
              </h2>
              <p className="text-sm text-slate-600 mt-1">
                No accessible case found. Create an investigation or attach this decision to an existing open case. A decision already linked elsewhere cannot be attached again.
              </p>
            </div>

            <div className="flex flex-wrap gap-3">
              <Button
                variant="outline"
                disabled={write.isPending}
                onClick={() => {
                  setMode("create");
                  setConfirm(false);
                }}
                className={cn(
                  "gap-1.5 text-sm font-medium",
                  mode === "create" ? "bg-blue-50 border-blue-200 text-blue-700" : ""
                )}
              >
                <FilePlus className="size-3.5" />
                Create a case
              </Button>
              <Button
                variant="outline"
                disabled={write.isPending}
                onClick={() => {
                  setMode("attach");
                  setConfirm(false);
                }}
                className={cn(
                  "gap-1.5 text-sm font-medium",
                  mode === "attach" ? "bg-blue-50 border-blue-200 text-blue-700" : ""
                )}
              >
                <FolderInput className="size-3.5" />
                Add to existing case
              </Button>
            </div>
          </div>

          <Problem error={inboxes.error} retry={() => void inboxes.refetch()} />

          {mode === "create" && (
            <Panel title="Create decision investigation">
              <form
                className="space-y-4"
                onSubmit={async (event) => {
                  event.preventDefault();
                  const data = new FormData(event.currentTarget);
                  try {
                    const result = (await write.mutateAsync({
                      path: "cases",
                      payload: {
                        name: String(data.get("name")).trim(),
                        inbox_id: data.get("inbox"),
                        type: "decision",
                        decision_ids: [decisionId],
                      },
                    })) as { case: CaseRecord };
                    router.push(detail(result.case.id));
                  } catch {
                    await membership.refetch();
                  }
                }}
              >
                <fieldset
                  disabled={write.isPending || !inboxes.isSuccess}
                  className="space-y-4"
                >
                  <label className={labelClass}>
                    <span>Case name</span>
                    <Input
                      name="name"
                      required
                      maxLength={300}
                      defaultValue={`Decision ${decisionId}`}
                      onInput={(event) =>
                        event.currentTarget.setCustomValidity(
                          event.currentTarget.value.trim() ? "" : "Enter a case name."
                        )
                      }
                      className="font-normal normal-case"
                    />
                  </label>
                  <label className={labelClass}>
                    <span>Inbox</span>
                    <select
                      name="inbox"
                      className={cn(fieldClass, "font-normal normal-case")}
                      required
                    >
                      <option value="">Choose an inbox</option>
                      {activeInboxes.map((inbox) => (
                        <option key={inbox.id} value={inbox.id}>
                          {inbox.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <p className="text-sm text-slate-500">
                    Creation starts an investigation and assigns you when you are an eligible inbox member. Existing capacity limits apply.
                  </p>
                  <Button
                    type="submit"
                    disabled={write.isPending || !activeInboxes.length}
                    className="text-sm font-medium"
                  >
                    {write.isPending ? "Creating…" : "Create investigation"}
                  </Button>
                </fieldset>
              </form>
            </Panel>
          )}

          {mode === "attach" && (
            <Panel title="Select investigation">
              <form
                className="space-y-4"
                onSubmit={async (event) => {
                  event.preventDefault();
                  if (!selected) return;
                  if (!confirm) {
                    setConfirm(true);
                    return;
                  }
                  try {
                    await write.mutateAsync({
                      path: `cases/${selected.id}/decisions`,
                      payload: { decision_id: decisionId },
                    });
                    router.push(detail(selected.id));
                  } catch {
                    await membership.refetch();
                  }
                }}
              >
                <fieldset disabled={write.isPending} className="space-y-4">
                  <label className={labelClass}>
                    <span>Find case</span>
                    <Input
                      value={search}
                      onChange={(event) => {
                        setSearch(event.target.value);
                        setTarget("");
                        setConfirm(false);
                      }}
                      placeholder="Filter open cases..."
                      className="font-normal normal-case"
                    />
                  </label>
                  <Problem error={candidates.error} retry={() => void candidates.refetch()} />
                  {candidates.isPending && (
                    <p role="status" className="text-sm text-slate-500">
                      Loading cases…
                    </p>
                  )}
                  <label className={labelClass}>
                    <span>Destination case</span>
                    <select
                      className={cn(fieldClass, "font-normal normal-case")}
                      required
                      value={target}
                      onChange={(event) => {
                        setTarget(event.target.value);
                        setConfirm(false);
                      }}
                    >
                      <option value="">Choose a case</option>
                      {choices.map((item) => (
                        <option key={item.id} value={item.id}>
                          {item.name} · {item.id}
                        </option>
                      ))}
                    </select>
                  </label>
                  {candidates.isSuccess && !choices.length && (
                    <p className="text-sm text-slate-500 italic">
                      No accessible open decision cases on this page.
                    </p>
                  )}
                  {candidates.hasNextPage && (
                    <Button
                      type="button"
                      variant="outline"
                      disabled={candidates.isFetchingNextPage}
                      onClick={() => void candidates.fetchNextPage()}
                      className="text-sm"
                    >
                      More cases
                    </Button>
                  )}
                  {confirm && selected && (
                    <p role="status" className="rounded-lg bg-blue-50 p-3 text-sm text-blue-900 border border-blue-200">
                      Attach decision {decisionId} to {selected.name}? This starts a pending investigation, may assign you if unassigned, and clears its attention flag. Its outcome and snooze time are preserved.
                    </p>
                  )}
                  <Button
                    type="submit"
                    disabled={!selected || write.isPending}
                    className="text-sm font-medium"
                  >
                    {write.isPending
                      ? "Attaching…"
                      : confirm
                      ? "Confirm attachment"
                      : "Review attachment"}
                  </Button>
                </fieldset>
              </form>
            </Panel>
          )}
        </div>
      )}
    </div>
  );
}
