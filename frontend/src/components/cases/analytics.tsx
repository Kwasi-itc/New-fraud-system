"use client";

import { useState } from "react";
import type { ReactNode } from "react";
import {
  AlertTriangle,
  BarChart3,
  Briefcase,
  Calendar,
  CheckCircle2,
  Clock,
  Filter,
  Inbox,
  RefreshCw,
  TrendingUp,
  User,
  Users,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import type { CaseAnalytics } from "@/lib/case-manager-api";
import { useCaseContext, useCaseQuery } from "@/lib/case-manager-query";
import {
  CaseNav,
  Panel,
  Problem,
  fieldClass,
  labelClass,
  timestamp,
} from "./shared";
import { cn } from "@/lib/utils";

const cell = "whitespace-nowrap px-4 py-3.5 text-left text-sm";

function hours(seconds: number | null) {
  return seconds === null ? "Unavailable" : `${(seconds / 3600).toFixed(1)} h`;
}

function Table({
  label,
  headings,
  children,
}: {
  label: string;
  headings: string[];
  children: ReactNode;
}) {
  return (
    <div className="max-h-[28rem] overflow-auto rounded-xl border border-slate-200/90 bg-white shadow-2xs">
      <table aria-label={label} className="w-full text-left text-sm">
        <thead className="sticky top-0 z-10 border-b border-slate-200 bg-slate-50/90 backdrop-blur-xs text-slate-700 font-semibold uppercase tracking-wider text-sm">
          <tr>
            {headings.map((h) => (
              <th key={h} scope="col" className={cell}>
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100">{children}</tbody>
      </table>
    </div>
  );
}

export function CaseAnalyticsPage({
  tenant,
  session,
  initialFrom,
  initialTo,
}: {
  tenant: string;
  session: string;
  initialFrom: string;
  initialTo: string;
}) {
  const { identity, inboxes } = useCaseContext(tenant, session);
  const [fromDate, setFromDate] = useState(initialFrom);
  const [toDate, setToDate] = useState(initialTo);
  const [selectedInbox, setSelectedInbox] = useState("");
  const [filters, setFilters] = useState({
    from: initialFrom,
    to: initialTo,
    inbox: "",
  });
  const [validation, setValidation] = useState<Error | null>(null);

  const params = new URLSearchParams({
    from: `${filters.from}T00:00:00Z`,
    to: `${filters.to}T00:00:00Z`,
  });
  if (filters.inbox) params.set("inbox_id", filters.inbox);

  const query = useCaseQuery<CaseAnalytics>(
    tenant,
    session,
    `case-analytics?${params}`,
    identity.isSuccess
  );

  const result = identity.error || query.error ? undefined : query.data;
  const totals = result?.totals;
  const maxDaily = Math.max(1, ...(result?.daily.map((day) => day.total) ?? []));
  const maxOpenWorkload = Math.max(
    1,
    ...(result?.assignments.map((a) => a.open) ?? [])
  );

  function applyPreset(days: number) {
    const now = new Date();
    const to = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1))
      .toISOString()
      .slice(0, 10);
    const from = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1 - days))
      .toISOString()
      .slice(0, 10);
    setFromDate(from);
    setToDate(to);
    setValidation(null);
    setFilters((prev) => ({ ...prev, from, to }));
  }

  function applyThisMonth() {
    const now = new Date();
    const from = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1))
      .toISOString()
      .slice(0, 10);
    const to = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1))
      .toISOString()
      .slice(0, 10);
    setFromDate(from);
    setToDate(to);
    setValidation(null);
    setFilters((prev) => ({ ...prev, from, to }));
  }

  return (
    <div className="space-y-6">
      <CaseNav tenant={tenant} />

      {/* Analytics Clean Toolbar Header */}
      <div className="rounded-2xl border border-slate-200/90 bg-white p-5 shadow-xs">
        <div className="flex flex-wrap items-center justify-between gap-3 pb-4 border-b border-slate-100">
          <div>
            <div className="flex items-center gap-2.5">
              <h1 className="text-xl sm:text-2xl font-bold tracking-tight text-slate-900">
                Case analytics
              </h1>
              {result && (
                <span className="rounded-full bg-blue-50 px-2.5 py-0.5 text-sm font-semibold text-blue-700 border border-blue-100">
                  Telemetry Active
                </span>
              )}
            </div>
            <p className="text-sm text-slate-500 mt-0.5">
              Current state of cases created in selected UTC range across permitted inboxes.
            </p>
          </div>

          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={query.isFetching || !identity.isSuccess}
              onClick={() => void query.refetch()}
              className="h-9 text-sm gap-2 rounded-xl border-slate-200"
            >
              <RefreshCw className={cn("size-3.5", query.isFetching ? "animate-spin" : "")} />
              Refresh analytics
            </Button>
          </div>
        </div>

        {/* Streamlined Filter Controls & Presets */}
        <form
          className="mt-4 flex flex-wrap items-center gap-3"
          onSubmit={(event) => {
            event.preventDefault();
            const data = new FormData(event.currentTarget);
            const from = String(data.get("from") ?? fromDate);
            const to = String(data.get("to") ?? toDate);
            const span =
              Date.parse(to + "T00:00:00Z") - Date.parse(from + "T00:00:00Z");
            if (!Number.isFinite(span) || span <= 0 || span > 366 * 86400000) {
              setValidation(
                new Error("Select a positive date range of at most 366 days.")
              );
              return;
            }
            setValidation(null);
            setFilters({ from, to, inbox: String(data.get("inbox") ?? selectedInbox) });
          }}
        >
          {/* Quick Presets Group */}
          <div className="flex items-center gap-1.5 rounded-xl border border-slate-200 bg-slate-50/70 p-1">
            <span className="px-2 text-sm font-semibold text-slate-500">Presets:</span>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={applyThisMonth}
              className="h-7 px-2.5 rounded-lg text-sm font-medium text-slate-700 hover:bg-white hover:shadow-2xs"
            >
              This month
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => applyPreset(7)}
              className="h-7 px-2.5 rounded-lg text-sm font-medium text-slate-700 hover:bg-white hover:shadow-2xs"
            >
              7d
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => applyPreset(30)}
              className="h-7 px-2.5 rounded-lg text-sm font-medium text-slate-700 hover:bg-white hover:shadow-2xs"
            >
              30d
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => applyPreset(90)}
              className="h-7 px-2.5 rounded-lg text-sm font-medium text-slate-700 hover:bg-white hover:shadow-2xs"
            >
              90d
            </Button>
          </div>

          <div className="flex items-center gap-2">
            <label className="flex items-center gap-1.5 text-sm font-medium text-slate-700">
              <span className="text-slate-500">From</span>
              <input
                aria-label="Created on or after (UTC)"
                type="date"
                name="from"
                required
                value={fromDate}
                onChange={(e) => setFromDate(e.target.value)}
                className="h-9 rounded-xl border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-900 focus:border-blue-500 focus:outline-hidden"
              />
            </label>

            <label className="flex items-center gap-1.5 text-sm font-medium text-slate-700">
              <span className="text-slate-500">To</span>
              <input
                aria-label="Created before (UTC)"
                type="date"
                name="to"
                required
                value={toDate}
                onChange={(e) => setToDate(e.target.value)}
                className="h-9 rounded-xl border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-900 focus:border-blue-500 focus:outline-hidden"
              />
            </label>
          </div>

          <label className="flex items-center gap-1.5 text-sm font-medium text-slate-700">
            <span className="text-slate-500">Inbox</span>
            <select
              aria-label="Analytics inbox"
              name="inbox"
              value={selectedInbox}
              onChange={(e) => setSelectedInbox(e.target.value)}
              className="h-9 rounded-xl border border-slate-300 bg-white px-2.5 text-sm font-normal text-slate-900 focus:border-blue-500 focus:outline-hidden min-w-[170px]"
            >
              <option value="">All permitted inboxes</option>
              {inboxes.data?.inboxes.map((i) => (
                <option key={i.id} value={i.id}>
                  {i.name}
                </option>
              ))}
            </select>
          </label>

          <Button
            type="submit"
            disabled={!identity.isSuccess}
            className="h-9 text-sm font-semibold rounded-xl bg-blue-600 hover:bg-blue-700 text-white shadow-none px-4 ml-auto"
          >
            Apply analytics filters
          </Button>
        </form>
      </div>

      <Problem error={validation} />
      <Problem
        error={identity.error ?? inboxes.error ?? query.error}
        retry={() => {
          void identity.refetch();
          void inboxes.refetch();
          void query.refetch();
        }}
      />

      {query.isFetching && (
        <div className="flex items-center gap-2 py-8 text-sm text-slate-500">
          <span className="inline-block size-4 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
          <p role="status">Loading analytics…</p>
        </div>
      )}

      {result && totals && (
        <div className="space-y-6">
          <div className="flex flex-wrap items-center justify-between gap-2 text-sm text-slate-500">
            <span>
              Cases created from <strong className="text-slate-800">{result.from.slice(0, 10)}</strong> up to <strong className="text-slate-800">{result.to.slice(0, 10)}</strong> (exclusive).
            </span>
            <span className="font-mono text-slate-400">Snapshot: {timestamp(result.as_of)}</span>
          </div>

          {/* Metric KPI Cards */}
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
            {/* Card 1: Total Cases */}
            <div className="rounded-2xl border border-slate-200/90 bg-white p-5 shadow-xs hover:border-slate-300 transition-colors">
              <div className="flex items-center justify-between">
                <span className="text-sm font-semibold uppercase tracking-wider text-slate-500">
                  Cases in range
                </span>
                <div className="flex size-9 items-center justify-center rounded-xl bg-blue-50 text-blue-600">
                  <Briefcase className="size-4" />
                </div>
              </div>
              <p className="mt-3 text-3xl font-bold tracking-tight text-slate-900">
                {totals.total}
              </p>
              <p className="mt-1 text-sm text-slate-500">Total cases in window</p>
            </div>

            {/* Card 2: Open Backlog */}
            <div className="rounded-2xl border border-slate-200/90 bg-white p-5 shadow-xs hover:border-slate-300 transition-colors">
              <div className="flex items-center justify-between">
                <span className="text-sm font-semibold uppercase tracking-wider text-slate-500">
                  Open backlog
                </span>
                <div className="flex size-9 items-center justify-center rounded-xl bg-amber-50 text-amber-600">
                  <Clock className="size-4" />
                </div>
              </div>
              <p className="mt-3 text-3xl font-bold tracking-tight text-slate-900">
                {totals.pending + totals.investigating}
              </p>
              <div className="mt-1 flex items-center gap-2 text-sm text-slate-500">
                <span className="inline-flex items-center gap-1 text-amber-700 font-medium">
                  <span className="size-2 rounded-full bg-amber-500" />
                  {totals.pending} pending
                </span>
                <span>·</span>
                <span className="inline-flex items-center gap-1 text-blue-700 font-medium">
                  <span className="size-2 rounded-full bg-blue-500" />
                  {totals.investigating} active
                </span>
              </div>
            </div>

            {/* Card 3: Closed Cases */}
            <div className="rounded-2xl border border-slate-200/90 bg-white p-5 shadow-xs hover:border-slate-300 transition-colors">
              <div className="flex items-center justify-between">
                <span className="text-sm font-semibold uppercase tracking-wider text-slate-500">
                  Closed cases
                </span>
                <div className="flex size-9 items-center justify-center rounded-xl bg-emerald-50 text-emerald-600">
                  <CheckCircle2 className="size-4" />
                </div>
              </div>
              <p className="mt-3 text-3xl font-bold tracking-tight text-slate-900">
                {totals.closed}
              </p>
              <p className="mt-1 text-sm font-medium text-emerald-700">
                {totals.total > 0
                  ? `${Math.round((totals.closed / totals.total) * 100)}% resolution rate`
                  : "No cases"}
              </p>
            </div>

            {/* Card 4: Open Cases Overdue */}
            <div className="rounded-2xl border border-slate-200/90 bg-white p-5 shadow-xs hover:border-slate-300 transition-colors">
              <div className="flex items-center justify-between">
                <span className="text-sm font-semibold uppercase tracking-wider text-slate-500">
                  Open cases overdue
                </span>
                <div
                  className={cn(
                    "flex size-9 items-center justify-center rounded-xl",
                    totals.overdue > 0
                      ? "bg-rose-50 text-rose-600"
                      : "bg-emerald-50 text-emerald-600"
                  )}
                >
                  {totals.overdue > 0 ? (
                    <AlertTriangle className="size-4" />
                  ) : (
                    <CheckCircle2 className="size-4" />
                  )}
                </div>
              </div>
              <p
                className={cn(
                  "mt-3 text-3xl font-bold tracking-tight",
                  totals.overdue > 0 ? "text-rose-600" : "text-slate-900"
                )}
              >
                {totals.overdue}
              </p>
              <p
                className={cn(
                  "mt-1 text-sm font-medium",
                  totals.overdue > 0 ? "text-rose-600" : "text-slate-500"
                )}
              >
                {totals.overdue > 0 ? "Requires SLA escalation" : "All cases on schedule"}
              </p>
            </div>

            {/* Card 5: Average Time to Close */}
            <div className="rounded-2xl border border-slate-200/90 bg-white p-5 shadow-xs hover:border-slate-300 transition-colors">
              <div className="flex items-center justify-between">
                <span className="text-sm font-semibold uppercase tracking-wider text-slate-500">
                  Avg. close time
                </span>
                <div className="flex size-9 items-center justify-center rounded-xl bg-blue-50 text-blue-600">
                  <TrendingUp className="size-4" />
                </div>
              </div>
              <p className="mt-3 text-3xl font-bold tracking-tight text-slate-900">
                {hours(totals.average_close_seconds)}
              </p>
              <p className="mt-1 text-sm text-slate-500">
                {totals.measured_closures} measured {totals.measured_closures === 1 ? "closure" : "closures"}
              </p>
            </div>
          </div>

          {totals.total === 0 ? (
            <p className="text-sm text-slate-500 italic py-6">
              No permitted cases were created in this range.
            </p>
          ) : (
            <div className="space-y-6">
              {/* Outcome Distribution Breakdown Banner */}
              <div className="rounded-2xl border border-slate-200/90 bg-white p-6 shadow-xs">
                <div className="flex flex-wrap items-center justify-between gap-4 mb-4">
                  <div>
                    <h3 className="text-base font-bold tracking-tight text-slate-900">
                      Investigation outcome breakdown
                    </h3>
                    <p className="mt-0.5 text-sm text-slate-500">
                      Classification of cases resolved in the selected timeframe
                    </p>
                  </div>
                  <span className="text-sm font-medium text-slate-600">
                    {totals.closed} resolved of {totals.total} total
                  </span>
                </div>

                {/* Segmented Progress Bar */}
                <div className="h-3.5 w-full rounded-full bg-slate-100 overflow-hidden flex shadow-inner">
                  {totals.false_positive > 0 && (
                    <div
                      style={{ width: `${(totals.false_positive / totals.total) * 100}%` }}
                      className="bg-blue-600 transition-all duration-300 hover:opacity-90"
                      title={`False positive: ${totals.false_positive} (${((totals.false_positive / totals.total) * 100).toFixed(1)}%)`}
                    />
                  )}
                  {totals.valuable_alert > 0 && (
                    <div
                      style={{ width: `${(totals.valuable_alert / totals.total) * 100}%` }}
                      className="bg-emerald-500 transition-all duration-300 hover:opacity-90"
                      title={`Valuable alert: ${totals.valuable_alert} (${((totals.valuable_alert / totals.total) * 100).toFixed(1)}%)`}
                    />
                  )}
                  {totals.confirmed_risk > 0 && (
                    <div
                      style={{ width: `${(totals.confirmed_risk / totals.total) * 100}%` }}
                      className="bg-rose-500 transition-all duration-300 hover:opacity-90"
                      title={`Confirmed risk: ${totals.confirmed_risk} (${((totals.confirmed_risk / totals.total) * 100).toFixed(1)}%)`}
                    />
                  )}
                  {totals.unset > 0 && (
                    <div
                      style={{ width: `${(totals.unset / totals.total) * 100}%` }}
                      className="bg-slate-300 transition-all duration-300 hover:opacity-90"
                      title={`Unset: ${totals.unset} (${((totals.unset / totals.total) * 100).toFixed(1)}%)`}
                    />
                  )}
                </div>

                {/* Outcome Badges Grid */}
                <div className="mt-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
                  <div className="flex items-center gap-3 rounded-xl border border-slate-100 bg-slate-50/70 p-3">
                    <span className="size-3 rounded-full bg-blue-600 shrink-0" />
                    <div className="min-w-0">
                      <p className="text-sm font-semibold text-slate-900">
                        False positive
                      </p>
                      <p className="text-sm text-slate-500">
                        {totals.false_positive} ({totals.total > 0 ? Math.round((totals.false_positive / totals.total) * 100) : 0}%)
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-3 rounded-xl border border-slate-100 bg-slate-50/70 p-3">
                    <span className="size-3 rounded-full bg-emerald-500 shrink-0" />
                    <div className="min-w-0">
                      <p className="text-sm font-semibold text-slate-900">
                        Valuable alert
                      </p>
                      <p className="text-sm text-slate-500">
                        {totals.valuable_alert} ({totals.total > 0 ? Math.round((totals.valuable_alert / totals.total) * 100) : 0}%)
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-3 rounded-xl border border-slate-100 bg-slate-50/70 p-3">
                    <span className="size-3 rounded-full bg-rose-500 shrink-0" />
                    <div className="min-w-0">
                      <p className="text-sm font-semibold text-slate-900">
                        Confirmed risk
                      </p>
                      <p className="text-sm text-slate-500">
                        {totals.confirmed_risk} ({totals.total > 0 ? Math.round((totals.confirmed_risk / totals.total) * 100) : 0}%)
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-3 rounded-xl border border-slate-100 bg-slate-50/70 p-3">
                    <span className="size-3 rounded-full bg-slate-300 shrink-0" />
                    <div className="min-w-0">
                      <p className="text-sm font-semibold text-slate-900">
                        Unset
                      </p>
                      <p className="text-sm text-slate-500">
                        {totals.unset} ({totals.total > 0 ? Math.round((totals.unset / totals.total) * 100) : 0}%)
                      </p>
                    </div>
                  </div>
                </div>
              </div>

              {/* Status by Inbox */}
              <Panel title="Status by inbox">
                <Table
                  label="Status by inbox"
                  headings={[
                    "Inbox",
                    "Total",
                    "Pending",
                    "Investigating",
                    "Closed",
                    "Snoozed",
                    "Open with SLA",
                    "Overdue",
                  ]}
                >
                  {result.inboxes.map((i) => (
                    <tr key={i.inbox_id} className="hover:bg-slate-50/80 transition-colors">
                      <td className={cn(cell, "font-semibold text-slate-900")}>
                        <div className="flex items-center gap-2">
                          <Inbox className="size-4 text-blue-600 shrink-0" />
                          <span>{i.inbox_name}</span>
                        </div>
                      </td>
                      <td className={cn(cell, "font-medium text-slate-900")}>{i.total}</td>
                      <td className={cn(cell, "text-amber-700 font-medium")}>{i.pending}</td>
                      <td className={cn(cell, "text-blue-700 font-medium")}>{i.investigating}</td>
                      <td className={cn(cell, "text-emerald-700 font-medium")}>{i.closed}</td>
                      <td className={cn(cell, "text-slate-600")}>{i.snoozed}</td>
                      <td className={cn(cell, "text-slate-700")}>{i.sla_configured}</td>
                      <td className={cn(cell)}>
                        {i.overdue > 0 ? (
                          <span className="inline-flex items-center gap-1 rounded-full bg-rose-50 px-2.5 py-0.5 text-sm font-bold text-rose-700">
                            {i.overdue}
                          </span>
                        ) : (
                          <span className="text-slate-400">0</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </Table>
                <p className="text-sm text-slate-500 mt-2">
                  SLA uses creation time and current inbox policy. Snoozing does not pause deadline calculation. All counts are scoped to the selected range.
                </p>
              </Panel>

              {/* Outcomes by Inbox */}
              <Panel title="Outcomes by inbox">
                <Table
                  label="Outcomes by inbox"
                  headings={[
                    "Inbox",
                    "False positive",
                    "Valuable alert",
                    "Confirmed risk",
                    "Unset",
                  ]}
                >
                  {result.inboxes.map((i) => (
                    <tr key={i.inbox_id} className="hover:bg-slate-50/80 transition-colors">
                      <td className={cn(cell, "font-semibold text-slate-900")}>
                        <div className="flex items-center gap-2">
                          <Inbox className="size-4 text-slate-400 shrink-0" />
                          <span>{i.inbox_name}</span>
                        </div>
                      </td>
                      <td className={cn(cell, "text-blue-700 font-medium")}>{i.false_positive}</td>
                      <td className={cn(cell, "text-emerald-700 font-medium")}>{i.valuable_alert}</td>
                      <td className={cn(cell, "text-rose-700 font-medium")}>{i.confirmed_risk}</td>
                      <td className={cn(cell, "text-slate-500")}>{i.unset}</td>
                    </tr>
                  ))}
                </Table>
              </Panel>

              {/* Closure and Activity */}
              <Panel title="Closure and activity">
                <div className="rounded-xl border border-slate-200/80 bg-slate-50/60 p-4 mb-4 text-sm text-slate-700">
                  Average time to close: <strong className="text-slate-900 text-base">{hours(totals.average_close_seconds)}</strong>. Measured closures: <strong className="text-slate-900">{totals.measured_closures}</strong>; closed cases without usable close history: <strong className="text-slate-900">{totals.closed - totals.measured_closures}</strong>.
                </div>
                <Table
                  label="Closure and activity"
                  headings={[
                    "Inbox",
                    "Measured closures",
                    "Average to close",
                    "Snooze actions",
                    "Escalations",
                  ]}
                >
                  {result.inboxes.map((i) => (
                    <tr key={i.inbox_id} className="hover:bg-slate-50/80 transition-colors">
                      <td className={cn(cell, "font-semibold text-slate-900")}>
                        <div className="flex items-center gap-2">
                          <Inbox className="size-4 text-slate-400 shrink-0" />
                          <span>{i.inbox_name}</span>
                        </div>
                      </td>
                      <td className={cn(cell, "text-slate-700")}>{i.measured_closures}</td>
                      <td className={cn(cell, "font-semibold text-slate-900")}>{hours(i.average_close_seconds)}</td>
                      <td className={cn(cell, "text-slate-700")}>{i.snooze_events}</td>
                      <td className={cn(cell, "text-slate-700")}>{i.escalations}</td>
                    </tr>
                  ))}
                </Table>
                <p className="text-sm text-slate-500 mt-2">
                  Time to close runs from creation to latest audited close for currently closed cases, including bulk closes. Reopening preserves original creation timestamp.
                </p>
              </Panel>

              {/* Assignment Workload */}
              <Panel title="Assignment workload">
                <Table
                  label="Assignment workload"
                  headings={["Current inbox", "Assignee", "Open cases in range"]}
                >
                  {result.assignments.map((a) => (
                    <tr
                      key={`${a.inbox_id}:${a.assignee ?? ""}`}
                      className="hover:bg-slate-50/80 transition-colors"
                    >
                      <td className={cn(cell, "font-semibold text-slate-900")}>
                        {result.inboxes.find((i) => i.inbox_id === a.inbox_id)
                          ?.inbox_name ?? a.inbox_id}
                      </td>
                      <td className={cell}>
                        <div className="flex items-center gap-2">
                          <User className="size-4 text-slate-400 shrink-0" />
                          <span className={cn(a.assignee ? "font-medium text-slate-800" : "italic text-amber-700 font-medium")}>
                            {a.assignee ?? "Unassigned"}
                          </span>
                        </div>
                      </td>
                      <td className={cell}>
                        <div className="flex items-center gap-3">
                          <span className="font-bold text-slate-900 w-8">{a.open}</span>
                          <div
                            aria-hidden="true"
                            className="h-2 w-28 rounded-full bg-slate-100 overflow-hidden"
                          >
                            <div
                              className="h-2 rounded-full bg-blue-600 transition-all duration-300"
                              style={{ width: `${(a.open / maxOpenWorkload) * 100}%` }}
                            />
                          </div>
                        </div>
                      </td>
                    </tr>
                  ))}
                </Table>
              </Panel>

              {/* Cases by Creation Date */}
              <Panel title="Cases by creation date">
                <p className="text-sm text-slate-500 mb-3">
                  Each UTC creation day shows current statuses. This is not a historical backlog reconstruction. Days without cases are omitted.
                </p>

                {/* Daily Trend Interactive Bar Chart */}
                {result.daily.length > 0 && (
                  <div className="mb-4 rounded-xl border border-slate-200/80 bg-slate-50/50 p-4">
                    <div className="flex items-center justify-between gap-2 mb-3">
                      <span className="text-sm font-semibold uppercase tracking-wider text-slate-600">
                        Daily Intake & Resolution Graph
                      </span>
                      <div className="flex items-center gap-3 text-sm">
                        <span className="inline-flex items-center gap-1.5 text-slate-600">
                          <span className="size-2.5 rounded-full bg-amber-500" />
                          Pending
                        </span>
                        <span className="inline-flex items-center gap-1.5 text-slate-600">
                          <span className="size-2.5 rounded-full bg-blue-600" />
                          Investigating
                        </span>
                        <span className="inline-flex items-center gap-1.5 text-slate-600">
                          <span className="size-2.5 rounded-full bg-emerald-500" />
                          Closed
                        </span>
                      </div>
                    </div>

                    <div className="flex items-end gap-2 overflow-x-auto pt-4 pb-2 h-44">
                      {result.daily.map((day) => {
                        const pendingPct = (day.pending / maxDaily) * 100;
                        const investigatingPct = (day.investigating / maxDaily) * 100;
                        const closedPct = (day.closed / maxDaily) * 100;
                        const totalHeightPct = Math.max(8, (day.total / maxDaily) * 100);

                        return (
                          <div
                            key={day.date}
                            className="group flex flex-col items-center gap-1.5 flex-1 min-w-[36px] max-w-[64px] h-full justify-end"
                          >
                            <span className="text-sm font-bold text-slate-700 opacity-80 group-hover:opacity-100 transition-opacity">
                              {day.total}
                            </span>
                            <div
                              style={{ height: `${totalHeightPct}%` }}
                              className="w-full rounded-t-md bg-slate-100 overflow-hidden flex flex-col-reverse shadow-2xs group-hover:ring-2 group-hover:ring-blue-400/50 transition-all"
                              title={`${day.date}: ${day.total} total (Pending: ${day.pending}, Investigating: ${day.investigating}, Closed: ${day.closed})`}
                            >
                              <div
                                style={{ height: `${(day.pending / day.total) * 100}%` }}
                                className="w-full bg-amber-500"
                              />
                              <div
                                style={{ height: `${(day.investigating / day.total) * 100}%` }}
                                className="w-full bg-blue-600"
                              />
                              <div
                                style={{ height: `${(day.closed / day.total) * 100}%` }}
                                className="w-full bg-emerald-500"
                              />
                            </div>
                            <span className="text-sm font-mono text-slate-500 truncate max-w-full">
                              {day.date.slice(5)}
                            </span>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}

                <Table
                  label="Cases by creation date"
                  headings={[
                    "UTC date",
                    "Volume",
                    "Total",
                    "Pending",
                    "Investigating",
                    "Closed",
                  ]}
                >
                  {result.daily.map((day) => (
                    <tr key={day.date} className="hover:bg-slate-50/80 transition-colors">
                      <td className={cn(cell, "font-mono font-medium text-slate-900")}>
                        {day.date}
                      </td>
                      <td className={cell}>
                        <div
                          aria-hidden="true"
                          className="h-2.5 w-36 rounded-full bg-slate-100 overflow-hidden flex"
                        >
                          {day.total > 0 && (
                            <>
                              <div
                                className="h-2.5 bg-amber-500 transition-all duration-300"
                                style={{ width: `${(day.pending / maxDaily) * 100}%` }}
                                title={`Pending: ${day.pending}`}
                              />
                              <div
                                className="h-2.5 bg-blue-600 transition-all duration-300"
                                style={{ width: `${(day.investigating / maxDaily) * 100}%` }}
                                title={`Investigating: ${day.investigating}`}
                              />
                              <div
                                className="h-2.5 bg-emerald-500 transition-all duration-300"
                                style={{ width: `${(day.closed / maxDaily) * 100}%` }}
                                title={`Closed: ${day.closed}`}
                              />
                            </>
                          )}
                        </div>
                      </td>
                      <td className={cn(cell, "font-bold text-slate-900")}>{day.total}</td>
                      <td className={cn(cell, "text-amber-700 font-medium")}>{day.pending}</td>
                      <td className={cn(cell, "text-blue-700 font-medium")}>{day.investigating}</td>
                      <td className={cn(cell, "text-emerald-700 font-medium")}>{day.closed}</td>
                    </tr>
                  ))}
                </Table>
              </Panel>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
