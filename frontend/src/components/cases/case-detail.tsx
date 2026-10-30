"use client";

import Link from "next/link";
import { FileUpload } from "./file-upload";
import { CaseReports } from "./reports";
import { AssigneeSearchDropdown } from "./assignee-search-dropdown";
import { useRouter } from "next/navigation";
import { useMemo, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { decisionEngineApi } from "@/lib/decision-engine-api";
import {
  Activity,
  AlertTriangle,
  Clock,
  ExternalLink,
  FileText,
  FolderOpen,
  GitBranch,
  MessageSquare,
  MoveRight,
  Network,
  Shield,
  ShieldAlert,
  Tag as TagIcon,
  Users,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  useCaseContext,
  useCaseLinks,
  useCaseQuery,
  useCaseQueue,
  useCaseWrite,
} from "@/lib/case-manager-query";
import type {
  CaseRecord,
  CaseEvent,
  CaseFile,
  DecisionLink,
  ScreeningLink,
  QueuePage,
} from "@/lib/case-manager-api";
import { displayValue, useRecordDisplays } from "@/components/detection/decision-display";
import {
  CaseNav,
  ClosingFindings,
  Panel,
  Problem,
  WorkspaceTabs,
  fieldClass,
  labelClass,
  readable,
  timestamp,
} from "./shared";
import { Evidence } from "./evidence";
import { DecisionTransfer } from "./decision-transfer";
import { InvestigatorReview } from "./investigator-review";
import { CaseExclusions } from "./exclusions";
import { RecordExplorer } from "./record-explorer";
import { CopyButton, StatusPill } from "./structured-data-viewer";
import { cn } from "@/lib/utils";

function ActionForm({
  children,
  label,
  busy,
  action,
}: {
  children: ReactNode;
  label: string;
  busy: boolean;
  action: (data: FormData) => Promise<unknown>;
}) {
  return (
    <form
      className="space-y-3"
      onSubmit={async (event) => {
        event.preventDefault();
        const form = event.currentTarget;
        try {
          await action(new FormData(form));
          form.reset();
        } catch {
          /* Shared mutation state displays the failure. */
        }
      }}
    >
      <fieldset disabled={busy} className="space-y-3">
        {children}
        <Button type="submit" size="sm" variant="outline" className="w-full text-sm font-medium">
          {label}
        </Button>
      </fieldset>
    </form>
  );
}

export function CaseDetail({
  tenant,
  session,
  id,
  queueFilters,
}: {
  tenant: string;
  session: string;
  id: string;
  queueFilters: string;
}) {
  const router = useRouter();
  const [activeTab, setActiveTab] = useState<"overview" | "investigation" | "screenings" | "files" | "activity">("overview");
  const { identity, inboxes, tags } = useCaseContext(tenant, session);
  const detail = useCaseQuery<{ case: CaseRecord }>(
    tenant,
    session,
    `cases/${id}/overview`,
    identity.isSuccess
  );
  const write = useCaseWrite(tenant, session);
  const item = detail.data?.case;

  const cursor = item
    ? btoa(
      JSON.stringify({
        id: item.id,
        at: item.created_at,
        boosted: !!item.boost_reason,
      })
    )
      .replaceAll("+", "-")
      .replaceAll("/", "_")
      .replace(/=+$/, "")
    : "";

  const next = useCaseQuery<QueuePage>(
    tenant,
    session,
    `case-queue?${queueFilters}&limit=1&cursor=${encodeURIComponent(cursor)}`,
    !!item && !!queueFilters
  );
  const related = useCaseQueue(
    tenant,
    session,
    `related_to=${id}&include_snoozed=true`,
    !!item
  );

  const submit = (path: string, payload: unknown = {}, method = "POST") =>
    write.mutateAsync({ path: `cases/${id}${path}`, payload, method });

  if (identity.error || detail.error)
    return (
      <div className="space-y-5">
        <CaseNav tenant={tenant} queue={queueFilters} />
        <Problem
          error={identity.error ?? detail.error}
          retry={() => {
            void identity.refetch();
            void detail.refetch();
          }}
        />
      </div>
    );

  if (!item)
    return (
      <div className="flex items-center gap-3 py-12 text-slate-500">
        <span className="inline-block size-4 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
        <p role="status">Loading case…</p>
      </div>
    );

  const inbox = inboxes.data?.inboxes.find((inbox) => inbox.id === item.inbox_id);

  const isOverdue =
    item.status !== "closed" &&
    item.sla_due_at &&
    new Date(item.sla_due_at) <= new Date();

  return (
    <div className="space-y-6">
      <CaseNav tenant={tenant} queue={queueFilters} />

      {/* Case Header Banner */}
      <header className="rounded-2xl border border-slate-200 bg-white p-6 shadow-xs">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-2 min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <span className="inline-flex items-center gap-1 rounded-md bg-slate-100 px-2 py-0.5 font-mono text-sm text-slate-600 font-medium">
                {item.id}
                <CopyButton text={item.id} />
              </span>
              <StatusPill value={item.status} />
              {item.outcome && item.outcome !== "unset" && (
                <StatusPill value={item.outcome} />
              )}
              {isOverdue && (
                <span className="inline-flex items-center gap-1 rounded-full bg-rose-50 px-2.5 py-0.5 text-sm font-semibold text-rose-700 border border-rose-200">
                  <Clock className="size-3" /> Overdue SLA
                </span>
              )}
            </div>

            <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-slate-900 break-words">
              {item.name}
            </h1>

            <p className="text-sm text-slate-500 flex flex-wrap items-center gap-2">
              <span className="font-medium text-slate-700">
                {inbox?.name ?? item.inbox_id}
              </span>
              <span>·</span>
              <span className="capitalize">{readable(item.type)}</span>
              <span>·</span>
              <span>Created {timestamp(item.created_at)}</span>
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            {next.data?.cases[0] && (
              <Link
                className="inline-flex items-center gap-1 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-sm font-semibold text-blue-700 hover:bg-slate-50 shadow-2xs transition-colors"
                href={`/cases/${next.data.cases[0].id}?tenant=${tenant}&queue=${encodeURIComponent(
                  queueFilters
                )}`}
              >
                Next case →
              </Link>
            )}
          </div>
        </div>
      </header>

      <Problem error={write.error} />
      <Problem error={inboxes.error ?? tags.error ?? next.error} />

      {/* Investigation Workspace Tabs */}
      <WorkspaceTabs
        tabs={[
          { id: "overview", label: "Overview & Decisions", icon: Shield },
          { id: "investigation", label: "Investigation & Connections", icon: Network },
          { id: "screenings", label: "Screening Matches", icon: ShieldAlert },
          { id: "files", label: "Files & Reports", icon: FileText },
          { id: "activity", label: "Activity & Audit", icon: Activity },
        ]}
        activeTab={activeTab}
        onChange={setActiveTab}
      />

      <div className="grid gap-6 xl:grid-cols-[1fr_360px]">
        {/* Main Workspace Column */}
        <div className="space-y-6 min-w-0">
          {/* Active Tab Panel Container */}
          <div className={activeTab === "overview" ? "space-y-6" : "hidden"}>
            {/* Linked Decisions & Connections */}
            <CaseDecisionsPanel
              tenant={tenant}
              session={session}
              id={id}
            />

            {/* Findings & Notes Panel */}
            <Panel title="Findings">
              <ActionForm
                label="Add comment"
                busy={write.isPending}
                action={(data) => submit("/comments", { comment: data.get("comment") })}
              >
                <label className={labelClass}>
                  <span>Investigation note</span>
                  <textarea
                    name="comment"
                    required
                    rows={3}
                    className={cn(fieldClass, "font-normal normal-case")}
                    placeholder="Record findings, investigator observations, or customer interview notes..."
                  />
                </label>
              </ActionForm>
            </Panel>

            {/* Related Cases */}
            <Panel title="Related cases">
              <Problem error={related.error} retry={() => void related.refetch()} />
              {related.isPending && (
                <p className="text-sm text-slate-500">Loading related cases…</p>
              )}
              {related.isSuccess && !related.data.pages.some((page) => page.cases.length) && (
                <p className="text-sm text-slate-500 italic">
                  No related cases in your permitted inboxes.
                </p>
              )}
              <ul className="divide-y divide-slate-100">
                {!related.error &&
                  related.data?.pages
                    .flatMap((page) => page.cases)
                    .map((other) => (
                      <li
                        key={other.id}
                        className="py-2.5 flex items-center justify-between gap-3 text-sm"
                      >
                        <Link
                          className="font-medium text-blue-700 hover:underline"
                          href={`/cases/${other.id}?tenant=${tenant}`}
                        >
                          {other.name}
                        </Link>
                        <StatusPill value={other.status} />
                      </li>
                    ))}
              </ul>
              {related.hasNextPage && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void related.fetchNextPage()}
                  disabled={related.isFetchingNextPage}
                  className="text-sm mt-2"
                >
                  More related cases
                </Button>
              )}
            </Panel>
          </div>

          {/* Dedicated Full-Width Investigation & Connections Tab Panel */}
          <div className={activeTab === "investigation" ? "space-y-6" : "hidden"}>
            <CaseInvestigationConnectionsPanel
              tenant={tenant}
              session={session}
              id={id}
            />
          </div>

          {/* Screenings Tab Panel */}
          <div className={activeTab === "screenings" ? "space-y-6" : "hidden"}>
            <CaseExclusions tenant={tenant} session={session} caseId={id} />
            <CaseScreeningsPanel
              tenant={tenant}
              session={session}
              id={id}
            />
          </div>

          {/* Files & SAR Tab Panel */}
          <div className={activeTab === "files" ? "space-y-6" : "hidden"}>
            <CaseFilesPanel
              tenant={tenant}
              session={session}
              id={id}
              allowUpload={item.status !== "closed"}
            />
            {/* SAR Reports */}
            <CaseReports tenant={tenant} session={session} id={id} />
          </div>

          {/* Activity Timeline Tab Panel */}
          <div className={activeTab === "activity" ? "space-y-6" : "hidden"}>
            <CaseTimeline
              tenant={tenant}
              session={session}
              id={id}
              currentUserId={identity.data?.subject}
            />
          </div>
        </div>

        {/* Sidebar / Triage Controls */}
        <aside className="space-y-6">
          {/* Investigation Metadata Panel */}
          <Panel title="Investigation">
            <dl className="grid gap-2.5 grid-cols-2">
              <div className="rounded-lg border border-slate-100 bg-slate-50/60 p-2.5">
                <dt className="text-xs font-medium uppercase tracking-wider text-slate-400">
                  Lifecycle
                </dt>
                <dd className="mt-1 font-semibold text-xs text-slate-900 capitalize">
                  {readable(item.status)}
                </dd>
              </div>

              <div className="rounded-lg border border-slate-100 bg-slate-50/60 p-2.5">
                <dt className="text-xs font-medium uppercase tracking-wider text-slate-400">
                  Outcome
                </dt>
                <dd className="mt-1 font-semibold text-xs text-slate-900 capitalize">
                  {readable(item.outcome)}
                </dd>
              </div>

              <div className="rounded-lg border border-slate-100 bg-slate-50/60 p-2.5 col-span-2">
                <dt className="text-xs font-medium uppercase tracking-wider text-slate-400">
                  Assignee
                </dt>
                <dd className="mt-1 font-medium text-xs text-slate-800 truncate" title={item.assigned_to ?? "Unassigned"}>
                  {item.assigned_to_name ? (
                    <span className="flex items-center gap-1.5">
                      <span className="font-semibold text-slate-900">{item.assigned_to_name}</span>
                      <span className="font-mono text-[11px] text-slate-400">({item.assigned_to?.slice(0, 8)}…)</span>
                    </span>
                  ) : item.assigned_to ? (
                    <span className="font-mono text-[11px] text-slate-700">{item.assigned_to.slice(0, 8)}…</span>
                  ) : (
                    "Unassigned"
                  )}
                </dd>
              </div>

              <div className="rounded-lg border border-slate-100 bg-slate-50/60 p-2.5">
                <dt className="text-xs font-medium uppercase tracking-wider text-slate-400">
                  Snoozed until
                </dt>
                <dd className="mt-1 text-xs font-medium text-slate-700">
                  {item.snoozed_until ? timestamp(item.snoozed_until) : "Not snoozed"}
                </dd>
              </div>

              <div className="rounded-lg border border-slate-100 bg-slate-50/60 p-2.5">
                <dt className="text-xs font-medium uppercase tracking-wider text-slate-400">
                  Attention
                </dt>
                <dd className="mt-1 text-xs font-medium text-slate-700">
                  {readable(item.boost_reason)}
                </dd>
              </div>

              <div className="rounded-lg border border-slate-100 bg-slate-50/60 p-2.5">
                <dt className="text-xs font-medium uppercase tracking-wider text-slate-400">
                  Review level
                </dt>
                <dd className="mt-1 text-xs font-medium text-slate-700">
                  {readable(item.review_level)}
                </dd>
              </div>

              <div className="rounded-lg border border-slate-100 bg-slate-50/60 p-2.5">
                <dt className="text-xs font-medium uppercase tracking-wider text-slate-400">
                  SLA deadline
                </dt>
                <dd className="mt-1 text-xs font-medium text-slate-700">
                  {item.sla_due_at ? timestamp(item.sla_due_at) : "No SLA"}
                </dd>
              </div>
            </dl>

            <div className="pt-2.5 border-t border-slate-100 space-y-2.5">
              <div className="space-y-1.5 text-xs text-slate-600">
                <div className="flex items-center gap-1.5 font-medium text-slate-500">
                  <Users className="size-3.5 text-slate-400" />
                  <span>Contributors:</span>
                </div>
                {item.contributors && item.contributors.length > 0 ? (
                  <div className="flex flex-wrap items-center gap-1.5">
                    {item.contributors.map((person) => {
                      const isSelf = identity.data?.subject && person.user_id === identity.data.subject;
                      return (
                        <span
                          key={person.user_id}
                          className={cn(
                            "inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium border shadow-2xs",
                            isSelf
                              ? "bg-blue-50 text-blue-800 border-blue-200"
                              : "bg-slate-50 text-slate-800 border-slate-200"
                          )}
                          title={person.user_id}
                        >
                          <span className="font-semibold">
                            {person.name || (isSelf ? "You" : `${person.user_id.slice(0, 8)}…`)}
                          </span>
                          {person.name && (
                            <span className="font-mono text-[10px] text-slate-400">
                              ({isSelf ? "You" : `${person.user_id.slice(0, 8)}…`})
                            </span>
                          )}
                          <CopyButton text={person.user_id} />
                        </span>
                      );
                    })}
                  </div>
                ) : (
                  <span className="italic text-slate-400 text-xs">None yet</span>
                )}
              </div>

              {item.tags && item.tags.length > 0 && (
                <div className="flex flex-wrap gap-1.5 pt-1">
                  {item.tags.map((tag) => (
                    <button
                      key={tag.id}
                      disabled={write.isPending}
                      className="inline-flex items-center gap-1 rounded-full border border-slate-200 bg-slate-50 px-2.5 py-0.5 text-xs font-medium text-slate-700 hover:bg-slate-100 hover:text-slate-900 transition-colors"
                      aria-label={`Remove tag ${tag.name}${tag.deleted_at ? " (archived)" : ""}`}
                      onClick={() => void submit(`/tags/${tag.id}`, {}, "DELETE").catch(() => { })}
                    >
                      <TagIcon className="size-3 text-slate-400" />
                      <span>
                        {tag.name}
                        {tag.deleted_at ? " (archived)" : ""}
                      </span>
                      <span className="font-bold text-slate-400 hover:text-slate-700">×</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </Panel>

          <Panel title="Case actions">
            <ActionForm
              label="Rename"
              busy={write.isPending}
              action={(data) => submit("", { name: data.get("name") }, "PATCH")}
            >
              <label className={labelClass}>
                <span>Case name</span>
                <Input name="name" defaultValue={item.name} required className="font-normal normal-case" />
              </label>
            </ActionForm>

            {item.status !== "closed" ? (
              <div className="space-y-4 pt-3 border-t border-slate-100">
                <ActionForm
                  label="Assign"
                  busy={write.isPending}
                  action={(data) => submit("/assign", { assignee_id: data.get("assignee") })}
                >
                  <AssigneeSearchDropdown
                    label="Inbox member ID"
                    name="assignee"
                    defaultValue={item.assigned_to ?? ""}
                    currentUserId={identity.data?.subject}
                    users={Array.from(
                      new Set(
                        [
                          identity.data?.subject,
                          item.assigned_to,
                          ...(item.contributors?.map((c) => c.user_id) ?? []),
                        ].filter(Boolean) as string[]
                      )
                    )}
                  />
                </ActionForm>

                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={write.isPending}
                    onClick={() =>
                      void submit("/assign", { assignee_id: identity.data?.subject }).catch(
                        () => { }
                      )
                    }
                    className="text-sm"
                  >
                    Assign to me
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={write.isPending || !item.assigned_to}
                    onClick={() => void submit("/unassign").catch(() => { })}
                    className="text-sm"
                  >
                    Unassign
                  </Button>
                </div>

                <ActionForm
                  label="Move case"
                  busy={write.isPending}
                  action={(data) => submit("", { inbox_id: data.get("inbox") }, "PATCH")}
                >
                  <label className={labelClass}>
                    <span>Destination inbox</span>
                    <select name="inbox" required className={cn(fieldClass, "font-normal normal-case")}>
                      <option value="">Select destination</option>
                      {inboxes.data?.inboxes
                        .filter((i) => i.status === "active" && i.id !== item.inbox_id)
                        .map((i) => (
                          <option key={i.id} value={i.id}>
                            {i.name}
                          </option>
                        ))}
                    </select>
                  </label>
                </ActionForm>

                <Button
                  variant="outline"
                  size="sm"
                  disabled={write.isPending || !inbox?.escalation_inbox_id}
                  onClick={async () => {
                    try {
                      await submit("/escalate");
                      router.push(`/cases?tenant=${tenant}`);
                    } catch {
                      /* Display mutation error. */
                    }
                  }}
                  className="w-full text-sm"
                >
                  Escalate to configured inbox
                </Button>

                <ActionForm
                  label="Snooze"
                  busy={write.isPending}
                  action={(data) =>
                    submit("/snooze", {
                      until: new Date(String(data.get("until"))).toISOString(),
                    })
                  }
                >
                  <label className={labelClass}>
                    <span>Snooze until</span>
                    <Input name="until" type="datetime-local" required className="font-normal normal-case" />
                  </label>
                </ActionForm>

                {item.snoozed_until && (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={write.isPending}
                    onClick={() => void submit("/unsnooze").catch(() => { })}
                    className="w-full text-sm"
                  >
                    Unsnooze
                  </Button>
                )}
              </div>
            ) : (
              <div className="space-y-3 pt-3 border-t border-slate-100">
                <div className="flex flex-wrap gap-2">
                  <Button
                    disabled={write.isPending}
                    onClick={() => void submit("", { status: "investigating" }, "PATCH").catch(() => { })}
                    className="text-sm"
                  >
                    Reopen investigation
                  </Button>
                  {item.assigned_to && (
                    <Button
                      variant="outline"
                      disabled={write.isPending}
                      onClick={() => void submit("/unassign").catch(() => { })}
                      className="text-sm"
                    >
                      Unassign
                    </Button>
                  )}
                </div>
                <p className="text-sm text-slate-500">
                  Reopening resets the outcome and keeps the assignee. The inbox must be active and the assignee must still be a member with available capacity.
                </p>
              </div>
            )}

            <div className="space-y-4 pt-3 border-t border-slate-100">
              <ActionForm
                label="Add tag"
                busy={write.isPending}
                action={(data) => submit("/tags", { tag_id: data.get("tag") })}
              >
                <label className={labelClass}>
                  <span>Tag</span>
                  <select name="tag" required className={cn(fieldClass, "font-normal normal-case")}>
                    <option value="">Choose tag</option>
                    {tags.data?.tags
                      .filter((tag) => !item.tags?.some((t) => t.id === tag.id))
                      .map((tag) => (
                        <option key={tag.id} value={tag.id}>
                          {tag.name}
                          {tag.deleted_at ? " (archived)" : ""}
                        </option>
                      ))}
                  </select>
                </label>
              </ActionForm>

              <ActionForm
                label="Set review level"
                busy={write.isPending}
                action={(data) =>
                  submit("", { review_level: data.get("review") || null }, "PATCH")
                }
              >
                <label className={labelClass}>
                  <span>Review</span>
                  <select
                    name="review"
                    defaultValue={item.review_level ?? ""}
                    className={cn(fieldClass, "font-normal normal-case")}
                  >
                    <option value="">Unset</option>
                    {["probable_false_positive", "investigate", "escalate"].map((level) => (
                      <option key={level} value={level}>
                        {readable(level)}
                      </option>
                    ))}
                  </select>
                </label>
              </ActionForm>
            </div>
          </Panel>

          {item.status !== "closed" && (
            <Panel title="Resolve case">
              <ActionForm
                label="Close case"
                busy={write.isPending}
                action={(data) =>
                  submit("/close", {
                    outcome: data.get("outcome"),
                    comment: data.get("comment"),
                  })
                }
              >
                <label className={labelClass}>
                  <span>Outcome</span>
                  <select name="outcome" required className={cn(fieldClass, "font-normal normal-case")}>
                    <option value="">Select outcome</option>
                    {["false_positive", "valuable_alert", "confirmed_risk"].map((value) => (
                      <option key={value} value={value}>
                        {readable(value)}
                      </option>
                    ))}
                  </select>
                </label>
                <ClosingFindings label="Closing findings" />
              </ActionForm>
            </Panel>
          )}
        </aside>
      </div>
    </div>
  );
}

function CaseDecisionCard({
  tenant,
  session,
  id,
  link,
  displays,
  scenarios,
}: {
  tenant: string;
  session: string;
  id: string;
  link: DecisionLink;
  displays: ReturnType<typeof useRecordDisplays>;
  scenarios: Map<string, string>;
}) {
  const decisionQuery = useQuery({
    queryKey: ["decision-engine", "decision", tenant, link.decision_id],
    queryFn: () => decisionEngineApi.getDecision(tenant, link.decision_id),
    enabled: Boolean(tenant && link.decision_id),
    staleTime: 60_000,
  });
  const decision = decisionQuery.data?.decision;
  const scenarioName = decision?.scenario_id
    ? scenarios.get(decision.scenario_id)
    : link.scenario_id
    ? scenarios.get(link.scenario_id)
    : undefined;
  const rec = displays.record(link);
  const cols = displays.allColumns[link.object_type] ?? [];
  const hasCaption = Boolean(rec?.caption && rec.caption !== link.object_id);
  const displayCols = cols;

  return (
    <article
      className="space-y-3 rounded-xl border border-slate-200 bg-slate-50/30 p-4 shadow-2xs hover:border-slate-300 transition-all"
      key={link.id}
    >
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200/70 pb-3">
        <div className="flex flex-wrap items-center gap-3">
          <span className="flex size-9 items-center justify-center rounded-xl bg-blue-50 text-blue-700 shadow-2xs">
            <Shield className="size-5" />
          </span>
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <Link
                className="text-base font-bold text-slate-900 hover:text-blue-700 hover:underline"
                href={`/cases/decisions/${link.decision_id}?${new URLSearchParams({
                  tenant,
                })}`}
              >
                {scenarioName ?? "Decision"}
              </Link>
              {decision?.outcome && <StatusPill value={decision.outcome} />}
              {decision?.score != null && (
                <span className="text-xs font-semibold px-2 py-0.5 rounded-full bg-slate-100 text-slate-700 border border-slate-200">
                  Score: {decision.score}
                </span>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-2 mt-0.5 text-xs text-slate-500">
              <span>Evaluated {timestamp(decision?.created_at || link.created_at)}</span>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <span className="text-xs uppercase tracking-wider text-slate-400 font-semibold">Target</span>
          <Link
            className="inline-flex items-center gap-1.5 rounded-lg bg-white px-2.5 py-1 text-xs font-semibold text-slate-700 border border-slate-200 shadow-2xs hover:border-slate-300 hover:bg-slate-50 transition-colors"
            href={`/cases/objects?tenant=${tenant}&caseId=${id}&objectType=${encodeURIComponent(
              link.object_type
            )}&objectId=${encodeURIComponent(link.object_id)}`}
          >
            <span className="capitalize">{link.object_type}</span>
            {rec?.caption && !rec.caption.startsWith("production-replay:") && rec.caption !== link.object_id ? (
              <>: <span className="font-semibold text-slate-900">{rec.caption}</span></>
            ) : null}
            <ExternalLink className="size-3 text-slate-400" />
          </Link>
        </div>
      </div>

      {/* Configured Display Attributes */}
      {displayCols.length > 0 && rec?.values && (
        <div className="flex flex-wrap gap-2 pt-1">
          {displayCols.map((col) => (
            <div
              key={col.id}
              className="rounded-lg bg-white border border-slate-200/90 px-2.5 py-1 text-xs flex items-center gap-1.5 shadow-2xs"
            >
              <span className="text-slate-500 font-medium">{col.label}:</span>
              <span className="text-slate-900 font-semibold">
                {displayValue(rec.values[col.id])}
              </span>
            </div>
          ))}
        </div>
      )}

      <div className="pt-1 space-y-3">
        <Evidence
          tenant={tenant}
          session={session}
          caseId={id}
          kind="decisions"
          resource={link.decision_id}
        />
        <div className="flex flex-wrap items-center gap-3 pt-1 border-t border-slate-100">
          <InvestigatorReview
            tenant={tenant}
            session={session}
            caseId={id}
            kind="decisions"
            resource={link.decision_id}
          />
          <DecisionTransfer
            key={`${id}:${link.id}`}
            tenant={tenant}
            session={session}
            caseId={id}
            decisionId={link.decision_id}
          />
        </div>
      </div>
    </article>
  );
}

function CaseInvestigationConnectionsPanel({
  tenant,
  session,
  id,
}: {
  tenant: string;
  session: string;
  id: string;
}) {
  const queryClient = useQueryClient();
  const decisions = useCaseLinks<DecisionLink>(tenant, session, id, "decisions");
  const items = decisions.data?.pages.flatMap((page) => page.items) ?? [];
  const objects = items.map((link) => ({ object_type: link.object_type, object_id: link.object_id }));
  const displays = useRecordDisplays(tenant, session, objects);
  const [selectedDecisionId, setSelectedDecisionId] = useState<string>("");

  const scenariosQuery = useQuery({
    queryKey: ["decision-engine", "scenarios", tenant],
    queryFn: () => decisionEngineApi.listScenarios(tenant),
    enabled: Boolean(tenant),
    staleTime: 300_000,
  });
  const scenarios = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of scenariosQuery.data?.scenarios ?? []) {
      map.set(s.id, s.name);
    }
    return map;
  }, [scenariosQuery.data]);

  const activeDecisionId = selectedDecisionId || (items[0]?.decision_id ?? "");
  const activeLink = items.find((item) => item.decision_id === activeDecisionId);
  const activeRec = activeLink ? displays.record(activeLink) : undefined;
  const activeCaption = activeRec?.caption && activeRec.caption !== activeLink?.object_id ? activeRec.caption : null;

  const activeDecisionQuery = useQuery({
    queryKey: ["decision-engine", "decision", tenant, activeDecisionId],
    queryFn: () => decisionEngineApi.getDecision(tenant, activeDecisionId),
    enabled: Boolean(tenant && activeDecisionId),
    staleTime: 60_000,
  });
  const activeDec = activeDecisionQuery.data?.decision;
  const activeScenarioName = activeDec?.scenario_id
    ? scenarios.get(activeDec.scenario_id)
    : activeLink?.scenario_id
    ? scenarios.get(activeLink.scenario_id)
    : undefined;

  return (
    <Panel
      title="Investigation & Relationship Exploration"
      action={
        items.length > 1 ? (
          <div className="flex items-center gap-2">
            <span className="text-xs uppercase tracking-wider text-slate-500 font-semibold">Scope:</span>
            <select
              value={activeDecisionId}
              onChange={(e) => setSelectedDecisionId(e.target.value)}
              className="rounded-lg border border-slate-300 bg-white px-2.5 py-1 text-xs font-medium text-slate-800 shadow-2xs focus:border-blue-500 focus:outline-hidden max-w-sm truncate"
            >
              {items.map((link) => {
                const rec = displays.record(link);
                const caption = rec?.caption && rec.caption !== link.object_id ? rec.caption : null;
                const cached = queryClient.getQueryData<{ decision: { scenario_id: string; outcome: string } }>([
                  "decision-engine",
                  "decision",
                  tenant,
                  link.decision_id,
                ]);
                const sName = cached?.decision.scenario_id
                  ? scenarios.get(cached.decision.scenario_id)
                  : link.scenario_id
                  ? scenarios.get(link.scenario_id)
                  : null;
                const outcomePrefix = cached?.decision.outcome ? `[${cached.decision.outcome.toUpperCase()}] ` : "";
                const title = sName || "Decision";
                return (
                  <option key={link.id} value={link.decision_id}>
                    {outcomePrefix}{title}{caption && !caption.startsWith("production-replay:") ? ` · (${caption})` : ` · (${link.object_type})`}
                  </option>
                );
              })}
            </select>
          </div>
        ) : undefined
      }
    >
      <Problem error={decisions.error} retry={() => void decisions.refetch()} />
      {decisions.isPending && <p className="text-sm text-slate-500">Loading decisions for relationship mapping…</p>}
      {decisions.isSuccess && items.length === 0 && (
        <p className="text-sm text-slate-500 italic">No linked decisions available for relationship mapping.</p>
      )}

      {activeDecisionId && (
        <div className="space-y-4">
          <div className="rounded-xl border border-blue-100 bg-blue-50/40 p-3.5 flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2.5 flex-wrap">
              <Network className="size-4 text-blue-600" />
              <span className="font-bold text-sm text-blue-950">
                {activeScenarioName ? `${activeScenarioName} — ` : ""}
                <span className="capitalize">{activeLink?.object_type ?? "Decision"}</span>
                {activeCaption && !activeCaption.startsWith("production-replay:") && activeCaption !== activeLink?.object_id
                  ? `: ${activeCaption}`
                  : ""}
              </span>
              {activeDec?.outcome && <StatusPill value={activeDec.outcome} />}
              {activeDec?.score != null && (
                <span className="text-xs font-semibold px-2 py-0.5 rounded-full bg-blue-100 text-blue-800 border border-blue-200">
                  Score: {activeDec.score}
                </span>
              )}
            </div>
          </div>

          <RecordExplorer
            tenant={tenant}
            session={session}
            caseId={id}
            decisionId={activeDecisionId}
          />
        </div>
      )}
    </Panel>
  );
}

function CaseDecisionsPanel({
  tenant,
  session,
  id,
}: {
  tenant: string;
  session: string;
  id: string;
}) {
  const decisions = useCaseLinks<DecisionLink>(tenant, session, id, "decisions");
  const items = decisions.data?.pages.flatMap((page) => page.items) ?? [];
  const objects = items.map((link) => ({ object_type: link.object_type, object_id: link.object_id }));
  const displays = useRecordDisplays(tenant, session, objects);

  const scenariosQuery = useQuery({
    queryKey: ["decision-engine", "scenarios", tenant],
    queryFn: () => decisionEngineApi.listScenarios(tenant),
    enabled: Boolean(tenant),
    staleTime: 300_000,
  });
  const scenarios = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of scenariosQuery.data?.scenarios ?? []) {
      map.set(s.id, s.name);
    }
    return map;
  }, [scenariosQuery.data]);

  return (
    <Panel title="Linked decisions">
      <Problem error={decisions.error} retry={() => void decisions.refetch()} />
      {decisions.isPending && <p className="text-sm text-slate-500">Loading decisions…</p>}
      {decisions.isSuccess && decisions.data.pages[0].items.length === 0 && (
        <p className="text-sm text-slate-500 italic">No linked decisions.</p>
      )}
      {!decisions.error &&
        items.map((link) => (
          <CaseDecisionCard
            key={link.id}
            tenant={tenant}
            session={session}
            id={id}
            link={link}
            displays={displays}
            scenarios={scenarios}
          />
        ))}
      {decisions.hasNextPage && (
        <Button
          variant="outline"
          size="sm"
          disabled={decisions.isFetchingNextPage}
          onClick={() => void decisions.fetchNextPage()}
          className="text-sm"
        >
          More decisions
        </Button>
      )}
    </Panel>
  );
}

