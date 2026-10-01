"use client";
import { useState } from "react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import type { CaseAnalytics } from "@/lib/case-manager-api";
import { useCaseContext, useCaseQuery } from "@/lib/case-manager-query";
import { CaseNav, Panel, Problem, TenantField, fieldClass, labelClass, timestamp } from "./shared";

const cell = "whitespace-nowrap px-3 py-2 text-left";
function hours(seconds: number | null) { return seconds === null ? "Unavailable" : `${(seconds / 3600).toFixed(1)} h`; }
function Table({ label, headings, children }: { label: string; headings: string[]; children: ReactNode }) {
  return <div className="max-h-96 overflow-auto"><table aria-label={label} className="w-full text-sm"><thead className="bg-slate-50"><tr>{headings.map(h => <th key={h} scope="col" className={cell}>{h}</th>)}</tr></thead><tbody className="divide-y divide-slate-100">{children}</tbody></table></div>;
}

export function CaseAnalyticsPage({ tenant, session, initialFrom, initialTo }: { tenant: string; session: string; initialFrom: string; initialTo: string }) {
  const { identity, inboxes } = useCaseContext(tenant, session);
  const [filters, setFilters] = useState({ from: initialFrom, to: initialTo, inbox: "" });
  const [validation, setValidation] = useState<Error | null>(null);
  const params = new URLSearchParams({ from: `${filters.from}T00:00:00Z`, to: `${filters.to}T00:00:00Z` });
  if (filters.inbox) params.set("inbox_id", filters.inbox);
  const query = useCaseQuery<CaseAnalytics>(tenant, session, `case-analytics?${params}`, identity.isSuccess);
  const result = identity.error || query.error ? undefined : query.data;
  const totals = result?.totals;
  const maxDaily = Math.max(1, ...(result?.daily.map(day => day.total) ?? []));
  return <div className="space-y-6">
    <CaseNav tenant={tenant} /><header className="space-y-2"><h1 className="text-3xl font-semibold">Case analytics</h1><p className="text-sm text-slate-600">Current state of cases created in the selected UTC range, restricted to your permitted inboxes.</p></header>
    <TenantField tenant={tenant} />
    <form className="flex flex-wrap items-end gap-3" onSubmit={event => {
      event.preventDefault(); const data = new FormData(event.currentTarget);
      const from = String(data.get("from")); const to = String(data.get("to"));
      const span = Date.parse(to + "T00:00:00Z") - Date.parse(from + "T00:00:00Z");
      if (!Number.isFinite(span) || span <= 0 || span > 366 * 86400000) { setValidation(new Error("Select a positive date range of at most 366 days.")); return; }
      setValidation(null); setFilters({ from, to, inbox: String(data.get("inbox")) });
    }}>
      <label className={labelClass}>Created on or after (UTC)<input type="date" name="from" required defaultValue={initialFrom} className={fieldClass} /></label>
      <label className={labelClass}>Created before (UTC)<input type="date" name="to" required defaultValue={initialTo} className={fieldClass} /></label>
      <label className={labelClass}>Analytics inbox<select name="inbox" className={fieldClass}><option value="">All permitted inboxes</option>{inboxes.data?.inboxes.map(i => <option key={i.id} value={i.id}>{i.name}</option>)}</select></label>
      <Button type="submit" disabled={!identity.isSuccess}>Apply analytics filters</Button>
      <Button type="button" variant="outline" disabled={query.isFetching || !identity.isSuccess} onClick={() => void query.refetch()}>Refresh analytics</Button>
    </form>
    <Problem error={validation} /><Problem error={identity.error ?? inboxes.error ?? query.error} retry={() => { void identity.refetch(); void inboxes.refetch(); void query.refetch(); }} />
    {query.isFetching && <p role="status">Loading analytics...</p>}
    {result && totals && <>
      <p className="text-sm text-slate-600">Created from {result.from.slice(0, 10)} up to {result.to.slice(0, 10)} (exclusive). Snapshot: {timestamp(result.as_of)}.</p>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{[
        ["Cases in range", totals.total], ["Open backlog", totals.pending + totals.investigating], ["Closed cases", totals.closed], ["Open cases overdue", totals.overdue],
      ].map(([label, value]) => <div key={label} className="rounded-xl border border-slate-200 bg-white p-4"><p className="text-sm text-slate-500">{label}</p><p className="mt-2 text-2xl font-semibold">{value}</p></div>)}</div>
      {totals.total === 0 ? <p className="text-sm text-slate-500">No permitted cases were created in this range.</p> : <>
        <Panel title="Status by inbox"><Table label="Status by inbox" headings={["Inbox", "Total", "Pending", "Investigating", "Closed", "Snoozed", "Open with SLA", "Overdue"]}>{result.inboxes.map(i => <tr key={i.inbox_id}>{[i.inbox_name, i.total, i.pending, i.investigating, i.closed, i.snoozed, i.sla_configured, i.overdue].map((v, n) => <td key={n} className={cell}>{v}</td>)}</tr>)}</Table><p className="text-xs text-slate-500">SLA uses creation time and the current inbox policy. Snoozing does not pause the deadline. All counts are for the selected creation range.</p></Panel>
        <Panel title="Outcomes by inbox"><Table label="Outcomes by inbox" headings={["Inbox", "False positive", "Valuable alert", "Confirmed risk", "Unset"]}>{result.inboxes.map(i => <tr key={i.inbox_id}>{[i.inbox_name, i.false_positive, i.valuable_alert, i.confirmed_risk, i.unset].map((v, n) => <td key={n} className={cell}>{v}</td>)}</tr>)}</Table></Panel>
        <Panel title="Closure and activity"><p className="text-sm">Average time to close: <strong>{hours(totals.average_close_seconds)}</strong>. Measured closures: {totals.measured_closures}; closed cases without usable close history: {totals.closed - totals.measured_closures}.</p><Table label="Closure and activity" headings={["Inbox", "Measured closures", "Average to close", "Snooze actions", "Escalations"]}>{result.inboxes.map(i => <tr key={i.inbox_id}>{[i.inbox_name, i.measured_closures, hours(i.average_close_seconds), i.snooze_events, i.escalations].map((v, n) => <td key={n} className={cell}>{v}</td>)}</tr>)}</Table><p className="text-xs text-slate-500">Time to close runs from creation to the latest audited close for currently closed cases, including bulk closes. Reopening does not reset creation time. Activity counts events within the selected dates for these cases, grouped by their current inbox.</p></Panel>
        <Panel title="Assignment workload"><Table label="Assignment workload" headings={["Current inbox", "Assignee", "Open cases in range"]}>{result.assignments.map(a => <tr key={`${a.inbox_id}:${a.assignee ?? ""}`}><td className={cell}>{result.inboxes.find(i => i.inbox_id === a.inbox_id)?.inbox_name ?? a.inbox_id}</td><td className={cell}>{a.assignee ?? "Unassigned"}</td><td className={cell}>{a.open}</td></tr>)}</Table></Panel>
        <Panel title="Cases by creation date"><p className="text-xs text-slate-500">Each UTC creation day shows current statuses. This is not a historical backlog reconstruction. Days without cases are omitted.</p><Table label="Cases by creation date" headings={["UTC date", "Volume", "Total", "Pending", "Investigating", "Closed"]}>{result.daily.map(day => <tr key={day.date}><td className={cell}>{day.date}</td><td className={cell}><div aria-hidden="true" className="h-2 w-28 rounded bg-slate-100"><div className="h-2 rounded bg-blue-600" style={{ width: `${day.total / maxDaily * 100}%` }} /></div></td>{[day.total, day.pending, day.investigating, day.closed].map((v, n) => <td key={n} className={cell}>{v}</td>)}</tr>)}</Table></Panel>
      </>}
    </>}
  </div>;
}
