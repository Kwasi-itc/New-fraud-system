"use client";
import Link from "next/link";
import { BulkActions } from "./bulk-actions";
import { useState } from "react";
import { ChevronDown, SlidersHorizontal } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useCaseContext, useCaseQueue, useCaseWrite } from "@/lib/case-manager-query";
import type { CaseRecord } from "@/lib/case-manager-api";
import { CaseNav, Panel, Problem, fieldClass, labelClass, readable, timestamp } from "./shared";

export function CaseQueue({ tenant, session }: { tenant: string; session: string }) {
  const router = useRouter();
  const location = useSearchParams();
  const { identity, inboxes, tags } = useCaseContext(tenant, session);
  const allowed = ["name", "inbox_id", "tag_id", "review_level", "assignee_id", "status", "unassigned", "include_snoozed", "overdue", "created_from", "created_to"];
  const active = new URLSearchParams(location.get("queue") ?? "status=pending&status=investigating");
  for (const key of [...active.keys()]) if (!allowed.includes(key)) active.delete(key);
  const filters = active.toString();
  const setFilters = (value: string) => router.push(`/cases?${new URLSearchParams({ tenant, queue: value })}`, { scroll: false });
  const statuses = active.getAll("status");
  const lifecycle = statuses.length === 2 && statuses.includes("pending") && statuses.includes("investigating") ? "active" : statuses.length === 1 ? statuses[0] : statuses.length ? "selected" : "";
  const dateValue = (key: string) => { const value = active.get(key); return value && Number.isFinite(Date.parse(value)) ? new Date(value).toISOString().slice(0, 16) : ""; };
  const [creating, setCreating] = useState(false);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const filterCount = new Set(active.keys()).size;
  const queue = useCaseQueue(tenant, session, filters, identity.isSuccess);
  const write = useCaseWrite(tenant, session);
  const items = queue.data?.pages.flatMap(page => page.cases) ?? [];
  const counts = queue.error || identity.error ? undefined : queue.data?.pages[0]?.counts;
  return <div className="space-y-6">
    <CaseNav tenant={tenant} />
    <header className="flex flex-wrap items-center justify-between gap-4"><div><h1 className="text-3xl font-semibold">Cases</h1><p className="mt-1 text-slate-500">Investigate alerts, record findings, and resolve cases.</p></div><Button onClick={() => setCreating(!creating)} disabled={!identity.isSuccess}>Create case</Button></header>
    {!tenant && <p>Your account needs an organization assignment to load cases.</p>}
    <Problem error={identity.error} retry={() => void identity.refetch()} />
    <Problem error={inboxes.error ?? tags.error} retry={() => { void inboxes.refetch(); void tags.refetch(); }} />
    {creating && <Panel title="Create investigation"><form className="grid gap-4 md:grid-cols-2" onSubmit={async event => {
      event.preventDefault(); const data = new FormData(event.currentTarget);
      try { const result = await write.mutateAsync({ path: "cases", payload: { name: data.get("name"), inbox_id: data.get("inbox"), type: data.get("type"), decision_ids: String(data.get("decisions") ?? "").split(",").map(id => id.trim()).filter(Boolean) } }) as { case: CaseRecord };
        router.push(`/cases/${result.case.id}?tenant=${tenant}`);
      } catch { /* The mutation error is rendered below. */ }
    }}>
      <label className={labelClass}>Case name<Input name="name" required maxLength={300} /></label>
      <label className={labelClass}>Inbox<select name="inbox" required className={fieldClass}><option value="">Select inbox</option>{inboxes.data?.inboxes.filter(inbox => inbox.status === "active").map(inbox => <option key={inbox.id} value={inbox.id}>{inbox.name}</option>)}</select></label>
      <label className={labelClass}>Type<select name="type" className={fieldClass}><option value="decision">Decision</option><option value="continuous_screening">Continuous screening</option></select></label>
      <label className={labelClass}>Decision IDs (optional, comma separated)<Input name="decisions" /></label>
      <Problem error={write.error} /><Button type="submit" disabled={write.isPending}>{write.isPending ? "Creating…" : "Create investigation"}</Button>
    </form></Panel>}
    <Panel title="Inbox queue">
      <div className="flex items-center justify-between gap-3">
        <Button variant="outline" aria-label="Filters" aria-expanded={filtersOpen} aria-controls="case-queue-filters" onClick={() => setFiltersOpen(!filtersOpen)} className="gap-2 rounded-lg">
          <SlidersHorizontal className="size-4" aria-hidden="true" />Filters
          {filterCount > 0 && <span className="rounded-full bg-blue-50 px-2 py-0.5 text-xs font-semibold text-blue-700" aria-label={`${filterCount} active filters`}>{filterCount}</span>}
          <ChevronDown aria-hidden="true" className={`size-4 transition-transform ${filtersOpen ? "rotate-180" : ""}`} />
        </Button>
        <span className="text-sm text-slate-500">{filterCount ? `${filterCount} active filter${filterCount === 1 ? "" : "s"}` : "All cases"}</span>
      </div>
      <div id="case-queue-filters" hidden={!filtersOpen}>
      <form key={filters} className="grid items-end gap-3 rounded-xl border border-slate-200 bg-slate-50 p-4 md:grid-cols-3 xl:grid-cols-4" onSubmit={event => {
      event.preventDefault(); const data = new FormData(event.currentTarget); const query = new URLSearchParams();
      for (const key of ["name", "inbox_id", "tag_id", "review_level", "assignee_id"]) { const value = String(data.get(key) ?? "").trim(); if (value) query.set(key, value); }
      const status = String(data.get("status")); if (status === "active") { query.append("status", "pending"); query.append("status", "investigating"); } else if (status === "selected") { for (const value of statuses) query.append("status", value); } else if (status) query.set("status", status);
      for (const key of ["unassigned", "include_snoozed", "overdue"]) if (data.get(key)) query.set(key, "true");
      for (const key of ["created_from", "created_to"]) { const value = String(data.get(key) ?? ""); if (value) query.set(key, new Date(`${value}Z`).toISOString()); }
      setFilters(query.toString());
    }}>
      <label className={labelClass}>Search<Input name="name" defaultValue={active.get("name") ?? ""} placeholder="Case name" /></label>
      <label className={labelClass}>Inbox<select name="inbox_id" defaultValue={active.get("inbox_id") ?? ""} className={fieldClass}><option value="">All permitted inboxes</option>{inboxes.data?.inboxes.map(inbox => <option key={inbox.id} value={inbox.id}>{inbox.name}{inbox.status === "archived" ? " (archived)" : ""}</option>)}</select></label>
      <label className={labelClass}>Lifecycle<select name="status" defaultValue={lifecycle} className={fieldClass}>{lifecycle === "selected" && <option value="selected">Selected statuses</option>}<option value="active">Open cases</option><option value="">All statuses</option><option value="pending">Pending</option><option value="investigating">Investigating</option><option value="closed">Closed</option></select></label>
      <label className={labelClass}>Tag<select name="tag_id" defaultValue={active.get("tag_id") ?? ""} className={fieldClass}><option value="">Any tag</option>{tags.data?.tags.map(tag => <option key={tag.id} value={tag.id}>{tag.name}</option>)}</select></label>
      <label className={labelClass}>Review level<select name="review_level" defaultValue={active.get("review_level") ?? ""} className={fieldClass}><option value="">Any review level</option>{["probable_false_positive", "investigate", "escalate"].map(v => <option key={v} value={v}>{readable(v)}</option>)}</select></label>
      <label className={labelClass}>Assignee ID<Input name="assignee_id" defaultValue={active.get("assignee_id") ?? ""} /></label>
      <label className={labelClass}>Created from (UTC)<Input name="created_from" defaultValue={dateValue("created_from")} type="datetime-local" /></label>
      <label className={labelClass}>Created before (UTC)<Input name="created_to" defaultValue={dateValue("created_to")} type="datetime-local" /></label>
      <label className="text-sm"><input name="unassigned" type="checkbox" defaultChecked={active.get("unassigned") === "true"} /> Unassigned only</label><label className="text-sm"><input name="include_snoozed" type="checkbox" defaultChecked={active.get("include_snoozed") === "true"} /> Include snoozed</label>
      <label className="text-sm"><input name="overdue" type="checkbox" defaultChecked={active.get("overdue") === "true"} /> Overdue SLA only</label><Button type="submit" variant="outline">Apply filters</Button>
    </form>
      </div>
      <div className="flex flex-wrap gap-2" aria-label="Active queue filters">
        {[...new Set(active.keys())].map(key => <Button key={key} size="sm" variant="outline" aria-label={`Remove ${readable(key)} filter`} onClick={() => { const next = new URLSearchParams(filters); next.delete(key); setFilters(next.toString()); }}>{readable(key)}: {active.getAll(key).map(readable).join(", ")} ×</Button>)}
        <Button size="sm" variant="outline" onClick={() => setFilters("status=pending&status=investigating")}>Reset filters</Button>
        <Button size="sm" variant="outline" disabled={!identity.isSuccess} onClick={() => { const next = new URLSearchParams(filters); next.delete("unassigned"); next.set("assignee_id", identity.data!.subject); setFilters(next.toString()); }}>Assigned to me</Button>
      </div>
      <div className="flex flex-wrap gap-6 border-t pt-4 text-sm text-slate-600" aria-live="polite"><span>Pending: {counts?.pending ?? 0}</span><span>Investigating: {counts?.investigating ?? 0}</span><span>Closed: {counts?.closed ?? 0}</span><span>Priority first · newest first</span></div>
      <Problem error={queue.error} retry={() => void queue.refetch()} />
      {queue.isFetching && !queue.data && <p role="status">Loading cases…</p>}
      {queue.isSuccess && !queue.error && items.length === 0 && <p className="py-8 text-center text-slate-500">No cases match these filters.</p>}
      {!queue.error && identity.isSuccess && <div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead className="border-b text-slate-500"><tr>{["Case", "Inbox", "Lifecycle / attention", "Outcome", "Assignee", "Created"].map(name => <th key={name} className="p-3 font-medium">{name}</th>)}</tr></thead><tbody>{items.map(item => <tr key={item.id} className="border-b hover:bg-slate-50">
        <td className="p-3"><Link className="font-semibold text-blue-700" href={`/cases/${item.id}?tenant=${tenant}&queue=${encodeURIComponent(filters)}`}>{item.name}</Link><div className="text-xs text-slate-500">{item.id.slice(0, 8)} · {item.tags?.map(tag => tag.name + (tag.deleted_at ? " (archived)" : "")).join(", ")}</div></td>
        <td className="p-3">{inboxes.data?.inboxes.find(inbox => inbox.id === item.inbox_id)?.name ?? item.inbox_id}</td><td className="p-3 capitalize">{readable(item.status)}{item.snoozed_until && new Date(item.snoozed_until) > new Date() && <div className="text-xs text-amber-700">Snoozed until {timestamp(item.snoozed_until)}</div>}{item.boost_reason && <div className="text-xs text-blue-700">Attention: {readable(item.boost_reason)}</div>}</td>
        <td className="p-3 capitalize">{readable(item.outcome)}</td><td className="p-3">{item.assigned_to ?? "Unassigned"}</td><td className="p-3 whitespace-nowrap">{timestamp(item.created_at)}{item.sla_due_at && <div className={item.status !== "closed" && new Date(item.sla_due_at) <= new Date() ? "text-xs text-red-700" : "text-xs text-slate-500"}>{item.status !== "closed" && new Date(item.sla_due_at) <= new Date() ? "Overdue: " : ""}SLA due {timestamp(item.sla_due_at)}</div>}</td>
      </tr>)}</tbody></table></div>}
      {queue.hasNextPage && <Button variant="outline" disabled={queue.isFetchingNextPage} onClick={() => void queue.fetchNextPage()}>{queue.isFetchingNextPage ? "Loading…" : "Load more cases"}</Button>}
    </Panel>
    {identity.isSuccess && !queue.error && <BulkActions tenant={tenant} session={session} cases={items} inboxes={inboxes.data?.inboxes ?? []} />}
  </div>;
}