function CaseScreeningsPanel({
  tenant,
  session,
  id,
}: {
  tenant: string;
  session: string;
  id: string;
}) {
  const screenings = useCaseLinks<ScreeningLink>(tenant, session, id, "screenings");

  return (
    <Panel title="Screening matches">
      <Problem error={screenings.error} retry={() => void screenings.refetch()} />
      {screenings.isPending && <p className="text-sm text-slate-500">Loading screenings…</p>}
      {screenings.isSuccess && screenings.data.pages[0].items.length === 0 && (
        <p className="text-sm text-slate-500 italic">No linked screenings.</p>
      )}
      {!screenings.error &&
        screenings.data?.pages
          .flatMap((page) => page.items)
          .map((link) => (
            <article
              className="space-y-3 rounded-xl border border-slate-200 bg-slate-50/30 p-4"
              key={link.id}
            >
              <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200/60 pb-2">
                <p className="break-all text-sm font-semibold text-slate-900">
                  Screening {link.screening_id}
                </p>
                <StatusPill value={link.status} />
              </div>

              <p className="text-sm text-slate-600 font-medium">
                {link.match_id ? `Match ${link.match_id}` : "Screening result"} · {readable(link.status)}
              </p>

              {link.partial && (
                <div
                  role="status"
                  className="flex items-start gap-2 rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900"
                >
                  <AlertTriangle className="size-4 shrink-0 text-amber-600 mt-0.5" />
                  <span>
                    Incomplete screening results. Review the source evidence even if no matches were returned.
                  </span>
                </div>
              )}

              <Evidence
                tenant={tenant}
                session={session}
                caseId={id}
                kind="screenings"
                resource={link.screening_id}
              />
              {link.match_id && (
                <InvestigatorReview
                  tenant={tenant}
                  session={session}
                  caseId={id}
                  kind="matches"
                  resource={link.match_id}
                />
              )}
            </article>
          ))}
      {screenings.hasNextPage && (
        <Button
          variant="outline"
          size="sm"
          disabled={screenings.isFetchingNextPage}
          onClick={() => void screenings.fetchNextPage()}
          className="text-sm"
        >
          More screenings
        </Button>
      )}
    </Panel>
  );
}

function CaseFilesPanel({
  tenant,
  session,
  id,
  allowUpload,
}: {
  tenant: string;
  session: string;
  id: string;
  allowUpload: boolean;
}) {
  const files = useCaseLinks<CaseFile>(tenant, session, id, "files");

  return (
    <Panel title="Evidence files">
      {allowUpload ? (
        <FileUpload tenant={tenant} session={session} caseId={id} />
      ) : (
        <p className="text-sm text-slate-500 italic">
          Reopen the case to upload additional evidence.
        </p>
      )}
      <Problem error={files.error} retry={() => void files.refetch()} />
      {files.isPending && <p className="text-sm text-slate-500">Loading files…</p>}
      {files.isSuccess && files.data.pages[0].items.length === 0 && (
        <p className="text-sm text-slate-500 italic">No attached files.</p>
      )}
      <ul className="divide-y divide-slate-100">
        {!files.error &&
          files.data?.pages
            .flatMap((page) => page.items)
            .map((file) => (
              <li
                className="py-3 text-sm flex flex-wrap items-center justify-between gap-3"
                key={file.id}
              >
                <div className="flex items-center gap-2 min-w-0">
                  <FileText className="size-4 text-slate-400 shrink-0" />
                  <div className="min-w-0">
                    {file.storage_key?.startsWith("case-db/") ? (
                      <a
                        className="font-medium text-blue-700 underline truncate block"
                        href={`/api/cases/tenants/${tenant}/cases/${id}/files/${file.id}/download`}
                      >
                        {file.file_name}
                      </a>
                    ) : (
                      <span className="font-medium text-slate-800 truncate block">
                        {file.file_name} (source storage)
                      </span>
                    )}
                    <span className="text-sm text-slate-400">
                      {file.content_type} · {file.file_size.toLocaleString()} bytes
                    </span>
                  </div>
                </div>

                <span className="text-sm text-slate-500">
                  {timestamp(file.created_at)} · {file.uploaded_by}
                </span>
              </li>
            ))}
      </ul>
      {files.hasNextPage && (
        <Button
          variant="outline"
          size="sm"
          onClick={() => void files.fetchNextPage()}
          className="text-sm mt-2"
        >
          More files
        </Button>
      )}
    </Panel>
  );
}

function CaseTimeline({
  tenant,
  session,
  id,
  currentUserId,
}: {
  tenant: string;
  session: string;
  id: string;
  currentUserId?: string;
}) {
  const events = useCaseLinks<CaseEvent>(tenant, session, id, "events");

  return (
    <Panel title="Activity · newest first">
      <Problem error={events.error} retry={() => void events.refetch()} />
      {events.isPending && <p className="text-sm text-slate-500">Loading activity…</p>}
      <ol className="relative border-l-2 border-slate-200 ml-3 space-y-5 py-2">
        {!events.error &&
          events.data?.pages
            .flatMap((page) => page.items)
            .map((event) => {
              const isSelf = currentUserId && event.user_id === currentUserId;
              const isComment = event.event_type === "comment_added";
              const isTrail =
                isComment &&
                Boolean(
                  event.additional_note?.toLowerCase().includes("investigation connection trail") ||
                  event.additional_note?.toLowerCase().includes("exploration path")
                );

              return (
                <li className="ml-5 relative" key={event.id}>
                  {/* Timeline Node Icon */}
                  <div className={cn(
                    "absolute -left-[27px] top-0.5 flex size-5 items-center justify-center rounded-full ring-4 ring-white",
                    isTrail ? "bg-purple-100 text-purple-700" : isComment ? "bg-amber-100 text-amber-700" : "bg-blue-100 text-blue-700"
                  )}>
                    {isTrail ? (
                      <Network className="size-3" />
                    ) : isComment ? (
                      <MessageSquare className="size-3" />
                    ) : (
                      <Activity className="size-3" />
                    )}
                  </div>

                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className={cn(
                        "text-sm font-semibold capitalize",
                        isTrail ? "text-purple-900" : isComment ? "text-amber-900" : "text-slate-900"
                      )}>
                        {isTrail ? "Connection trail recorded" : readable(event.event_type)}
                      </span>
                      {isTrail && (
                        <span className="inline-flex items-center rounded-md bg-purple-50 px-2 py-0.5 text-xs font-semibold text-purple-700 border border-purple-200">
                          Trail Note
                        </span>
                      )}
                    </div>
                    <div className="flex items-center gap-1.5 text-xs text-slate-500 font-mono">
                      <span>{timestamp(event.created_at)}</span>
                      <span>·</span>
                      {event.user_id ? (
                        <span
                          className={cn(
                            "inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 font-medium border text-xs",
                            isSelf
                              ? "bg-blue-50 text-blue-700 border-blue-200"
                              : "bg-slate-50 text-slate-700 border-slate-200"
                          )}
                          title={event.user_id}
                        >
                          <span>{isSelf ? "You" : `${event.user_id.slice(0, 8)}…`}</span>
                          <CopyButton text={event.user_id} />
                        </span>
                      ) : (
                        <span className="text-slate-400">System</span>
                      )}
                    </div>
                  </div>

                  {event.additional_note && (
                    <TimelineNoteContent
                      note={event.additional_note}
                      isTrail={isTrail}
                      tenant={tenant}
                    />
                  )}

                  {event.new_value && (
                    <p className="mt-1 text-sm text-slate-600">
                      {event.previous_value && `${auditValue(event.previous_value)} → `}
                      <span className="font-medium text-slate-800">{auditValue(event.new_value)}</span>
                    </p>
                  )}
                </li>
              );
            })}
      </ol>
      {events.hasNextPage && (
        <Button
          variant="outline"
          size="sm"
          disabled={events.isFetchingNextPage}
          onClick={() => void events.fetchNextPage()}
          className="text-sm mt-3"
        >
          Earlier activity
        </Button>
      )}
    </Panel>
  );
}

function formatRelationInEnglish(rel: string): string {
  let r = rel.trim();
  r = r.replace(/^Traversed via\s+/i, "");
  r = r.replace(/^Connected via\s+/i, "");
  r = r.replace(/^Linked via\s+/i, "");

  if (!r || r.toLowerCase() === "related" || r.toLowerCase() === "linked record" || r.toLowerCase() === "related connection") {
    return "Connected record";
  }
  if (r.toLowerCase() === "shared pivot") {
    return "Shared pivot link";
  }
  if (r.toLowerCase().endsWith("(reverse)")) {
    const base = r.replace(/\(reverse\)/i, "").trim();
    return `Reverse ${readable(base)}`;
  }
  if (r.toLowerCase() === "navigation") {
    return "Navigation link";
  }
  return readable(r);
}

function cleanEntityText(text: string): string {
  let t = text.trim();
  if (t.includes(":")) {
    const [tbl, val] = t.split(":");
    const trimmedVal = val.trim();
    if (trimmedVal.match(/^[a-f0-9\-]{20,}$/i) || trimmedVal.startsWith("(replay record)")) {
      return readable(tbl.trim());
    }
    return `${readable(tbl.trim())} · ${trimmedVal}`;
  }
  if (t.includes("·")) {
    const [tbl, val] = t.split("·");
    const trimmedVal = val.trim();
    if (trimmedVal.match(/^[a-f0-9\-]{20,}$/i) || trimmedVal.startsWith("(replay record)")) {
      return readable(tbl.trim());
    }
    return `${readable(tbl.trim())} · ${trimmedVal}`;
  }
  return readable(t);
}

function parseStepLine(line: string) {
  let text = line.replace(/^\s*\d+\.\s*/, "").trim();
  text = text.replace(/\[(link|reverse):[a-f0-9\-]+\]\s*/gi, "");
  text = text.replace(/production-replay:[^:\s]+:[a-f0-9]+/gi, "");

  const attributes: { key: string; val: string }[] = [];
  const parenMatch = text.match(/\(([^)]+)\)$/);
  if (parenMatch) {
    const attrStr = parenMatch[1];
    if (attrStr.includes(":")) {
      const parts = attrStr.split(/[,·]\s*/);
      for (const p of parts) {
        const cIdx = p.indexOf(":");
        if (cIdx > 0) {
          attributes.push({
            key: p.slice(0, cIdx).trim(),
            val: p.slice(cIdx + 1).trim(),
          });
        }
      }
      text = text.replace(/\(([^)]+)\)$/, "").trim();
    }
  }

  let relationText = "";
  let entityText = "";

  if (text.includes("→")) {
    const parts = text.split("→");
    relationText = parts[0].trim();
    entityText = parts[1].trim();
  } else if (text.toLowerCase().includes(" to ")) {
    const parts = text.split(/\s+to\s+/i);
    relationText = parts[0].trim();
    entityText = parts[1].trim();
  } else if (text.includes(":")) {
    const parts = text.split(":");
    relationText = "Connected record";
    entityText = `${readable(parts[0].trim())}${parts[1].trim() ? ` · ${parts[1].trim()}` : ""}`;
  } else {
    relationText = "Exploration step";
    entityText = readable(text);
  }

  return {
    relationText: formatRelationInEnglish(relationText),
    entityText: cleanEntityText(entityText),
    attributes,
  };
}

function TimelineNoteContent({
  note,
  isTrail,
  tenant,
}: {
  note: string;
  isTrail: boolean;
  tenant: string;
}) {
  const shouldRenderTrail =
    isTrail ||
    note.toLowerCase().includes("investigation connection trail") ||
    note.toLowerCase().includes("exploration path");

  if (shouldRenderTrail) {
    const lines = note.split("\n");
    let terminalRecord = "";
    let observed = "";
    const displayFields: { label: string; value: string }[] = [];
    const steps: string[] = [];

    let inFields = false;
    let inSteps = false;

    for (const rawLine of lines) {
      const line = rawLine.trim();
      if (!line) continue;

      if (line.includes("**Terminal Record**:")) {
        const parts = line.split("**Terminal Record**:");
        let term = parts[1]?.trim().replace(/[`]/g, "") || "";
        if (term.includes("/")) {
          const sub = term.split("/");
          const table = sub[0]?.trim() || "";
          const idPart = sub[1]?.trim() || "";
          term = idPart.startsWith("production-replay:") ? readable(table) : `${readable(table)} · ${idPart}`;
        }
        terminalRecord = term;
        inFields = false;
        inSteps = false;
      } else if (line.includes("**Observed**:")) {
        const parts = line.split("**Observed**:");
        observed = parts[1]?.trim().replace(/[`]/g, "") || "";
        inFields = false;
        inSteps = false;
      } else if (line.toLowerCase().includes("display fields")) {
        inFields = true;
        inSteps = false;
      } else if (line.toLowerCase().includes("exploration path")) {
        inFields = false;
        inSteps = true;
      } else if (inFields && (line.startsWith("•") || line.startsWith("-") || line.startsWith("*"))) {
        const cleaned = line.replace(/^[•\-\*]\s*/, "");
        const colonIdx = cleaned.indexOf(":");
        if (colonIdx > 0) {
          const label = cleaned.slice(0, colonIdx).replace(/[*`]/g, "").trim();
          const value = cleaned.slice(colonIdx + 1).replace(/[*`]/g, "").trim();
          if (label && value) {
            displayFields.push({ label, value });
          }
        }
      } else if (inSteps && (line.match(/^\d+\./) || line.startsWith("•") || line.startsWith("("))) {
        let cleaned = line.replace(/^\s*\d+\.\s*/, "");
        cleaned = cleaned.replace(/\[(link|reverse):[a-f0-9\-]+\]\s*/gi, "");
        cleaned = cleaned.replace(/production-replay:[^:\s]+:[a-f0-9]+/gi, "(replay record)");
        steps.push(cleaned);
      } else if (
        !line.startsWith("Investigation") &&
        !line.startsWith("###") &&
        !line.startsWith("Live record") &&
        !line.startsWith("_Note:") &&
        !line.startsWith("• **Decision ID**:") &&
        !line.startsWith("Decision:")
      ) {
        if (line.startsWith("• **") && line.includes("**:")) {
          const cleaned = line.replace(/^[•\-\*]\s*/, "");
          const colonIdx = cleaned.indexOf(":");
          if (colonIdx > 0) {
            const label = cleaned.slice(0, colonIdx).replace(/[*`]/g, "").trim();
            const value = cleaned.slice(colonIdx + 1).replace(/[*`]/g, "").trim();
            if (label && value && !["decision id", "terminal record", "observed"].includes(label.toLowerCase())) {
              displayFields.push({ label, value });
            }
          }
        }
      }
    }

    return (
      <div className="mt-2 rounded-xl border border-purple-200/90 bg-gradient-to-br from-purple-50/50 via-white to-purple-50/30 p-4 text-sm text-slate-800 shadow-2xs space-y-3.5">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-purple-100 pb-2.5">
          <div className="flex items-center gap-2">
            <Network className="size-4 text-purple-600" />
            <span className="font-bold text-purple-950 text-xs uppercase tracking-wider">
              Investigation Connection Trail
            </span>
          </div>
          {observed && (
            <span className="text-xs text-slate-500 font-mono">
              Observed {timestamp(observed)}
            </span>
          )}
        </div>

        {terminalRecord && (
          <div className="rounded-lg bg-white border border-purple-100 p-3 shadow-2xs space-y-2.5">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-[11px] font-bold text-purple-900 uppercase tracking-wider bg-purple-50 border border-purple-200 px-2 py-0.5 rounded">
                Terminal Record
              </span>
              <span className="text-sm font-semibold text-slate-900">
                {terminalRecord}
              </span>
            </div>

            {displayFields.length > 0 && (
              <div className="grid gap-2 pt-1 sm:grid-cols-2 md:grid-cols-3">
                {displayFields.map((f, i) => (
                  <div
                    key={i}
                    className="rounded-md border border-slate-200/70 bg-slate-50/60 px-2.5 py-1.5 text-xs"
                  >
                    <div className="text-[10px] font-semibold text-slate-400 uppercase tracking-wider">
                      {f.label}
                    </div>
                    <div className="font-semibold text-slate-900 mt-0.5 break-words">
                      {f.value}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {steps.length > 0 && (
          <div className="space-y-2">
            <div className="text-xs font-semibold uppercase tracking-wider text-purple-950 flex items-center gap-1.5">
              <GitBranch className="size-3.5 text-purple-600" />
              Exploration Path ({steps.length} {steps.length === 1 ? "hop" : "hops"})
            </div>
            <div className="space-y-2">
              {steps.map((rawStep, idx) => {
                const { relationText, entityText, attributes } = parseStepLine(rawStep);
                return (
                  <div
                    key={idx}
                    className="rounded-xl border border-purple-100/90 bg-white p-3 shadow-2xs hover:border-purple-200 transition-colors flex items-start gap-3"
                  >
                    <div className="flex size-6 shrink-0 items-center justify-center rounded-full bg-purple-600 text-white text-xs font-bold shadow-2xs">
                      {idx + 1}
                    </div>
                    <div className="flex-1 space-y-1.5 min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-[11px] font-semibold px-2 py-0.5 rounded-full bg-purple-50 text-purple-700 border border-purple-200/80">
                          {relationText}
                        </span>
                        <span className="text-sm font-bold text-slate-900 break-words">
                          {entityText}
                        </span>
                      </div>

                      {attributes.length > 0 && (
                        <div className="flex flex-wrap gap-1.5 pt-0.5">
                          {attributes.map((attr, aIdx) => (
                            <span
                              key={aIdx}
                              className="inline-flex items-center gap-1 rounded-md bg-slate-50 border border-slate-200/80 px-2 py-0.5 text-xs text-slate-700"
                            >
                              <span className="text-slate-400 font-medium">{attr.key}:</span>
                              <span className="font-semibold text-slate-900">{attr.val}</span>
                            </span>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )}

        <div className="text-xs text-slate-400 italic">
          Live graph traversal record; not a static evidence snapshot.
        </div>
      </div>
    );
  }

  return (
    <div className="mt-2 rounded-xl border border-slate-200/90 bg-white p-3.5 text-sm text-slate-800 whitespace-pre-wrap leading-relaxed shadow-2xs">
      {note}
    </div>
  );
}

function auditValue(value: string) {
  if (value.startsWith("{")) {
    try {
      const state = JSON.parse(value) as Partial<CaseRecord>;
      if (state.status && state.inbox_id)
        return `Status: ${readable(state.status)}; outcome: ${readable(
          state.outcome
        )}; assignee: ${state.assigned_to ?? "Unassigned"}; inbox: ${state.inbox_id}`;
    } catch {
      /* Older events retain their original text. */
    }
  }
  return readable(value);
}
